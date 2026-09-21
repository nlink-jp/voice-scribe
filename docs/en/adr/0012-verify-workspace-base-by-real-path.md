# ADR-0012: Verify the workspace base by real path — `os.Root` cannot vouch for its own anchor

- Status: Accepted
- Date: 2026-09-21

## Context

Since ADR-0003 this server has left path containment **to the kernel**. A
workspace-relative path is opened through an `os.Root`; there is no string
matching. ADR-0010 made `work_dir` a required per-call argument and fixed the
workspace at `<work_dir>/<workspace_id>`.

What `os.Root` guarantees is operations **within** the root. The root's own path
is resolved normally. That is where the hole was.

The old `EnsureUnder` built the base with `filepath.Join(work_dir, id)`, called
`os.Mkdir`, ignored `fs.ErrExist`, and handed that base to `os.OpenRoot`. So
**a symlink pre-planted at the name `<work_dir>/<id>` made the containment
anchor land on the link's target.** Every read and write afterwards, transcripts
included, went outside `work_dir` — and **the call reported success**.

Who can plant the link? Anything that can write to `work_dir`: another tool
using the same work directory, code running in a sandbox, leftovers from an
earlier job. `work_dir` is the one path the caller vouched for, but **vouching
for it is not vouching for the names beneath it.**

Measured: `os.OpenRoot` on a symlinked base writes into the link's target and
returns no error at all.

This failure has the shape ADR-0010 removed when it deleted the default
workspace root — *the job succeeds and the path it returns is not where you
meant*. ADR-0010 closed the door marked "default"; this route stayed open.

There is a second reason containment alone is not enough. **`BaseDir` is
afterwards handed to code outside any root**: to `os.OpenRoot`, and for the
recording, to the audio decoder. Both resolve the path themselves.

## Decision

### 1. Create through an `os.Root` on `work_dir`

`makeBaseDir` opens `os.OpenRoot(work_dir)` and creates with
`root.Mkdir(id, 0o755)`. The root refuses a path that leaves it, so **creation
itself cannot traverse a link.** It stays `Mkdir` rather than `MkdirAll` for
ADR-0010's reason: the work directory is the caller's and must already exist, so
a missing parent is the caller's typo.

### 2. Compare real paths after creating

Compare `filepath.EvalSymlinks(work_dir)` joined with `id` against
`filepath.EvalSymlinks(filepath.Join(work_dir, id))`.

**The comparison is the part that has to exist.** Creating through a root is not
sufficient, because `BaseDir` then travels to code outside any root — so
something must ask, once, directly, **before handing it over**: is what we made
where we meant it to be?

### 3. Refuse a mismatch with `path_not_allowed`, and name it

The error names the workspace id and **what it actually resolved to**. It is
`path_not_allowed` rather than `workspace_failed` so the caller can fix it as a
problem with the location they named — the same reason ADR-0010 split its error
codes apart.

### 4. Resolve both sides before comparing

Resolving only one side produces a mismatch for reasons that have nothing to do
with the attack. On macOS `t.TempDir()` sits under `/var`, which is itself a link
to `/private/var`, so an unresolved `work_dir` always differs from a resolved
base. **The comparison is between two resolved paths.**

### 5. Do not verify `work_dir` itself

It is the single path the caller vouched for, and it has already passed
ADR-0010's checks (absolute, no `~`, no `..`, an existing directory, writable,
not a system location). What is defended here is **the `<id>` beneath it**.

### 6. Pin it with a test

`TestEnsureUnderRefusesLinkedWorkspaceDir` requires that `EnsureUnder` on a
work_dir with a planted link fails with `path_not_allowed` and that **nothing
was created in the link's target**. The test **was checked against the unfixed
source**: before the fix it finds `output/` created in the outside directory and
the workspace succeeding with no error anywhere.

## Consequences

- Nothing changes for a normal call. If the base is a real directory, the real
  paths match.
- Two extra `EvalSymlinks` per workspace creation. Negligible against a
  transcription job.
- `path_not_allowed` now means more than it did (previously: a path outside the
  workspace). The error table in `internal/mcp/tools/usage.md` was updated in the
  same commit — what the model reads is part of the product, so it cannot be left
  behind.
- The refusal happens **once, at creation**. The anchor is decided once and
  governs every later read and write, so refusing at the entrance is both the
  cheapest and the earliest place to do it.
- data-toolbox-mcp already has the same shape (`makeWorkDir` in
  `internal/workspace/manager.go`, for the path it hands to podman as a bind
  mount). But **it has no ADR, and its refusal is a plain `fmt.Errorf` with no
  stable code.** Anyone porting this shape to another server should route the
  refusal through that server's `toolerr`.
- Holes of this kind appear **wherever a path leaves the root**. That a piece of
  code uses `os.Root` says nothing about what holds once the path is outside it.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Rely on `os.Root` containment alone | `os.Root` contains operations within the root but resolves the root path itself normally. Measured: `os.OpenRoot` on a symlinked base wrote into the link's target with no error |
| `os.Lstat` the base and check only whether it is a symlink | TOCTOU — it can be swapped between the check and the use. It also misses a link at an intermediate element. Comparing real paths asks directly whether what was made is where it was meant to be, which covers both |
| Check containment with a string prefix (`strings.HasPrefix`) | Comparing unresolved spellings establishes nothing. It is a return to exactly the string matching ADR-0003 discarded |
| Accept a linked base and treat its target as the work directory | The caller vouched for `work_dir`, not for the link's target. Writing somewhere else while reporting success *is* the original defect; this would make it the specification |
| Verify on every read and write instead of at creation | The anchor is fixed once at creation and governs every later operation. There is no reason to turn a single check at the entrance into an `EvalSymlinks` on every call |
| Subject `work_dir` to the same comparison | It is the one path the caller vouched for and has already passed ADR-0010's checks. Doubting it leaves the caller with nowhere they are allowed to name |

## References

- ADR-0003 (MCP server): the record that put kernel containment via `os.Root` in
  place
- ADR-0010 (work dir contract): the `<work_dir>/<workspace_id>` structure, and
  the failure shape where a call succeeds and the caller cannot read the result
- Organization ADR-021 (work-directory contract for file-mediated MCP servers)
- `internal/mcp/workspace/manager.go`: `makeBaseDir`
- `internal/mcp/workspace/containment_test.go`:
  `TestEnsureUnderRefusesLinkedWorkspaceDir`
- data-toolbox-mcp `internal/workspace/manager.go`: `makeWorkDir` (the same
  shape, for a bind mount)
