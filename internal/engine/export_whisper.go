//go:build cgo_whisper

package engine

// This file holds the functions the C trampolines call back into. cgo forbids
// a preamble containing definitions in any file that uses //export, which is
// why the trampolines themselves live in session_whisper.go and this preamble
// is limited to a system header of pure typedefs.

/*
#include <stdint.h>
*/
import "C"

import (
	"runtime/cgo"
	"strings"
)

//export vsWhisperLog
func vsWhisperLog(level C.int, text *C.char) {
	if text == nil {
		return
	}
	// ggml emits partial lines; trimming here keeps a handler from having to
	// reassemble them just to print something readable.
	msg := strings.TrimRight(C.GoString(text), "\n")
	if msg == "" {
		return
	}
	dispatchLog(LogLevel(level), msg)
}

//export vsWhisperProgress
func vsWhisperProgress(handle C.uintptr_t, progress C.int) {
	h := cgo.Handle(handle)
	fn, ok := h.Value().(Progress)
	if !ok || fn == nil {
		return
	}
	fn(int(progress))
}
