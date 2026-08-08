# voice-scribe

[English README](README.md)

macOS 向けのローカル音声文字起こし。
[whisper.cpp](https://github.com/ggml-org/whisper.cpp) で音声を文字起こしする CLI と、
音声を扱えないモデルで動くエージェントに同じ能力を渡す MCP サーバー。

API キー不要。音声はマシンの外に出ません。

> **プレリリース。** 文字起こしは実際に動きます。話者分離と MCP サーバーは未実装で、
> `voice-scribe mcp` を実行するとその旨を報告します。[CHANGELOG.md](CHANGELOG.md)
> を参照してください。

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
voice-scribe models pull kotoba-whisper-v2.2
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
| `--prompt` | 固有名詞・専門用語で語彙をバイアス |
| `-f, --format` | `json`（既定）/ `text` / `md` / `srt` / `vtt` |
| `--offset` / `--duration` | 音声の一部だけを処理 |
| `--vad` | 無音区間をゲートしてハルシネーションを抑制。`models pull silero-vad` が必要 |
| `-q, --quiet` | stderr への進捗表示を止める |

`voice-scribe models list --catalog` で導入可能なモデル（日本語特化・多言語、
サイズとライセンス付き）を一覧できます。`voice-scribe doctor` はバイナリに
リンクされているランタイムと ggml バックエンドを、ランタイム自身から読み取って
報告します。

話者分離（`--diarize`）と MCP サーバーは未実装です。設計の全体は
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
