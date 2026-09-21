# ADR-0002: Do speaker diarization with sherpa-onnx

- Status: Accepted
- Date: 2026-08-08

## Context

gem-transcribe infers speakers on the cloud side (Gemini). A local counterpart without it
would read as a "degraded substitute", so at the RFP stage the user decided to **include
diarization in v1**.

whisper.cpp alone cannot do it. The bundled tinydiarize is limited to two English speakers
and is no use for Japanese meeting records. A second runtime therefore has to be carried.

The RFP named this as a Phase 2a risk and left homework: "static linking of ONNX Runtime is
less mature than whisper.cpp, so measure the effect on binary size and on signing when the
work starts."

## Decision

**Statically link sherpa-onnx (k2-fsa) through CGO.** Two models: pyannote-segmentation-3.0
(detecting speaker boundaries) and 3D-Speaker campplus (speaker embeddings).

The build tag is **`cgo_sherpa`, raised separately** from whisper's `cgo_whisper`. The
reason is the "upstream pin was broken" case below, which showed it has to stay possible to
build a transcription binary in an environment where the ONNX Runtime archive cannot be
fetched.

### Pin the submodule to a release tag

**Following master did not work.** The hash that master (`xcframework-14-g634265c9`) pinned
for onnxruntime 1.27.1 does not match the asset actually published. The zip itself is sound
(it passes `unzip -t`). At release tag **v1.13.4** (onnxruntime 1.27.0) the hash matched.

**Mechanism**: `xcframework` is an upstream **rolling tag**, and assets are re-uploaded to
the same tag from time to time (on the GitHub release page the asset dates are mixed — only
some of them are new). The commit message, too, was
`Set onnxruntime version to 1.27.1 in SPM`: the state was mid-move of the onnxruntime
version. The cause is that **we depended on a mutable release**, which is a problem with how
we chose the pin rather than an upstream defect.

The submodule is therefore **pinned to an immutable version tag** (currently the tag commit
of v1.13.4 itself). A rolling tag, or an arbitrary commit on master, is not reproducible,
because a third party can swap out the contents. **When updating the submodule, always
choose a tag in `vX.Y.Z` form.**

### The Makefile fetches ONNX Runtime first

sherpa-onnx's cmake goes out for onnxruntime's static archive during configure, but
**cmake's built-in downloader fails in this environment** (curl reaches the same URL with a
200). sherpa-onnx's cmake has a fallback that looks for a local file, so the Makefile
fetches it with curl, puts it in the build tree, and lets that be picked up.

The URL and the hash are not copied into the Makefile but **extracted from the submodule's
cmake file**. This is to make them follow a submodule update automatically, and **the hash
is verified after the fetch** (given that an upstream pin has gone bad once, there is no
reason to skip verification).

> **Makefile trap**: writing a regular expression that contains parentheses inside
> `$(shell ...)` breaks make's parser (`unterminated call to function 'shell'`). Write the
> extraction in a form that uses no parentheses.

## Consequences

### Measured (2026-08-08, M2 Max / macOS 26)

| Item | Result |
|---|---|
| Binary | 10.2 MB → **29.5 MB** (+19.3 MB, +190%) |
| What the increase consists of | Almost entirely ONNX Runtime's static libraries |
| Dynamic dependencies added | Security.framework / AVFAudio — **both ship with the OS**; zero third-party dylibs |
| Time diarization takes | About 2 s for 17 s of audio (CPU; Metal not used) |

**Signing and notarization are not expected to be affected** (no third-party dylib was
added). Confirm it on the real thing at release time all the same.

### Models and licenses (the RFP's homework)

| Model | Distributor | License |
|---|---|---|
| pyannote-segmentation-3.0 (ONNX) | csukuangfj/sherpa-onnx-pyannote-segmentation-3-0 | **MIT © 2022 CNRS** (the ONNX package bundles the original LICENSE) |
| 3D-Speaker campplus | csukuangfj/speaker-embedding-models | **Apache-2.0** (derived from modelscope/3D-Speaker) |
| sherpa-onnx itself | k2-fsa/sherpa-onnx | Apache-2.0 |

**Upstream pyannote/segmentation-3.0 is gated on Hugging Face, but sherpa-onnx's exported
version is ungated**. The same shape as what image-forge taught: "for a gated repo, look
for an ungated mirror."

### sherpa's config must not be passed as zero values

The C API has no defaults getter equivalent to whisper's `whisper_full_default_params()`.
Go's `var cfg C.Sherpa...Config` is zero-initialized, so **it ends up being passed with
threshold = 0 / min_duration = 0, and every turn collapses into one speaker**. The first
implementation did exactly this, and the symptom was "diarization works, but everyone is
A". The defaults (threshold 0.5 / min_duration_on 0.3 / min_duration_off 0.5) are written
out explicitly.

### `--min-speakers` / `--max-speakers` are not implemented (a deviation from the RFP)

The RFP listed these two flags, but **sherpa-onnx's clusterer has no such notion**.
`FastClusteringConfig` takes either `num_clusters` (an exact count) or `threshold` (a
distance), with nothing in between (the branch in `fast-clustering.cc` was checked in
place).

Better to expose a knob that exists than a flag for a feature that does not. It is
therefore **replaced with `--speaker-threshold`**. `[diarize] min_speakers` /
`max_speakers` were replaced with `threshold` as well.

### Speaker labels follow order of appearance, not cluster number

The speaker sherpa returns is a cluster number and carries no meaning (number 3 is not "the
third person"). A reader expects "A is the person who spoke first", so **they are
renumbered in time order**. The names from `--speaker-hint` are assigned in that order too.

### A pitfall hit during verification (on the fixture side)

The first test audio was made with `say -v Kyoko` and `say -v Otoya`, but **Otoya was not
installed and `say` silently fell back to the default voice**, so all four clips were the
same speaker. Diarization reporting "one speaker" was **correct**.

Rebuilt with two voices that do exist (Kyoko / Rocko), it detected two speakers
automatically and reproduced the speaker changes correctly across all five turns. **Verify
the fixture before suspecting the code.**

### Costs accepted

- The binary becomes nearly three times as large. This is imposed on users who do not use
  diarization too
- The build needs an ONNX Runtime download (18 MB)
- Diarization runs on the CPU (ONNX Runtime's CoreML EP is not used)
