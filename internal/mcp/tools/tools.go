// Package tools implements the MCP tools exposed by `voice-scribe mcp`.
//
// The server is stateful and async: transcribe enqueues a job and returns a
// job_id, which check_job polls. Recordings live in a workspace the agent
// prepares, and transcripts are written under its output/ subdirectory.
//
// Unlike the image and audio servers this skeleton comes from, results are not
// strictly file-mediated. A transcript is text, and making an agent read a file
// to see three lines of it wastes a round trip. Short transcripts come back
// inline; long ones come back as a path plus an excerpt. See resultFor.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/nlink-jp/voice-scribe/internal/mcp/job"
	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
	"github.com/nlink-jp/voice-scribe/internal/mcp/workdir"
	"github.com/nlink-jp/voice-scribe/internal/mcp/workspace"
	"github.com/nlink-jp/voice-scribe/internal/transcript"
)

// Transcriber turns one recording into a transcript. It is an interface so
// tests can supply a fake and the protocol tests run under the plain (no-cgo)
// build; the production implementation loads whisper and, when asked, the
// diarization models.
//
// Transcribe is NOT safe for concurrent use — whisper keeps decoding state on
// its context, and two calls would also load gigabytes of model at once. The
// job manager serialises calls through a single worker.
type Transcriber interface {
	Transcribe(ctx context.Context, req Request, report func(fraction float64, message string)) (transcript.Result, error)
}

// Request is the engine-neutral transcription request. Paths are absolute and
// already verified to be regular files inside the workspace.
type Request struct {
	// Audio is the recording to transcribe.
	Audio string
	// Model is an installed model name; empty means resolve from Language and
	// the configured default.
	Model string
	// Language is an ISO 639-1 code; empty means detect.
	Language string
	// Translate additionally produces English, which costs a second decode.
	Translate bool
	// Prompt biases the decoder's vocabulary.
	Prompt string
	// VAD gates silent stretches through the voice-activity model, which the
	// wiring resolves to a model path the same way the CLI does — including
	// honouring the config default (ADR-0009).
	VAD bool
	// OffsetSec and DurationSec restrict the run to a slice of the audio.
	OffsetSec   float64
	DurationSec float64

	// Diarize labels who is speaking, which needs the diarization models.
	Diarize bool
	// Speakers pins the speaker count; zero works it out.
	Speakers int
	// SpeakerThreshold tunes clustering when the count is not pinned.
	SpeakerThreshold float64
	// SpeakerHints replaces A/B/C, in order of first appearance.
	SpeakerHints []string
}

// ModelLister returns the installed and/or catalog model views for the given
// scope ("installed"|"catalog"|"all"). It is injected rather than importing
// internal/cli, which would be an import cycle; the bootstrap wires in the same
// views that back `models list --json`.
type ModelLister func(scope string) (any, error)

// Deps carries the shared dependencies of all tools.
type Deps struct {
	// WS materializes workspaces under the caller's work directory.
	WS *workspace.Manager
	// WorkDir resolves and validates the per-call work directory. The zero
	// value works; Denied names directories this server refuses to write
	// into on a caller's say-so.
	WorkDir workdir.Resolver
	// Transcribe performs the actual work (real engine or a test fake).
	Transcribe Transcriber
	// ListModels backs the list_models tool.
	ListModels ModelLister
	// Jobs tracks background transcriptions via a single FIFO worker.
	Jobs *job.Manager
	// MaxBytes caps how much of the transcript a result carries. It bounds
	// the response and nothing else — the transcript file is written either
	// way. Zero uses DefaultMaxBytes; a negative value means no cap.
	MaxBytes int
	// Logger is optional.
	Logger *slog.Logger
}

// Register attaches all tools to the MCP server.
func Register(srv *mcpserver.Server, d *Deps) {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Jobs == nil {
		d.Jobs = job.NewManager(context.Background())
	}
	registerGetUsage(srv, d)
	registerTranscribe(srv, d)
	registerCheckJob(srv, d)
	registerListModels(srv, d)
}

// retiredWorkDirNames are the spellings the work directory argument carried
// across the fleet before org ADR-021 settled on work_dir.
var retiredWorkDirNames = []string{"workspace_root", "workspaceRoot", "workspace_dir"}

// unmarshalStrict decodes tool arguments, rejecting unknown fields so agent
// typos surface as invalid_arguments instead of being silently ignored.
//
// A caller sending one of the retired work-directory spellings is told the new
// name rather than left to guess from "unknown field": the rename is ours, and
// a caller working from an older manual should need one turn to recover, not a
// schema re-read.
func unmarshalStrict(args json.RawMessage, into any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		msg := err.Error()
		for _, old := range retiredWorkDirNames {
			if strings.Contains(msg, `unknown field "`+old+`"`) {
				return toolerr.Newf(toolerr.CodeWorkDirRequired,
					"%q was renamed to work_dir: pass the absolute path of a directory you can read back", old)
			}
		}
		return toolerr.Newf(toolerr.CodeInvalidArguments, "invalid arguments: %v", err)
	}
	return nil
}
