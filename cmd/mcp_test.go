package cmd

import (
	"os"
	"strings"
	"testing"
)

// TestClaimStdoutSendsStrayWritesToStderr is the defence this file exists for.
//
// `voice-scribe mcp` puts JSON-RPC on stdout, so a single stray line from
// anywhere — this program, a linked C library, a dependency added years from
// now — corrupts the protocol. claimStdout takes a private handle on the real
// stdout and repoints the well-known one at stderr, so a leak becomes noise in
// a log instead of a broken session.
//
// The test drives the mechanism directly rather than inferring it from a quiet
// server: the runtime's chatter is already suppressed at the source by a log
// handler, so an empty stderr proves nothing about this layer.
func TestClaimStdoutSendsStrayWritesToStderr(t *testing.T) {
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() { os.Stdout, os.Stderr = realOut, realErr })

	protocol, err := claimStdout()
	if err != nil {
		t.Fatalf("claimStdout: %v", err)
	}

	if _, err := protocol.Write([]byte("PROTOCOL\n")); err != nil {
		t.Fatalf("write to the protocol handle: %v", err)
	}
	// The stray write: exactly what a careless fmt.Println or a C library would do.
	if _, err := os.Stdout.Write([]byte("STRAY\n")); err != nil {
		t.Fatalf("write to os.Stdout: %v", err)
	}

	protocol.Close()
	outW.Close()
	errW.Close()

	onProtocol := read(t, outR)
	onStderr := read(t, errR)

	if !strings.Contains(onProtocol, "PROTOCOL") {
		t.Errorf("the protocol stream did not receive its own write; got %q", onProtocol)
	}
	if strings.Contains(onProtocol, "STRAY") {
		t.Errorf("a stray write reached the protocol stream, which would corrupt JSON-RPC; got %q", onProtocol)
	}
	if !strings.Contains(onStderr, "STRAY") {
		t.Errorf("the stray write was lost rather than diverted to stderr; got %q", onStderr)
	}
}

func read(t *testing.T, f *os.File) string {
	t.Helper()
	defer f.Close()

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			return sb.String()
		}
	}
}
