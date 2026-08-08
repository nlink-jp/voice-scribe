package cmd

import (
	"bytes"
	"testing"
)

// TestVersionSpellingsAgree pins both spellings to the same output. `brew test`
// runs `--version`, humans tend to type `version`, and a formula that passes
// while the two disagree is exactly the drift this test exists to prevent.
func TestVersionSpellingsAgree(t *testing.T) {
	fromFlag := runRoot(t, "--version")
	fromSubcommand := runRoot(t, "version")

	if fromFlag != fromSubcommand {
		t.Fatalf("--version printed %q but `version` printed %q", fromFlag, fromSubcommand)
	}
	if fromFlag != Version+"\n" {
		t.Errorf("printed %q, want the injected version %q with a trailing newline", fromFlag, Version)
	}
}

func runRoot(t *testing.T, args ...string) string {
	t.Helper()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("voice-scribe %v: %v", args, err)
	}
	return out.String()
}
