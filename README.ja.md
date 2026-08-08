# voice-scribe

[English README](README.md)

macOS 向けのローカル音声文字起こし。
[whisper.cpp](https://github.com/ggml-org/whisper.cpp) で音声を文字起こしする CLI と、
音声を扱えないモデルで動くエージェントに同じ能力を渡す MCP サーバー。

API キー不要。音声はマシンの外に出ません。

## 目的

音声を聞けないモデルで動くエージェントも、録音を読む必要はあります。クラウド API に
送れば分単位で課金され、音声は他人のディスクに置かれます。voice-scribe は同じ仕事を
ローカルで行い、出力は
[gem-transcribe](https://github.com/nlink-jp/gem-transcribe) と互換のエンベロープなので、
後段のツールはクラウド版・ローカル版のどちらも同一のパーサで読めます。

## 動作要件

- Apple Silicon Mac（**darwin/arm64 専用** — CGO + Metal はクロスコンパイル不可）
- ソースからビルドする場合: Go、cmake、Xcode コマンドラインツール

## インストール

```bash
brew install nlink-jp/tap/voice-scribe
```

ソースからビルドする場合:

```bash
git clone --recurse-submodules https://github.com/nlink-jp/voice-scribe.git
cd voice-scribe
make build-engine
```

`make build-engine` は先に whisper.cpp を静的ライブラリとしてビルドするため、初回は
数分かかります。生成物は `dist/` 配下の単一自己完結バイナリで、システムフレームワーク
以外のランタイム依存はありません。

## 使い方

モデルを導入してから文字起こしします:

```bash
voice-scribe models pull kotoba-whisper-v2.0
```

```bash
voice-scribe transcribe meeting.m4a --lang ja
```

既定では JSON を stdout に出力します。タイムスタンプ付きで、エンベロープは
gem-transcribe と互換です。進捗は stderr に出るので、文字起こし結果をそのまま
パイプに流せます。

その他の形式・オプション:

```bash
voice-scribe transcribe interview.mp4 -f srt -o interview.srt
```

| フラグ | 内容 |
|---|---|
| `-m, --model` | モデル指定。省略時は `--lang` に合うモデル、次に設定の既定モデルが選ばれる |
| `--lang` | 入力言語（ISO 639-1）。省略時は自動判定 |
| `--translate` | 英訳も生成。whisper の translate は別デコードなので音声を 2 回通し、タイムスタンプでマージする |
| `--prompt` | デコーダへの文脈。**羅列ではなく文で書く** — 下記参照 |
| `-f, --format` | `json`（既定）/ `text` / `md` / `srt` / `vtt` |
| `--offset` / `--duration` | 音声の一部だけを処理 |
| `--vad` | 無音区間をゲートしてハルシネーションを抑制。`models pull silero-vad` が必要 |
| `--diarize` | 誰が話しているかをラベル付け。話者分離モデル 2 本が必要 |
| `--speakers` | 話者数が分かっているときに固定する |
| `--speaker-threshold` | 話者数不明時のクラスタリング距離。小さいほど分割されやすい |
| `--speaker-hint` | A/B/C の代わりに使う名前。登場順に割り当てる |
| `-q, --quiet` | stderr への進捗表示を止める |

話者ラベルを付けるには、話者分離モデル 2 本を導入して `--diarize` を付けます:

```bash
voice-scribe models pull pyannote-segmentation-3 && voice-scribe models pull campplus-speaker-embedding
```

```bash
voice-scribe transcribe meeting.m4a --lang ja --diarize --speaker-hint 田中,佐藤
```

話者分離は 2 つのモデルの協働です（一方が話者交代を見つけ、もう一方がどの交代が同一人物
かを判断する）。**失敗は両方向に起きます。**

**全員が 1 話者に統合された場合**は、`--speakers` で人数を固定するか
`--speaker-threshold` を既定の 0.5 より**下げて**ください（分割されやすくなる）。

**逆に話者が異常に多い場合**（数十人、その多くが 1 回しか喋らない）は、人数を固定するか
閾値を 0.5 より**上げて**ください（統合されやすくなる）。後者は **BGM が途切れず鳴っている
素材**で典型的に起こります — 埋め込みモデルが「音楽＋声」から特徴を取るため、同一人物の
ベクトルが散ってしまうためです。実測では、BGM が全編に入った 39 分のドラマ音源で既定値は
93 話者を返し、0.9 で妥当な結果になりました。

過剰分割が疑われるときは**警告を出します**（転記自体はどちらでも正常な形で出てくるため、
他に気づく手段がありません）。

**較正は全編ではなく一部で行ってください。** `--offset` / `--duration` は話者分離にも
効くので、数分ぶんで閾値を探せます:

```bash
voice-scribe transcribe long.wav --lang ja --diarize --offset 300 --duration 300 --speaker-threshold 0.9
```

カタログの全モデルは SHA256 で pin されており、ダウンロード時に検証されます。
差し替えられたファイルは解析されず拒否されます。導入済みのモデルは
`voice-scribe models verify` で検証でき、結果は記録されます。`models list` は
各エントリが検証済みかを表示します — 何を検証していないか言えない一覧は、
それ自体が保証のように読めてしまうためです。

### `--prompt` の書き方

`--prompt` は whisper の initial prompt です。**語彙を宣言するものではなくデコーダを
条件付けるもの**で、内容より**書き方のほうが結果を左右します**。想定される話し方の
レジスタで、録音の内容を1〜2文で書いてください。

```bash
voice-scribe transcribe meeting.m4a --lang ja \
  --prompt "社内の定例ミーティングの録音です。新機能のリリース時期とテスト計画について話しています。"
```

**名前をカンマで並べたものは逆効果です。** 日本語音源で実測したところ、名詞の羅列は
prompt なしでは正しく取れていた行を壊し（prompt 中の語が無関係な行に混入した）、
同じ音源に文の形で与えた場合は prompt なしで落ちていた行が復元されました
（失われていた人名を含む）。

**特定の聞き間違いを直す用途には使えません。** これが一番期待される用途ですが、上記の
音源で苗字が一貫して化けるケースに対し、正しい表記を含む prompt を 4 通り
（漢字・カタカナ・羅列・文中で使用）試してもいずれも直らず、一部は他の正しい行を
犠牲にしました。prompt が効くのはレジスタと文脈であって、音響モデルの聞き取りではありません。
**固有名詞の揺れは転記後に、文脈を持つ側で直すもの**と考えてください。

**下手な prompt は悪化させます。** 長い音源に適用する前に `--offset`/`--duration` で
一部を切り出して比較してください。

`voice-scribe models list --catalog` で導入可能なモデル（日本語特化・多言語、
サイズとライセンス付き）を一覧できます。`voice-scribe doctor` はバイナリに
リンクされているランタイムと ggml バックエンドを、ランタイム自身から読み取って
報告します。

## MCP サーバーとして使う

`voice-scribe mcp` は stdio 上で Model Context Protocol を話し、音声を扱えない
モデルで動くエージェントに録音を読む手段を与えます。クライアントには
`voice-scribe mcp` というコマンドとして登録してください（引数は不要です）。

ツールは 4 本: `get_usage` / `transcribe` / `check_job` / `list_models`。文字起こしは
非同期で、`transcribe` が返すジョブ ID を `check_job` でポーリングします。短い転記は
インラインで、長い転記はファイルパス＋抜粋で返ります（ファイルはどちらの場合も書かれます）。

録音はエージェントが用意して呼び出しごとに指定するワークスペースに置き、すべてのパスは
カーネルによってその中に封じ込められます。`get_usage` が完全なマニュアルを返すので、
エージェントは最初の文字起こしの前に一度呼ぶべきです。

**モデルのダウンロードは意図的に MCP から使えません** — 数百 MB の取得は端末にいる人間の
判断です。`voice-scribe models pull` を使ってください。

設計の全体は
[docs/ja/voice-scribe-rfp.ja.md](docs/ja/voice-scribe-rfp.ja.md)（正本）にあります。

## 設定

[`config.example.toml`](config.example.toml) を
`~/.config/voice-scribe/config.toml` にコピーして編集してください。環境変数がファイルを
上書きし、`--config` が両方を上書きします。

## ライセンス

MIT — [LICENSE](LICENSE) を参照。

このバイナリは第三者のパーミッシブライセンスのコードを静的リンクしています。各ライセンスが
要求する著作権表示を以下に保持します:

- [whisper.cpp](https://github.com/ggml-org/whisper.cpp) — MIT © 2023-2026 The ggml authors
- [ggml](https://github.com/ggml-org/ggml) — MIT © 2023-2026 The ggml authors
- [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) — Apache-2.0, The k2-fsa authors
- [ONNX Runtime](https://github.com/microsoft/onnxruntime) — MIT © Microsoft Corporation

モデルは同梱せずダウンロードする形で、それぞれ独自のライセンスを持ちます: pyannote
segmentation は MIT © 2022 CNRS、3D-Speaker の埋め込みモデルは Apache-2.0 です。
導入済みモデルのライセンスは `voice-scribe models list` で確認できます。
