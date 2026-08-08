# voice-scribe

[日本語版 README](README.ja.md)

Local speech-to-text for macOS: a CLI that transcribes audio with
[whisper.cpp](https://github.com/ggml-org/whisper.cpp), and an MCP server that
hands the same capability to an agent whose model cannot process audio.

No API key. No audio leaves the machine.

> **Pre-release.** Everything the design calls for works: transcription,
> speaker diarization, and the MCP server. Not released yet — see
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
voice-scribe models pull kotoba-whisper-v2.0
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
| `--diarize` | Label who is speaking. Needs the two diarization models |
| `--speakers` | Pin the speaker count when you know it |
| `--speaker-threshold` | Clustering distance when the count is unknown; lower splits more readily |
| `--speaker-hint` | Names to use instead of A/B/C, in order of first appearance |
| `-q, --quiet` | No progress on stderr |

To label who is speaking, install the two diarization models and add `--diarize`:

```bash
voice-scribe models pull pyannote-segmentation-3 && voice-scribe models pull campplus-speaker-embedding
```

```bash
voice-scribe transcribe meeting.m4a --lang ja --diarize --speaker-hint Tanaka,Sato
```

Diarization is two models working together — one finds speaker changes, the
other decides which changes belong to the same person — and it can fail in
either direction.

If everyone comes back merged into **one** speaker, pin the count with
`--speakers` or *lower* `--speaker-threshold`. If it comes back with implausibly
**many** — dozens of people, most speaking once — pin the count or *raise* the
threshold above its 0.5 default. Continuous background music is the usual cause
of the second: the embedding model sees music mixed with voice, so one person's
embeddings scatter. voice-scribe prints a warning when the count looks like
over-splitting, because the transcript is well-formed either way.

Calibrate on a slice rather than the whole file — `--offset` and `--duration`
apply to diarization too:

```bash
voice-scribe transcribe long.wav --lang ja --diarize --offset 300 --duration 300 --speaker-threshold 0.9
```

Every catalog model is pinned to a SHA256 and verified on download, so a
substituted file is refused rather than parsed. `voice-scribe models verify`
checks what is already installed and records the result; `models list` shows
which entries have been checked, because a listing that cannot say otherwise
reads as assurance.

`voice-scribe models list --catalog` shows what can be installed —
Japanese-specialised models alongside multilingual ones, with sizes and
licenses. `voice-scribe doctor` reports the runtime linked into the binary and
the ggml backends it was compiled with, read from the runtime itself.

## As an MCP server

`voice-scribe mcp` speaks the Model Context Protocol over stdio, giving an agent
whose model cannot process audio a way to read recordings. Register it with your
client as the command `voice-scribe mcp`; it takes no arguments.

Four tools: `get_usage`, `transcribe`, `check_job`, `list_models`. Transcription
is asynchronous — `transcribe` returns a job id that `check_job` polls. A short
transcript comes back inline and a long one as a file path with an excerpt; the
file is written either way.

Recordings live in a workspace directory the agent prepares and names per call,
and every path is confined to it by the kernel. `get_usage` returns the full
manual; agents should call it once before their first transcription.

Downloading models is deliberately **not** available over MCP — hundreds of
megabytes is a decision for whoever is at the terminal. Use
`voice-scribe models pull`.

The full design is in [docs/en/voice-scribe-rfp.md](docs/en/voice-scribe-rfp.md)
(the Japanese edition is the source of truth).

## Configuration

Copy [`config.example.toml`](config.example.toml) to
`~/.config/voice-scribe/config.toml` and edit. Environment variables override
the file; `--config` overrides both.

## License

MIT — see [LICENSE](LICENSE).

This binary statically links third-party code under permissive licences, whose
copyright notices are retained here as those licences require:

- [whisper.cpp](https://github.com/ggml-org/whisper.cpp) — MIT © 2023-2026 The ggml authors
- [ggml](https://github.com/ggml-org/ggml) — MIT © 2023-2026 The ggml authors
- [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) — Apache-2.0, The k2-fsa authors
- [ONNX Runtime](https://github.com/microsoft/onnxruntime) — MIT © Microsoft Corporation

The models are downloaded rather than bundled, and carry their own licences:
pyannote segmentation is MIT © 2022 CNRS, and the 3D-Speaker embedding model is
Apache-2.0. `voice-scribe models list` shows the licence of everything installed.
