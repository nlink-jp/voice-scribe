# transcribe.cpp spike

ADR-0007 の実測を再現するための使い捨てハーネス。**製品には入らない。**
ルートモジュールから外すために独自の `go.mod` を持つ（`go list ./...` に現れない）。

答えたい問いは 4 つ:

1. **Go から C API を直接叩けるか** — transcribe.cpp に Go バインディングは無い
2. **Sortformer が日本語の話者分離に使えるか** — 使えるなら sherpa-onnx を落とせる
3. **kotoba-whisper-v2.0 を GGUF に変換できるか**
4. **他の日本語モデルは kotoba-whisper より良いか** — CER と、**タイムスタンプの粒度**

## 実行

```
make all
```

段階的に:

| target | 内容 | 所要 |
|--------|------|------|
| `deps` | transcribe.cpp を clone → cmake ビルド → `build/install` へ install | 約 2 分 |
| `build` | Go ハーネスを静的リンク | 数秒 |
| `fixture` | Kyoko / Rocko 交互 5 ターンの日本語 WAV（16 kHz mono） | 数秒 |
| `diarize` | Sortformer Q8_0 を DL（139 MB）して話者分離 | DL 次第 |
| `kotoba` | kotoba-whisper-v2.0 を GGUF 化 → Q5_0 量子化 → 文字起こし | 約 5 分 + HF DL |
| `eval` | 6 モデル × 2 コーパスの日本語 CER | モデル約 5 GB の DL |
| `caps` | capability matrix（タイムスタンプ / ライセンス / 音声長上限） | 数秒 |

必要なもの: cmake、uv、ffmpeg、Xcode の Metal ツールチェイン。

評価コーパスは kotoba-whisper の model card が使っているものと同じ 2 本
（Common Voice 8.0 ja / JSUT basic5000、いずれも ungated）。**公表値と
突き合わせられることがハーネスの検算になる** — kotoba-whisper-v2.0 で
CV8 8.88% / JSUT 7.24%、公表値は 9.2 / 8.4。

## 見どころ

- **`main.go` の LDFLAGS は手書きではない** — `build/install/lib/transcribe-link.json`
  が必要なライブラリとフレームワークを列挙するので、そこから転記している。
  voice-scribe 本体が 22 行の `#cgo LDFLAGS` を手で維持しているのと対照的。
- **capability は「数」ではなく「所属」で見る** — Sortformer は言語を 1 件
  (`en`) 名乗るくせに、あらゆる言語ヒントを `UNSUPPORTED_LANGUAGE` で弾く。
  `n_languages > 0` を条件にすると必ず踏む。
- **stderr は 0 バイト** — `transcribe_log_set` に no-op を渡すだけでネイティブ側の
  ログが止まる。fd 隔離を自作しなくてよい（ADR-0003 の懸念に対する上流側の答え）。
- **`make caps` を CER より先に読む** — 日本語で最も正確なモデルは時刻を出せない。
  精度表だけ見て選ぶと、出力エンベロープを満たせないモデルを選んでしまう。

## 注意

`REV` は **タグではなくコミット**を指している。話者分離は未リリースで、
最新タグ v0.1.3 には影も形も無いため。この一点が ADR-0007 の結論を決めている。
