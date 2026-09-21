# ADR-0001: Statically link whisper.cpp through CGO

- Status: Accepted
- Date: 2026-08-08

## Context

voice-scribe transcribes audio locally. How the inference runtime is carried decides the
distribution format, the build complexity, and the runtime dependencies. RFP Phase 1
placed this as **the project's largest risk** and decided to confirm it works end to end
before any other implementation.

There were three options.

1. **Statically link** whisper.cpp (ggml/Metal) through CGO into a single binary
2. Use Apple's SpeechAnalyzer / SpeechTranscriber (macOS 26) from Swift
3. Launch a Python implementation (MLX Whisper / faster-whisper) as a child process

## Decision

**Take (1).** Carry whisper.cpp as a submodule, build it into a static library with cmake,
and link it into a single binary through CGO. The runtime is linked only under the
`cgo_whisper` build tag; in a build without the tag (`make build`) the engine returns
`ErrNoRuntime`.

What decided it was that image-forge has already been all the way down the same path with
stable-diffusion.cpp (CGO × ggml × Metal × Developer ID signing × notarization), and
**whisper.cpp, which sits on the same ggml, can take over almost all of that as it
stands**. The project's largest build risk becomes, in practice, a known and solved
problem.

(2) has real advantages (on-device, no model management, low power draw), but it would mean
writing the MCP server in Swift, for which there is no precedent in the org. It is
**deferred, not rejected**, leaving room for a future "lightweight sibling". (3) is
rejected because it exposes a Python runtime and model placement to the user.

### A build tag switches between the real thing and a stub

`make build` produces a binary without the runtime, and `make build-engine` links the real
thing. This is the same arrangement as image-forge, and it satisfies two things at once:

- Scaffolding work can be done in an environment that has neither cmake nor the Metal toolchain
- The pure Go layer that sits on top of the runtime can be tested without putting a 1.6 GB
  model on disk

## Consequences

### Measured (2026-08-08, M2 Max / macOS 26 / Go 1.26.5 / cmake 4.4.2)

Facts confirmed by the end-to-end spike:

| Item | Result |
|---|---|
| Binary | **6.0 MB, a single Mach-O arm64** |
| Dynamic dependencies | Metal / MetalKit / Foundation / Accelerate / CoreFoundation / libc++ / libSystem / libobjc — **all of them ship with the OS**. Zero third-party dylibs |
| Metal enablement | `whisper_print_system_info()` reports `MTL : EMBED_LIBRARY = 1`. The GPU is recognized as `Apple M2 Max` / `MTLGPUFamilyApple8` |
| Metal library initialization | **8.797 s the first time → 0.011 s from the second time on** |
| Whole process (warm) | 0.05 s |
| stdout | **Not polluted** (see below) |

### Link order of the static archives

The dependent side has to come first. Getting the order wrong shows up not at configure
time but as an undefined symbol at link time:

```
libwhisper.a → libparakeet.a → libggml.a → libggml-cpu.a
             → libggml-metal.a → libggml-blas.a → libggml-base.a
```

**`libparakeet.a` is an archive that image-forge's sd.cpp did not have**; the current
whisper.cpp ships it. The set of archives may change when the submodule is updated, so if
a link error appears, check what is actually there with `find build -name '*.a'`.

### The 8.8 s of Metal is not a per-process cost

The 8.797 s of the first run is Metal shader compilation, which the OS caches. From the
second run on it is 0.011 s. So **the RFP's statement that "a resident process is needed
because there is a Metal cold load" is inaccurate**; the value of a resident process lies
in model load time (to be measured in Phase 1). This has to be measured again before it is
written in the documentation.

### ggml is initialized lazily

ggml registers its backends not at process start but when the runtime is first called.
Measured, the stderr of `voice-scribe --version` was **0 lines**. `brew test` invokes
`--version`, so this property is convenient.

### stdout is not polluted, but the defence goes in anyway

Measured with `1>file 2>file` to separate the two, all 16 lines of ggml's initialization
log went to **stderr**, and stdout carried nothing but our own output. Even so, following
the lesson of image-forge v0.9.0, the Phase 2b MCP implementation puts in two layers:

1. At the source — replace the log callback with `whisper_log_set` / `ggml_log_set`
2. At the transport — dup the real stdout and keep it for the transport alone, and point
   fd 1 at stderr

"It is not leaking right now" is only an observation that depends on an upstream
implementation detail; it is not a contract.

### Costs accepted

- **No cross-compilation. A single darwin/arm64 release.** Because Metal has no
  Linux/Windows/amd64 target. Deliberately scoped this way in the RFP
- The build needs cmake and the Metal toolchain (`make deps`)
- Third-party MIT code is statically linked, so the README needs an attribution notice
  (whisper.cpp and ggml are both MIT © 2023-2026 The ggml authors. Check the upstream
  LICENSE in place — the year is updated)
- `go test ./...` **works as it is**. whisper.cpp's Go bindings are a nested module with
  their own `bindings/go/go.mod`, so they fall out of the expansion of `./...` naturally
  (image-forge needed a `PKGS` filter with sd.cpp because its libwebp swig bindings were
  not a nested module). The `PKGS` in the Makefile is kept only as insurance in case
  upstream drops the go.mod, and is currently equivalent to `./...`
