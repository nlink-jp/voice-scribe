package tools

import (
	"context"
	"encoding/json"

	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
)

func registerListModels(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "list_models",
		Description: "List models: what is installed, what the catalog offers, or both. " +
			"Downloading is deliberately not exposed here — models run to hundreds of megabytes, " +
			"so `voice-scribe models pull <name>` is a decision for the operator at a terminal. " +
			"Use this to find out what is already available, and to name the model in a transcribe call.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "scope": {"type": "string", "enum": ["installed", "catalog", "all"], "description": "Default installed"}
  },
  "additionalProperties": false
}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in struct {
			Scope string `json:"scope"`
		}
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		if in.Scope == "" {
			in.Scope = "installed"
		}
		switch in.Scope {
		case "installed", "catalog", "all":
		default:
			return nil, toolerr.Newf(toolerr.CodeInvalidScope,
				"scope %q is not one of installed, catalog, all", in.Scope)
		}
		if d.ListModels == nil {
			return nil, toolerr.New(toolerr.CodeWorkspaceFailed, "model listing is not wired up")
		}
		return d.ListModels(in.Scope)
	})
}
