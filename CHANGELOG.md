# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Scaffold and build spike. **Nothing transcribes yet** — the command tree exists
so it can be reviewed against the RFP, and every unimplemented leaf returns an
error naming the phase that fills it in.

### Added

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

- The build spike passed on M2 Max / macOS 26: a single 6.0 MB arm64 binary with
  no third-party dynamic dependencies, Metal active, `EMBED_LIBRARY = 1`.
