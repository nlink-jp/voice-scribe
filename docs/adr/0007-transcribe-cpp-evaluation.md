# ADR-0007: transcribe.cpp への乗り換えは保留する（技術的には通った、リリースが無い）

- Status: Proposed
- Date: 2026-08-12

## Context

voice-scribe は 2 つのネイティブランタイムを静的リンクしている — 文字起こしの
whisper.cpp（ADR-0001）と、話者分離の sherpa-onnx / ONNX Runtime（ADR-0002）。
2 本目の代償は ADR-0002 に列挙したとおりで、バイナリは 10.2 MB から 29.5 MB に増え、
`go test ./...` が使えなくなり、上流の壊れた pin を Makefile 側で先回りして
ハッシュ検証する羽目になった。

[transcribe.cpp](https://github.com/handy-computer/transcribe.cpp)（MIT、
Handy の作者、2026-04 開始）は ggml の上に 16 以上の ASR ファミリを載せた
C ライブラリで、**話者分離を同じ C API の一級市民として持っている**。
本当なら 2 本目のランタイムが消える。

確かめるべきは 3 点だった。Go バインディングが無いこと、日本語で話者分離が効くか、
そして現行の日本語モデルを持ち込めるか。

## 実測

M2 Max、2026-08-12。transcribe.cpp は **main の `856d7c1`**（後述）。
ハーネスは `spike/transcribe-cpp/`（`make all` で再現できる）。
フィクスチャは `say -v Kyoko` / `say -v Rocko` の交互 5 ターン、17.7 秒、
16 kHz mono。ADR-0002 の教訓に従い、2 音声が同一波形でないことを先に検証している。

### 1. Go から C API を叩けるか → 通る

公式バインディングは Python / TypeScript / Rust / Swift のみで Go は無い。
約 200 行の cgo ハーネスで文字起こしと話者分離の両方が動いた。

| 観測 | 値 |
|------|-----|
| ビルド（cmake、Metal 埋め込み） | 約 2 分 |
| リンクする静的ライブラリ | **5 本**（現行は whisper 7 + sherpa 10 の計 **17 本**を手書き） |
| ハーネスのバイナリ | 6.8 MB（現行 voice-scribe は 31.0 MB） |
| 動的依存 | OS 標準のみ（Accelerate / Foundation / Metal / MetalKit / libc++） |
| stdout / stderr | JSON のみ / **0 バイト** |

3 点、設計として効いている:

- **`build/install/lib/transcribe-link.json`** がリンクすべきライブラリ・
  フレームワーク・システムライブラリを列挙する。cgo の LDFLAGS を手書きせず
  生成できる。現行の 17 行は上流の構成が変わるたび壊れる種類のコードで、
  これはその種類を消す。
- **`transcribe_log_set` に no-op を渡すだけでネイティブ側のログが止まる。**
  fd 隔離を自作せずに stderr 0 バイトを達成した（ADR-0003 の 2 層防御のうち、
  上流が 1 層目を保証してくれる形）。
- **capability 照会**（`transcribe_model_get_capabilities` /
  `transcribe_model_supports`）でモデルごとの差を問い合わせられる。
  ファイル名や設定から推測しなくてよい。

**踏んだ罠が 1 つ**: Sortformer は言語を 1 件（`en`）名乗るくせに、あらゆる
言語ヒントを `UNSUPPORTED_LANGUAGE` で弾く。**capability は「数」ではなく
「所属」で判定すること** — `n_languages > 0` を条件にすると必ず踏む。

### 2. Sortformer は日本語の話者分離に使えるか → 使える

`diar_streaming_sortformer_4spk-v2.1` Q8_0（139 MB）。
言語ヒント無しで、**2 話者・5 ターンすべてを正しく分離した**。

| ターン | 正解 | 検出 |
|--------|------|------|
| A | 0 – 3935 ms | spk1 0 – 4000 |
| B | 3935 – 6654 | spk2 4000 – 6080 |
| A | 6654 – 10329 | spk1 6640 – 10400 |
| B | 10329 – 15096 | spk2 10320 – 11520 ＋ 12080 – 14480（文中の間で分割） |
| A | 15096 – 17733 | spk1 15120 – 17760 |

境界誤差は最大 71 ms。話者番号は初出順で、取り違えなし。
モデルロードは初回 9.4 秒 / 2 回目以降 **104 ms**（ADR-0001 で観測した
Metal シェーダキャッシュの挙動がそのまま再現した）。推論は 17.7 秒の音声に
369 ms（RTF 0.021）。

代償は **4 話者のハード上限**。現行のクラスタリング方式に上限は無い。
ただし ADR-0006 で BGM 入り音源が 93 話者に割れた実績を思えば、
上限があること自体が常に悪いわけではない。

### 3. kotoba-whisper-v2.0 を GGUF 化できるか → できる（1 行の追加で）

`scripts/convert-whisper.py` は checkpoint を正しく読んだ
（encoder 32 層 / decoder 2 層 / mel 128 / vocab 51866 — distil 構成）。
落ちたのは表示名の allowlist だけで、そこには既にコミュニティ fine-tune
（MediaTek の breeze-asr-25）が載っている。**1 行足せば通る**し、
そのまま上流に出せる（`spike/transcribe-cpp/kotoba-variant.patch`）。

BF16 1.45 GB → Q5_0 **550 MB**（現行の ggml q5_0 は 537 MB）。

同じフィクスチャでの文字起こし比較:

| | 出力 |
|---|---|
| 現行 voice-scribe（whisper.cpp + kotoba-whisper-v2.2 q5_0） | 4 ターン。**5 ターン目を丸ごと落とした** |
| transcribe.cpp（自前変換 kotoba-whisper-v2.0 Q5_0） | **5 ターンすべて**。RTF 0.032 |

両者に共通して「おはようございます」と「ログの解析結果もお願いします」が
欠けた。**つまりこの脱落は移植の欠陥ではなく kotoba-whisper 自身の挙動**で、
移植版はむしろ現行より 1 ターン多く拾っている。

**Q5_0 で 1 件の劣化**: 最終セグメントの終端タイムスタンプが 13.54 秒
（正しくは 17.54 秒、BF16 では正しい）。テキストは BF16 と同一。
上流が Sortformer で K 量子化を撤回したのと同種の、量子化がタイミング判断を
ずらす現象に見える。自前変換したモデルは上流の「参照実装に対して数値検証済み」の
**外側**にあることを忘れないこと。

## 決定を止めている一点

**話者分離はまだリリースされていない。**

最新タグは **v0.1.3（2026-07-12）で、diarization は影も形も無い** —
ヘッダに `diar` の 3 文字が 1 度も出てこず、`convert-sortformer.py` も
`docs/models/diar_*` も存在しない。上の実測はすべて **main のコミット**
（`856d7c1`、2026-08-07）に対するものである。

これは ADR-0002 で自分たちが定めた規律と正面から衝突する。sherpa-onnx の
`xcframework` がローリングタグだった件から、**submodule は `vX.Y.Z` 形式の
不変タグに固定する**と決めている。commit SHA への固定は不変ではあるが、
「上流がリリースとして保証していないコード」を製品に静的リンクすることになる。
v0.1.3 から main まで 1 か月で ABI もモデル群も動いており、この幅は
リリース前の実装が動く幅そのものである。

## 決定

**乗り換えは保留する。** 現行の whisper.cpp + sherpa-onnx 構成を維持する。

再検討のトリガーは 1 つだけ: **diarization を含むタグが切られること。**
そのとき本 ADR の実測はそのまま使え、判断は「4 話者上限を受け入れるか」と
「モデルカタログを全面的に作り直す工数」の 2 点に縮む。

## 保留の間に取り込めるもの（乗り換えとは独立）

1. **リンク指定の機械生成** — 17 行の手書き `#cgo LDFLAGS` は、上流の構成変更で
   壊れるまで壊れたと分からない。transcribe.cpp の link manifest と同じものを
   whisper.cpp / sherpa-onnx のビルドから生成できるなら、同じ危険が消える。
2. **capability 照会**（所属判定）— モデルごとの可否をファイル名や設定から
   推測している箇所を、モデル自身への問い合わせに寄せる。
3. **カタログの「正しい出力か」層** — 現行の SHA256 は「正しいバイトか」までしか
   見ていない（ADR-0004）。上流が全モデルで参照実装との数値照合と WER を
   公開していることは、その上に置ける層があることを示している。

## 積み残し

- 日本語 WER の実測をしていない。フィクスチャは `say` の合成音声 5 ターンで、
  精度比較には足りない。SenseVoice / Qwen3-ASR / FunASR nano mlt が
  kotoba-whisper の代わりになるかは**未検証**（いずれも日本語を明記している）。
- Sortformer のライセンスは **NVIDIA Open Model License**（ライブラリ本体の
  MIT とは別）。カタログに載せるなら表示義務の確認が要る。
- ストリーミング（`transcribe_stream_*`）は触っていない。
