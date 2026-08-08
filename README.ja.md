# voice-scribe

[English README](README.md)

macOS 向けのローカル音声文字起こし。
[whisper.cpp](https://github.com/ggml-org/whisper.cpp) で音声を文字起こしする CLI と、
音声を扱えないモデルで動くエージェントに同じ能力を渡す MCP サーバー。

API キー不要。音声はマシンの外に出ません。

> **プレリリース。** 現在このリポジトリにあるのは scaffold と疎通確認済みのビルド
> スパイクだけです。whisper.cpp のリンクと Metal の有効化は確認済みですが、文字起こしは
> まだ実装されていません。[CHANGELOG.md](CHANGELOG.md) を参照してください。

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

```bash
voice-scribe doctor
```

バイナリにリンクされているランタイムと、実際にコンパイルされた ggml バックエンドを、
ランタイム自身から読み取って報告します:

```
runtime:      whisper.cpp
capabilities: WHISPER : COREML = 0 | OPENVINO = 0 | MTL : EMBED_LIBRARY = 1 | ...
```

残りのコマンド（`transcribe` / `models` / `mcp`）は scaffold のみで未実装です。
実行するとどのフェーズで実装されるかを報告します。設計の全体は
[docs/ja/voice-scribe-rfp.ja.md](docs/ja/voice-scribe-rfp.ja.md)（正本）にあります。

## 設定

[`config.example.toml`](config.example.toml) を
`~/.config/voice-scribe/config.toml` にコピーして編集してください。環境変数がファイルを
上書きし、`--config` が両方を上書きします。

## ライセンス

MIT — [LICENSE](LICENSE) を参照。

このバイナリは第三者の MIT ライセンスコードを静的リンクしています。ライセンスが要求する
著作権表示を以下に保持します:

- [whisper.cpp](https://github.com/ggml-org/whisper.cpp) — MIT © 2023-2026 The ggml authors
- [ggml](https://github.com/ggml-org/ggml) — MIT © 2023-2026 The ggml authors
