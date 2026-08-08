package cmd

import (
	"strings"
	"testing"
)

// TestPlannedCommandsRefuse pins that every scaffolded leaf fails loudly rather
// than exiting 0 with no output — an empty success is how a stub reaches a
// release unnoticed.
func TestPlannedCommandsRefuse(t *testing.T) {
	for _, args := range [][]string{
		{"mcp"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			rootCmd.SetArgs(args)
			rootCmd.SetOut(new(strings.Builder))
			rootCmd.SetErr(new(strings.Builder))
			t.Cleanup(func() {
				rootCmd.SetArgs(nil)
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
			})

			err := rootCmd.Execute()
			if err == nil {
				t.Fatal("succeeded; a not-yet-implemented command must return an error")
			}
			if !strings.Contains(err.Error(), "not implemented yet") {
				t.Errorf("error = %q, want it to say the command is not implemented yet", err)
			}
		})
	}
}
