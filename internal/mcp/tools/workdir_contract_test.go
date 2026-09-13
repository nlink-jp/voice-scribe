package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
	"github.com/nlink-jp/voice-scribe/internal/mcp/workdir"
)

// The work-directory contract (org ADR-021, project ADR-0010) is a rule about
// every tool, not about one of them. Stated only in prose it is re-decided by
// whoever adds the next tool, so it is pinned here instead.

// TestNoToolSchemaCarriesARetiredWorkDirName is the arch test: one name for
// this argument across the fleet, and the old spellings never creep back.
func TestNoToolSchemaCarriesARetiredWorkDirName(t *testing.T) {
	h := newHarness(t)
	for _, tool := range h.srv.Tools() {
		for _, old := range retiredWorkDirNames {
			if strings.Contains(string(tool.InputSchema), `"`+old+`"`) {
				t.Errorf("tool %q declares %q; the name is work_dir", tool.Name, old)
			}
		}
	}
}

// TestWorkDirIsRequiredWhereverItIsDeclared: an optional work directory is an
// invitation to fall back to a server-owned default, which is the failure the
// contract removes.
func TestWorkDirIsRequiredWhereverItIsDeclared(t *testing.T) {
	h := newHarness(t)
	for _, tool := range h.srv.Tools() {
		var schema struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("%s: input schema is not valid JSON: %v", tool.Name, err)
		}
		_, declares := schema.Properties["work_dir"]
		if _, hasWorkspace := schema.Properties["workspace_id"]; hasWorkspace && !declares {
			t.Errorf("tool %q takes a workspace_id but never says which work_dir it lives in", tool.Name)
		}
		if !declares {
			continue
		}
		if !contains(schema.Required, "work_dir") {
			t.Errorf("tool %q declares work_dir but does not require it", tool.Name)
		}
	}
}

func TestTranscribeRequiresAWorkDir(t *testing.T) {
	h := newHarness(t)
	err := h.callErr(t, "transcribe", map[string]any{"audio": "meeting.m4a"})
	if !errors.Is(err, toolerr.New(toolerr.CodeWorkDirRequired, "")) {
		t.Fatalf("err = %v, want work_dir_required", err)
	}
}

// TestARetiredSpellingNamesTheNewOne: the rename is ours, so a caller working
// from an older manual should recover in one turn rather than re-reading the
// schema to guess what "unknown field" meant.
func TestARetiredSpellingNamesTheNewOne(t *testing.T) {
	h := newHarness(t)
	for _, old := range retiredWorkDirNames {
		err := h.callErr(t, "transcribe", map[string]any{"audio": "meeting.m4a", old: h.root})
		if !errors.Is(err, toolerr.New(toolerr.CodeWorkDirRequired, "")) {
			t.Errorf("%s: err = %v, want work_dir_required", old, err)
		}
		if !strings.Contains(err.Error(), "work_dir") {
			t.Errorf("%s: error does not name the new argument: %v", old, err)
		}
	}
}

// TestTranscribeTakesTheWorkDirFromRequestMeta covers the second channel: the
// runtimes we write ourselves set it on every tools/call, so the model does
// not have to remember an argument it cannot get wrong.
func TestTranscribeTakesTheWorkDirFromRequestMeta(t *testing.T) {
	h := newHarness(t)
	hint, err := json.Marshal(h.root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := mcpserver.WithRequestMeta(context.Background(),
		map[string]json.RawMessage{workdir.MetaKey: hint})

	args, err := json.Marshal(map[string]any{"audio": "meeting.m4a", "format": "text"})
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := h.srv.Call(ctx, "transcribe", args)
	if err != nil {
		t.Fatalf("transcribe with only a _meta work dir: %v", err)
	}

	ack, ok := submitted.(map[string]any)
	if !ok {
		t.Fatalf("submission returned %T", submitted)
	}
	if got := ack["work_dir"]; got != resolved(t, h.root) {
		t.Errorf("ack work_dir = %v, want %q", got, resolved(t, h.root))
	}

	st := h.await(t, submitted)
	res, ok := st.Result.(Result)
	if !ok {
		t.Fatalf("result is %T, want Result", st.Result)
	}
	if res.WorkDir != resolved(t, h.root) {
		t.Errorf("result work_dir = %q, want %q", res.WorkDir, resolved(t, h.root))
	}
	if res.WorkspaceID != "default" {
		t.Errorf("result workspace_id = %q", res.WorkspaceID)
	}
	if res.AbsolutePath != filepath.Join(resolved(t, h.wsDir), res.Path) {
		t.Errorf("absolute_path = %q, does not sit under the echoed work_dir", res.AbsolutePath)
	}
}

func contains(haystack []string, want string) bool {
	for _, s := range haystack {
		if s == want {
			return true
		}
	}
	return false
}

// resolved mirrors the symlink resolution the validator applies, so the
// assertions hold on darwin where the temporary directory is a symlink.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The initialize `instructions` field is the first thing the model reads about
// this server — before any tool list — so the contract has to survive there
// too. It did not: the string described "a workspace directory you prepare"
// and never named the argument, while every tool required it.
func TestInstructionsNameTheWorkDirContract(t *testing.T) {
	for _, want := range []string{"work_dir", "absolute", "required"} {
		if !strings.Contains(Instructions, want) {
			t.Errorf("the initialize instructions do not mention %q; a model that "+
				"reads only this will omit an argument every tool requires", want)
		}
	}
	for _, old := range retiredWorkDirNames {
		if strings.Contains(Instructions, old) {
			t.Errorf("the initialize instructions name %q; the name is work_dir", old)
		}
	}
}
