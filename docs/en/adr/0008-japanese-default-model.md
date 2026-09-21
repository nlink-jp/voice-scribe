# ADR-0008: Make large-v3-turbo the default model for Japanese

- Status: Accepted
- Date: 2026-08-12

## Context

The default model for Japanese has been `kotoba-whisper-v2.0` since the first version,
on the grounds that "it is a Japanese-specialized derivative model". It was not chosen
by measurement.

While investigating a successor engine in ADR-0007, a by-product result came out:
**`large-v3-turbo` may be better for Japanese**. `large-v3-turbo` is
**already in the catalog** (`internal/catalog/catalog.go`), so this is a question that
can be decided with the current configuration as it is, unrelated to the engine
migration.

kotoba-whisper's model card shows an advantage over whisper-**large-v3**
(CER 11.6 against 14.9 on ReazonSpeech). But large-v3-turbo is a different model from
large-v3, and that comparison says nothing about turbo.

## Measurements

M2 Max, 2026-08-12. **Everything was measured in voice-scribe proper** (whisper.cpp,
q5_0) — a default of our own cannot be decided on another runtime's numbers.
The harness is `spike/transcribe-cpp/eval/`.

### Accuracy with a reference (CER, 100 utterances each, fixed seed, identical WAV)

| Corpus | Nature | kotoba-whisper-v2.0 | large-v3-turbo |
|---|---|---:|---:|
| Common Voice 8.0 ja | read speech, multi-speaker crowd | **9.41%** | 9.57% |
| JSUT basic5000 | read speech, studio | 7.15% | **6.29%** |
| ReazonSpeech test | broadcast audio | 9.08% | **7.82%** |

**ReazonSpeech is the in-domain set kotoba-whisper used for training.** Losing there is
what carries weight. The 0.16 points on CV8 cannot be called a difference at n=100.

### Behaviour on real material (39 minutes, background music throughout, the same source as ADR-0006)

There is no reference text, so CER cannot be measured. **Structural soundness** was
measured instead — the breakdown ADR-0006 recorded on this source is precisely the kind
that shows up as structure (decoding falling into a repetition loop, audio picked up by
no segment at all).

Since it became clear during measurement that **the same settings give different
results from run to run**, the no-VAD case is run **three times each** and shown as a
range (see "Reproducibility" below). The VAD cases are one run each.

| Run | Segments | Characters | Longest loop run | Wall clock |
|---|---:|---:|---:|---:|
| kotoba-whisper-v2.0 ×3 | 640–677 | 8,009–8,163 | **2–3** | 86 s |
| large-v3-turbo ×3 | 784–829 | 11,718–11,929 | **19–48** | 173 s |
| kotoba-whisper-v2.0 `--vad` | 421 | 5,543 | 2 (0 loops) | 68 s |
| large-v3-turbo `--vad` | 787 | 8,041 | 2 (0 loops) | 80 s |

Three things can be read from this.

**1. Without VAD, turbo falls into repetition loops.** It fell into them all three
times (longest runs 19 / 45 / 48; the audio swallowed was 49–82 seconds, 2.6–4.1% of
the output). kotoba's longest run was 2–3 all three times, with nothing that deserves
to be called a loop. **This is a defect that appears in the default configuration.**

**2. Adding VAD makes the loops disappear for both.** That is, the loops are
hallucination against regions that are not speech (music, in this source), not
misrecognition of speech.

**3. Under the condition that removes the loops, turbo emits 1.45× the characters
kotoba does** (8,041 against 5,543). The ratio agrees with the no-VAD condition
(11,718–11,929 against 8,009–8,163, i.e. 1.44–1.49×), so turbo's larger output volume
cannot be explained as padding from loops. The ranges over the three runs do not
overlap.

### Reproducibility — the same input and the same settings give different results

This had not been noticed until it was measured. **Three runs with the same binary, the
same model, the same audio and the same settings transcribe differently every time.**
The swing in character count is small (kotoba ±1%, turbo ±1%), but **the loop structure
swings widely** (turbo's longest run was 19 / 45 / 48). The reasonable view is that the
tiny floating-point differences produced by whisper.cpp's multi-threaded reduction
flip the decision at the branch points of repetition.

**So a metric for long audio must not be stated from a single run.** This section's
conclusion holds because the ranges of the two models being compared do not overlap
(character count 8.0k–8.2k against 11.7k–11.9k, longest run 2–3 against 19–48).

Since there is no reference, **there is no guarantee that the 1.45× is correct text.**
But taking together that turbo is at least equal on all three corpora that have a
reference, and that ADR-0006 records "kotoba dropping a whole sentence" on this source,
the natural reading is that kotoba is the one failing to pick this source up.

### VAD cannot be the default

`--vad` **cut the character count by 31%** (kotoba 8,114 → 5,543, turbo
11,733 → 8,041). Removing the loops alone does not explain a reduction of that size
(the loops are 0.1% and 4.1% of the output). The reasonable view is that silero-vad
judges speech riding on top of background music to be non-speech, so
**VAD is a tool against a known noise source, not a preprocessing step that may be
made the default.**

## Decision

**Change the default model for Japanese to `large-v3-turbo`.** Grounds:

- On the three corpora with a reference, two wins and one draw (no loss). One of them
  is the rival model's in-domain set
- On real material it emits 1.45× the text, and the ratio is preserved under the
  loop-free condition too
- Speed is near-even when VAD is used, 80 s against 68 s (without VAD, 173 s against
  86 s)
- Size is 547 MB against 513 MB and the license MIT against Apache-2.0; neither is a
  problem

`kotoba-whisper-v2.0` stays in the catalog. It is kept selectable with `--model`;
it is only taken out of the default. **A default written in a user's configuration file
is not changed** — an explicitly chosen value is not followed along (the principle of
`feedback_config_redirect_vs_persisted_paths`).

## What goes in at the same time

**A repetition-loop warning.** turbo's 48 consecutive repetitions are a breakdown of
the same nature as speaker over-splitting — **the output is perfectly well-formed, the
JSON is valid, and no error is raised at all.**
Do for consecutive identical segments what `internal/transcript/diagnose.go` does for
the speaker count. Since the default is being changed to a model more prone to loops,
this is something that should go in the same commit.

## Leftovers

- Whether the 1.45× difference really is "recovery of what was missed" cannot be
  settled as long as this source has no reference text. Settling it would take a few
  minutes of hand transcription.
- The breakdown of the 31% VAD dropped (hallucination against real speech) has not been
  measured. A comparison against putting ADR-0006's source separation (Spleeter) in
  front of it is the sound next move.
- ReazonSpeech / CV8 / JSUT are all collections of short utterances, so breakdowns
  originating in the chunk boundaries of long audio are not measured.
