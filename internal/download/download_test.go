package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var payload = []byte(strings.Repeat("model-weights-", 512))

// rangeServer serves payload with Range support, so resume can be exercised
// against something that behaves like Hugging Face rather than a mock.
func rangeServer(t *testing.T, onRequest func(r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onRequest != nil {
			onRequest(r)
		}
		// A zero modtime keeps ServeContent from emitting Last-Modified, so the
		// resume path is exercised without conditional-request interference.
		http.ServeContent(w, r, "model.bin", time.Time{}, strings.NewReader(string(payload)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchWritesTheWholeFile(t *testing.T) {
	srv := rangeServer(t, nil)
	dest := filepath.Join(t.TempDir(), "model.bin")

	if err := Fetch(context.Background(), srv.URL, dest, Options{}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded %d bytes, want %d", len(got), len(payload))
	}
}

// TestFetchLeavesNoPartialFileBehind is why the download goes to a .part
// sibling: a half-written file at the real path would load as a corrupt model.
func TestFetchLeavesNoPartialFileBehind(t *testing.T) {
	srv := rangeServer(t, nil)
	dir := t.TempDir()
	dest := filepath.Join(dir, "model.bin")

	if err := Fetch(context.Background(), srv.URL, dest, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Error(".part file survived a successful download")
	}
}

// TestFetchResumes is the behaviour that keeps a dropped connection from
// costing the whole transfer.
func TestFetchResumes(t *testing.T) {
	var ranges []string
	srv := rangeServer(t, func(r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
	})

	dir := t.TempDir()
	dest := filepath.Join(dir, "model.bin")

	// Pretend a previous run stopped a third of the way through.
	prefix := len(payload) / 3
	if err := os.WriteFile(dest+".part", payload[:prefix], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Fetch(context.Background(), srv.URL, dest, Options{}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("resumed download produced %d bytes, want %d", len(got), len(payload))
	}
	if len(ranges) == 0 || !strings.HasPrefix(ranges[0], "bytes=") {
		t.Errorf("no Range header sent; the partial file was re-downloaded from zero (%v)", ranges)
	}
}

// TestFetchRestartsWhenTheServerIgnoresRange covers the case where a resume is
// impossible: appending to the stale prefix would corrupt the file.
func TestFetchRestartsWhenTheServerIgnoresRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(dest+".part", []byte("stale prefix"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Fetch(context.Background(), srv.URL, dest, Options{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("stale prefix was kept; got %d bytes, want %d", len(got), len(payload))
	}
}

func TestFetchRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "model.bin")
	if err := Fetch(context.Background(), srv.URL, dest, Options{Attempts: 4}); err != nil {
		t.Fatalf("Fetch gave up on a transient failure: %v", err)
	}
	if calls.Load() < 3 {
		t.Errorf("server saw %d calls, want the retries", calls.Load())
	}
}

func TestFetchGivesUpAndSaysWhy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "model.bin")
	err := Fetch(context.Background(), srv.URL, dest, Options{Attempts: 2})
	if err == nil {
		t.Fatal("Fetch succeeded against a 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q should carry the server's status", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("a failed download left a file at the destination")
	}
}

// TestFetchDetectsTruncation matters because a short model file loads with an
// opaque runtime error rather than an obvious one.
func TestFetchDetectsTruncation(t *testing.T) {
	srv := rangeServer(t, nil)
	dest := filepath.Join(t.TempDir(), "model.bin")

	err := Fetch(context.Background(), srv.URL, dest, Options{ExpectedSize: int64(len(payload)) + 1})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("a truncated download was promoted to the destination path")
	}
}

func TestFetchReportsProgress(t *testing.T) {
	srv := rangeServer(t, nil)
	dest := filepath.Join(t.TempDir(), "model.bin")

	var lastDone, lastTotal int64
	err := Fetch(context.Background(), srv.URL, dest, Options{
		OnProgress: func(done, total int64) { lastDone, lastTotal = done, total },
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastTotal != 0 && lastTotal != int64(len(payload)) {
		t.Errorf("reported total %d, want %d or 0 when unknown", lastTotal, len(payload))
	}
	if lastDone < 0 {
		t.Errorf("reported %d bytes done", lastDone)
	}
}

func TestFetchHonoursContextCancellation(t *testing.T) {
	srv := rangeServer(t, nil)
	dest := filepath.Join(t.TempDir(), "model.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Fetch(ctx, srv.URL, dest, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
