//go:build !cgo_sherpa

package diarize

// Linked reports whether the diarization runtime is compiled into this binary.
const Linked = false

// Run always fails without the runtime. Returning the error rather than
// omitting the symbol keeps the layers above compiling and testable.
func Run(samples []float32, m Models, p Params, onProgress Progress) ([]Turn, error) {
	return nil, ErrNoRuntime
}
