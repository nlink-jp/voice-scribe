package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nlink-jp/voice-scribe/internal/mcp/job"
	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/toolerr"
	"github.com/nlink-jp/voice-scribe/internal/mcp/transport"
	"github.com/nlink-jp/voice-scribe/internal/mcp/workspace"
	"github.com/nlink-jp/voice-scribe/internal/transcript"
)

// fakeTranscriber stands in for the real engine so the protocol and plumbing
// are testable under the plain (no-cgo) build, with no model on disk.
type fakeTranscriber struct {
	result transcript.Result
	err    error
	seen   Request
}

func (f *fakeTranscriber) Transcribe(ctx context.Context, req Request, report func(float64, string)) (transcript.Result, error) {
	f.seen = req
	report(0.5, "halfway")
	if f.err != nil {
		return transcript.Result{}, f.err
	}
	return f.result, nil
}

func transcriptOf(lines ...string) transcript.Result {
	r := transcript.Result{
		Metadata: transcript.Metadata{Source: "meeting.m4a", Model: "test-model", Languages: []string{"ja"}},
	}
	for i, line := range lines {
		r.Segments = append(r.Segments, transcript.Segment{
			Start: float64(i) * 5, End: float64(i)*5 + 5,
			Speaker: transcript.SingleSpeaker,
			Text:    map[string]string{"ja": line},
		})
	}
	r.Normalize()
	return r
}

type harness struct {
	srv   *mcpserver.Server
	deps  *Deps
	fake  *fakeTranscriber
	root  string
	wsDir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	root := t.TempDir()
	fake := &fakeTranscriber{result: transcriptOf("こんにちは。", "本日はテストです。")}
	deps := &Deps{
		WS:         workspace.NewManager(filepath.Join(root, "default-root")),
		Transcribe: fake,
		Jobs:       job.NewManager(context.Background()),
		ListModels: func(scope string) (any, error) { return map[string]any{"scope": scope}, nil },
	}

	srv := mcpserver.New("voice-scribe", "test", transport.NewStdioTransport(strings.NewReader(""), os.Stderr), nil)
	Register(srv, deps)

	wsDir := filepath.Join(root, "ws", "default")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "meeting.m4a"), []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &harness{srv: srv, deps: deps, fake: fake, root: filepath.Join(root, "ws"), wsDir: wsDir}
}

func (h *harness) call(t *testing.T, name string, args map[string]any) any {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.srv.Call(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return got
}

func (h *harness) callErr(t *testing.T, name string, args map[string]any) error {
	t.Helper()
	raw, _ := json.Marshal(args)
	_, err := h.srv.Call(context.Background(), name, raw)
	if err == nil {
		t.Fatalf("%s succeeded, want an error", name)
	}
	return err
}

// await drains a submitted job to completion.
func (h *harness) await(t *testing.T, submitted any) job.Status {
	t.Helper()
	m, ok := submitted.(map[string]any)
	if !ok {
		t.Fatalf("submission returned %T, want a map with job_id", submitted)
	}
	id, _ := m["job_id"].(string)
	if id == "" {
		t.Fatalf("submission carried no job_id: %v", m)
	}

	// Poll the way a client would, with a deadline. A tight loop without a
	// sleep can outrun the worker goroutine and report "never finished" for a
	// job that simply had not been scheduled yet.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st, err := h.deps.Jobs.Get(id)
		if err != nil {
			t.Fatalf("check_job: %v", err)
		}
		if st.State == job.StateDone || st.State == job.StateError {
			return st
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("job never finished within the deadline")
	return job.Status{}
}

func TestTranscribeWritesTheTranscriptAndReturnsItInline(t *testing.T) {
	h := newHarness(t)
	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":          "meeting.m4a",
		"workspace_root": h.root,
		"format":         "text",
	}))

	if st.State != job.StateDone {
		t.Fatalf("job state %s: %v", st.State, st.Error)
	}
	res, ok := st.Result.(Result)
	if !ok {
		t.Fatalf("result is %T, want Result", st.Result)
	}

	if res.Truncated {
		t.Error("a two-line transcript was reported as truncated")
	}
	if !strings.Contains(res.Text, "こんにちは。") {
		t.Errorf("inline text missing the transcript: %q", res.Text)
	}
	if res.Path != filepath.Join("output", "meeting.text") {
		t.Errorf("Path = %q, want it under output/", res.Path)
	}

	// The file is written whether or not the text came back inline.
	onDisk, err := os.ReadFile(filepath.Join(h.wsDir, res.Path))
	if err != nil {
		t.Fatalf("transcript was not written: %v", err)
	}
	if string(onDisk) != res.Text {
		t.Error("the file and the inline text disagree")
	}
}

// TestLongTranscriptsComeBackAsAPath is the other half of the two-tier
// contract: past the threshold the agent gets a pointer and a taste, not the
// whole thing crowding out its context.
func TestLongTranscriptsComeBackAsAPath(t *testing.T) {
	h := newHarness(t)
	// Comfortably past DefaultInlineThreshold; "長い行です。" is 18 bytes.
	h.fake.result = transcriptOf(strings.Repeat("長い行です。", 800))

	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":          "meeting.m4a",
		"workspace_root": h.root,
		"format":         "text",
	}))
	res := st.Result.(Result)

	if !res.Truncated {
		t.Fatalf("a %d-byte transcript was returned inline", res.Bytes)
	}
	if res.Text != "" {
		t.Error("a truncated result should not also carry the full text")
	}
	if res.Excerpt == "" {
		t.Error("a truncated result carries no excerpt, so the agent cannot tell what it got")
	}
	if len(res.Excerpt) > excerptLimit+16 {
		t.Errorf("excerpt is %d bytes, want about %d", len(res.Excerpt), excerptLimit)
	}
	if _, err := os.ReadFile(filepath.Join(h.wsDir, res.Path)); err != nil {
		t.Errorf("transcript was not written: %v", err)
	}
}

func TestInlineThresholdIsOverridablePerCall(t *testing.T) {
	h := newHarness(t)
	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":            "meeting.m4a",
		"workspace_root":   h.root,
		"inline_threshold": 1,
	}))

	if res := st.Result.(Result); !res.Truncated {
		t.Error("inline_threshold=1 still returned the transcript inline")
	}
}

// TestExcerptDoesNotSplitARune guards the audience this tool exists for: a
// byte-wise cut through Japanese text produces replacement characters.
func TestExcerptDoesNotSplitARune(t *testing.T) {
	japanese := strings.Repeat("あ", 500)
	for n := 1; n < 40; n++ {
		got := excerpt(japanese, n)
		if strings.ContainsRune(got, '�') {
			t.Fatalf("excerpt(%d) split a rune: %q", n, got)
		}
	}
}

func TestTranscribeArgumentsReachTheEngine(t *testing.T) {
	h := newHarness(t)
	h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":            "meeting.m4a",
		"workspace_root":   h.root,
		"model":            "kotoba-whisper-v2.2",
		"language":         "ja",
		"translate":        true,
		"prompt":           "voice-scribe",
		"diarize":          true,
		"speakers":         2,
		"speaker_hints":    []string{"田中", "佐藤"},
		"offset_seconds":   1.5,
		"duration_seconds": 30.0,
	}))

	got := h.fake.seen
	if got.Model != "kotoba-whisper-v2.2" || got.Language != "ja" || !got.Translate {
		t.Errorf("model/language/translate lost: %+v", got)
	}
	if !got.Diarize || got.Speakers != 2 || len(got.SpeakerHints) != 2 {
		t.Errorf("diarization arguments lost: %+v", got)
	}
	if got.OffsetSec != 1.5 || got.DurationSec != 30 {
		t.Errorf("slice arguments lost: %+v", got)
	}
	if !strings.HasSuffix(got.Audio, filepath.Join("default", "meeting.m4a")) {
		t.Errorf("Audio = %q, want an absolute path inside the workspace", got.Audio)
	}
}

// TestPathsAreConfinedToTheWorkspace covers the containment boundary from the
// argument side; workspace's own tests cover the kernel-enforced half.
func TestPathsAreConfinedToTheWorkspace(t *testing.T) {
	h := newHarness(t)
	for name, audio := range map[string]string{
		"absolute": "/etc/passwd",
		"escaping": "../../etc/passwd",
	} {
		t.Run(name, func(t *testing.T) {
			err := h.callErr(t, "transcribe", map[string]any{
				"audio":          audio,
				"workspace_root": h.root,
			})
			if !isCode(err, toolerr.CodePathNotAllowed) {
				t.Errorf("err = %v, want path_not_allowed", err)
			}
		})
	}
}

func TestMissingRecordingIsReportedBeforeAnyJobStarts(t *testing.T) {
	h := newHarness(t)
	err := h.callErr(t, "transcribe", map[string]any{
		"audio":          "absent.m4a",
		"workspace_root": h.root,
	})
	if !isCode(err, toolerr.CodeInputNotFound) {
		t.Errorf("err = %v, want input_not_found", err)
	}
}

func TestUnknownArgumentsAreRejected(t *testing.T) {
	h := newHarness(t)
	err := h.callErr(t, "transcribe", map[string]any{
		"audio":          "meeting.m4a",
		"workspace_root": h.root,
		"langauge":       "ja",
	})
	if !isCode(err, toolerr.CodeInvalidArguments) {
		t.Errorf("err = %v, want invalid_arguments for a mistyped argument", err)
	}
}

func TestEngineFailuresGetStableCodes(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"no runtime":   {errors.New("transcription runtime not linked; build with `make build-engine`"), toolerr.CodeNoRuntime},
		"no model":     {errors.New(`model "x" is not installed (run ` + "`voice-scribe models pull x`)"), toolerr.CodeModelNotFound},
		"bad audio":    {errors.New("file has no audio track"), toolerr.CodeDecodeFailed},
		"no diarizers": {errors.New("--diarize needs two models"), toolerr.CodeDiarizeFailed},
		"anything":     {errors.New("something went wrong"), toolerr.CodeTranscribeFailed},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.fake.err = tc.err
			st := h.await(t, h.call(t, "transcribe", map[string]any{
				"audio":          "meeting.m4a",
				"workspace_root": h.root,
			}))
			if st.State != job.StateError {
				t.Fatalf("state = %s, want error", st.State)
			}
			if st.Error == nil || st.Error.Code != tc.want {
				t.Errorf("code = %v, want %s", st.Error, tc.want)
			}
		})
	}
}

func TestSilentRecordingIsItsOwnError(t *testing.T) {
	h := newHarness(t)
	h.fake.result = transcript.Result{Metadata: transcript.Metadata{Model: "m", Languages: []string{"ja"}}}

	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":          "meeting.m4a",
		"workspace_root": h.root,
	}))
	if st.Error == nil || st.Error.Code != toolerr.CodeEmptyTranscript {
		t.Errorf("code = %v, want empty_transcript", st.Error)
	}
}

func TestListModelsValidatesScope(t *testing.T) {
	h := newHarness(t)
	h.call(t, "list_models", map[string]any{})
	h.call(t, "list_models", map[string]any{"scope": "catalog"})

	err := h.callErr(t, "list_models", map[string]any{"scope": "everything"})
	if !isCode(err, toolerr.CodeInvalidScope) {
		t.Errorf("err = %v, want invalid_scope", err)
	}
}

func TestCheckJobRequiresAnID(t *testing.T) {
	h := newHarness(t)
	if err := h.callErr(t, "check_job", map[string]any{"job_id": ""}); !isCode(err, toolerr.CodeMissingArgument) {
		t.Errorf("err = %v, want missing_argument", err)
	}
	if err := h.callErr(t, "check_job", map[string]any{"job_id": "nope"}); !isCode(err, toolerr.CodeJobNotFound) {
		t.Errorf("err = %v, want job_not_found", err)
	}
}

func isCode(err error, code string) bool {
	var te *toolerr.Error
	return errors.As(err, &te) && te.Code == code
}
