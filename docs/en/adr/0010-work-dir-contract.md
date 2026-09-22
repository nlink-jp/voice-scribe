# ADR-0010: The work dir is taken as a per-call `work_dir`, with no default root

- Status: Accepted — its implementation (the checks of §3 and the Amendment's blacklist) is
  replaced by ADR-0013 (nlink-jp/pathguard)
- Date: 2026-09-13

## Context

As the **reference implementation** of organization ADR-021 (the work dir contract for
file-mediated MCP servers), this repository implements the contract first. Six of the
nine servers are of the "work dir + `workspace_id`" shape, and among them this server
is the smallest (one tool affected), and the body of `workspace.Manager.EnsureIn` that
becomes the transplant source is here.

There are two problems with the current state.

**1. The spelling and the meaning are scattered.** Across the fleet the same argument
exists under three spellings — `workspace_root` / `workspaceRoot` /
`workspace_dir` — and three roles are mixed into them: "the root that workspaces are
created under", "the dir written into directly", and "the base for resolving input
relative paths". The model has to remember a different name per server, and strict
decode (correctly) rejects it when it gets them wrong.

**2. When omitted it falls silently back to a server default.** This server's
`workspace_root` is optional, and when omitted it writes to
`~/.local/share/voice-scribe/mcp-workspaces`. The calling side's file tools cannot open
that, so the failure appears only in the form **the job succeeds and the returned path
cannot be opened**.

Measured across four runtimes (Claude Code / ChatGPT Codex / gem-agent / lagent),
for MCP's `roots` Codex declares no capability and returns an empty array, and Claude
Code returns only a project dir that does not include the scratchpad. Environment
variables are stripped by Codex. **The per-call argument is the only route common to
all four runtimes**, and a design that has a default value amounts to betting on
"whether the place the operator wrote happens to be readable by the caller".

## Decision

### 1. The argument is `work_dir`, required for `transcribe`

`workspace_root` is retired and unified into `work_dir`. Its meaning is
**"an absolute path the caller can read back"**. `transcribe` resolves its input
recording from under `work_dir` and writes the transcript under it too, so it is placed
in `required` rather than being conditional. `workspace_id` is left as it is (a unit of
work inside `work_dir`).

### 2. The resolution order is argument → `_meta` → error

`params._meta["jp.nlink/work_dir"]` is read as the second route. It is a
schema-independent route that our own runtimes (gem-agent / lagent) can attach to every
`tools/call`, and the server can receive it without a per-tool implementation.
If neither is present, `work_dir_required` is returned. **There is no server default.**

### 3. Validation is a closed list

`absolute` / no `~` / no `..` / an existing dir / writable / not a system location.
These return `work_dir_invalid`, `work_dir_not_found`, `work_dir_not_writable` and
`work_dir_denied` respectively. Separating them from the existing `path_not_allowed` is
so that the caller can tell "forgot to pass it", "the place does not exist" and
"refused" apart and fix them.

**Dirs are not created.** The caller's work dir always already exists, so a path that
does not exist is a typo, and creating it silently returns to "it returned but cannot
be opened".

### 4. Delete the default workspace root

`defaultWorkspaceRoot()` and `Manager`'s default root (`Ensure` / `Root` / `List` /
`Delete`) are deleted. An unreachable default is nothing but a foothold for the next
person to revive it as "the fallback when omitted".

### 5. The result echoes where it went

The results of `transcribe` / `check_job` carry the resolved `work_dir` (absolute
path). A caller that had a work dir injected via `_meta` can learn the destination only
from the result, not from its own request.

### 6. Enforcement is a test

That there is no tool which does not expose `work_dir`, that no retired spelling
remains in a schema, and that `additionalProperties:false` holds, are fixed by a test
that walks the tool schemas. A convention in prose gets re-decided by whoever adds the
next tool.

## Consequences

- **A breaking change.** A call that sends `workspace_root` is rejected by strict
  decode, and omitting `work_dir` becomes `work_dir_required`. Both are loud failures
  that name the name, which is better than falling silently back to a default. An
  update on the mcp-tactics side is the counterpart.
- With the default root deleted, `Manager` becomes a type that only "creates a
  workspace under the caller's dir". If workspace listing and deletion tools are added
  in future, they will have to be rebuilt in a form that takes `work_dir`.
- This repository's `internal/mcp/workdir` becomes the transplant source for the other
  eight servers.

## Amendment (2026-09-13): reads are allowed outside `work_dir` too

Organization ADR-021 §7 was settled, and along with retiring the `allowed_paths`
approach it became "**reads anywhere but the blacklist, writes only under
`work_dir`**". This server follows suit.

- `audio` **accepts an absolute path**. It is read in place, not copied. Duplicating a
  one-hour recording into the workspace before transcribing it is pure waste, and the
  caller can read that file itself. A relative path is workspace-relative as before,
  with kernel containment by `os.Root` in effect
- What is refused is the locations of credentials and agent-control files (`~/.ssh`,
  `~/.aws`, `~/.gnupg`, `~/.config/gcloud`, `~/Library/Keychains`, `~/.claude`,
  `~/.codex`, any `.env`). **The check runs on both spellings of the path (as passed /
  after symlink resolution) × both spellings of the entry** —— a hole pcap-analyzer
  found on real hardware, where resolving before comparing lets a path reached through
  a link, such as `~/.ssh/config`, pass straight through
- Output is under `work_dir` only, as before. The blacklist is a floor, not a boundary

## References

- Organization ADR-021 (the work dir contract for file-mediated MCP servers)
- ADR-0003 (the MCP server): the premises of the workspace and os.Root containment
