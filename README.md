# voice-scribe

[日本語版 README](README.ja.md)

Local speech-to-text for macOS: a CLI that transcribes audio with
[whisper.cpp](https://github.com/ggml-org/whisper.cpp), and an MCP server that
hands the same capability to an agent whose model cannot process audio.

No API key. No audio leaves the machine.

> **Pre-release.** Transcription works end to end. Speaker diarization and the
> MCP server are not implemented yet — `voice-scribe mcp` says so when run. See
> [CHANGELOG.md](CHANGELOG.md).

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

Install a model, then transcribe:

```bash
voice-scribe models pull kotoba-whisper-v2.2
```

```bash
voice-scribe transcribe meeting.m4a --lang ja
```

Output is JSON on stdout by default, with timestamps and an envelope compatible
with gem-transcribe. Progress goes to stderr, so piping the transcript is safe.

Other formats and options:

```bash
voice-scribe transcribe interview.mp4 -f srt -o interview.srt
```

| Flag | What it does |
|---|---|
| `-m, --model` | Pick a model. Without it, a model matching `--lang` is chosen, then the configured default |
| `--lang` | Input language (ISO 639-1). Detected when omitted |
| `--translate` | Also produce English. Whisper's translate task is a separate decode, so this runs the audio through twice and merges by timestamp |
| `--prompt` | Bias the decoder's vocabulary with proper nouns and jargon |
| `-f, --format` | `json` (default), `text`, `md`, `srt`, `vtt` |
| `--offset` / `--duration` | Transcribe a slice of the audio |
| `--vad` | Gate silence, suppressing hallucinated text. Needs `models pull silero-vad` |
| `-q, --quiet` | No progress on stderr |

`voice-scribe models list --catalog` shows what can be installed —
Japanese-specialised models alongside multilingual ones, with sizes and
licenses. `voice-scribe doctor` reports the runtime linked into the binary and
the ggml backends it was compiled with, read from the runtime itself.

Speaker diarization (`--diarize`) and the MCP server are not implemented yet.
The full design is in [docs/en/voice-scribe-rfp.md](docs/en/voice-scribe-rfp.md)
(the Japanese edition is the source of truth).

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
