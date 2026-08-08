package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:   "voice-scribe",
	Short: "Local speech-to-text engine and MCP server",
	Long: `voice-scribe transcribes audio locally with whisper.cpp — no API key, no
audio leaving the machine — and serves that capability over MCP so an agent
whose model cannot handle audio can still read a recording.

Output carries speaker labels and timestamps in an envelope compatible with
gem-transcribe, so downstream tools parse cloud and local transcripts alike.`,
	// Don't dump the usage help on RunE errors; cobra still prints "Error: ..." to stderr.
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "",
		"Path to config.toml (default: search ~/.config/voice-scribe/config.toml then ./config.toml)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// cobra has already printed "Error: ..." to stderr.
		os.Exit(1)
	}
}
