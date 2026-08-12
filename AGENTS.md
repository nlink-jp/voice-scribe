# AGENTS.md — voice-scribe

## What this is

A local speech-to-text tool: a CLI that transcribes audio with whisper.cpp and
labels who is speaking with sherpa-onnx, plus an MCP server that hands the same
capability to agents whose model cannot process audio. No API key; audio never
leaves the machine.

It is the local counterpart of **gem-transcribe** (Vertex AI Gemini) and the
reverse direction of **voice-studio-mcp** (local TTS). Downstream consumers such
as the **meeting-notes** skill read its JSON directly.

- Module path: `github.com/nlink-jp/voice-scribe`
- Series: util-series
- Platform: **darwin/arm64 only** — CGO + Metal cannot cross-compile
- Design: `docs/ja/voice-scribe-rfp.ja.md` (source of truth; `docs/en/` is a translation)

## Status

**Released**, on the org's Homebrew tap, signed and notarized. Transcription,
speaker diarization and the MCP server all work end to end; nothing is
scaffolded (`cmd/planned.go` is gone).

`git tag` is the authority on which version — **do not name one here.** This
section said "not released yet" through four releases, and both READMEs repeated
it directly above their own `brew install` line. A status written at scaffold
time does not update itself; anything here that a release changes is a bug
waiting to happen.

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
  mcp.go                     the MCP server command and stdout isolation
  mcp_wiring.go              adapts the CLI paths to the MCP tool interfaces
internal/
  audio/                     AVFoundation decoder (cgo + Objective-C)
  config/                    TOML resolution, strict decoding
  diarize/                   sherpa-onnx wrapper, behind cgo_sherpa
  catalog/                   the curated model list
  download/                  resumable HTTP fetch
  engine/                    whisper.cpp wrapper, split across a build tag
  mcp/                       the MCP server: skeleton ported from image-forge,
                             plus tools/ (four tools and their usage.md)
  store/                     the installed-model registry
  transcript/                output envelope, formatters, language merging
third_party/whisper.cpp/     submodule (ggml-org/whisper.cpp)
third_party/sherpa-onnx/     submodule (k2-fsa/sherpa-onnx), pinned to a release tag
docs/{en,ja}/                RFP and guides; ja is the source of truth
docs/adr/                    architecture decision records
scripts/                     codesign / notarize / homebrew, from org templates
```

## Gotchas

**stdout is the transport.** `voice-scribe mcp` speaks JSON-RPC over stdout,
and `transcribe` writes the transcript there, so everything else —
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

**A quiet server does not prove the stdout isolation works.** There are two
defences — log callbacks at the source and `claimStdout()` repointing fd 1 at
stderr — and the first one alone explains an empty stderr, because it filters
info-level chatter. Nearly recorded that as evidence for the second. The
mechanism is tested directly in `cmd/mcp_test.go` instead: write to the protocol
handle, write to `os.Stdout`, check where each lands. When two defences cover
the same failure, an observation explained by one is not evidence for both.

**usage.md is machine-checked against the code.** It is the only document a
client reads before operating the server, so drift is a real failure.
`usage_test.go` pins that every registered tool, every returnable error code,
every `transcribe` argument and every output format appears in it. Adding an
argument without documenting it fails the build.

**The MCP server opens a session per call rather than keeping one resident.**
Deliberate — see ADR-0003. Jobs are serialised through one worker, so residency
would only skip a reload between consecutive calls, at the cost of holding half
a gigabyte in a server that may idle for hours. Change it when there is a
measurement, not before.

**The MCP skeleton is a port, and ports carry their origin's vocabulary.** When
updating it from image-forge again, re-read the comments: "generation project",
"init/mask images" and "rendered PNGs" all had to be rewritten, and a reader who
meets them believes they are in an image-generation server.

**Model downloads are hash-verified, and the reuse path is the one that matters.**
`models pull` skips the download when the file is already present. That skip is
exactly where a size-only check would read as verification while providing none,
so it hashes too. See ADR-0004 — and note that adding a catalog entry without a
SHA256 fails `catalog_test.go`.

**whisper.cpp is deliberately pinned past its last release tag.** The commits
between v1.9.2 and the pin include two memory-safety fixes on paths this tool
exercises with untrusted input: a heap out-of-bounds read on very short audio,
and a stack-buffer-overflow from a malformed model header. Moving "back to a
release tag" for tidiness would drop both. Move forward when v1.9.3 ships.

**A listing that cannot say what it has not checked reads as assurance.** v0.1.1
added hash verification for downloads and shipped a `models list` that rendered
a never-verified model exactly like a verified one — so the only evidence a user
had was a table that looked healthy. The CHECKED column and `models verify`
exist for that. When adding a safety check, ask what the existing state looks
like to someone who has it, not just what new operations will do.

**Diarization fails in both directions, and only one was documented.** The
guidance covered "everyone merged into one speaker → lower the threshold" and
said nothing about over-splitting, whose remedy is the opposite. Real material
found it: a 39-minute recording with continuous background music returned 93
speakers under the default threshold, and 0.9 gave a plausible cast. When
writing guidance for a tunable, describe both ends of it.

**A well-formed wrong answer needs a warning, because nothing else will say
anything.** That diarization produced valid JSON with every segment labelled;
no error, no failed validation. `transcript.Diagnose` exists for exactly that
shape of failure. Its thresholds are deliberately loose — a warning that fires
on good results teaches people to ignore it.

It returns a slice, and there are two checks: over-split speakers and decoder
repetition loops. A loop is the same shape of failure one layer down — the audio
under it is absent from the transcript, not mistranscribed, and the segments
around it look perfectly ordinary. Add the third check by writing a
`diagnose*` function and appending it in `Diagnose`; both call sites (the CLI
and the MCP result) already iterate.

**Model defaults are measured, not reasoned about.** The Japanese default was
`kotoba-whisper-v2.0` because a Japanese-specialised model must surely beat a
multilingual one. It does not (ADR-0008). The harness that settled it is in
`spike/transcribe-cpp/eval/`; it scores
against the corpora kotoba-whisper's own model card uses, so its numbers can be
checked against published ones instead of trusted.

**Removing the music does not fix the over-splitting, and the separation models
cannot be shipped anyway.** sherpa-onnx — already linked — exposes source
separation (Spleeter, UVR) and denoising (GTCRN, DPDFNet), so it looks like a
free win sitting right there. Measured: separating vocals took over-splitting
from 18 speakers to 14 on the same five minutes, still far off the real cast and
still warning. It did recover a line the original lost to a repetition loop, and
it left the misheard surname exactly as it was. The blocker is licensing —
neither Spleeter's nor UVR's *weights* state one. See ADR-0006 before reaching
for this again; threshold calibration remains the only remedy for over-splitting.

**`--prompt` conditions the decoder; it does not declare a vocabulary.** Phrasing
dominates content: a sentence describing the recording helps, a comma-separated
name list measurably degrades output. The docs originally called it "cheap and
effective" — written from expectation, never measured, and wrong. CLAUDE.md
already forbids exactly that ("do not write model behaviour from memory"); the
rule was in the file and still got broken, so treat any performance or accuracy
adjective in these docs as a claim that needs a measurement behind it.

**`--prompt` does not fix misheard proper nouns.** Four prompts containing a
correct surname — kanji, katakana, in a list, used in a sentence — all still
produced the wrong one on the same audio, and some cost correct lines elsewhere.
The initial prompt conditions register and context; it is not a lexicon.

**Nor does grammar-constrained decoding — that earlier note here was wrong.**
whisper_full_params carries grammar_rules, and this file used to call it "the
mechanism" for constraining vocabulary. Measured: a grammar permitting arbitrary
text plus the wanted name produces output byte-identical to no grammar at all,
because the penalty only subtracts from tokens the grammar *rejects* and nothing
is rejected. A grammar tight enough to bite destroys the transcript instead —
and still does not emit the name. See ADR-0005 for the mechanism, the numbers,
and where proper nouns get fixed instead. **Do not re-open this by reasoning; it
is measured.**
