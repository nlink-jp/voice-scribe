# ADR-0003: Implementation decisions for the MCP server

- Status: Accepted
- Date: 2026-08-08

## Context

The RFP placed the MCP server as Phase 2b and had decided on four tools, asynchronous jobs,
`workspace_root` plus `os.Root` containment, and the two-tier return. The skeleton reuses
the one that has already been ported three times: data-toolbox-mcp → video-studio-mcp →
image-forge.

Only **what was decided during implementation** is recorded here. For what the RFP had
already settled (the tool set, the two-tier return policy, the decision not to expose
`models pull`), see the RFP.

## Decision

### The skeleton was ported wholesale from image-forge, with the domain vocabulary rewritten

`jsonrpc` / `transport` / `mcpserver` / `toolerr` / `workspace` / `job` were copied and the
module paths rewritten. **All of the donor's domain vocabulary was rewritten along with
it** — leave wording like "generation project", "init/mask images" or "rendered PNGs" in
place, and the next person to read it reads it as an image generation server. The error
codes were re-papered to this domain's failures as well: `render_failed` →
`transcribe_failed`, among others.

### A session is opened and closed per request (nothing stays resident)

The RFP's Phase 2c listed "model switching in a resident engine (reload key)", but it is
**not adopted**.

Jobs are serialized through a single worker, so all a resident engine buys is "skipping the
reload between consecutive calls". The price is that a server which may sit idle for hours
goes on holding more than 500MB. It can go in **once there is a measurement showing the
reload is actually a problem**.

### The file is always written, even above the two-tier return threshold

Whether the return is inline decides only "whether the text comes back along with it".
**The design does not let the threshold change whether the file exists** — an agent that
decided to keep it would be forced to fetch it again, and a tuning item deciding whether an
artifact exists is too surprising.

The excerpt is **cut at a rune boundary**. Cutting Japanese text by bytes produces a
replacement character, and that is precisely the script of this tool's main user base.

### A quiet server is no verification of the stdout isolation

`claimStdout()` dups the real stdout and keeps it for the transport alone, and points fd 1
at stderr. Together with the source side (`engine.SetLogHandler`), that is two layers.

**The verification nearly went wrong once**: in a real end-to-end run stdout held only JSON
and stderr was 0 lines, and this was nearly read as "the isolation is working" — but that
is **only the log handler dropping the info level**, not evidence that the isolation layer
did anything. It was replaced with unit tests that hit the mechanism itself directly (write
to the protocol handle and it comes out on the transport; write to `os.Stdout` and it comes
out on stderr).

**Lesson: when there are two defences, an observation that one of them alone explains must
not be taken as evidence for both.**

### Only warnings and above from the runtime log go to stderr

The 16 lines of ggml initialization log come out every time, and they are not worth filling
an MCP client's stderr with. A model load failure and the like come out at error level, so
they pass straight through.

## Consequences

### Measured (2026-08-08, a real stdio client)

```
server:       voice-scribe
instructions: present
tools:        get_usage, transcribe, check_job, list_models
usage:        5804 bytes of markdown
transcribe -> job_id -> check_job -> done
model: kotoba-whisper-v2.2 | segments: 6 | speakers: [田中, 佐藤] | returned: inline
```

That every line of stdout parses as JSON was confirmed with a transcription actually
running.

### usage.md is kept from rotting by a machine check

It is the only document a client reads before operating this server, so drift from the code
does real harm. The tests fix the following:

- Every registered tool appears in usage.md
- Every error code that can be returned is in the recovery table
- Every argument in `transcribe`'s schema appears in usage.md
- Every tool's schema is `additionalProperties:false` (a misspelled argument is not
  silently ignored)

### Costs accepted

- The model is loaded again on every consecutive call (deliberate, as above)
- Jobs are not persisted. After a restart a `job_id` gives `job_not_found`, and
  resubmitting is the recovery procedure
- `models pull` cannot be used from MCP. An agent is pointed at the CLI procedure
