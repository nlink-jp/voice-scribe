package cmd

import (
	"fmt"

	"github.com/nlink-jp/voice-scribe/internal/engine"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report what this binary can actually do",
	Long: `doctor reports the runtime linked into this binary and the ggml backends it
was compiled with, straight from the runtime itself rather than from what the
build was supposed to enable.

As the remaining pieces land it will also check the model registry, audio
decoding, and the diarization runtime.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		info := engine.Describe()

		if !info.Available {
			fmt.Fprintln(out, "runtime:      none")
			fmt.Fprintf(out, "              %v\n", engine.ErrNoRuntime)
			return nil
		}

		fmt.Fprintf(out, "runtime:      %s\n", info.Runtime)
		fmt.Fprintf(out, "capabilities: %s\n", info.Capabilities)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
