# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Transcription works end to end. Speaker diarization and the MCP server are still
scaffolded; `voice-scribe mcp` returns an error naming the phase that fills it in.

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
