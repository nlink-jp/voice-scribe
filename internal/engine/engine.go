// Package engine wraps the local speech-to-text runtime (whisper.cpp on ggml,
// with the Metal backend) behind a Go API.
//
// The runtime is linked in only under the `cgo_whisper` build tag. Binaries
// built without it — `make build` — still compile and run, but every call that
// needs the runtime reports ErrNoRuntime. That split keeps scaffold work and
// machines without cmake or the Metal toolchain buildable, and it lets the pure
// Go layers above be tested without a 1.6 GB model on disk.
package engine

import "errors"

// ErrNoRuntime is returned when the binary was built without the `cgo_whisper`
// tag, so no transcription runtime is linked.
var ErrNoRuntime = errors.New("transcription runtime not linked; build with `make build-engine`")

// Info describes the runtime linked into this binary.
type Info struct {
	// Available reports whether a runtime is linked at all.
	Available bool
	// Runtime names the linked runtime, e.g. "whisper.cpp". Empty when none is.
	Runtime string
	// Capabilities is the runtime's own capability line, listing which ggml
	// backends were compiled in (METAL, BLAS, NEON, ...). Reported verbatim so
	// that a support question can be answered from what the binary actually
	// says rather than from what the build was supposed to enable.
	Capabilities string
}
