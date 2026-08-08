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

	"github.com/nlink-jp/voice-scribe/internal/mcp/job"
	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
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
	// WS manages workspaces (default root + agent-prepared roots).
	WS *workspace.Manager
	// Transcribe performs the actual work (real engine or a test fake).
	Transcribe Transcriber
	// ListModels backs the list_models tool.
	ListModels ModelLister
	// Jobs tracks background transcriptions via a single FIFO worker.
	Jobs *job.Manager
	// InlineThreshold is the transcript size in bytes at or below which the
	// text is returned inline instead of only as a path. Zero uses
	// DefaultInlineThreshold.
	InlineThreshold int
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

// unmarshalStrict decodes tool arguments, rejecting unknown fields so agent
// typos surface as invalid_arguments instead of being silently ignored.
func unmarshalStrict(args json.RawMessage, into any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return toolerr.Newf(toolerr.CodeInvalidArguments, "invalid arguments: %v", err)
	}
	return nil
}
