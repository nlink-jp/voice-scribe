package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// TestFetchRejectsAContentMismatch is the supply-chain check. Size alone is not
// integrity: anyone able to substitute the file can preserve its length, and the
// substituted bytes then reach a parser that has had memory-safety bugs.
func TestFetchRejectsAContentMismatch(t *testing.T) {
	srv := rangeServer(t, nil)
	dest := filepath.Join(t.TempDir(), "model.bin")

	err := Fetch(context.Background(), srv.URL, dest, Options{
		ExpectedSize:   int64(len(payload)),
		ExpectedSHA256: strings.Repeat("00", 32),
	})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("a file failing its checksum was promoted to the destination path")
	}
	if _, statErr := os.Stat(dest + ".part"); !os.IsNotExist(statErr) {
		t.Error("the rejected download was left behind as a .part file")
	}
}

// TestFetchAcceptsTheRightContent pins the other direction, and that the check
// is case-insensitive about hex.
func TestFetchAcceptsTheRightContent(t *testing.T) {
	srv := rangeServer(t, nil)
	sum := sha256.Sum256(payload)

	for name, want := range map[string]string{
		"lowercase": hex.EncodeToString(sum[:]),
		"uppercase": strings.ToUpper(hex.EncodeToString(sum[:])),
	} {
		t.Run(name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "model.bin")
			if err := Fetch(context.Background(), srv.URL, dest, Options{ExpectedSHA256: want}); err != nil {
				t.Fatalf("Fetch rejected the correct content: %v", err)
			}
			if _, err := os.Stat(dest); err != nil {
				t.Errorf("verified download was not written: %v", err)
			}
		})
	}
}

// TestFetchCatchesASubstitutionThatKeepsTheSize is the attack this exists for,
// spelled out: same length, different bytes.
func TestFetchCatchesASubstitutionThatKeepsTheSize(t *testing.T) {
	tampered := make([]byte, len(payload))
	copy(tampered, payload)
	tampered[len(tampered)/2] ^= 0xFF

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tampered)
	}))
	t.Cleanup(srv.Close)

	sum := sha256.Sum256(payload)
	dest := filepath.Join(t.TempDir(), "model.bin")

	err := Fetch(context.Background(), srv.URL, dest, Options{
		ExpectedSize:   int64(len(payload)), // passes: the length is unchanged
		ExpectedSHA256: hex.EncodeToString(sum[:]),
	})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("a same-length substitution was accepted: %v", err)
	}
}

func TestSHA256File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SHA256File(path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(payload)
	if got != hex.EncodeToString(want[:]) {
		t.Errorf("SHA256File = %s, want %s", got, hex.EncodeToString(want[:]))
	}
}
