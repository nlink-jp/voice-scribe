# ADR-0007: Migrating to transcribe.cpp is deferred (it passed technically; there is no release)

- Status: Proposed
- Date: 2026-08-12

## Context

voice-scribe statically links two native runtimes — whisper.cpp for transcription
(ADR-0001), and sherpa-onnx / ONNX Runtime for speaker diarization (ADR-0002).
The price of the second one is exactly as enumerated in ADR-0002: the binary grew from
10.2 MB to 29.5 MB, `go test ./...` stopped working, and we were left verifying
hashes in the Makefile to get ahead of upstream's broken pin.

[transcribe.cpp](https://github.com/handy-computer/transcribe.cpp) (MIT, by the
author of Handy, started 2026-04) is a C library that puts 16 or more ASR families on
top of ggml, and it **has speaker diarization as a first-class citizen of the same C API**.
If that holds, the second runtime disappears.

Four things had to be confirmed. That there are no Go bindings, whether diarization
works for Japanese, whether the current Japanese model can be brought over, and
**whether the other Japanese models transcribe.cpp carries are better than
kotoba-whisper**.

## Measurements

M2 Max, 2026-08-12. transcribe.cpp at **`856d7c1` on main** (see below).
The harness is `spike/transcribe-cpp/` (reproducible with `make all`).
The fixture is five alternating turns of `say -v Kyoko` / `say -v Rocko`, 17.7 seconds,
16 kHz mono. Following the lesson of ADR-0002, that the two voices are not the same
waveform was verified first.

### 1. Can the C API be called from Go? → It passes

The official bindings are Python / TypeScript / Rust / Swift only; there is no Go.
A cgo harness of about 200 lines ran both transcription and diarization.

| Observation | Value |
|------|-----|
| Build (cmake, Metal embedded) | about 2 minutes |
| Static libraries to link | **5** (the current build hand-writes whisper 7 + sherpa 10, **17** in total) |
| Harness binary | 6.8 MB (the current voice-scribe is 31.0 MB) |
| Dynamic dependencies | OS-provided only (Accelerate / Foundation / Metal / MetalKit / libc++) |
| stdout / stderr | JSON only / **0 bytes** |

Three things work as design:

- **`build/install/lib/transcribe-link.json`** enumerates the libraries,
  frameworks and system libraries to link against. cgo's LDFLAGS can be generated
  rather than hand-written. The current 17 lines are the kind of code that breaks
  whenever upstream's layout changes; this removes that kind.
- **Passing a no-op to `transcribe_log_set` is enough to stop the native side's logging.**
  stderr at 0 bytes was achieved without building our own fd isolation (of
  ADR-0003's two layers of defence, upstream guarantees the first).
- **Capability queries** (`transcribe_model_get_capabilities` /
  `transcribe_model_supports`) let per-model differences be asked about.
  No guessing from a file name or a setting.

**One trap was stepped on**: Sortformer names exactly one language (`en`) and yet
rejects every language hint with `UNSUPPORTED_LANGUAGE`. **Judge a capability by
"membership", not by "count"** — making `n_languages > 0` the condition steps on this
every time.

### 2. Can Sortformer diarize Japanese? → It can

`diar_streaming_sortformer_4spk-v2.1` Q8_0 (139 MB).
With no language hint it **separated all five turns of the two speakers correctly**.

| Turn | Reference | Detected |
|--------|------|------|
| A | 0 – 3935 ms | spk1 0 – 4000 |
| B | 3935 – 6654 | spk2 4000 – 6080 |
| A | 6654 – 10329 | spk1 6640 – 10400 |
| B | 10329 – 15096 | spk2 10320 – 11520 + 12080 – 14480 (split at a pause inside the sentence) |
| A | 15096 – 17733 | spk1 15120 – 17760 |

The largest boundary error is 71 ms. Speaker numbers follow order of first appearance,
with no mix-up. Model load took 9.4 seconds the first time and **104 ms** on every
later one (the Metal shader cache behaviour observed in ADR-0001 reproduced exactly).
Inference took 369 ms for 17.7 seconds of audio (RTF 0.021).

The price is a **hard ceiling of 4 speakers**. The current clustering approach has no
ceiling.

**Comparison on real material (39 minutes, background music throughout, the same
source as ADR-0006):**

| | Speakers | Share of speech time held by the top 4 | Speakers under 2 seconds | Wall clock |
|---|---:|---:|---:|---:|
| Current (sherpa clustering) | **102** | 25.7% | 16 | **452 s** |
| Sortformer | 4 | 100% | 0 | **28 s** |

**Neither is correct on this source**, but they are wrong in opposite directions, and
the cost differs. Sortformer returns exactly 4, its architectural ceiling, so it is
certainly collapsing distinct people together. The current build splits into 102,
where the top 4 hold only a quarter of the speech time — the same breakdown as seeing
93 speakers in ADR-0006. **Four collapsed speakers is a readable result;
102 fragments is not.** In voice-scribe's actual uses — meetings, IR calls,
interviews (2 to 6 people) — the existence of a ceiling is unlikely to be the defect.

The speed difference is not negligible either: **28 s against 452 s (16×)** for
39 minutes.

### 3. Can kotoba-whisper-v2.0 be converted to GGUF? → It can (with one added line)

`scripts/convert-whisper.py` read the checkpoint correctly
(encoder 32 layers / decoder 2 layers / mel 128 / vocab 51866 — the distil
configuration). The only thing it tripped on was the display-name allowlist, which
already carries a community fine-tune (MediaTek's breeze-asr-25). **One added line
makes it pass**, and it can go upstream as it is
(`spike/transcribe-cpp/kotoba-variant.patch`).

BF16 1.45 GB → Q5_0 **550 MB** (the current ggml q5_0 is 537 MB).

Transcription compared on the same fixture:

| | Output |
|---|---|
| Current voice-scribe (whisper.cpp + kotoba-whisper-v2.2 q5_0) | 4 turns. **Dropped the fifth turn entirely** |
| transcribe.cpp (self-converted kotoba-whisper-v2.0 Q5_0) | **All five turns**. RTF 0.032 |

Both of them were missing 「おはようございます」 and 「ログの解析結果もお願いします」.
**So the dropout is not a defect of the port but kotoba-whisper's own behaviour**;
the ported version in fact picks up one turn more than the current build.

**One regression with Q5_0**: the end timestamp of the final segment is 13.54 seconds
(correctly 17.54 seconds; BF16 gets it right). The text is identical to BF16.
It looks like the same kind of phenomenon as the one for which upstream withdrew
K quantization for Sortformer, where quantization shifts timing decisions.
Do not forget that a self-converted model sits **outside** upstream's
"numerically verified against the reference implementation".

### 4. Japanese accuracy — 6 models × 2 corpora

The evaluation sets chosen were **the ones kotoba-whisper's own model card uses**
(`japanese-asr/ja_asr.common_voice_8_0` and `ja_asr.jsut_basic5000`, both
ungated). Being able to line the results up against the published figures makes a
mistake on the harness side visible.
100 utterances each (CV8 8.3 min / JSUT 7.0 min), fixed seed, **every model reads the
same WAV**.

The metric is **CER** (because Japanese has no word boundaries). Normalization is
NFKC, whitespace removal and punctuation removal only. No kana folding and no number
normalization — a normalization that rewrites content unfairly favours the models that
share that convention.

Calibration: kotoba-whisper-v2.0's published CER is CV8 **9.2** / JSUT **8.4**. This
harness gives **8.88 / 7.24** for the same model. That is within what a 100-utterance
subset and a difference in normalization explain, so the harness can be trusted.

| Model (Q8_0) | Size | **Timestamps** | CV8 CER | JSUT CER | CV8 speed |
|---|---:|---|---:|---:|---:|
| Fun-ASR MLT Nano 2512 | 891 MB | **none** | **4.78%** | **5.04%** | 18.5× |
| whisper-large-v3-turbo (official) | 886 MB | segment | 9.20% | 5.94% | 13.5× |
| SenseVoice small | **253 MB** | **none** | 9.30% | 7.19% | **51×** |
| kotoba-whisper-v2.0 (self-converted) | 830 MB | segment | 8.88% | 7.24% | 9.4× |
| Qwen3-ASR 1.7B | 2185 MB | **none** | 9.09% | 7.67% | 27.5× |
| Qwen3-ASR 0.6B | 850 MB | **none** | 10.95% | 9.65% | 20.6× |

(Speed is relative to real time, computed from wall-clock time including model load.
kotoba at Q5_0 was measured too, and its CER was 8.88%, identical to Q8_0.)

### 5. Timestamps eliminate the options

**The column to read before CER was "Timestamps".**

| Capability | whisper family | SenseVoice | Fun-ASR | Qwen3-ASR | Sortformer |
|---|---|---|---|---|---|
| Segment times | **segment** | none | none | none | (speaker spans only) |
| Length handled at once | unlimited (splits internally) | **30 s** | 40 min | 87 min | unlimited |
| License | apache-2.0 | FunASR Model License | apache-2.0 | apache-2.0 | NVIDIA Open Model License |

voice-scribe's output envelope assumes times — the gem-transcribe-compatible
`segments[]`, SRT / VTT, and merging with the diarization result. **An engine that
cannot emit times cannot satisfy the current envelope, however accurate it is.**

So **Fun-ASR's 4.78% (roughly half of kotoba's error) is unusable as it stands.**
SenseVoice's "kotoba-equivalent at 253 MB and 5× the speed" is not a replacement
candidate for the same reason. If times are needed for Japanese, it is
**the whisper family or nothing**.

This constraint suggests one configuration: **Sortformer has times.**
Cutting on speaker spans and handing the pieces to Fun-ASR could make
"times, high accuracy, high speed" hold at once.
But it is a rebuild of the pipeline, and the times disappear when diarization is
turned off. This ADR leaves it **unverified**.

### 6. Self-conversion may be unnecessary

The official **whisper-large-v3-turbo** GGUF scored **5.94%** on JSUT, beating the
self-converted kotoba-whisper-v2.0's 7.24% (on CV8 they are near-even, 9.20% against
8.88%). It is faster too, 13.5× against 9.4×.

That changes the shape of the migration. **If an official artifact that upstream has
put through numerical verification and WER tests can be used as it is, both the patch
to the conversion script and the unease about a self-converted model sitting outside
upstream's verification disappear together.**

## The one thing holding the decision back

**Diarization has not been released yet.**

The latest tag is **v0.1.3 (2026-07-12), and there is no trace of diarization in it** —
the three letters `diar` never appear once in a header, and neither
`convert-sortformer.py` nor `docs/models/diar_*` exists. Everything measured above is
against **a commit on main** (`856d7c1`, 2026-08-07).

This collides head-on with the discipline we set for ourselves in ADR-0002. After the
sherpa-onnx `xcframework` turned out to be a rolling tag, we decided that
**submodules are pinned to immutable tags of the form `vX.Y.Z`**. Pinning to a commit
SHA is immutable, but it means statically linking "code upstream does not guarantee as
a release" into the product. Between v0.1.3 and main, in one month, both the ABI and
the model set moved, and that range is exactly the range within which a pre-release
implementation moves.

## Decision

**The migration is deferred.** The current whisper.cpp + sherpa-onnx configuration
is kept.

There is exactly one trigger for reconsideration: **a tag being cut that includes
diarization.**

Measurement made the gain from collapsing to one runtime larger rather than smaller —
17 static libraries become 5, the link specification goes from hand-written to
machine-generated, and ONNX Runtime no longer has to be carried for diarization.
That is precisely why the conclusion is **wait for the tag**. There is no reason to do
a replacement of this size on top of code upstream does not guarantee as a release.

By the measurements in this ADR, the migration once a tag exists has settled into this
shape:

1. **Transcription is the whisper family**, because it is the only one that can emit
   times.
2. **The first candidate model is the official GGUF (whisper-large-v3-turbo)**. It is
   better than the self-converted kotoba-whisper on JSUT, faster, and inside upstream's
   verification. Whether kotoba-whisper stays is to be decided by re-measuring on
   material closer to real use.
3. **Diarization is Sortformer**. Whether to accept the 4-speaker ceiling is the only
   judgement here.
4. **SenseVoice / Fun-ASR / Qwen3-ASR drop out of the candidates** (no times).
   Fun-ASR's error is roughly half of kotoba's, so a configuration that cuts on
   Sortformer's spans and hands them over is worth evaluating separately.

## What can be taken up during the deferral (independent of the migration)

1. **Machine-generated link specification** — 17 hand-written lines of
   `#cgo LDFLAGS` do not reveal that they are broken until an upstream layout change
   breaks them. If the same thing as transcribe.cpp's link manifest can be generated
   from the whisper.cpp / sherpa-onnx builds, the same hazard disappears.
2. **Capability queries** (judging by membership) — move the places that guess
   per-model support from a file name or a setting towards asking the model itself.
3. **An "is the output correct" layer for the catalog** — the current SHA256 only goes
   as far as "are these the correct bytes" (ADR-0004). That upstream publishes
   numerical comparison against the reference implementation and WER for every model
   shows that there is a layer that can sit on top of it.

## Leftovers

- ~~Re-measurement on material closer to real use~~ → **done in ADR-0008**.
  Re-measured over three corpora including ReazonSpeech (broadcast audio) and the
  39-minute real source, which led to a proposal to change the default model.
  Note that ADR-0007's CER figures were measured with the ported build on the
  transcribe.cpp side; the values re-measured in voice-scribe proper (whisper.cpp)
  are in ADR-0008.
- **The Sortformer × Fun-ASR configuration** (cut on speaker spans and hand them to a
  high-accuracy model) is unverified. It is a design that moves the origin of times to
  the diarization side, and if it holds it halves the error.
- **Streaming** (`transcribe_stream_*`) has not been touched.
- **Licensing**: the library itself is MIT, but the models are not uniform —
  Sortformer is **NVIDIA Open Model License**, SenseVoice is
  **FunASR Model Open Source License**, and the whisper family and Fun-ASR are
  apache-2.0. Check the attribution obligation for each model put in the catalog.
  Note that NVIDIA's model card states outright that "quality may degrade for
  non-English", but no degradation was observed on this ADR's two-speaker Japanese
  fixture (this is a small sample of 17.7 seconds and 5 turns).
