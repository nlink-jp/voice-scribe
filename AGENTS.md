# AGENTS.md — voice-scribe

## What this is

A local speech-to-text tool: a CLI that transcribes audio with whisper.cpp and
labels who is speaking with sherpa-onnx, plus (eventually) an MCP server that
hands the same capability to agents whose model cannot process audio. No API
key; audio never leaves the machine.

It is the local counterpart of **gem-transcribe** (Vertex AI Gemini) and the
reverse direction of **voice-studio-mcp** (local TTS). Downstream consumers such
as the **meeting-notes** skill read its JSON directly.

- Module path: `github.com/nlink-jp/voice-scribe`
- Series: util-series
- Platform: **darwin/arm64 only** — CGO + Metal cannot cross-compile
- Design: `docs/ja/voice-scribe-rfp.ja.md` (source of truth; `docs/en/` is a translation)

## Status

**Pre-release.** Transcription and speaker diarization work end to end. Still
scaffolded, in `cmd/planned.go`: the MCP server (Phase 2b).

## Build and test

```bash
make build          # scaffold binary, no runtimes (they report ErrNoRuntime)
make deps           # build both native runtimes (cmake + Metal toolchain)
make build-engine   # full binary with both runtimes statically linked
make test           # test suite (equivalent to `go test ./...`)
make test-engine    # same suite against the real runtimes (both tags)
make build-all      # build-engine + Developer ID signing
make package        # signed + notarized release zip
```

Never run `go build` directly — it drops a binary in the project root. Always
`make build`, which outputs to `dist/`.

There are two build tags, one per native runtime: `cgo_whisper` for
transcription and `cgo_sherpa` for diarization. They are separate so that a
machine which cannot fetch the ONNX Runtime archive still gets a working
transcription binary — which is not hypothetical, see ADR-0002.

Most of the code is testable without either runtime: everything above
`internal/engine` and `internal/diarize` is pure Go and `make test` covers it.
Only changes to the cgo bridges need `make test-engine`.

## Structure

```
main.go                      thin entrypoint; calls cmd.Execute()
cmd/
  root.go                    root command, --config
  version.go                 --version and `version` print the same string
  context.go                 resolves config + registry, deferred to run time
  transcribe.go              the main path
  models.go                  list / pull / import / rm
  doctor.go                  reports the linked runtime and its backends
  progress.go                stderr status, terminal-aware
  planned.go                 what is still scaffolded
internal/
  audio/                     AVFoundation decoder (cgo + Objective-C)
  config/                    TOML resolution, strict decoding
  diarize/                   sherpa-onnx wrapper, behind cgo_sherpa
  catalog/                   the curated model list
  download/                  resumable HTTP fetch
  engine/                    whisper.cpp wrapper, split across a build tag
  store/                     the installed-model registry
  transcript/                output envelope, formatters, language merging
third_party/whisper.cpp/     submodule (ggml-org/whisper.cpp)
third_party/sherpa-onnx/     submodule (k2-fsa/sherpa-onnx), pinned to a release tag
docs/{en,ja}/                RFP and guides; ja is the source of truth
docs/adr/                    architecture decision records
scripts/                     codesign / notarize / homebrew, from org templates
```

## Gotchas

**Do not release with `cmd/planned.go` in the tree.** It exists so the RFP's
command surface can be reviewed; every leaf returns an error naming the phase
that fills it in. `planned_test.go` pins that they fail loudly rather than
exiting 0 — an empty success is how a stub reaches a release unnoticed.

**stdout is the transport.** `voice-scribe mcp` will speak JSON-RPC over stdout,
and `transcribe` already writes the transcript there, so everything else —
progress, runtime logs, warnings — goes to stderr. `engine.SetLogHandler` exists
for exactly this: the runtime's log callbacks are installed unconditionally so
the destination is ours to choose. Measured 2026-08-08: whisper's own output all
goes to stderr, and a real transcription leaves stdout parseable as JSON. That is
an observation about the current upstream, not a contract. When checking it,
remember that **"invisible in the console" ≠ "not written to stdout"**: measure
with `1>file 2>file` separated, never by eyeballing a terminal.

**`--translate` runs the audio through twice.** Whisper's translate task is a
separate decode, not an extra output: one `whisper_full` call yields either the
transcription or the English translation. Producing both means two passes, which
choose their own segment boundaries, so `transcript.MergeLanguage` aligns them by
time overlap. That merge is an approximation and is documented as such — do not
"fix" a translation that reads as slightly offset by tightening the merge without
first checking the two passes' actual boundaries.

**Static archive link order matters** (dependents first):
`libwhisper.a` → `libparakeet.a` → `libggml.a` → `libggml-cpu.a` →
`libggml-metal.a` → `libggml-blas.a` → `libggml-base.a`. A wrong order surfaces
as undefined symbols at link time, not at cmake configure time. If the submodule
is updated and linking breaks, run `find third_party/whisper.cpp/build -name '*.a'`
— upstream changes which archives it produces (`libparakeet.a` did not exist in
the sd.cpp equivalent image-forge started from).

**`GGML_METAL_EMBED_LIBRARY=ON` is load-bearing.** Without it the runtime looks
for a `ggml-metal.metal` file next to the executable at load time, and the
release stops being a single self-contained binary. Confirm with
`voice-scribe doctor`: the capability line must say `EMBED_LIBRARY = 1`.

**The 8.8-second Metal init is one-time, not per-process.** First run on a given
machine compiles the Metal shaders (8.797 s measured on M2 Max); the OS caches
them and subsequent runs initialise in 0.011 s. Do not cite it as a per-process
cost, and do not use it as the argument for a resident engine — the real
per-process cost is model loading.

**ggml initialises lazily**, on the first runtime call rather than at process
load. `voice-scribe --version` therefore emits zero stderr, which keeps
`brew test` clean. Do not move runtime calls into command construction.

**Whisper timestamps are centiseconds**, not milliseconds. Dividing by 1000
produces a transcript whose segments all sit in the first second.

**AVFoundation downmixes stereo with equal power, not by averaging.** A tone
duplicated across both channels comes back at √2 times its amplitude, so decoded
samples can exceed ±1.0. That is left alone: whisper's log-mel front end
tolerates it, and normalising would silently change levels between files.

**Progress output is terminal-aware.** Off a terminal it emits one plain line per
stage and no percentages. Writing the in-place redraw unconditionally turned a
single model download into 63 KB of carriage returns in a log file.

**`go test ./...` does NOT work — use `make test`.** whisper.cpp's Go bindings
are a nested module and are skipped, but sherpa-onnx carries a *non*-module Go
package at `scripts/go/` that `./...` picks up and fails to build. The
Makefile's `PKGS` filter excludes `third_party/` and is what makes the suite
runnable. (This was insurance until the second submodule arrived and made it
load-bearing.)

**Catalog entries are facts, not guesses.** Every repo, filename, size and
license in `internal/catalog` was read from the Hugging Face API. A wrong size
defeats the truncation check; a wrong filename fails at download time. Verify
upstream when adding an entry rather than copying a neighbour's values.

**sherpa-onnx's config has no defaults getter.** Unlike whisper's
`whisper_full_default_params()`, the C API offers nothing equivalent, so a
zero-initialised `SherpaOnnxOfflineSpeakerDiarizationConfig` is not "sensible
defaults" — it is a clustering threshold of 0, which collapses every turn into a
single speaker. `internal/diarize` writes the documented values out explicitly;
do not remove them believing the library will fill them in.

**The sherpa-onnx submodule is pinned to a release tag, deliberately.** Its
master branch pinned an ONNX Runtime archive whose SHA256 no longer matched the
published asset, so the build could not configure at all. `make deps` fetches
that archive itself (cmake's own downloader fails on this toolchain) and
verifies the hash. If the checksum step fails after a submodule bump, read
ADR-0002 before working around it.

**Duplicate `-lc++` is deliberate.** Both cgo packages request it and the linker
warns. Removing it from one breaks any build that links that package alone —
`go test ./internal/diarize` most obviously. A package declares what it needs.

**Speaker labels are remapped by first appearance.** sherpa returns arbitrary
cluster indices; cluster 3 is not "the third person to speak". `AssignSpeakers`
renumbers by time so that "A" is whoever spoke first, and `--speaker-hint` names
follow the same order.

**Verify the fixture before blaming the code.** A two-speaker test recording
built with `say -v Otoya` was silently a *one*-speaker recording, because that
voice is not installed and `say` falls back to the default without a word.
Diarization reporting one speaker was correct. `say -v '?'` lists what exists.
