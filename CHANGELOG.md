# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Feature-complete against the design: transcription, speaker diarization and the
MCP server all work end to end. Nothing is scaffolded any more.

### Added — MCP server (Phase 2b)

- `voice-scribe mcp` serves the Model Context Protocol over stdio with four
  tools: `get_usage`, `transcribe`, `check_job`, `list_models`. Transcription is
  asynchronous through a single-worker job queue.
- Recordings live in a workspace the agent prepares and names per call. Every
  path is confined to it by the kernel (`os.Root`), so a symlink planted in the
  workspace cannot make the server read or write outside it.
- Transcripts come back inline when short and as a path with an excerpt when
  long; the file is written either way, and the excerpt is cut at a rune
  boundary so Japanese text does not arrive as replacement characters.
- `get_usage` returns a full operating manual, and tests pin it against the code:
  every tool, error code, transcribe argument and output format must appear in
  it.
- stdout is claimed for the protocol and fd 1 is redirected to stderr, so a
  stray write from anywhere becomes log noise instead of a corrupt session.

### Added — speaker diarization (Phase 2a)

- `--diarize` labels who is speaking, using sherpa-onnx with a pyannote
  segmentation model and a 3D-Speaker embedding model. `--speakers` pins the
  count when it is known, `--speaker-threshold` tunes the clustering when it is
  not, and `--speaker-hint` replaces A/B/C with real names.
- Speaker labels follow first appearance rather than the clusterer's arbitrary
  indices, so "A" is whoever spoke first.
- The diarization runtime sits behind its own `cgo_sherpa` build tag, so a
  machine that cannot fetch the ONNX Runtime archive still gets a working
  transcription binary.

### Changed

- **`--min-speakers` and `--max-speakers` were dropped before they shipped.**
  sherpa-onnx's clusterer takes either an exact speaker count or a distance
  threshold, with no notion of a range, so the flags the RFP named could not
  have done anything. `--speaker-threshold` replaces them, and the
  `[diarize] min_speakers`/`max_speakers` settings became `[diarize] threshold`.

### Added — transcription (Phase 1)

- `voice-scribe transcribe`: any container macOS can read → text with
  timestamps, decoded through AVFoundation so the binary stays self-contained
  (no ffmpeg). Flags for language, vocabulary biasing, thread count, and
  transcribing a slice of the audio.
- Five output formats — json, text, md, srt, vtt — in an envelope compatible
  with gem-transcribe, so downstream consumers parse cloud and local transcripts
  with one parser. Subtitles split into one file per language when a transcript
  carries more than one.
- `--translate`: produces the original and an English translation together.
  Whisper's translate task is a separate decode rather than an extra output, so
  this runs the audio through twice and merges the passes by time overlap.
- `voice-scribe models {list,pull,import,rm}` with a curated catalog:
  Japanese-specialised kotoba-whisper alongside multilingual large-v3-turbo,
  large-v3, base, and the Silero VAD model. Downloads resume after an
  interruption and are checked against the expected size.
- Model resolution that does not require configuration: an explicit `--model`
  wins, then the configured default, then a model specialised for the requested
  language, then any multilingual one.
- `config.toml` resolution with strict decoding — a mistyped key is an error
  rather than a setting that silently does nothing.
- `--vad`, gating silence to suppress whisper's hallucinated repetition. It
  needs its own model and says so when that model is missing.

### Added — scaffold and build spike

- Project scaffold following the org conventions: cobra command tree, Makefile
  with `build` / `build-engine` / `package`, MIT LICENSE, bilingual README,
  `config.example.toml`, `docs/{en,ja}` and `docs/adr/`.
- `third_party/whisper.cpp` as a submodule, with `make deps` building it into
  static libraries (Metal backend, embedded shader library).
- `internal/engine`: runtime wrapper split across the `cgo_whisper` build tag,
  so a binary without the runtime still builds and reports `ErrNoRuntime`.
- `voice-scribe doctor`: reports the linked runtime and the ggml backends it was
  actually compiled with.
- `--version` and the `version` subcommand, pinned to identical output by a test
  (`brew test` runs the flag).
- ADR-0001 recording the CGO static-link decision and the spike measurements.

### Notes

- The build spike passed on M2 Max / macOS 26: a single arm64 binary with no
  third-party dynamic dependencies, Metal active, `EMBED_LIBRARY = 1`.
- End-to-end verified on real Japanese audio: an m4a transcribed with
  kotoba-whisper-v2.2 at a real-time factor of about 0.04, with stdout carrying
  nothing but the transcript.
- Progress output detects whether it is writing to a terminal. It previously
  emitted in-place redraws unconditionally, which turned a single model download
  into 63 KB of carriage returns in a log file.
- Diarization verified on a two-speaker Japanese recording: five turns, both
  speakers detected automatically, every line attributed correctly.
- Linking ONNX Runtime takes the binary from 10.2 MB to 29.5 MB. The only added
  dynamic dependencies are system frameworks.
