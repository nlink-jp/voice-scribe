# ADR-0011: Cap the response size with `max_bytes`; do not switch the delivery mode

- Status: Accepted
- Date: 2026-09-14

## Context

`[mcp] inline_threshold` (default 8192 bytes) was **a switch of delivery mode**: if the
transcript was at or under the threshold, return the whole text in `text`; if it went
over, discard `text` and return `excerpt` + `path`. The file itself was always written
regardless of the threshold, so this was not a spill mechanism. What it decides,
however, is of the same nature — **the server is judging "does this result land in the
caller's context"**.

As an organization we decided on 2026-09-06 that "putting an over-large result in a
file is the runtime's job", and splunk-mcp (ADR-0004) and pcap-analyzer-mcp (ADR-0009)
removed their spills the same day and moved towards "an explicit cap plus accounting
for what was dropped". The point of both is that a server cannot observe the model's
context window, so **the threshold is certain to be wrong for some caller**. A default
of 8192 bytes is no more than enough to hold 40 minutes of conversation as plain text,
or a few minutes as JSON, and the moment it is exceeded the model receives no body at
all.

This judgement, on the other hand, differs from the previous two in one respect.
**The transcript file is a product.** An srt/vtt is a real file to hand to a video
player, and the json is read by downstream tools as well. In pcap terms it corresponds
to the extracted objects of `extract_objects`, and the reason `work_dir` is needed
remains as it is. What should be withdrawn is not the file but **the structure in which
the response size governs whether there is a file and whether there is a body**.

## Decision

1. Retire `inline_threshold` and replace it with **`max_bytes`** (config
   `[mcp] max_bytes`, tool argument `max_bytes`, default 65536, `0` for unlimited).
   65536 is the same value as pcap-analyzer-mcp's `output.max_bytes`, so that a model
   that has met another server of this org meets the same number.
2. **The result always carries `text`.** Even when the cap is exceeded, the body up to
   the allowed extent is included (cut on rune / line boundaries, so as not to break
   Japanese). The `excerpt` field is retired — the very concept of "a preview in place
   of the body" is gone.
3. **Count what was dropped exactly.** `truncated: true`, `omitted_bytes` (the exact
   difference), `note` (an explanation in words). `bytes` is always the exact size of
   the whole, and `path` / `absolute_path` reach the full text.
4. **The file is always written.** The cap binds only the response and has nothing at
   all to do with whether the product exists. `work_dir` therefore remains required
   (ADR-0010).
5. A config holding an old key is **refused by name** and does not start. A value the
   operator wrote deliberately is not dismissed as an "unknown key".

## Consequences

- A breaking change. A call that sends `inline_threshold` is rejected by strict decode,
  and an operator who still has it in a config is told the replacement by name at
  startup.
- The state in which the model receives no body at all for a long transcript is gone.
  With the default of 65536, 8× what 8192 carried is carried, and if that is not
  enough, `max_bytes: 0` makes it unlimited.
- A caller that depended on `excerpt` can just read `text` (the behaviour for short
  results is the same).
- gem-scribe (ADR-0003) is brought into the same shape. The two share the same result
  structure.

## Alternatives considered

| Option | Why not |
|---|---|
| Retire the transcript file as well, taking exactly the same shape as splunk / pcap | The transcript is a product. An srt/vtt is a real file to hand to a video, and the json is read downstream. Deleting the file would remove the need for `work_dir`, but in exchange the full text of a long transcript would be reachable only for the lifetime of the job. pcap too kept its file and `work_dir` for `extract_objects` |
| Only raise `inline_threshold`'s default value | The name goes on saying "past the threshold the delivery mode changes". What the model reads is the name and the description, not the implementation |
| Set no cap and always return the whole text | The server would be able to break the caller's context. In taking away a knob the caller should hold, it is the same mistake as imposing a default |
| Cap by character count (rune count) | `bytes` is already returned, and the other servers in the fleet count in bytes. Keep the unit to one |
