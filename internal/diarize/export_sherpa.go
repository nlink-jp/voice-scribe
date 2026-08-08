//go:build cgo_sherpa

package diarize

// The function the C trampoline calls back into. cgo forbids a preamble
// containing definitions in any file that uses //export, which is why the
// trampoline lives in diarize_sherpa.go and this preamble is limited to a
// system header of pure typedefs.

/*
#include <stdint.h>
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

//export vsDiarizeProgress
func vsDiarizeProgress(processed, total C.int32_t, arg unsafe.Pointer) C.int32_t {
	if total <= 0 {
		return 0
	}
	fn, ok := cgo.Handle(uintptr(arg)).Value().(Progress)
	if ok && fn != nil {
		fn(int(processed) * 100 / int(total))
	}
	// A non-zero return aborts the run; nothing here should.
	return 0
}
