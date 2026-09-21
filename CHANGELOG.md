# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`weights_repo` on every catalog entry**, in `models list --catalog --json`
  and in the MCP `list_models` catalog scope. It names who published the
  weights, which is where that entry's licence was read from — never the repo
  the file is downloaded from. The field is required: `TestEveryEntryIsComplete`
  fails on an empty one, or on one equal to `Repo`, and
  `TestLicenceProvenanceIsPinned` pins the (weights repo, licence) pair for all
  7 entries, so changing a licence fails the build and names the model card to
  re-read. Nothing pinned these values before, which is why the defect below
  shipped silently.

### Fixed

- **`large-v3` and `base` were listed under the wrong licence.** Both carried
  `mit`, which is what `ggerganov/whisper.cpp` declares for the conversion repo
  the ggml files are fetched from. The weights are OpenAI's and their model
  cards declare `apache-2.0`, so `models list` and `models pull` reported terms
  that were not the ones attached to the model. Checked against the Hugging
  Face model cards on 2026-09-21; `models list` now shows `apache-2.0` for both.
- The family is **not uniform**, which is what hid this: `large-v3-turbo` — the
  default, and the entry anyone spot-checking would look at — really is `mit`.
  It is unchanged, and now says next to its value why, so it does not get
  aligned with its neighbours later. `kotoba-whisper-v2.0` was already right.
- **All 7 entries were checked against their upstream weights**, not just the
  two that were wrong: `pyannote/segmentation-3.0` (mit), 3D-Speaker
  (apache-2.0) and `snakers4/silero-vad` (mit) are each fetched from a mirror
  too, so all three had the shape that produced the defect; all three were
  already correct.

## [0.4.4] - 2026-09-21

### Security

- **A symlink planted at `<work_dir>/<workspace_id>` no longer redirects the
  whole workspace.** `os.Root` confines operations inside a root but resolves
  the root path itself normally, so if anything else with write access to your
  work directory (another tool, sandboxed code) left a link at the workspace's
  name, every read and write anchored on the link's target: transcripts were
  written outside the directory you named and the call reported success. The
  workspace directory is now created through an `os.Root` on `work_dir` and
  then verified by real path; a workspace whose name resolves elsewhere is
  refused with `path_not_allowed`, naming the id and what it resolved to. See
  [ADR-0012](docs/en/adr/0012-verify-workspace-base-by-real-path.md).

### Documentation

- **The ADR log is bilingual, with Japanese as the source of truth.** The
  eleven records were written in Japanese. 43f2988 moved them to `docs/en/adr/`
  on the stated premise that the log was "English only" — it never was, so the
  move filed Japanese prose under `docs/en/`. 83719a5 put the records back at
  `docs/ja/adr/NNNN-slug.ja.md`, ace4af2 added the English translations at
  `docs/en/adr/NNNN-slug.md`, and every reference now points at the reader's own
  language. Measurements, identifiers, paths, flags and quoted output are
  verbatim across each pair; the Japanese record is what a decision is recorded
  in, and the English one follows it.

## [0.4.3] - 2026-09-14

### Added

- `TestEveryRequiredNameIsDeclared` — a schema that lists a name in `required`
  without declaring it in `properties` makes a strict client refuse the whole
  tool list (Vertex AI: "schema at top-level requires unspecified property").
  data-toolbox-mcp shipped exactly that and broke a session outright; the
  existing contract test checked declared ⇒ required only, so the fleet is
  pinned in both directions now.

## [0.4.2] - 2026-09-14

### Changed

- **A relative `audio` is now looked for in `work_dir` itself as well as in the
  workspace** (the workspace wins if both hold the name). The workspace is a
  level below the work directory the caller named, which is not where an agent
  that has just written a file puts it: two real sessions lost rounds to exactly
  that. An absolute path anywhere readable was already accepted, so resolving a
  relative name one level up costs no containment.
- **An absolute `audio` that is not there now says where the file actually is**,
  when a file of that name sits in the workspace or the work directory. A
  session passed `<work_dir>/x.aiff` for a file at `<work_dir>/<id>/x.aiff` and
  spent a round discovering the level. The search is those two directories
  only — never a tree walk.

## [0.4.1] - 2026-09-14

### Fixed

- **`input_not_found` named nothing.** "input %q is not in the workspace —
  place it there first" does not say where the workspace is, and a real agent
  (2026-09-14) answered it by inventing `~/sessions/current_session/work`, being
  denied, fetching `get_usage`, re-recording, and finally passing an absolute
  path: four rounds to recover from one sentence. The error now names the
  absolute path it looked at and offers the escape — a recording may be an
  absolute path to wherever it already is, read in place.
- The `audio` argument's description says the workspace is
  `<work_dir>/<workspace_id>/`, **a level below `work_dir` itself**. That is the
  confusion the agent actually had: it had just set `work_dir`, put the file
  there, and passed a name relative to it.

## [0.4.0] - 2026-09-14

### Changed

- **Breaking: `[mcp] inline_threshold` is now `max_bytes`, and the result always
  carries the transcript.** The old threshold switched the *delivery mode*: at or
  below 8 KB you got the whole text, above it you got an `excerpt` and a path and
  no text at all. That is a judgement about your context window, which this
  server cannot make — the same reason splunk-mcp and pcap-analyzer-mcp dropped
  their spills. `max_bytes` (config `[mcp] max_bytes`, tool argument `max_bytes`,
  default 65536, `0` means no cap) bounds the response and nothing else: the
  result carries as much text as the cap allows, `truncated` and `omitted_bytes`
  say exactly what it left out, `bytes` stays the full size, and `path` /
  `absolute_path` reach all of it. See
  [ADR-0011](docs/en/adr/0011-response-cap-not-delivery-mode.md).
- **The transcript file is written either way, as before.** It is this server's
  product — an srt you hand to a video player, a json a downstream tool reads —
  so `work_dir` stays required (ADR-0010). The cap never decides whether the
  artifact exists.
- A config still carrying `[mcp] inline_threshold` fails to load, naming
  `max_bytes` as the replacement.

### Added

- `TestModelFacingTextNamesNoWithdrawnDeliveryMode` — walks the initialize
  instructions, the usage manual and every tool's description and schema for the
  words that described the withdrawn switch. It found two tool descriptions the
  rename had missed.

### Removed

- The `excerpt` field. A preview standing in for text that was withheld has
  nothing to stand in for any more.

## [0.3.1] - 2026-09-14

### Fixed

- **The initialize `instructions` field never mentioned `work_dir`.** It is the
  first thing the model reads about this server — before any tool list — and it
  still described "a workspace directory you prepare" while every tool required
  an argument it did not name. It now states the contract: `work_dir` is the
  absolute path of a directory you can read back, required, with no default.

### Added

- `TestInstructionsNameTheWorkDirContract` — the schema and description tests
  walked `tools/list`; nothing walked what `initialize` returns (ADR-0010).

## [0.3.0] - 2026-09-13

### Changed

- **Breaking: the MCP work directory is now `work_dir`, and it is required.**
  It replaces `workspace_root` on `transcribe`, and it means what the caller
  means by it: the absolute path of a directory the caller can read back.
  Recordings are read from `<work_dir>/<workspace_id>/` and transcripts written
  there. A call that sends one of the retired names (`workspace_root`,
  `workspaceRoot`, `workspace_dir`) is refused with `work_dir_required` naming
  the replacement. This is the reference implementation of the organization's
  work-directory contract for file-mediated MCP servers — see
  [ADR-0010](docs/en/adr/0010-work-dir-contract.md).
- **The server no longer has a default workspace root.** Omitting the argument
  used to write under `~/.local/share/voice-scribe/mcp-workspaces`, which no
  calling agent can read back: the job succeeded and the path it returned could
  not be opened. There is now no destination the caller did not name, and
  `~/.local/share/voice-scribe/mcp-workspaces` is no longer used or created
  (existing contents are left alone; delete them at your leisure).
- A runtime may supply the directory instead of the model: the server reads
  `_meta["jp.nlink/work_dir"]` from the `tools/call` request when the argument
  is absent. The argument always wins.
- Results echo where they went: `transcribe` acknowledgements and finished
  results carry the resolved `work_dir` and `workspace_id`.

- `audio` may now be **an absolute path to a recording anywhere you can read**.
  It is read in place and never copied, since staging an hour of audio into the
  workspace to transcribe it is waste and the caller could have read the file
  itself. Credential and agent-control locations (`~/.ssh`, `~/.aws`,
  `~/.gnupg`, `~/.config/gcloud`, `~/Library/Keychains`, `~/.claude`, `~/.codex`,
  any `.env`) are refused, checked on the path as given and on its
  symlink-resolved form — resolving alone walks past a link such as
  `~/.ssh/config` that points into a cloud-sync folder. Relative paths are
  workspace-relative as before, with kernel-enforced containment. Transcripts
  still land in the workspace.

### Added

- Five error codes that say which part of the contract failed:
  `work_dir_required`, `work_dir_invalid`, `work_dir_not_found`,
  `work_dir_not_writable`, `work_dir_denied`. The work directory must already
  exist (the server does not create it), must be writable, and may not be a
  system location, the home directory itself, or this server's own data
  directory.

## [0.2.2] - 2026-08-31

### Changed

- The `workspace_root` argument now says plainly that the caller should pass a
  root it can read back: every result is returned as a path under that root, so
  a workspace the caller cannot open leaves it holding a path to nothing. Text
  only — the behaviour is unchanged.

## [0.2.1] - 2026-08-27

Two VAD defects reported against v0.2.0, fixed together because the second was
hiding behind the first — see [ADR-0009](docs/en/adr/0009-vad-offset-composition.md).

### Fixed

- **`--vad` with `--offset`/`--duration` silently transcribed a different
  stretch of the audio.** Upstream runs VAD before applying the offset, so the
  offset counted seconds into the silence-stripped audio while the returned
  timestamps still pointed at the original timeline — the result looked
  perfectly consistent and was off by the length of every silence before the
  requested point. The window is now cut on the Go side before VAD runs, so
  offset and duration mean seconds of the original recording whether or not
  VAD is on. A window that starts past the end of the audio now says so,
  instead of claiming the recording may be silent.
- **`vad = true` in the config was silently ignored over MCP.** The MCP wiring
  never consulted it — while `threads` from the same table was honoured — so
  the CLI gated hallucinations and the MCP server did not, with nothing
  reporting the difference. Both paths now resolve VAD with the same
  flag-or-config expression.

### Added

- **A `vad` argument on the MCP `transcribe` tool**, matching the CLI's
  `--vad`. There was previously no way to enable VAD over MCP at all. Asking
  for it without the model installed fails as `model_not_found`, naming the
  `models pull silero-vad` that fixes it — downloading stays a decision for
  whoever is at the terminal.

## [0.2.0] - 2026-08-12

The Japanese default was picked by reasoning and never measured. Measuring it
changed the answer, and the model that won brings a failure of its own — so the
warning for that failure ships in the same release.

### Changed

- **The default model for Japanese is now `large-v3-turbo`**, replacing
  `kotoba-whisper-v2.0`. The old default was chosen on the reasoning that a
  Japanese-specialised model must be better at Japanese; measuring it says
  otherwise. Character error rate on this machine, q5_0, 100 utterances per
  corpus: Common Voice 8 **9.57%** vs 9.41%, JSUT basic5000 **6.29%** vs 7.15%,
  ReazonSpeech **7.82%** vs 9.08% — turbo ahead on two, behind on one by a
  margin too small to call, and ahead on the corpus kotoba-whisper was trained
  on. `kotoba-whisper-v2.0` stays in the catalog and `--model` still reaches it.
  A `default_model` already written to a config file is left alone. See
  [ADR-0008](docs/en/adr/0008-japanese-default-model.md).

### Added

- **A warning when the decoder falls into a repetition loop.** Whisper repeats
  one line over and over when it loses the thread; the audio under the loop is
  missing from the transcript rather than mistranscribed, and nothing else about
  the result looks wrong. The warning names the number of loops, the repeated
  segments, the seconds swallowed, and where the longest run starts. Measured on
  a 39-minute recording with continuous music, three runs per model: the new
  default produced runs of 19, 45 and 48, while `kotoba-whisper-v2.0` on the
  same audio never exceeded 3. (Three runs, because the same input does not
  produce the same transcript twice — see the ADR.)
  The MCP result carries it in `warning` alongside any diarization warning; the
  CLI prints one line per diagnosis to stderr.

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
  `config.example.toml`, `docs/{en,ja}` and an ADR log.
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
