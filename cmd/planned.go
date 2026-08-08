package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// What remains scaffolded. Each leaf refuses rather than pretending, and names
// the phase that fills it in. Nothing here may ship — see AGENTS.md.

func planned(phase string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("`voice-scribe %s` is not implemented yet (%s; see docs/ja/voice-scribe-rfp.ja.md)",
			cmd.CommandPath()[len("voice-scribe "):], phase)
	}
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve transcription over MCP (stdio)",
	Args:  cobra.NoArgs,
	RunE:  planned("Phase 2b"),
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
