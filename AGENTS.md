# AGENTS.md — voice-scribe

## What this is

A local speech-to-text tool: a CLI that transcribes audio with whisper.cpp, plus
an MCP server (`voice-scribe mcp`) that hands the same capability to agents whose
model cannot process audio. No API key; audio never leaves the machine.

It is the local counterpart of **gem-transcribe** (Vertex AI Gemini) and the
reverse direction of **voice-studio-mcp** (local TTS). Downstream consumers such
as the **meeting-notes** skill read its JSON directly.

- Module path: `github.com/nlink-jp/voice-scribe`
- Series: util-series
- Platform: **darwin/arm64 only** — CGO + Metal cannot cross-compile
- Design: `docs/ja/voice-scribe-rfp.ja.md` (source of truth; `docs/en/` is a translation)

## Status

**Pre-release: scaffold + build spike only.** The command tree exists and the
whisper.cpp runtime links, but nothing transcribes yet. Everything in
`cmd/planned.go` returns "not implemented yet". See CHANGELOG.md.

## Build and test

```bash
make build          # scaffold binary, no runtime (engine reports ErrNoRuntime)
make deps           # build whisper.cpp static libraries (cmake + Metal toolchain)
make build-engine   # full binary with the runtime statically linked
make test           # test suite (equivalent to `go test ./...`)
make test-engine    # same suite against the real runtime (-tags cgo_whisper)
make build-all      # build-engine + Developer ID signing
make package        # signed + notarized release zip
```

Never run `go build` directly — it drops a binary in the project root. Always
`make build`, which outputs to `dist/`.

## Structure

```
main.go                      thin entrypoint; calls cmd.Execute()
cmd/                         cobra command tree
  root.go                    root command, --config
  version.go                 --version and `version` print the same string
  doctor.go                  reports the linked runtime and its backends
  planned.go                 scaffolded commands that refuse to pretend
internal/engine/             the runtime wrapper
  engine.go                  Info, ErrNoRuntime — shared by both builds
  engine_whisper.go          //go:build cgo_whisper — the real cgo bridge
  engine_stub.go             //go:build !cgo_whisper — reports "no runtime"
third_party/whisper.cpp/     submodule (ggml-org/whisper.cpp)
docs/{en,ja}/                RFP and guides; ja is the source of truth
docs/adr/                    architecture decision records
scripts/                     codesign / notarize / homebrew, from org templates
```

## Gotchas

**Do not release with `cmd/planned.go` in the tree.** It exists so the RFP's
command surface can be reviewed; every leaf returns an error naming the phase
that fills it in. `planned_test.go` pins that they fail loudly rather than
exiting 0 — an empty success is how a stub reaches a release unnoticed.

**stdout is the transport.** `voice-scribe mcp` speaks JSON-RPC over stdout, so
anything else written there corrupts the protocol. Measured on 2026-08-08: all
16 lines of ggml init chatter go to **stderr**, and stdout stays clean. That is
an observation about the current upstream, not a contract — Phase 2b still
installs `whisper_log_set` / `ggml_log_set` callbacks at the source and dups the
real stdout for transport use only. When checking this, remember that
**"invisible in the console" ≠ "not written to stdout"**: measure with
`1>file 2>file` separated, never by eyeballing a terminal.

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
per-process cost is model loading, which Phase 1 must measure.

**ggml initialises lazily**, on the first runtime call rather than at process
load. `voice-scribe --version` therefore emits zero stderr, which keeps
`brew test` clean. Do not move runtime calls into command construction.

**`go test ./...` works here.** whisper.cpp's Go bindings are a nested module
(`bindings/go/go.mod`), so `./...` skips them. The Makefile's `PKGS` filter is
insurance against upstream dropping that go.mod, not a current requirement.
