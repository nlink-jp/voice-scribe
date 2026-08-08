// Package download fetches model weights over HTTP.
//
// Model files run to hundreds of megabytes, so a dropped connection partway
// through is a normal event rather than an exceptional one. Every fetch resumes
// from what is already on disk and retries with backoff, because the
// alternative — starting a 574 MB download again from zero — is what makes a
// tool feel broken on a bad network.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Progress reports bytes transferred out of a total. total is zero when the
// server does not say how large the file is.
type Progress func(done, total int64)

// Options control one fetch.
type Options struct {
	// Attempts is the number of tries, including the first. Zero means 5.
	Attempts int
	// Client is the HTTP client to use; nil means a default with no timeout,
	// since a large model over a slow link legitimately takes many minutes.
	Client *http.Client
	// OnProgress, when set, is called as bytes arrive.
	OnProgress Progress
	// ExpectedSize, when non-zero, is checked against the finished file. A
	// truncated model loads with a confusing runtime error rather than an
	// obvious one, so it is worth catching here.
	ExpectedSize int64
	// ExpectedSHA256, when non-empty, is checked against the finished file.
	//
	// Size alone is not integrity: an attacker who can substitute the file can
	// trivially preserve its length. It matters here specifically because
	// whisper parses these files, and upstream has already fixed a
	// stack-buffer-overflow reachable from a malformed tensor header — a
	// tampered model is a memory-safety problem, not just a wrong answer.
	ExpectedSHA256 string
}

func (o Options) attempts() int {
	if o.Attempts <= 0 {
		return 5
	}
	return o.Attempts
}

func (o Options) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{}
}

// ErrIncomplete reports a download that finished at the wrong size.
var ErrIncomplete = errors.New("downloaded file is incomplete")

// ErrChecksumMismatch reports a download whose content does not match the
// expected hash. Treat it as hostile until proven otherwise: it means the bytes
// served are not the bytes the catalog was built against.
var ErrChecksumMismatch = errors.New("downloaded file does not match its expected checksum")

// Fetch downloads url to dest, resuming a partial file if one is present.
//
// The download goes to a .part sibling and is renamed on success, so an
// interrupted run never leaves something that looks like a usable model.
func Fetch(ctx context.Context, url, dest string, opts Options) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	part := dest + ".part"
	var lastErr error

	for attempt := 1; attempt <= opts.attempts(); attempt++ {
		if attempt > 1 {
			delay := time.Duration(1<<uint(attempt-2)) * time.Second
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := fetchOnce(ctx, url, part, opts)
		if err == nil {
			lastErr = nil
			break
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		lastErr = err
	}
	if lastErr != nil {
		return fmt.Errorf("after %d attempts: %w", opts.attempts(), lastErr)
	}

	if opts.ExpectedSize > 0 {
		info, err := os.Stat(part)
		if err != nil {
			return err
		}
		if info.Size() != opts.ExpectedSize {
			return fmt.Errorf("%w: got %d bytes, expected %d", ErrIncomplete, info.Size(), opts.ExpectedSize)
		}
	}

	// The checksum is verified before the rename, so a file that fails it never
	// exists at the destination path even briefly.
	if opts.ExpectedSHA256 != "" {
		sum, err := SHA256File(part)
		if err != nil {
			return err
		}
		if !strings.EqualFold(sum, opts.ExpectedSHA256) {
			os.Remove(part)
			return fmt.Errorf("%w: got %s, expected %s (the file served is not the one this build was pinned to; do not use it)",
				ErrChecksumMismatch, sum, opts.ExpectedSHA256)
		}
	}

	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("finalise download: %w", err)
	}
	return nil
}

func fetchOnce(ctx context.Context, url, part string, opts Options) error {
	var have int64
	if info, err := os.Stat(part); err == nil {
		have = info.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}

	resp, err := opts.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusOK:
		// The server ignored the range (or there was nothing to resume), so
		// what follows is the whole file and anything already written is stale.
		have = 0
		flags |= os.O_TRUNC
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusRequestedRangeNotSatisfiable:
		// Already complete: the local file covers the whole resource.
		return nil
	default:
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	total := resp.ContentLength
	if total > 0 {
		total += have
	}

	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := io.Writer(f)
	if opts.OnProgress != nil {
		w = &progressWriter{w: f, done: have, total: total, report: opts.OnProgress}
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return err
	}
	return f.Sync()
}

// SHA256File hashes a file in a streaming pass, so a gigabyte model does not
// have to be held in memory to be checked. It is exported because the same
// check has to happen on the path that reuses an already-present file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressWriter struct {
	w      io.Writer
	done   int64
	total  int64
	report Progress
	last   time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)

	// Rate-limit the callback: a terminal redrawing for every 32 KB chunk of a
	// 574 MB file spends more time on output than on the download.
	if now := time.Now(); now.Sub(p.last) > 200*time.Millisecond {
		p.last = now
		p.report(p.done, p.total)
	}
	return n, err
}
