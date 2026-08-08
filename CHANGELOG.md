# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Grammar-constrained decoding will not be added, and the note recommending it
  was wrong.** After `--prompt` turned out not to fix misheard proper nouns,
  AGENTS.md pointed at whisper's `grammar_rules` as "the mechanism" for
  constraining vocabulary. Measured on the same recording: a grammar permitting
  arbitrary text plus the wanted name gives output *byte-identical* to no grammar
  at all — the penalty only subtracts from tokens the grammar rejects, and a
  permissive grammar rejects nothing. A grammar tight enough to bite collapsed 50
  seconds of dialogue into two sentences and still never produced the name, which
  was in its vocabulary. ADR-0005 records the mechanism, the measurements, and
  the decision to leave proper nouns to whoever has the context downstream.
- **Source separation will not be added either.** sherpa-onnx is already linked
  and already exposes it, and it is cheap (RTF 0.038). Measured on the same
  recording: separating vocals recovered a line the original lost to a repetition
  loop, left the misheard surname unchanged, and took diarization from 18
  speakers to 14 on five minutes — still nowhere near the real cast, still
  warning. Neither Spleeter's nor UVR's model *weights* state a licence, so
  neither can go in the catalog. ADR-0006.

## [0.1.3] - 2026-08-09

Everything here came out of running one real 39-minute drama recording — music
throughout, a cast of voice actors — through the MCP server. It came back with
**93 speakers**, and nothing in the output said anything was wrong.

### Fixed

- **`--offset` / `--duration` now apply to diarization.** They only ever reached
  whisper, so transcribing thirty seconds of a forty-minute file still computed
  speaker embeddings over the whole forty minutes. Measured on that recording: a
  60-second slice with diarization went from about five minutes to 22 seconds.
  This is also what makes calibrating a threshold practical at all.

### Added

- **A warning when the speaker count looks like over-splitting** — many
  speakers, a large share of them speaking exactly once. Diarization can fail
  while producing perfectly well-formed output: every segment labelled, the JSON
  valid, nothing raised. The MCP result carries it as `warning`; the CLI prints
  it to stderr.

### Changed

- **`--prompt` guidance was wrong, and the docs called it "cheap and
  effective" without anyone having measured it.** It is whisper's initial
  prompt: it conditions the decoder rather than declaring a vocabulary, and
  phrasing matters more than content. Measured on two windows of a Japanese
  drama recording: a comma-separated name list *broke* lines that were correct
  with no prompt at all, while a sentence-form prompt over the same audio
  recovered whole lines the unprompted run had dropped, including a name it had
  lost entirely. Both READMEs and the MCP manual now show the two forms side by
  side and recommend trying it on a slice first.
- **The documentation covered only half the failure.** It said what to do when
  everyone merges into one speaker (lower the threshold) and nothing about the
  opposite, whose remedy is the reverse. Both directions are now described, with
  continuous background music named as the usual cause of over-splitting, and
  calibrating on a slice recommended over the whole file.

## [0.1.2] - 2026-08-08

### Added

- **`voice-scribe models verify`** hashes every installed model against the
  catalog and records the result. v0.1.1 verified downloads but left everything
  installed before it permanently unchecked, with no way to check it short of
  deleting and re-downloading gigabytes that were almost certainly already
  correct. A model that passes is recorded as verified in place.
- **`--reconcile`** re-files an entry under the catalog name whose file it
  actually matches. The v0.1.1 rename of the default Japanese model orphaned
  existing installs: `models pull` no longer knew the name, and nothing said so.
  The bytes on disk identify the model, so the fix is a registry rename —
  nothing is downloaded and the file is not moved.

### Changed

- **`models list` reports whether each entry has been checked**, and whether the
  catalog still knows its name. The previous listing rendered a never-verified
  model exactly like a verified one, so a healthy-looking table was the only
  evidence a user had. An inventory that cannot say what it has not checked is
  worse than no inventory: it reads as assurance.

## [0.1.1] - 2026-08-08

### Security

- **Model downloads are verified by SHA256, not only by size.** Size alone is not
  integrity — anyone able to substitute the file can preserve its length — and
  these files are parsed by a runtime that has already had a stack-buffer-overflow
  reachable from a malformed tensor header, so a tampered model is a memory-safety
  problem rather than a wrong transcript. Every catalog entry now pins a hash,
  checked before the download is promoted to its final path **and** on the path
  that reuses an already-present file, which is where a size-only check reads as
  verification while providing none.

### Changed

- **Breaking: the default Japanese model is now `kotoba-whisper-v2.0`**, fetched
  from `kotoba-tech`, the model's own authors. The previous default came from a
  third-party mirror labelled "v2.2" that serves a **byte-identical file** — same
  SHA256, same blob. Two names for one file is a menu that lies about the choice
  on offer, and there is no reason to take the default model from an unaffiliated
  re-upload. Update `default_model` in config if you set it explicitly.
- `models list --json` reports each installed model's `sha256`.

### Notes

- The whisper.cpp submodule stays on a post-release commit rather than moving back
  to tag v1.9.2, deliberately: the intervening commits include two memory-safety
  fixes on paths this tool exercises directly — a heap out-of-bounds read on very
  short audio, and the malformed-model stack overflow above. See ADR-0004.

## [0.1.0] - 2026-08-08

First release. Local speech-to-text for macOS: a CLI that transcribes audio and
labels who is speaking, and an MCP server that hands the same capability to an
agent whose model cannot process audio. No API key, and no audio leaves the
machine.

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
