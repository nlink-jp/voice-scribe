package cmd

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// progressReporter writes status to stderr — never stdout, which may be a pipe
// carrying the transcript and is the JSON-RPC transport under `voice-scribe mcp`.
//
// It behaves differently depending on where it is writing. On a terminal it
// redraws one line in place. Anywhere else — a log file, a pipe, CI — the
// in-place redraw is meaningless and the percentages turn into tens of
// kilobytes of carriage returns, so only the stage transitions are emitted, one
// plain line each.
type progressReporter struct {
	mu       sync.Mutex
	w        io.Writer
	quiet    bool
	tty      bool
	stageMsg string
	last     time.Time
	dirty    bool
}

func newProgressReporter(w io.Writer, quiet bool) *progressReporter {
	return &progressReporter{w: w, quiet: quiet, tty: isTerminal(w)}
}

// isTerminal reports whether w is an interactive terminal. Checking for a
// character device keeps this dependency-free; a pipe, a regular file and
// /dev/null all correctly come back false.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (p *progressReporter) stage(format string, args ...any) {
	if p == nil || p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stageMsg = fmt.Sprintf(format, args...)
	if !p.tty {
		fmt.Fprintln(p.w, p.stageMsg)
		return
	}
	p.clearLine()
	fmt.Fprintf(p.w, "%s...", p.stageMsg)
	p.dirty = true
}

func (p *progressReporter) percent(pct int) {
	if p == nil || p.quiet || !p.tty {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// The runtime's callback fires far more often than a terminal can usefully
	// redraw, and it runs on a thread that should not wait on a write.
	now := time.Now()
	if now.Sub(p.last) < 200*time.Millisecond && pct < 100 {
		return
	}
	p.last = now

	p.clearLine()
	fmt.Fprintf(p.w, "%s... %d%%", p.stageMsg, pct)
	p.dirty = true
}

func (p *progressReporter) done() {
	if p == nil || p.quiet || !p.tty {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.clearLine()
	p.dirty = false
}

// clearLine rewrites the current line in place, using a carriage return and
// padding rather than an ANSI escape — only reached when the target is a
// terminal, but this keeps the output readable if that check is ever wrong.
func (p *progressReporter) clearLine() {
	if p.dirty {
		fmt.Fprintf(p.w, "\r%-70s\r", "")
	}
}
