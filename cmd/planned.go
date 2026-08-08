package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// The command tree below is scaffolding: the surface the RFP specifies, wired
// up so it can be reviewed against the design, with every leaf refusing to
// pretend it works. Each one names the phase that fills it in.
//
// Nothing here may ship. `make package` is gated on these being real — see
// AGENTS.md, "Do not release with planned/ in the tree".

func planned(phase string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("`voice-scribe %s` is not implemented yet (%s; see docs/ja/voice-scribe-rfp.ja.md)",
			cmd.CommandPath()[len("voice-scribe "):], phase)
	}
}

var transcribeCmd = &cobra.Command{
	Use:   "transcribe <file>",
	Short: "Transcribe an audio or video file",
	Args:  cobra.ExactArgs(1),
	RunE:  planned("Phase 1"),
}

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage transcription and diarization models",
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed and catalog models",
	Args:  cobra.NoArgs,
	RunE:  planned("Phase 1"),
}

var modelsPullCmd = &cobra.Command{
	Use:   "pull <name>",
	Short: "Download a catalog model",
	Args:  cobra.ExactArgs(1),
	RunE:  planned("Phase 1"),
}

var modelsImportCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Register a local model file",
	Args:  cobra.ExactArgs(1),
	RunE:  planned("Phase 1"),
}

var modelsRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove an installed model",
	Args:  cobra.ExactArgs(1),
	RunE:  planned("Phase 1"),
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve transcription over MCP (stdio)",
	Args:  cobra.NoArgs,
	RunE:  planned("Phase 2b"),
}

func init() {
	rootCmd.AddCommand(transcribeCmd, modelsCmd, mcpCmd)
	modelsCmd.AddCommand(modelsListCmd, modelsPullCmd, modelsImportCmd, modelsRmCmd)
}
