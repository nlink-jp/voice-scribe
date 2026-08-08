# voice-scribe MCP server

Transcribes recordings locally with whisper.cpp, and optionally labels who is
speaking. No API key, and no audio leaves the machine.

## The shape of a session

1. Put the recording somewhere and tell the server where that somewhere is.
2. Call `transcribe`. It returns a `job_id` immediately.
3. Poll `check_job` until it reports `done`.
4. Read the transcript from the result, or from the file it names.

## Workspaces

A workspace is a directory holding one project's recordings and transcripts:

```
<workspace_root>/<workspace_id>/
├── meeting.m4a          you put this here
└── output/
    └── meeting.json     the server writes this
```

`workspace_root` is an absolute path you pass per call — **the server works in
the workplace you prepared**. Omit it and the server uses its own directory
under `~/.local/share/voice-scribe/mcp-workspaces`, which is fine for one-off
work but means you must put the recording there first. `workspace_id` defaults
to `default`.

Paths in arguments are always **relative to the workspace**. Absolute paths and
paths escaping the workspace are refused with `path_not_allowed`, and so are
symlinks pointing outside it — containment is enforced by the kernel, not by
string matching.

## Tools

### `transcribe`

Needs `audio`. Everything else has a default worth knowing:

| Argument | Default | Notes |
|---|---|---|
| `model` | resolved from `language` | Name from `list_models`. A Japanese-specialised model is picked for Japanese, a multilingual one otherwise |
| `language` | detected | ISO 639-1. Naming it is faster and more reliable than detection on short or noisy audio |
| `format` | `json` | `json`, `text`, `md`, `srt`, `vtt` |
| `output` | `output/<name>.<format>` | Where the transcript is written |
| `translate` | off | Adds English. **Runs the audio through a second time**, so it roughly doubles the wait |
| `prompt` | none | Context for the decoder. **Write it as a sentence, not a keyword list** — see below |
| `diarize` | off | Labels who is speaking. Needs two more models |
| `speakers` | worked out | Pin it when you know it — far more reliable than letting the clusterer decide |
| `speaker_hints` | `A`, `B`, … | Names, in order of first appearance |
| `offset_seconds` / `duration_seconds` | whole file | Transcribe a slice |

Returns `{job_id, state, output, next}`. It does **not** wait.

### `check_job`

Takes `job_id`. While running it reports progress; when `done` the result holds
the transcript.

**Short transcripts come back inline** in `text`. Long ones come back with
`truncated: true`, an `excerpt`, and a `path` to read. The file is written in
both cases — the threshold only decides whether you also get the text without
asking. Override it per call with `inline_threshold`.

Either way the result carries `model`, `language`, `segments`, `duration_seconds`,
and `speakers` when diarization ran.

Jobs are in-memory. After a server restart an old `job_id` returns
`job_not_found`; re-submit `transcribe`.

### Writing a useful `prompt`

`prompt` is whisper's initial prompt: it conditions the decoder, it does not
declare a vocabulary. **Phrasing changes the result more than content does.**

Write a sentence or two describing the recording, in the register you expect to
hear — who is in it, where it is set, what it is about. A comma-separated list
of names is not that, and measurably degrades the output: on a Japanese drama
recording, a noun list turned "いくらフェア中だからって" into "カモスタ中だからって"
and broke neighbouring lines, while a sentence-form prompt over the same audio
recovered whole lines the unprompted run had dropped, including a name it had
lost entirely.

```
good:  "ここはピアキャロット。美優先輩とさくらちゃんが働いています。期末試験の話をしています。"
bad:   "ピアキャロット、美優、さくら、期末試験"
```

**It does not reliably fix a specific misheard name**, which is the thing people
most want it for. On the recording above, a surname came out as カモスタ / かもした;
four prompts containing the correct name — kanji, katakana, listed, and used in a
sentence — all still produced the wrong one, and some cost correct lines
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
| `path_not_allowed` | Path was absolute, escaped the workspace, or was a symlink out of it | Use a workspace-relative path to a real file |
| `input_not_found` | The recording is not in the workspace | Put it there, or fix `workspace_root` / `workspace_id` |
| `decode_failed` | The container or codec could not be read | Convert to m4a or wav |
| `model_not_found` | The named model is not installed | `list_models`, then `voice-scribe models pull <name>` at a terminal |
| `no_runtime` | This binary was built without the transcription runtime | Rebuild with `make build-engine` |
| `diarize_failed` | Diarization failed, usually a missing model | Install both diarization models |
| `empty_transcript` | Decoding worked but found no speech | Check the recording is not silent, and that `language` is right |
| `transcribe_failed` | Anything else | The message carries the runtime's own words |
| `job_not_found` | Unknown `job_id`, usually after a restart | Re-submit `transcribe` |
| `invalid_scope` | `list_models` scope was not installed/catalog/all | Use one of those three |
