package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
	"github.com/nlink-jp/voice-scribe/internal/transcript"
)

// The manual is the only thing a client reads before operating this server, so
// it drifting away from the code is a real failure, not a documentation nit.
// These tests pin the parts that can drift silently.

func TestUsageDocumentsEveryTool(t *testing.T) {
	h := newHarness(t)
	for _, tool := range h.srv.Tools() {
		if !strings.Contains(usageMarkdown, "`"+tool.Name+"`") {
			t.Errorf("tool %q is registered but never mentioned in usage.md", tool.Name)
		}
	}
}

func TestUsageDocumentsEveryErrorCode(t *testing.T) {
	// Every code a tool can actually return has to appear in the recovery
	// table; a client that hits an undocumented code has nowhere to look.
	for _, code := range []string{
		toolerr.CodeMissingArgument,
		toolerr.CodeInvalidArguments,
		toolerr.CodePathNotAllowed,
		toolerr.CodeInputNotFound,
		toolerr.CodeDecodeFailed,
		toolerr.CodeModelNotFound,
		toolerr.CodeNoRuntime,
		toolerr.CodeDiarizeFailed,
		toolerr.CodeEmptyTranscript,
		toolerr.CodeTranscribeFailed,
		toolerr.CodeJobNotFound,
		toolerr.CodeInvalidScope,
	} {
		if !strings.Contains(usageMarkdown, "`"+code+"`") {
			t.Errorf("error code %q is not in the usage recovery table", code)
		}
	}
}

// TestUsageDocumentsEveryTranscribeArgument is the one most likely to rot: a
// new argument gets added to the schema and nobody updates the manual.
func TestUsageDocumentsEveryTranscribeArgument(t *testing.T) {
	h := newHarness(t)

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	for _, tool := range h.srv.Tools() {
		if tool.Name != "transcribe" {
			continue
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("transcribe input schema is not valid JSON: %v", err)
		}
	}
	if len(schema.Properties) == 0 {
		t.Fatal("transcribe declares no properties")
	}

	for name := range schema.Properties {
		if !strings.Contains(usageMarkdown, name) {
			t.Errorf("transcribe argument %q is in the schema but not in usage.md", name)
		}
	}
}

func TestUsageDocumentsEveryOutputFormat(t *testing.T) {
	for _, f := range transcript.Formats() {
		if !strings.Contains(usageMarkdown, "`"+string(f)+"`") {
			t.Errorf("output format %q is supported but not in usage.md", f)
		}
	}
}

// TestEveryToolSchemaIsValidAndClosed keeps agent typos surfacing as
// invalid_arguments rather than being silently ignored.
func TestEveryToolSchemaIsValidAndClosed(t *testing.T) {
	h := newHarness(t)
	for _, tool := range h.srv.Tools() {
		t.Run(tool.Name, func(t *testing.T) {
			var schema struct {
				Type                 string `json:"type"`
				AdditionalProperties *bool  `json:"additionalProperties"`
			}
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Fatalf("input schema is not valid JSON: %v", err)
			}
			if schema.Type != "object" {
				t.Errorf("schema type = %q, want object", schema.Type)
			}
			if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
				t.Error("schema should set additionalProperties:false so typos are caught")
			}
			if tool.Description == "" {
				t.Error("tool has no description; it is all a client sees before calling")
			}
		})
	}
}

// TestInstructionsPointAtGetUsage: the initialize hint is the only text a
// client is guaranteed to see, so it has to name the tool that explains the rest.
func TestInstructionsPointAtGetUsage(t *testing.T) {
	if !strings.Contains(Instructions, "get_usage") {
		t.Error("instructions do not mention get_usage, so the manual is undiscoverable")
	}
}

// TestUsageDoesNotPromiseModelDownloads pins a deliberate omission: pulling
// models is an operator decision at a terminal, and the manual says so.
func TestUsageDoesNotPromiseModelDownloads(t *testing.T) {
	if !strings.Contains(usageMarkdown, "models pull") {
		t.Error("usage.md should tell the reader how models get installed")
	}
	if !strings.Contains(usageMarkdown, "not exposed here") {
		t.Error("usage.md should say plainly that downloading is not available over MCP")
	}
}
