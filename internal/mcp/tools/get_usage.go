package tools

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
)

// usageMarkdown is the client-neutral operating manual returned by get_usage.
// A stateful, async, workspace-scoped server is not something a client should
// have to work out by trial and error. Coherence with the real tools, error
// codes and schema is pinned by tools_test.go.
//
//go:embed usage.md
var usageMarkdown string

// Instructions is the short initialize-time hint that makes get_usage
// discoverable (surfaced via the MCP `instructions` field).
const Instructions = "voice-scribe transcribes recordings locally with whisper.cpp — no API key, and no audio " +
	"leaves the machine. Every call names work_dir: the absolute path of a directory you can read back " +
	"(your session or working directory). It is required and has no default, and the workspace is " +
	"<work_dir>/<workspace_id>/. It is stateful and async: transcribe returns a job_id which you poll with " +
	"check_job, and a finished transcript comes back inline when it is short and as a file path with an " +
	"excerpt when it is long. audio may be an absolute path to a recording anywhere you can read. " +
	"It can also label who is speaking. " +
	"Call the get_usage tool before your first transcription to learn the workspace model, the transcribe " +
	"arguments, the job lifecycle, and the error recovery table."

func registerGetUsage(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "get_usage",
		Description: "Return this server's operating manual (markdown): the work_dir contract and the workspace model, " +
			"the transcribe arguments, the async job lifecycle (transcribe -> job_id -> check_job), how " +
			"transcripts are returned, speaker diarization, and the error recovery table. " +
			"Call it once before your first transcription.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in struct{}
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		return mcpserver.RawResult{
			Content: []mcpserver.ContentBlock{{Type: "text", Text: usageMarkdown}},
		}, nil
	})
}
