package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"

	"github.com/nlink-jp/voice-scribe/internal/mcp/job"
	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
	"github.com/nlink-jp/voice-scribe/internal/mcp/workspace"
	"github.com/nlink-jp/voice-scribe/internal/transcript"
)

func registerTranscribe(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "transcribe",
		Description: "Transcribe a recording that is already in the workspace. Returns a job_id immediately; " +
			"poll it with check_job. When the job finishes, a short transcript comes back inline and a long " +
			"one comes back as a path with an excerpt — the file is written either way. Optionally labels " +
			"who is speaking (diarize) and adds an English translation (translate).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "required": ["audio"],
  "properties": {
    "audio": {"type": "string", "description": "Recording path, relative to the workspace"},
    "workspace_root": {"type": "string", "description": "Absolute path of an agent-prepared workspace root"},
    "workspace_id": {"type": "string", "description": "Workspace within the root; defaults to \"default\""},
    "model": {"type": "string", "description": "Installed model name; omit to pick one from language"},
    "language": {"type": "string", "description": "ISO 639-1 code; omit to detect"},
    "translate": {"type": "boolean", "description": "Also produce English; costs a second decode"},
    "prompt": {"type": "string", "description": "Bias the decoder's vocabulary with proper nouns and jargon"},
    "offset_seconds": {"type": "number", "minimum": 0},
    "duration_seconds": {"type": "number", "minimum": 0},
    "format": {"type": "string", "enum": ["json", "text", "md", "srt", "vtt"], "description": "Default json"},
    "output": {"type": "string", "description": "Transcript path relative to the workspace; defaults under output/"},
    "diarize": {"type": "boolean", "description": "Label who is speaking; needs the diarization models"},
    "speakers": {"type": "integer", "minimum": 0, "description": "Pin the speaker count when known"},
    "speaker_threshold": {"type": "number", "minimum": 0, "description": "Clustering distance when the count is unknown"},
    "speaker_hints": {"type": "array", "items": {"type": "string"}, "description": "Names replacing A/B/C, in order of first appearance"},
    "inline_threshold": {"type": "integer", "minimum": 0, "description": "Bytes at or below which the transcript is returned inline"}
  },
  "additionalProperties": false
}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in struct {
			Audio            string   `json:"audio"`
			WorkspaceRoot    string   `json:"workspace_root"`
			WorkspaceID      string   `json:"workspace_id"`
			Model            string   `json:"model"`
			Language         string   `json:"language"`
			Translate        bool     `json:"translate"`
			Prompt           string   `json:"prompt"`
			OffsetSeconds    float64  `json:"offset_seconds"`
			DurationSeconds  float64  `json:"duration_seconds"`
			Format           string   `json:"format"`
			Output           string   `json:"output"`
			Diarize          bool     `json:"diarize"`
			Speakers         int      `json:"speakers"`
			SpeakerThreshold float64  `json:"speaker_threshold"`
			SpeakerHints     []string `json:"speaker_hints"`
			InlineThreshold  int      `json:"inline_threshold"`
		}
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		if in.Audio == "" {
			return nil, toolerr.New(toolerr.CodeMissingArgument, "audio is required")
		}

		format := transcript.FormatJSON
		if in.Format != "" {
			f, err := transcript.ParseFormat(in.Format)
			if err != nil {
				return nil, toolerr.Newf(toolerr.CodeInvalidArguments, "%v", err)
			}
			format = f
		}

		if in.WorkspaceID == "" {
			in.WorkspaceID = "default"
		}
		ws, err := d.WS.EnsureIn(in.WorkspaceRoot, in.WorkspaceID)
		if err != nil {
			return nil, err
		}

		audioRel, err := ws.ResolveInside(in.Audio)
		if err != nil {
			return nil, err
		}
		// The decoder cannot inherit os.Root, so the containment check happens
		// here, immediately before the absolute path is handed over.
		if err := ws.VerifyRegular(audioRel); err != nil {
			return nil, err
		}

		outRel, err := resolveOutput(ws, in.Output, audioRel, string(format))
		if err != nil {
			return nil, err
		}

		threshold := in.InlineThreshold
		if threshold == 0 {
			threshold = d.InlineThreshold
		}

		req := Request{
			Audio:            ws.Path(audioRel),
			Model:            in.Model,
			Language:         in.Language,
			Translate:        in.Translate,
			Prompt:           in.Prompt,
			OffsetSec:        in.OffsetSeconds,
			DurationSec:      in.DurationSeconds,
			Diarize:          in.Diarize,
			Speakers:         in.Speakers,
			SpeakerThreshold: in.SpeakerThreshold,
			SpeakerHints:     in.SpeakerHints,
		}

		jobID := d.Jobs.Submit(func(ctx context.Context, report func(job.Progress)) (any, error) {
			result, err := d.Transcribe.Transcribe(ctx, req, func(fraction float64, message string) {
				report(job.Progress{Fraction: fraction, Message: message})
			})
			if err != nil {
				return nil, classify(err)
			}

			files, err := transcript.Render(result, format)
			if err != nil {
				if errors.Is(err, transcript.ErrEmpty) {
					return nil, toolerr.New(toolerr.CodeEmptyTranscript,
						"the recording produced no speech; it may be silent, or in a language the model does not handle")
				}
				return nil, toolerr.Newf(toolerr.CodeTranscribeFailed, "render transcript: %v", err)
			}

			// Subtitle formats split per language when a transcript carries
			// more than one. The first file is the primary; the rest are
			// written beside it and named in the result.
			var extra []string
			for i, f := range files {
				rel := withSuffix(outRel, f.Suffix)
				if err := ws.WriteFileAtomic(rel, []byte(f.Content)); err != nil {
					return nil, err
				}
				if i > 0 {
					extra = append(extra, rel)
				}
			}

			primary := withSuffix(outRel, files[0].Suffix)
			out := resultFor(primary, ws.Path(primary), string(format), files[0].Content, threshold, result)
			if len(extra) == 0 {
				return out, nil
			}
			return map[string]any{"transcript": out, "additional_files": extra}, nil
		})

		return describeJob(jobID, outRel), nil
	})
}

// resolveOutput picks where the transcript is written: the caller's choice, or
// output/<recording>.<ext> beside it.
func resolveOutput(ws *workspace.Workspace, requested, audioRel, format string) (string, error) {
	if requested != "" {
		return ws.ResolveInside(requested)
	}
	base := path.Base(audioRel)
	if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return ws.ResolveInside(path.Join(workspace.DirOutput, base+"."+format))
}

// withSuffix inserts a language tag before the extension, matching how the CLI
// names split subtitle files.
func withSuffix(rel, suffix string) string {
	if suffix == "" {
		return rel
	}
	ext := path.Ext(rel)
	return strings.TrimSuffix(rel, ext) + suffix + ext
}

// classify maps engine failures onto stable tool-error codes so an agent can
// branch on the cause rather than parse prose.
func classify(err error) error {
	var te *toolerr.Error
	if errors.As(err, &te) {
		return te
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not linked"):
		return toolerr.New(toolerr.CodeNoRuntime, msg)
	case strings.Contains(msg, "is not installed"), strings.Contains(msg, "models pull"):
		return toolerr.New(toolerr.CodeModelNotFound, msg)
	case strings.Contains(msg, "no audio track"), strings.Contains(msg, "unsupported container"),
		strings.Contains(msg, "no such file"):
		return toolerr.New(toolerr.CodeDecodeFailed, msg)
	case strings.Contains(msg, "--diarize"):
		return toolerr.New(toolerr.CodeDiarizeFailed, msg)
	default:
		return toolerr.New(toolerr.CodeTranscribeFailed, msg)
	}
}
