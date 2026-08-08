# voice-scribe

[日本語版 README](README.ja.md)

Local speech-to-text for macOS: a CLI that transcribes audio with
[whisper.cpp](https://github.com/ggml-org/whisper.cpp), and an MCP server that
hands the same capability to an agent whose model cannot process audio.

No API key. No audio leaves the machine.

> **Pre-release.** This repository currently contains the scaffold and a
> verified build spike — the whisper.cpp runtime links and Metal is active, but
> transcription is not implemented yet. See [CHANGELOG.md](CHANGELOG.md).

## Why

An agent driven by a model that cannot hear still needs to read recordings.
Sending them to a cloud API costs money per minute and puts the audio on someone
else's disk. voice-scribe does the same job locally, and emits an envelope
compatible with [gem-transcribe](https://github.com/nlink-jp/gem-transcribe) so
downstream tools parse cloud and local transcripts with one parser.

## Requirements

- Apple Silicon Mac (**darwin/arm64 only** — CGO + Metal cannot cross-compile)
- To build from source: Go, cmake, and the Xcode command line tools

## Installation

```bash
brew install nlink-jp/tap/voice-scribe
```

Or build from source:

```bash
git clone --recurse-submodules https://github.com/nlink-jp/voice-scribe.git
cd voice-scribe
make build-engine
```

`make build-engine` compiles whisper.cpp into static libraries first, so the
first build takes a few minutes. The result is a single self-contained binary in
`dist/` — no runtime dependency beyond the system frameworks.

## Usage

```bash
voice-scribe doctor
```

Reports the runtime linked into the binary and the ggml backends it was compiled
with, read from the runtime itself:

```
runtime:      whisper.cpp
capabilities: WHISPER : COREML = 0 | OPENVINO = 0 | MTL : EMBED_LIBRARY = 1 | ...
```

The remaining commands — `transcribe`, `models`, `mcp` — are scaffolded but not
implemented yet; each reports which phase fills it in. The full design is in
[docs/en/voice-scribe-rfp.md](docs/en/voice-scribe-rfp.md) (the Japanese edition
is the source of truth).

## Configuration

Copy [`config.example.toml`](config.example.toml) to
`~/.config/voice-scribe/config.toml` and edit. Environment variables override
the file; `--config` overrides both.

## License

MIT — see [LICENSE](LICENSE).

This binary statically links third-party MIT-licensed code, whose copyright
notices are retained here as that license requires:

- [whisper.cpp](https://github.com/ggml-org/whisper.cpp) — MIT © 2023-2026 The ggml authors
- [ggml](https://github.com/ggml-org/ggml) — MIT © 2023-2026 The ggml authors
