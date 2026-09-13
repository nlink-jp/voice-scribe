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
		WS:         workspace.NewManager(),
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
		"audio":    "meeting.m4a",
		"work_dir": h.root,
		"format":   "text",
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

// TestTranscriptPastTheCapIsCountedNotSwapped is the replacement for the old
// two-tier test (ADR-0011 withdrew the delivery-mode switch): a transcript past
// the cap still comes back as text — as much as the cap allows — and what was
// left out is counted rather than replaced by a preview.
func TestTranscriptPastTheCapIsCountedNotSwapped(t *testing.T) {
	h := newHarness(t)
	// Comfortably past DefaultMaxBytes; "長い行です。" is 18 bytes.
	h.fake.result = transcriptOf(strings.Repeat("長い行です。", 8000))

	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":    "meeting.m4a",
		"work_dir": h.root,
		"format":   "text",
	}))
	res := st.Result.(Result)

	if !res.Truncated {
		t.Fatalf("a %d-byte transcript was returned whole", res.Bytes)
	}
	if res.Text == "" {
		t.Error("a capped result still carries text; it is not swapped for a preview")
	}
	if len(res.Text) > DefaultMaxBytes+16 {
		t.Errorf("text is %d bytes, want at most about %d", len(res.Text), DefaultMaxBytes)
	}
	if res.OmittedBytes != res.Bytes-len(res.Text) {
		t.Errorf("omitted_bytes = %d, want %d — the count must be exact",
			res.OmittedBytes, res.Bytes-len(res.Text))
	}
	if res.Note == "" {
		t.Error("the drop must be stated in words too")
	}
	if _, err := os.ReadFile(filepath.Join(h.wsDir, res.Path)); err != nil {
		t.Errorf("transcript was not written: %v", err)
	}
}

func TestMaxBytesIsOverridablePerCall(t *testing.T) {
	h := newHarness(t)
	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":     "meeting.m4a",
		"work_dir":  h.root,
		"max_bytes": 1,
	}))

	if res := st.Result.(Result); !res.Truncated {
		t.Error("max_bytes=1 still returned the whole transcript")
	}
}

// Zero is the caller saying "no cap", which is not the same as saying nothing.
func TestMaxBytesZeroMeansNoCap(t *testing.T) {
	h := newHarness(t)
	h.fake.result = transcriptOf(strings.Repeat("長い行です。", 8000))

	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":     "meeting.m4a",
		"work_dir":  h.root,
		"format":    "text",
		"max_bytes": 0,
	}))
	res := st.Result.(Result)
	if res.Truncated || res.OmittedBytes != 0 {
		t.Errorf("max_bytes=0 must mean no cap, got truncated=%v omitted=%d", res.Truncated, res.OmittedBytes)
	}
	if len(res.Text) != res.Bytes {
		t.Errorf("text is %d bytes but the transcript is %d", len(res.Text), res.Bytes)
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
		"work_dir":         h.root,
		"model":            "kotoba-whisper-v2.2",
		"language":         "ja",
		"translate":        true,
		"prompt":           "voice-scribe",
		"vad":              true,
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
	if !got.VAD {
		t.Errorf("vad lost: %+v", got)
	}
	if !strings.HasSuffix(got.Audio, filepath.Join("default", "meeting.m4a")) {
		t.Errorf("Audio = %q, want an absolute path inside the workspace", got.Audio)
	}
}

// TestRelativePathsAreConfinedToTheWorkspace covers the containment boundary
// from the argument side; workspace's own tests cover the kernel-enforced half.
func TestRelativePathsAreConfinedToTheWorkspace(t *testing.T) {
	h := newHarness(t)
	err := h.callErr(t, "transcribe", map[string]any{
		"audio":    "../../etc/passwd",
		"work_dir": h.root,
	})
	if !isCode(err, toolerr.CodePathNotAllowed) {
		t.Errorf("err = %v, want path_not_allowed", err)
	}
}

// An absolute recording is read where it lies rather than staged: the caller
// could have read it itself, and copying an hour of audio in to transcribe it
// would be pure waste (org ADR-021 §7).
func TestAnAbsoluteRecordingIsReadInPlace(t *testing.T) {
	h := newHarness(t)
	outside := filepath.Join(t.TempDir(), "interview.m4a")
	if err := os.WriteFile(outside, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := h.await(t, h.call(t, "transcribe", map[string]any{
		"audio":    outside,
		"work_dir": h.root,
		"format":   "text",
	}))
	if st.State != job.StateDone {
		t.Fatalf("job state %s: %v", st.State, st.Error)
	}
	// The resolved spelling is what the decoder gets: resolution happens once,
	// at the boundary, and the rest of the server works with the real path.
	want, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	if h.fake.seen.Audio != want {
		t.Errorf("decoder was handed %q, want the recording where it lies (%q)", h.fake.seen.Audio, want)
	}
	res := st.Result.(Result)
	// The transcript still lands in the workspace, named after the recording.
	if res.Path != filepath.Join("output", "interview.text") {
		t.Errorf("Path = %q, want it under output/ named after the recording", res.Path)
	}
}

// The floor under that: a recording named in a credential location is refused,
// whichever way it is spelled.
func TestAnAbsoluteRecordingInACredentialLocationIsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(ssh, "notes.m4a")
	if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t)
	err := h.callErr(t, "transcribe", map[string]any{
		"audio":    secret,
		"work_dir": h.root,
	})
	if !isCode(err, toolerr.CodePathNotAllowed) {
		t.Errorf("err = %v, want path_not_allowed", err)
	}
}

func TestMissingRecordingIsReportedBeforeAnyJobStarts(t *testing.T) {
	h := newHarness(t)
	err := h.callErr(t, "transcribe", map[string]any{
		"audio":    "absent.m4a",
		"work_dir": h.root,
	})
	if !isCode(err, toolerr.CodeInputNotFound) {
		t.Errorf("err = %v, want input_not_found", err)
	}
}

func TestUnknownArgumentsAreRejected(t *testing.T) {
	h := newHarness(t)
	err := h.callErr(t, "transcribe", map[string]any{
		"audio":    "meeting.m4a",
		"work_dir": h.root,
		"langauge": "ja",
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
				"audio":    "meeting.m4a",
				"work_dir": h.root,
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
		"audio":    "meeting.m4a",
		"work_dir": h.root,
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

// TestMissingAudioErrorNamesThePathAndTheEscape is the regression for what a
// real agent did with the old message. Told only "place it in the workspace",
// it invented ~/sessions/current_session/work, was denied, read get_usage,
// re-made the recording and finally passed an absolute path — four rounds to
// recover from one sentence that named nothing.
func TestMissingAudioErrorNamesThePathAndTheEscape(t *testing.T) {
	h := newHarness(t)

	// The check happens on the call that supplied the argument, not in the
	// job: a recording that is not there cannot become there later.
	err := h.callErr(t, "transcribe", map[string]any{
		"audio":    "not-there.m4a",
		"work_dir": h.root,
	})
	msg := err.Error()
	// The absolute path it looked at, so the agent can put the file there.
	if !strings.Contains(msg, filepath.Join(h.wsDir, "not-there.m4a")) {
		t.Errorf("error does not name the path it looked for: %q", msg)
	}
	// The escape, so the agent can point at the file it already has.
	if !strings.Contains(msg, "absolute path") {
		t.Errorf("error does not offer the absolute-path escape: %q", msg)
	}
}
