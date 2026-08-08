//go:build !cgo_whisper

package engine

// Linked reports whether the real runtime is compiled into this binary. It is a
// build-tag constant so that callers (and tests) can branch on it without a cgo
// dependency of their own.
const Linked = false

// Describe reports that no runtime is linked.
func Describe() Info {
	return Info{Available: false}
}

// Open always fails in a binary built without the runtime. Returning the error
// here rather than omitting the symbol keeps everything above this package
// compiling and testable without cmake or the Metal toolchain.
func Open(modelPath string) (Session, error) {
	return nil, ErrNoRuntime
}
