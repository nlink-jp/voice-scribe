package cmd

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/nlink-jp/voice-scribe/internal/engine"
	"github.com/nlink-jp/voice-scribe/internal/mcp/job"
	"github.com/nlink-jp/voice-scribe/internal/mcp/mcpserver"
	"github.com/nlink-jp/voice-scribe/internal/mcp/tools"
	"github.com/nlink-jp/voice-scribe/internal/mcp/transport"
	"github.com/nlink-jp/voice-scribe/internal/mcp/workspace"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve transcription over MCP (stdio)",
	Long: `mcp speaks the Model Context Protocol over stdin/stdout, giving an agent whose
model cannot process audio a way to read recordings.

Four tools: get_usage, transcribe, check_job, list_models. Transcription is
asynchronous — transcribe returns a job_id that check_job polls.`,
	Args: cobra.NoArgs,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	rt, err := newRuntimeContext()
	if err != nil {
		return err
	}

	// stdout is the JSON-RPC transport from here on. Take a private duplicate
	// of it and point fd 1 at stderr, so that anything which writes to "stdout"
	// — this program, a linked C library, a dependency added in five years —
	// lands in the log rather than corrupting the protocol.
	//
	// This is the second layer of the defence; the first is engine's log
	// callbacks, installed below. image-forge shipped a stdout leak from a
	// native library once, and one layer was not enough to catch it.
	protocolOut, err := claimStdout()
	if err != nil {
		return err
	}
	defer protocolOut.Close()

	// The runtime's own chatter goes to the real stderr. Nothing routes it to
	// the transport, and nothing is dropped silently.
	engine.SetLogHandler(func(level engine.LogLevel, text string) {
		if level >= engine.LogWarn {
			fmt.Fprintln(os.Stderr, text)
		}
	})

	deps := &tools.Deps{
		WS:              workspace.NewManager(defaultWorkspaceRoot()),
		Transcribe:      newMCPTranscriber(rt),
		ListModels:      func(scope string) (any, error) { return listModelsView(rt, scope) },
		Jobs:            job.NewManager(cmd.Context()),
		InlineThreshold: rt.Config.MCP.InlineThreshold,
	}

	srv := mcpserver.New("voice-scribe", Version, transport.NewStdioTransport(os.Stdin, protocolOut), nil)
	srv.SetInstructions(tools.Instructions)
	tools.Register(srv, deps)

	if err := srv.Serve(cmd.Context()); err != nil && !isShutdown(err) {
		return err
	}
	return nil
}

// claimStdout duplicates the real stdout for exclusive use by the transport and
// redirects fd 1 to stderr.
//
// Returning an *os.File rather than writing through fd 1 is the point: after
// this call there is no way to reach the protocol stream except through the
// returned handle.
func claimStdout() (*os.File, error) {
	fd, err := syscall.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return nil, fmt.Errorf("duplicate stdout for the MCP transport: %w", err)
	}
	if err := syscall.Dup2(int(os.Stderr.Fd()), int(os.Stdout.Fd())); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("redirect stdout to stderr: %w", err)
	}
	return os.NewFile(uintptr(fd), "mcp-stdout"), nil
}

// isShutdown reports whether err is the ordinary end of a session: the client
// closed stdin, or the context was cancelled.
func isShutdown(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
