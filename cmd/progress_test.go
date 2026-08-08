package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProgressStaysReadableOffATerminal is the defect this file exists for: an
// in-place redraw against a pipe or a log file produced tens of kilobytes of
// carriage returns for a single model download.
func TestProgressStaysReadableOffATerminal(t *testing.T) {
	var buf strings.Builder
	p := newProgressReporter(&buf, false)

	p.stage("fetching %s", "model.bin")
	for pct := 0; pct <= 100; pct++ {
		p.percent(pct)
	}
	p.stage("loading %s", "model")
	p.done()

	out := buf.String()
	if strings.Contains(out, "\r") {
		t.Errorf("carriage returns emitted to a non-terminal:\n%q", out)
	}
	if lines := strings.Count(out, "\n"); lines != 2 {
		t.Errorf("got %d lines, want one per stage:\n%q", lines, out)
	}
	if !strings.Contains(out, "fetching model.bin") || !strings.Contains(out, "loading model") {
		t.Errorf("stage transitions missing from:\n%q", out)
	}
	if strings.Contains(out, "%") {
		t.Errorf("percentages should be suppressed off a terminal:\n%q", out)
	}
}

func TestProgressIsSilentWhenQuiet(t *testing.T) {
	var buf strings.Builder
	p := newProgressReporter(&buf, true)

	p.stage("decoding")
	p.percent(50)
	p.done()

	if buf.Len() != 0 {
		t.Errorf("--quiet still wrote %q", buf.String())
	}
}

// TestIsTerminalRejectsFilesAndBuffers guards the check itself: a regular file
// must not be mistaken for a terminal, which is the case that produced the
// original mess.
func TestIsTerminalRejectsFilesAndBuffers(t *testing.T) {
	if isTerminal(new(strings.Builder)) {
		t.Error("a strings.Builder was reported as a terminal")
	}

	path := filepath.Join(t.TempDir(), "log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("a regular file was reported as a terminal")
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	// /dev/null *is* a character device, so this documents the one place the
	// cheap check is generous — writing progress there is harmless.
	_ = isTerminal(devNull)
}

// TestNilReporterIsUsable keeps call sites free of nil checks.
func TestNilReporterIsUsable(t *testing.T) {
	var p *progressReporter
	p.stage("x")
	p.percent(1)
	p.done()
}
