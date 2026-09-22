# ADR-0013: Leave path judgement to nlink-jp/pathguard — keep no copy

- Status: Accepted
- Date: 2026-09-22

## Context

Since ADR-0010, `work_dir` validation and the read blacklist (`workdir.Sensitive`) lived in
`internal/mcp/workdir`. As the reference implementation of organization ADR-021, that copy was
transplanted into eight other servers. Every copy compared places **by name**. APFS is
case-insensitive by default, so `~/.SSH`, `.ENV` and `/USR/local` named the same places and passed
the checks. When the home directory could not be determined, `Sensitive` returned "" and passed
everything.

The organization moved this judgement into one module (`nlink-jp/pathguard`, lib-series). It compares
places by file identity and by names folded the way the disk folds them, and it catches a place that
does not exist yet through the identity of its parent. It holds one list, the same as gem-agent's and
lagent's.

## Decision

- Depend on `github.com/nlink-jp/pathguard` v0.1.0. No code from outside this organization comes
  with it.
- `internal/mcp/workdir` becomes a **thin adapter**. It keeps only:
  - taking the request's `_meta` from the context and passing it to `pathguard/workdir`'s `Resolve`,
  - moving that `*workdir.Error` onto `toolerr` with the same code, message and details,
  - `NewResolver(serverDataDir)` — passing this server's data directory as a protected place
    (`pathguard.ServerDir`), and its one sentence for `work_dir_required` as `RequiredHint`,
  - `Sensitive` — `pathguard/workdir.Sensitive` (the Local policy), passed through.
- The call sites (`Resolve`, `Validate`, `Sensitive`) do not change. What changes is the one line that
  builds the resolver (`cmd/mcp.go`) and the tests that built it as a zero value.
- The tests of the judgement itself are in pathguard. What stays here are the adapter's tests (taking
  `_meta`, carrying the error across, the protected place, a zero value refusing) and the existing
  contract tests.

## Consequences

`transcribe` behaves differently (the CHANGELOG says so):

- **Refused now**: the real places under your home from the runtimes' list (`~/.kube`,
  `~/.config/gh`, `~/.azure`, `~/.terraform.d`, `~/.gemini`, `~/.config/mcp-bridge`, `~/.netrc`,
  `~/.npmrc`, `~/.pypirc`, `~/.git-credentials`, `~/.vault-token`, `~/.docker/config.json`,
  `~/.claude.json`, `~/.bash_history`, `~/.zsh_history`); every spelling of any floor place — case
  variants, links, firmlinks; wherever a link directly inside one of those directories points (a
  `~/.ssh/config` that links into a sync folder protects the file it points at). When `$HOME`
  names another directory than the account's home, both are protected.
- **Accepted now**: `.env.example`, `.env.sample`, `.env.template`, `.env.dist` (templates, not
  secrets).
- **An unknown home refuses audio paths and every `work_dir`.** It used to pass everything.
- A relative `XDG_DATA_HOME` is ignored, as the XDG spec says (it put the data directory under the
  working directory; now a server directory that is not absolute would refuse every call).
- `work_dir_denied` carries `reason` in its `details`.
- One check costs about 2 ms (measured in pathguard) — nothing next to a transcription.

With no copy here, a fix to the judgement is a pathguard release and a one-line dependency update.

## References

- Organization ADR-021 (the work-dir contract of the file-mediated MCP servers)
- ADR-0010 (work-dir contract): the closed list of checks and the read blacklist — whose
  implementation this replaces
- nlink-jp/pathguard's RFP (`docs/en/pathguard-rfp.md`)
