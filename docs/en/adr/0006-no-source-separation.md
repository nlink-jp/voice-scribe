# ADR-0006: Do not put source separation in pre-processing

- Status: Accepted
- Date: 2026-08-09

## Context

After ADR-0005 concluded that "proper nouns cannot be fixed at decode time", a direction
came up: **if pre-processing extracted the voice alone, perhaps the problems on the
acoustic side would shrink**. The real material is a drama CD with background music playing
throughout, and that music does known harm — diarization split 39 minutes into 93 speakers
(v0.1.3's warning was born from this material).

The division "acoustic problems in pre-processing, context problems on the agent side" is
coherent. The question is **what each of them actually fixes**, and that cannot be known
without measuring.

## Feasibility

**No third runtime is needed.** sherpa-onnx v1.13.4, already statically linked, has the C
API for it:

| Capability | Models |
|------|--------|
| Source separation (vocals / accompaniment) | Spleeter 2stems, UVR MDX-Net |
| Noise reduction | GTCRN, DPDFNet |

It goes in with one CGO bridge of the same shape as diarization. **There is no technical
obstacle.**

## Measured

2026-08-09, M2 Max. Spleeter 2stems (fp16). The same material, default thresholds.

**The cost is negligible.** The RTF of separation is **0.038** (one fifth of
transcription's 0.186).

### What it did fix — text dropped from the transcript

In a 50-second stretch, **a whole sentence that the original audio had swallowed into a
repetition loop was recovered**. On the original side the same phrase was repeated twice
and ate into the next utterance; after separation that repetition decreased and the
sentence that belonged there appeared.

### What it did not fix (1) — proper nouns

The surname in question is **unchanged** before and after separation (the same error still).
This reinforces ADR-0005's conclusion: proper nouns are not a problem on the acoustic side.

### What it did not fix (2) — speaker over-splitting

**This was the real hope, and it fell short.** 5 minutes, default threshold 0.5:

| | Segments | Speakers | Spoke only once | Over-splitting warning |
|---|---|---|---|---|
| Original audio | 81 | **18** | 8 | Shown |
| After vocal extraction | 66 | **14** | 5 | **Shown** |

There is an improvement (18 → 14). But the actual cast is a handful of people, and 14 is
still far off the mark. The warning is still shown, too. **Removing the background music
left the over-splitting in place** — why it remains is not known. Nothing more can be said
than that what the embedding is picking up is not the background music alone.

Threshold calibration (raising `--speaker-threshold`, pinning with `--speakers`) therefore
remains the main remedy, and separation is no substitute for it.

## Decision

**Neither source separation nor noise reduction goes in.**

It is technically possible, the cost is cheap, and it does actually help with text dropped
from the transcript. The reason it still does not go in is **the license**:

| Model | Code | **Weights** |
|--------|--------|----------|
| Spleeter | MIT | **Not stated.** Only a note saying "if you use it on copyrighted works, obtain the rights holder's permission" |
| UVR MDX-Net | MIT (the GUI itself) | **Not stated.** What it was converted from is a third party's aggregation repository as well |

Under the organization's rules, **the license of weights is derived from the upstream
distributor** (`nlink-jp/knowledge`). **"Not stated" is not a license.** Since it cannot go
in the default catalog, it cannot be lined up in the list of `voice-scribe models pull`
either.

It is also just after ADR-0004 moved the default model back to the author's distribution.
Adding weights whose license is unknown to the catalog right after that is not coherent.

Making it an opt-in flag and having users supply the model themselves was considered, but
**not adopted**. Since the main motivation — improving diarization — was measured and fell
short, the remaining gain comes down to the single point of "less text dropped from the
transcript", which does not justify **adding a stereo path** (below).

## Consequences

- **The decoder can stay mono/16k.** Spleeter **requires stereo** (pass it mono and it dies
  with `Invalid channel 1`), so putting it in would have required a stereo path in
  `internal/audio`. That change is not needed
- For diarization on material with background music, **threshold calibration remains the
  only remedy**. v0.1.3's over-splitting warning keeps its role of telling the user so
- **Conditions for reconsideration**: when a source separation model whose weight license
  is stated appears in a format sherpa-onnx supports (Spleeter / UVR). Even then, measure
  the effect on diarization again — this time's 18 → 14 was a number on the "not a reason
  to put it in" side
- The upstream CLI used for the measurement (`sherpa-onnx-offline-source-separation`) can be
  built straight from the submodule. It is the same move as ADR-0005's `whisper-cli`, and
  it works as **the standard way to evaluate an unused feature of a runtime already linked
  in**
