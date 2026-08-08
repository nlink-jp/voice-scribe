//go:build cgo_whisper

package engine

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/whisper.cpp/include
#cgo CFLAGS: -I${SRCDIR}/../../third_party/whisper.cpp/ggml/include

// Static archives, dependents first: whisper depends on parakeet and ggml, the
// ggml backends depend on ggml-base. Getting this order wrong surfaces as
// undefined-symbol errors at link time, not at build-engine configure time.
#cgo LDFLAGS: ${SRCDIR}/../../third_party/whisper.cpp/build/src/libwhisper.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/whisper.cpp/build/src/libparakeet.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/whisper.cpp/build/ggml/src/libggml.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/whisper.cpp/build/ggml/src/libggml-cpu.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/whisper.cpp/build/ggml/src/ggml-metal/libggml-metal.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/whisper.cpp/build/ggml/src/ggml-blas/libggml-blas.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/whisper.cpp/build/ggml/src/libggml-base.a
#cgo LDFLAGS: -framework Metal -framework MetalKit -framework Foundation -framework Accelerate -lc++

#include <whisper.h>
*/
import "C"

// Linked reports whether the real runtime is compiled into this binary.
const Linked = true

// Describe reports the linked runtime and the backends it was compiled with.
//
// Despite its name, whisper_print_system_info() prints nothing: it returns a
// static C string. Nothing here touches stdout, which matters because stdout is
// the MCP transport (see AGENTS.md, "stdout is the transport").
func Describe() Info {
	return Info{
		Available:    true,
		Runtime:      "whisper.cpp",
		Capabilities: C.GoString(C.whisper_print_system_info()),
	}
}
