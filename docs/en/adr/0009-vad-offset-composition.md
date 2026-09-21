# ADR-0009: VAD and range selection are composed on the original audio's timeline

- Status: Accepted
- Date: 2026-08-27

## Context

An external bug report identified two defects around VAD. The actual material was a
one-hour meeting recording (the last 2 minutes 40 seconds or so silent), and the
reporter ran into both while trying to suppress hallucination in the silent region
with `--vad`.

**Finding 1: the MCP server cannot enable VAD at all.** The `transcribe` tool's
InputSchema has no `vad` (and since `additionalProperties: false`, there is no way to
pass one either), and further, `mcp_wiring.go` does not set `VADModelPath` when it
assembles `engine.Params`, and no place references `Config.Transcribe.VAD` either.
Where the CLI ORs the `--vad` flag with the config, over MCP the config's
`vad = true` is silently ignored. Because `Threads` and `Diarize.Threshold` are read
from the config over MCP too, the expectation that "the config works over MCP as well"
arises naturally, and nothing tells you it has been betrayed.

**Finding 2: using `--vad` together with `--offset`/`--duration` silently transcribes
a different range.** Upstream `whisper_full` runs VAD first and replaces `samples`
with a compacted buffer of speech only, and then `whisper_full_with_state` applies
`offset_ms`. That is, **the offset points into the compacted timeline**.
Meanwhile the output timestamps are correctly mapped back to the original timeline by
`vad_mapping_table`, so the returned JSON looks entirely consistent — the only thing
that appears nowhere is that the range is not the one requested. In the report's
measurement, against a requested offset of 1800 seconds the actual start was
2101.1 seconds (the discrepancy is the accumulated length VAD judged to be non-speech,
and it increases monotonically with the offset). A selection towards the end drops all
of the real speech in the requested range and returns nothing but hallucination, and
past the compacted end, `empty_transcript` comes out with an explanation contrary to
fact — "the audio is silent, or in a language the model cannot handle".
With `--diarize` as well, the diarization side cuts the correct range on the Go side
(`diarizeSlice`), so the correspondence between speaker labels and text breaks too.

## Decision

### Finding 2: cut+shift in the engine layer

When `VADModelPath != ""` and an offset/duration is given, `Session.Transcribe`

1. cuts the samples to the requested range on the Go side,
2. passes `offset_ms`/`duration_ms` to whisper as 0 (so VAD runs only inside the cut
   window),
3. adds the offset to the returned timestamps to put them back on the original
   timeline.

This is an application of the pattern `diarizeSlice` already uses. The implementation
was placed in the engine layer rather than the cmd layer because the CLI, MCP and
translate's second pass all go through `Session.Transcribe`, so one place fixes every
route and the range mismatch with diarize resolves itself as well. If the cut leaves
the window past the end of the audio, an error is returned that states that fact as it
is.

As a side effect, the VAD cost of looking at only part of a long recording drops to the
window's share as well.

### Finding 1: expose `vad` over MCP, and read the config with the same expression as the CLI

Add `vad` (boolean) to the `transcribe` tool, and have the wiring go through the same
`resolveVAD(rt, req.VAD || Config.Transcribe.VAD)` as the CLI. When the VAD model is
not installed, the existing `classify` sorts the error into `model_not_found`
(because the message contains `models pull`; no extra branch is needed).

### Ship them together, and fix Finding 2 first

Because VAD cannot be enabled from MCP, Finding 2 was hidden behind Finding 1. Fixing
only Finding 1 first would newly create a route by which a client passing `vad` and
`offset_seconds` together steps on Finding 2. So the fix order is Finding 2 first, and
the release is both together as one version.

## Alternatives considered

**Reject the combination with an error.** On the safe side, but `--vad` is precisely
the feature one wants to use on a long recording, and forbidding its use together with
a range selection lowers the feature's value.

**Fix upstream whisper.cpp.** A change that applies the offset before VAD is a
behaviour change for upstream's existing users, and it also collides with the policy of
pinning submodules to immutable tags (ADR-0002). cut+shift on the Go side gets the same
result without touching upstream.

**Cut in the cmd layer.** Symmetrical with `diarizeSlice`, but it means writing the
same processing in both the CLI and MCP, which risks creating again in future the same
kind of asymmetry as Finding 1, where only one side gets fixed. The engine layer, the
common path, was chosen.

## Consequences

- The meaning of offset/duration agrees as "seconds of the original audio" whether VAD
  is in use or not.
- An offset given with VAD enabled will now return different segments from before
  (= the range actually requested). No use that depends on the old behaviour is
  conceivable, so no compatibility accommodation is made.
- ADR-0008's VAD measurements were all full-length runs (no offset), so they are
  unaffected by this defect, and the conclusion that "VAD cannot be the default" and
  the README's statement about the reduced character count both remain valid as they
  are. This report gives one data point for that ADR's open item, "the breakdown of
  what VAD dropped (hallucination against real speech)", but note that it is a
  consequence of the range shift, not VAD's judgement accuracy.
  Measuring the breakdown on a full-length run remains open.
