// Command voice-scribe transcribes audio locally and serves that capability
// over MCP. See docs/ja/voice-scribe-rfp.ja.md for the design.
package main

import "github.com/nlink-jp/voice-scribe/cmd"

func main() {
	cmd.Execute()
}
