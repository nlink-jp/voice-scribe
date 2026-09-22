# voice-scribe MCP server

Transcribes recordings locally with whisper.cpp, and optionally labels who is
speaking. No API key, and no audio leaves the machine.

## The shape of a session

1. Put the recording somewhere and tell the server where that somewhere is.
2. Call `transcribe`. It returns a `job_id` immediately.
3. Poll `check_job` until it reports `done`.
4. Read the transcript from the result, or from the file it names.

## The work directory

Every call names `work_dir`: **the absolute path of a directory you can read
back** — your session or working directory. The server works in the workplace
you prepared. A workspace is one project's recordings and transcripts inside it:

```
<work_dir>/<workspace_id>/
├── meeting.m4a          you put this here
└── output/
    └── meeting.json     the server writes this
```

It is required, and there is no default: a transcript written somewhere you
cannot open is a successful call and a useless one. The directory must already
exist — it is yours, so a path that is not there is a typo, and the server will
not create it. Nothing expands `~` or resolves a relative path on the way here.
`workspace_id` defaults to `default`.

Every result echoes the resolved `work_dir` and `workspace_id`, which is how
you find your files when your runtime supplied the directory for you (it may
set `_meta["jp.nlink/work_dir"]` on the call instead of you passing it).

If you pass `workspace_root`, `workspaceRoot` or `workspace_dir`, the call is
refused: those are the old names for this argument.

Transcript paths and other workspace files are named **relative to the
workspace**. A relative path escaping it is refused with `path_not_allowed`, and
so is a symlink pointing outside — containment is enforced by the kernel, not by
string matching. The workspace directory itself is checked the same way: if
`<work_dir>/<workspace_id>` is a symlink rather than a real directory, the call
is refused instead of silently working somewhere else.

`audio` is the exception, and deliberately: it may be **an absolute path to a
recording anywhere you can read**, and a relative name is looked for in the
workspace *and* in `work_dir` itself (the workspace wins if both hold it), so a
file you just wrote next to your work directory is found without moving it. It
is read in place, never copied —
copying an hour of audio into the workspace to transcribe it would be waste.
What is refused there is a credential or agent-control location under your home
(`~/.ssh`, `~/.aws`, `~/.kube`, `~/.gnupg`, `~/.config/gcloud`, `~/.config/gh`,
`~/.netrc`, `~/Library/Keychains`, `~/.claude`, `~/.codex` and the rest of the
list gem-agent and lagent use) and any `.env` file except its templates
(`.env.example`, `.env.sample`, `.env.template`, `.env.dist`), and so is wherever a link directly inside one of those
directories points. It is found under any spelling — another case, a link, the
path as given or resolved — and the refusal names the location. It is refused
whether or not a file is there, with the same answer either way, and a relative
name is judged at both places it may mean before either is looked at. The transcript still lands in the workspace either
way.

## Tools

### `transcribe`

Needs `work_dir` and `audio`. Everything else has a default worth knowing:

| Argument | Default | Notes |
|---|---|---|
| `model` | resolved from `language` | Name from `list_models`. A Japanese-specialised model is picked for Japanese, a multilingual one otherwise |
| `language` | detected | ISO 639-1. Naming it is faster and more reliable than detection on short or noisy audio |
| `format` | `json` | `json`, `text`, `md`, `srt`, `vtt` |
| `output` | `output/<name>.<format>` | Where the transcript is written |
| `translate` | off | Adds English. **Runs the audio through a second time**, so it roughly doubles the wait |
| `prompt` | none | Context for the decoder. **Write it as a sentence, not a keyword list** — see below |
| `vad` | the config's `vad`, else off | Gates silence through the VAD model, suppressing hallucinated text over it. Needs `silero-vad` installed — a decision for whoever is at the terminal (`models pull silero-vad`), like every model |
| `diarize` | off | Labels who is speaking. Needs two more models |
| `speakers` | worked out | Pin it when you know it — far more reliable than letting the clusterer decide |
| `speaker_hints` | `A`, `B`, … | Names, in order of first appearance |
| `offset_seconds` / `duration_seconds` | whole file | Transcribe a slice |

Returns `{job_id, state, work_dir, workspace_id, output, next}`. It does
**not** wait.

### `check_job`

Takes `job_id`. While running it reports progress; when `done` the result holds
the transcript.

**The transcript comes back in `text`**, up to `max_bytes` (default 65536; set
`max_bytes: 0` for no cap). Past the cap the result still carries text — as much
as the cap allows — plus `truncated: true`, an exact `omitted_bytes`, and a
`note`. `bytes` is always the full size, and `path` / `absolute_path` reach all
of it: the transcript file is written either way, because it is this server's
product. The cap bounds the response and nothing else — what fits in your
context is your judgement, not this server's.

Either way the result carries `model`, `language`, `segments`, `duration_seconds`,
and `speakers` when diarization ran.

Jobs are in-memory. After a server restart an old `job_id` returns
`job_not_found`; re-submit `transcribe`.

### Writing a useful `prompt`

`prompt` is whisper's initial prompt: it conditions the decoder, it does not
declare a vocabulary. **Phrasing changes the result more than content does.**

Write a sentence or two describing the recording, in the register you expect to
hear — who is in it, where it is set, what it is about. A comma-separated list
of names is not that, and measurably degrades the output: on a Japanese
recording, a noun list injected one of its own terms into an unrelated line and
broke the lines around it, while a sentence-form prompt over the same audio
recovered whole lines the unprompted run had dropped, including a name it had
lost entirely.

```
good:  "社内の定例ミーティングの録音です。新機能のリリース時期とテスト計画について話しています。"
bad:   "定例ミーティング、リリース、テスト計画、新機能"
```

**It does not reliably fix a specific misheard name**, which is the thing people
most want it for. On the recording above a surname came out wrong every time, and
four prompts containing the correct one — kanji, katakana, listed, and used in a
sentence — all still produced the wrong one, some at the cost of correct lines
elsewhere. The prompt shapes register and context, not the acoustic model's ear.

So: use it to improve overall coherence, and expect to fix stubborn names by
editing the transcript afterwards. Try it on a slice
(`offset_seconds`/`duration_seconds`) and compare before applying it to a long
recording.

### `list_models`

`scope` is `installed` (default), `catalog`, or `all`.

**Downloading is not exposed here on purpose.** Models are hundreds of megabytes,
and starting that is a decision for the operator at a terminal:
`voice-scribe models pull <name>`.

### `get_usage`

This document.

## Speaker diarization

Two models working together — one finds where the speaker changes, the other
decides which of those stretches are the same person. Both must be installed:

```
voice-scribe models pull pyannote-segmentation-3
voice-scribe models pull campplus-speaker-embedding
```

It needs voices that genuinely differ, and it can fail in **either** direction.

**Too few speakers.** Everyone comes back as one. That is usually the honest
answer, not a failure — but if you know there were more, pin `speakers`, or
*lower* `speaker_threshold` below the default 0.5 so it splits more readily.

**Too many speakers.** A result carrying dozens of "people", many of whom speak
exactly once. This is the common failure on material with **continuous
background music**: the embedding model sees music mixed with voice, so one
person's embeddings scatter and get split apart. Pin `speakers`, or *raise*
`speaker_threshold` above 0.5 so it merges more readily. Measured on a 39-minute
drama recording with music throughout: the default gave 93 speakers, and 0.9
gave a plausible cast.

The result carries a `warning` field when the speaker count looks like
over-splitting rather than a real cast. It is well-formed output either way, so
nothing else would tell you.

**Calibrate on a slice, not the whole file.** `offset_seconds` and
`duration_seconds` apply to diarization too, so a few minutes of audio is enough
to find a threshold and costs seconds rather than minutes.

Labels follow first appearance, so `A` is whoever spoke first.

## What this server will not do

- **Transcribe from a URL or from bytes.** Put the file in the workspace.
- **Download models.** See `list_models`.
- **Summarise or structure minutes.** That is a separate job; feed the JSON to
  something that does it. The envelope is compatible with gem-transcribe, so
  anything that reads one reads the other.
- **Handle mkv or webm.** macOS cannot decode them. Convert first.
- **Stream, or transcribe live audio.** Files only.

## Errors

Every failure carries a stable `code` you can branch on.

| Code | What happened | What to do |
|---|---|---|
| `missing_argument` | A required argument was absent | Read the message; it names the argument |
| `invalid_arguments` | Unknown or mistyped argument | Arguments are strict — check the spelling against the schema |
| `path_not_allowed` | A relative path escaped the workspace or was a symlink out of it, the workspace directory was itself a symlink, or `audio` (an absolute path, or a relative name at either place it may mean) is a credential or agent-control location or a `.env` file, whether or not it exists | Use a workspace-relative path to a real file, or an absolute path outside those locations, and a `workspace_id` that is a real directory under `work_dir` |
| `work_dir_required` | No `work_dir` argument, and your runtime set no `_meta` hint (or you sent one of the old names) | Pass the absolute path of a directory you can read back |
| `work_dir_invalid` | Not absolute, started with `~`, or contained `..` | Pass the path you mean, spelled out |
| `work_dir_not_found` | The directory is not there, or is not a directory | It is your directory, so this is a typo — the server will not create it |
| `work_dir_not_writable` | The server cannot write there | Pass a directory you own |
| `work_dir_denied` | A system location, your home directory itself, a credential or agent-control location (or where a link directly inside one points), the server's own data directory, or the home directory cannot be determined — for `work_dir`, and for the workspace directory `<work_dir>/<workspace_id>` it would use — `details.reason` says which: `system_dir`, `home_dir`, `sensitive_path`, `server_dir`, `home_unknown`, `unconfigured`, `unresolvable_path` | Pass your session or working directory |
| `input_not_found` | The recording is not in the workspace | Put it there, or fix `work_dir` / `workspace_id` |
| `decode_failed` | The container or codec could not be read | Convert to m4a or wav |
| `model_not_found` | The named model is not installed | `list_models`, then `voice-scribe models pull <name>` at a terminal |
| `no_runtime` | This binary was built without the transcription runtime | Rebuild with `make build-engine` |
| `diarize_failed` | Diarization failed, usually a missing model | Install both diarization models |
| `empty_transcript` | Decoding worked but found no speech | Check the recording is not silent, and that `language` is right |
| `transcribe_failed` | Anything else | The message carries the runtime's own words |
| `job_not_found` | Unknown `job_id`, usually after a restart | Re-submit `transcribe` |
| `invalid_scope` | `list_models` scope was not installed/catalog/all | Use one of those three |
