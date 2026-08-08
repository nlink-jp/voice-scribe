# RFP: voice-scribe

> Generated: 2026-08-08
> Status: Draft
> Source of truth: the Japanese edition (`docs/ja/voice-scribe-rfp.ja.md`). This is a translation.

## 1. Problem Statement

A tool that gives agents driven by an LLM that cannot handle audio — or handles
it poorly — **local, self-contained transcription over MCP**. It does the same
job as gem-transcribe (Vertex AI Gemini) at zero API cost and without sending
audio off the machine.

The target users are the nlink-jp operator and any agent (Claude Code or another
client) that consumes the util-series MCP servers. Input is a local audio or
video file; output is structured text with speaker labels and timestamps.
Structuring and summarising meeting minutes is out of scope — that work belongs
to the downstream meeting-notes skill.

## 2. Functional Specification

### Commands / API Surface

A single binary, `voice-scribe`, hosts both the CLI and the MCP server (the same
shape as image-forge, following the org's single-binary-subcommand convention).

#### CLI

| Subcommand | Purpose |
|---|---|
| `voice-scribe transcribe <file>` | The main path: transcription plus optional diarization |
| `voice-scribe models list` | List installed / catalog models (`--catalog` / `--all` / `--json`) |
| `voice-scribe models pull <name>` | Fetch a model (weights + profile registration) |
| `voice-scribe models import <path>` | Register a local ggml/ONNX model (`--kind`) |
| `voice-scribe models rm <name>` | Remove |
| `voice-scribe mcp` | Start the MCP server (stdio) |
| `voice-scribe doctor` | Diagnose models, Metal, decodability, ONNX Runtime |
| `voice-scribe --version` | Version (from `git describe`; Homebrew's `brew test` calls it) |

Flags for `transcribe`:

| Flag | Meaning |
|---|---|
| `-m, --model <name>` | Registry name. Falls back to `default_model` in config |
| `--lang <code>` | Input language. Auto-detected when omitted |
| `--translate` | Also emit an English translation (whisper's translate task); adds an `en` key to `text` |
| `--diarize` | Enable speaker diarization |
| `--speakers <N>` | Pin the speaker count |
| ~~`--min-speakers` / `--max-speakers`~~ -> `--speaker-threshold` | **Corrected 2026-08-08**: sherpa-onnx's clusterer takes either an exact count or a distance, with no notion of a range. Replaced with the knob that exists (ADR-0002) |
| `--speaker-hint <names>` | Assign given names instead of A/B/C labels |
| `--prompt <text>` | Vocabulary biasing (whisper's initial prompt), for proper nouns and jargon |
| `--offset <sec>` / `--duration <sec>` | Process a portion only |
| `-f, --format <fmt>` | `json` (default) / `text` / `md` / `srt` / `vtt` |
| `-o, --output-file <path>` | Output path; stdout when omitted |
| `--threads <N>` | Inference thread count |
| `--vad` / `--no-vad` | VAD gating on silence (suppresses hallucination) |

#### MCP tools (four)

| Tool | Kind | Description |
|---|---|---|
| `get_usage` | sync | Full usage text (embedded `usage.md`). Read this first |
| `transcribe` | **async job** | Returns a `job_id` immediately; arguments mirror the CLI |
| `check_job` | sync | Poll a job; returns the result on completion |
| `list_models` | sync | Installed models (same view as `models list --json`) |

**`models pull` is deliberately not exposed over MCP** — an agent should not
kick off a multi-gigabyte download on its own judgement. When a model is
missing, `list_models` and the error paths point at the CLI procedure.

### Input / Output

#### Input

- Local audio or video files (m4a / mp3 / wav / aiff / caf / mp4 / mov / flac, …)
- Decoding goes through **AVFoundation via CGO** (converted to 16 kHz mono
  float32 PCM)
- Over MCP, all I/O is confined under `workspace_root` (absolute path, defaulting
  to `~/.voice-scribe`) with kernel-level `os.Root` containment — the same design
  as voice-studio-mcp ADR-0010

#### Output (JSON)

The envelope is **compatible with gem-transcribe**, so that downstream consumers
(meeting-notes and friends) can parse either source with the same parser.

```json
{
  "metadata": {
    "source": "meeting.m4a",
    "model": "kotoba-whisper-v2.2-q5_0",
    "duration_seconds": 3421.5,
    "languages": ["ja"],
    "speaker_hints": [],
    "dropped_segments": 0
  },
  "segments": [
    {
      "start": 0.0,
      "end": 4.12,
      "speaker": "A",
      "text": { "ja": "それでは始めます。" }
    }
  ]
}
```

- `text` is a **language-code → text map**, the same shape gem-transcribe uses
  for `--lang=en,ja` (original plus translation). In voice-scribe an `en` key
  appears when `--translate` is given. **Added 2026-08-08**: whisper's translate
  task is a separate decode rather than an extra output, so getting both the
  original and the translation means running the audio through twice. It roughly
  doubles the time, and because the two passes choose different segment
  boundaries the results are merged by time overlap, which is an approximation.
- `speaker` is a constant `"A"` for every segment when `--diarize` is off; with
  diarization it is `A`/`B`/…, or the supplied names when `--speaker-hint` is used.
- `dropped_segments` is always present for shape compatibility and is normally 0
  (whisper does not produce the malformed-JSON failure mode that motivated it).
- voice-scribe-specific information (engine, quantization, RTF, diarization
  parameters) rides along as **additional fields** in `metadata`, which
  gem-transcribe consumers can ignore.

#### Return strategy over MCP — two-tier

Because the artifact is text, the file-mediated principle used by the image and
audio MCP servers is not applied wholesale.

- **8 KB or less** (default): the text is returned inline — one round trip
- Above the threshold: a file path under `output/`, plus a leading excerpt and
  the total segment count
- The threshold is overridable via `[mcp] inline_threshold` in config and via a
  tool argument

### Configuration

`~/.config/voice-scribe/config.toml` (XDG_CONFIG_HOME-compliant; `~/.config` is
searched on macOS as well; `$VOICE_SCRIBE_CONFIG` overrides explicitly).

```toml
default_model = "kotoba-whisper-v2.2"
models_dir = "~/.local/share/voice-scribe/models"   # relocatable to a larger disk

[transcribe]
format = "json"
vad = true
threads = 0          # 0 = automatic

[diarize]
enabled = false
# threshold = 0.5   # corrected 2026-08-08: a distance, not min/max

[mcp]
inline_threshold = 8192
```

Environment variables (`$VOICE_SCRIBE_MODELS_DIR`, …) take precedence over
config. The config schema and loader follow the org-wide convention; the path is
per-tool.

### External Dependencies

| Dependency | Kind | Notes |
|---|---|---|
| whisper.cpp (ggml) | **statically linked** (CGO) | MIT; Metal backend |
| sherpa-onnx + ONNX Runtime | **statically linked** (CGO, Phase 2a) | Diarization; Apache-2.0 |
| AVFoundation / CoreMedia / AudioToolbox | macOS system frameworks | Audio decoding |
| Metal / MetalKit / Foundation / Accelerate | macOS system frameworks | Inference |
| Hugging Face | network (only during `models pull`) | Ungated; no token required |

**No external process dependency at runtime** (ffmpeg is not required).

## 3. Design Decisions

### Why Go + CGO static linking (whisper.cpp)

image-forge already statically links stable-diffusion.cpp through CGO and carries
it all the way through embedded Metal shaders, static ggml, Developer ID signing
and notarization. whisper.cpp sits on the same ggml, so **the largest build risk
in this project is already solved**. The LDFLAGS framework list, the cmake
`build-engine` procedure, and the practice of excluding `third_party` from
`go test ./...` all transfer directly.

Alternatives considered:

| Option | Why rejected / deferred |
|---|---|
| Apple SpeechAnalyzer / SpeechTranscriber (macOS 26) | The on-device execution, OS-managed models and low power draw are genuine advantages, but the MCP server would have to be written in Swift, for which the org has no precedent; supported locales, timestamp granularity and vocabulary biasing are all at the OS's discretion. **Deferred, not rejected** — it can arrive later as a "lightweight sibling", mirroring instant-translate ↔ quick-translate |
| MLX Whisper / faster-whisper (Python) | A natural sibling for gem-transcribe (also Python), but it exposes a Python runtime and model placement to the user; losing single-binary convenience costs more than it buys |
| parakeet family | Weak on Japanese, which is the primary use case |

### Why AVFoundation decoding rather than ffmpeg

whisper.cpp only accepts raw 16 kHz mono PCM, so a conversion layer is mandatory.
An external ffmpeg dependency would cover every format with zero implementation,
but it **breaks the single-binary distribution strength that image-forge
established**. Since this is a darwin-only release, AVFoundation is always
present and no portability is lost. The cost is an Objective-C bridge, accepted
explicitly as a Phase 1 risk. Containers AVFoundation cannot handle (mkv, webm,
…) are **rejected with an explicit error** that points at pre-converting with
ffmpeg.

### Why diarization ships in v1

gem-transcribe infers speakers in the cloud, so a local counterpart without it
would read as a degraded substitute. whisper.cpp alone cannot do it (tinydiarize
is limited to two English speakers), so sherpa-onnx's pyannote-segmentation-3.0
(a 6.6 MB ONNX file) plus a speaker embedding model is added. The risk of a
second engine is managed by **keeping Phase 1 to the transcription core and
carving diarization out as an independently reviewable Phase 2a**.

### Why a model catalog

Whisper-family models trade off quantization, language fit and speed sharply
enough that forcing the choice onto the user means the tool goes unused. Adopting
image-forge's profile approach (hide the pitfalls behind defaults), both
`kotoba-whisper v2.x` (Japanese-optimised; 6.3× faster than large-v3 at
comparable WER) and `large-v3-turbo` (multilingual) live in the catalog, selected
automatically from the requested language.

### Relationship to existing nlink-jp tools

| Tool | Relationship |
|---|---|
| gem-transcribe | **The cloud counterpart.** Output envelope kept compatible |
| voice-studio-mcp | **The reverse direction** (TTS ↔ STT). Shares workspace and job design |
| meeting-notes | **Downstream.** Consumes voice-scribe JSON directly |
| image-forge | **Skeleton donor** (CGO×ggml×Metal, catalog/store/download, co-resident MCP) |
| data-toolbox-mcp / video-studio-mcp | Donors of the MCP skeleton (jsonrpc / transport / mcpserver / toolerr / job / workspace) |

### Explicitly out of scope

- Real-time / streaming recognition and microphone input (file input only)
- Model training or fine-tuning
- Fallback to a cloud STT service
- Structuring, summarising or extracting action items from minutes (meeting-notes' job)
- Platforms other than darwin/arm64
- Audio editing or post-production (voice-studio / video-studio territory)

## 4. Development Plan

### Phase 1: Core (independently reviewable — complete as a standalone CLI)

1. **Put the build spike first** — add whisper.cpp as a submodule, build it as a
   static library with cmake, link it via CGO, call
   `whisper_print_system_info()` from Go and confirm Metal initialises on real
   hardware. This kills the project's largest risk up front
2. Scaffold (Go module, Makefile, `docs/{en,ja}`, LICENSE (MIT),
   `config.example.toml`, AGENTS.md, `.gitignore` limited to `dist/`)
3. AVFoundation decoder (CGO / Objective-C): any format → 16 kHz mono float32
4. Engine layer: `Open` / `Transcribe` / `Close` as a Session (resident-capable),
   progress callbacks, and **stdout isolation for logs** (see "Known landmines")
5. Port `internal/{store,catalog,download}` from image-forge (registry, profiles,
   resumable downloads)
6. CLI: `transcribe` / `models` / `doctor` / `--version`
7. Five formatters (json / text / md / srt / vtt) over the gem-transcribe-
   compatible envelope
8. config.toml
9. Tests (pure functions first; the engine switches between real and stub via
   build tags, and `go test ./...` excludes `third_party`)

**Phase 1 exit criterion**: an end-to-end run producing Japanese JSON from a real
audio file through the CLI.

### Phase 2: Features

**Phase 2a — Diarization (independently reviewable)**

- CGO static linking of sherpa-onnx + ONNX Runtime
- Add pyannote-segmentation-3.0 and a speaker embedding model to the catalog
  (extend `kind`)
- Merge the diarization timeline with whisper segments (boundary-mismatch
  resolution factored out as a pure, tested function)
- `--speakers` / `--speaker-threshold` / `--speaker-hint` (min/max withdrawn; ADR-0002)
- Verify the ONNX redistribution license and add attribution

**Phase 2b — MCP server (independently reviewable)**

- Port the skeleton (jsonrpc / transport / mcpserver / toolerr / job (single FIFO
  worker) / workspace)
- Four tools, async jobs, `workspace_root` + `os.Root` containment
- Two-tier return (inline / file) and the threshold
- `instructions` on `initialize` and `get_usage` (with a consistency test against
  the embedded `usage.md`)
- End-to-end run against the real engine through a dummy stdio client

**Phase 2c — Finishing**

- `--prompt` (vocabulary biasing), `--translate`, `--offset` / `--duration`, VAD
- Model switching in the resident engine (reload key)

### Phase 3: Release

1. README.md / README.ja.md, the three-layer `docs/{en,ja}`, ADRs recording the
   decisions retroactively
2. `make build-all` → Developer ID signing → zip → notarization (Accepted + staple)
3. Verify the extracted zip for real (`--version` responds; `spctl` reports
   `Notarized Developer ID`)
4. GitHub Release (public repository, LICENSE mandatory)
5. Homebrew tap (set `BREW_MACOS_FLOOR`; `brew audit --cask --online`)
6. Update the util-series submodule pointer
7. Sync all three catalog surfaces: `nlink-jp/.github/profile/README.md`
   (alphabetical), the util-series README, and the web-site catalog EN/JA
8. `check-org.sh` all green
9. Feed reusable knowledge back to `nlink-jp/knowledge`

### Independently reviewable units

- **Phase 1** — verifiable end to end through the CLI, without MCP
- **Phase 2a** (diarization) and **Phase 2b** (MCP) are independent of each
  other; they can run in parallel or in either order

## 5. Required API Scopes / Permissions

**None.**

- No credentials, API keys, OAuth scopes or IAM roles of any kind
- No microphone permission (`NSMicrophoneUsageDescription`) — file input only
- The only network access is HTTPS to Hugging Face during `models pull`. The
  models involved (the kotoba-whisper family, ggerganov/whisper.cpp, sherpa-onnx
  releases) are ungated and need no token
- Inference itself does not use the network, but the documentation will not claim
  "fully offline" — neither model acquisition nor OS behaviour can be guaranteed

## 6. Series Placement

**Series: util-series**

Rationale:

- It is a pipe-friendly transformation CLI (audio → structured text), which is
  exactly the util-series definition
- Its siblings — gem-transcribe, voice-studio-mcp, video-studio-mcp, image-forge —
  all live in util-series and share its naming, skeleton and release procedure
- It is not an interactive client for an external service (cli-series), not Slack
  automation (chatops-series), and not a security tool (cybersecurity-series)
- It is not experimental; it has a defined deliverable and defined users, so
  lab-series does not apply

## 7. External Platform Constraints

### Decoding

- **Containers AVFoundation cannot handle (mkv, webm, …) are unsupported.** They
  are rejected with an explicit error pointing at pre-conversion with ffmpeg

### Whisper and models

- Silence and music-only stretches provoke **hallucinated repetition of the same
  phrase**. A Silero VAD gate (supported in whisper.cpp) is on by default to
  mitigate this
- Whisper works in 30-second chunks. Long recordings are handled by sequential
  long-form transcription (already implemented in whisper.cpp), but sentences
  split across chunk boundaries are unavoidable
- `large-v3-turbo` at q5 is about 550 MB. ~~Metal has a cold-load cost, which is
  what makes the resident (MCP / serve) mode valuable~~ — **corrected 2026-08-08**:
  the 8.8-second Metal library init is one-time shader compilation cached by the
  OS, and warm runs take 0.011 s. The case for a resident engine rests on model
  loading, not Metal. See ADR-0001
- Hugging Face issues 302 redirects to Xet storage (a known issue from
  image-forge: a range GET returns a manifest while a full GET returns real bytes)

### Build and distribution

- **CGO × Metal cannot be cross-compiled → a single darwin/arm64 release**
  (the same constraint as image-forge)
- Static linking of ONNX Runtime -> **measured 2026-08-08**: the binary goes from
  10.2 MB to 29.5 MB (+19.3 MB). The only added dynamic dependencies are system
  frameworks; no third-party dylib. See ADR-0002, which also records how
  upstream's pinned hash was found broken
- Redistribution licence of the pyannote-segmentation-3.0 ONNX export ->
  **confirmed 2026-08-08**: MIT (c) 2022 CNRS, with the original LICENSE shipped
  inside the ONNX package. The 3D-Speaker campplus embedding model is
  Apache-2.0. Attribution added to the README

### Known landmines (pre-empted)

The **"native library output leaks to stdout and corrupts JSON-RPC"** trap hit in
image-forge v0.9.0 recurs here in the same shape. It is defended in two layers:

1. **At the source** — install log callbacks via `whisper_log_set` /
   `ggml_log_set` and disable `print_progress` / `print_realtime`
2. **At the MCP layer** — dup the real stdout for transport use only and point
   fd 1 at stderr (defense in depth)

And because **"invisible in the console" ≠ "not written to stdout"**, verify by
measuring with `1>file 2>file` separated.

---

## Discussion Log

**Origin (2026-08-08)** — Started from the request: "I want to offer a
transcription MCP server for agents whose model is poor at audio; external APIs
cost money, so I would rather run a local model."

**Comparing approaches** — whisper.cpp (CGO static linking), Apple
SpeechAnalyzer (macOS 26), MLX Whisper / faster-whisper (Python) and the parakeet
family were compared. whisper.cpp won because the CGO static-linking path proven
by image-forge with stable-diffusion.cpp transfers wholesale, meaning the
project's largest build risk is already solved. Apple SpeechAnalyzer's advantages
were acknowledged and it was deferred rather than rejected, leaving room for a
later "lightweight sibling" in the shape of instant-translate ↔ quick-translate.

**Model research** — Confirmed that kotoba-whisper v2.x is distributed officially
in GGML form (`kotoba-tech/kotoba-whisper-v2.0-ggml` and others) at 6.3× the speed
of large-v3 with comparable WER. That margin in the primary Japanese use case is
what ruled out the single-model option in favour of a catalog plus profiles.

**Decisions taken (by the user)**

| Question | Decision | Notes |
|---|---|---|
| Tool name | `voice-scribe` | `voice-scribe-mcp` was chosen first, but the `-mcp` suffix was dropped once the co-resident CLI was decided. This keeps the org-wide distinction consistent with image-forge: tools carrying both a CLI and an MCP server do not take the suffix |
| Language scope | Multilingual, Japanese-optimised by default | Deliberately different from voice-studio-mcp's Japanese-only stance, to stay consistent with gem-transcribe's multilingual output |
| Diarization | **Ships in v1** | The user decided to adopt it after the two-engine risk was raised. The risk is managed by carving it out as an independently reviewable Phase 2a |
| Binary shape | CLI with a co-resident `mcp` subcommand | End-to-end verification and troubleshooting work without an MCP client |
| Audio decoding | **AVFoundation via CGO** | An external ffmpeg dependency was rejected in favour of a self-contained single binary. Nothing portable is lost in a darwin-only release. The Objective-C bridge cost is accepted as a Phase 1 risk |
| MCP return strategy | Two-tier (inline below a threshold) | Because the artifact is text, the file-mediated principle of the image and audio MCP servers is not applied uniformly |

**Settling the output schema** — Inspecting gem-transcribe's `models.py` directly
revealed that `Segment.text` is a **language-code → text map** (holding the
original and its translation in the same segment for `--lang=en,ja`). Whisper's
translate task (to English) fits that shape exactly, so compatibility was raised
from a loose "aim to align" to **full envelope compatibility**. Downstream
consumers such as meeting-notes can therefore parse cloud and local output with
one parser.

**Considered and not adopted**

- A single model (`large-v3-turbo` only), dropping the catalog machinery —
  rejected because it throws away the 6× Japanese speed margin
- Exposing `models pull` as an MCP tool — rejected so that an agent cannot start
  a multi-gigabyte download on its own judgement; it stays CLI-only
- Always returning file-mediated results (contract identical to voice-studio /
  video-studio / image-forge) — maximally consistent, but rejected because it
  forces a file-read round trip on the agent even for short audio
