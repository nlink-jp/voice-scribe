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
	"leaves the machine. It is stateful and async: recordings live in a workspace directory you prepare, " +
	"transcribe returns a job_id which you poll with check_job, and a finished transcript comes back inline " +
	"when it is short and as a file path with an excerpt when it is long. It can also label who is speaking. " +
	"Call the get_usage tool before your first transcription to learn the workspace model, the transcribe " +
	"arguments, the job lifecycle, and the error recovery table."

func registerGetUsage(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "get_usage",
		Description: "Return this server's operating manual (markdown): the workspace model and workspace_root, " +
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
