# ADR-0004: Supply chain verification policy

- Status: Accepted
- Date: 2026-08-08

## Context

After the v0.1.0 release, prompted by the remark "I want the submodules pinned to release
tags, because supply chain attacks are frightening", the third-party artifacts fetched at
build time and at run time were inventoried.

To give the conclusion first: **the place the pinning was aimed at was not the problem;
the hole was somewhere else.**

## Inventory results

| What is fetched | When | Verification (as of v0.1.0) |
|---|---|---|
| whisper.cpp | Build time (submodule) | git SHA — **content-addressed, so it cannot be tampered with** |
| sherpa-onnx | Build time (submodule) | git SHA + release tag (ADR-0002) |
| ONNX Runtime prebuilt zip | Build time (`make deps`) | SHA256 verified (ADR-0002) |
| **Model files (8 of them)** | **Run time (`models pull`)** | **Size only** ← **the hole** |

## Decision

### whisper.cpp is not moved back to a release tag

Pinning to `v1.9.2` was considered in response to the remark, but **the current pin
`592feef0` is 17 commits ahead of v1.9.2, and two memory safety fixes are among them**:

- `8631825d` — `log_mel_spectrogram` reads off the end of the heap for audio shorter than
  201 samples. This is **a tool that is fed arbitrary user audio**, and over MCP a crafted
  file reaches it too
- `df1547b6` — a stack buffer overflow on a corrupted model file with an invalid `n_dims`.
  This is **a tool that downloads models off the network**, which is precisely a supply
  chain path

Going back to v1.9.2 loses those two. **Containing known vulnerability fixes takes priority
over being a release tag.**

Straightening out the premise as well: **a git submodule is already pinned by commit SHA,
and a SHA is content-addressed, so against tampering it is stronger than a tag name** (a
tag can be moved). What a release tag mainly adds is auditability (matching against CVEs),
not tamper resistance itself.

So: **when a release version appears, move to it. Until then, stay on the commit that has
the fixes.** Either way, the pin is by an immutable SHA.

### Model files are verified by SHA256 (the main point)

**v0.1.0's verification was size only.** An attacker who can swap a file out can easily
keep the size. And as `df1547b6` shows, **a corrupted model file is a memory safety
problem**, not something that ends at "the transcript comes out wrong".

Hugging Face publishes the SHA256 of every LFS file (`lfs.sha256` under `?blobs=true`). It
is pinned for every entry in the catalog and verified along the following two paths:

1. **On download** — verification happens **before** the rename, so a file that fails never
   exists at the proper path even for an instant
2. **On reuse of an existing file** — the path where `models pull` skips the download
   because the size matches. **Letting this through would turn a size-only check into "we
   think we verified"**, so the hash is looked at here too. A mismatch is an error rather
   than a silent re-download (the existence of a file whose size matches and whose contents
   differ is worth telling the operator about)

The tests explicitly fail the case where **the contents alone were swapped at the same
size**.

### The default model is taken from the author's repository

While collecting the hashes, two entries in the catalog turned out to be **byte-for-byte
identical**:

```
kenrouse/kotoba-whisper-v2.2-ggml/kotoba-whisper-v2.2-ggml-q5_0.bin
kotoba-tech/kotoba-whisper-v2.0-ggml/ggml-kotoba-whisper-v2.0-q5_0.bin
→ size / sha256 / blobId all identical
```

That is, **a third party's re-upload calling itself "v2.2" was distributing the same
contents as the author's v2.0**. voice-scribe had made it **the default model**.

Response:

- The default changes to `kotoba-whisper-v2.0` from `kotoba-tech` (the author)
- The duplicate entry is removed (a catalog that gives one set of contents two names lies
  about the choices)
- **Breaking change**: the default model name goes from `kotoba-whisper-v2.2` to
  `kotoba-whisper-v2.0`

As a general rule, **what goes in by default is taken from the upstream author**. A mirror
is only for when the author does not distribute it, or the author's distribution format
cannot be used.

## Consequences

- All 7 entries in the catalog — the 8 of the inventory above, less the one
  duplicate removed — have a 64-digit SHA256 (fixed by a test)
- That no two entries share a hash is fixed by a test
- That the default Japanese model comes from `kotoba-tech/` is fixed by a test
- `models list --json` prints `sha256`, so a user can check it locally
- Breaking: the default value of `default_model` and the argument to `models pull` change

### Remaining exposure (accepted)

- **The ONNX Runtime prebuilt zip is fetched from a third-party individual's repository**
  (`csukuangfj/onnxruntime-libs`). The SHA256 is verified, but that hash itself comes from
  sherpa-onnx's pin, so the root of trust is "upstream as of the moment the submodule was
  pinned" (TOFU). Microsoft's official distribution has no build for static linking, so
  there is currently no alternative
- The model hashes are likewise TOFU, rooted in "HF as of the moment the catalog was
  written". But **the root being fixed** is what has value: any later substitution can be
  detected
