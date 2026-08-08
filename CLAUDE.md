# CLAUDE.md — voice-scribe

Organization rules: https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md
Project orientation, build commands, and gotchas: **AGENTS.md** (read it first).

## Project-specific rules

- **Never claim "fully offline".** Inference uses no network, but `models pull`
  fetches from Hugging Face and OS behaviour is not ours to guarantee. Say what
  is true: no API key, no audio leaves the machine.
- **Measure stdout with `1>file 2>file`.** A clean terminal proves nothing about
  what a pipe receives, and `voice-scribe mcp` puts JSON-RPC on stdout. See
  AGENTS.md, "stdout is the transport".
- **Keep the output envelope compatible with gem-transcribe.** `segments[].text`
  is a language-code → text map, not a string. Changing the envelope breaks
  downstream consumers (meeting-notes) that parse both tools with one parser.
- **Do not write model behaviour from memory.** Speed and accuracy claims about
  whisper models belong in the docs only after being measured on this machine,
  with the model and quantization named.
- **Prefer pure functions at the boundaries.** Anything that can be tested
  without the runtime (formatters, timeline merging, config resolution, speaker
  assignment) lives outside the cgo files and is tested without a build tag.
- **`cmd/planned.go` must not survive to a release.** See AGENTS.md.

## Before declaring a task done

The org checklist applies in full. The items that bite most often here:

- [ ] `make build` (never `go build` directly)
- [ ] `make test` passes; if the change touches the runtime, `make test-engine` too
- [ ] README.md **and** README.ja.md updated in the same commit as behaviour changes
- [ ] CHANGELOG.md entry added
- [ ] AGENTS.md still describes reality (structure, build commands, gotchas)
- [ ] Non-obvious design decisions recorded as an ADR in `docs/adr/` **before**
      implementing
- [ ] No absolute paths, machine-local identifiers, or personal directory names
      in anything committed
