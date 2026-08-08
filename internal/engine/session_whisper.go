//go:build cgo_whisper

package engine

/*
#include <stdlib.h>
#include <stdint.h>
#include <whisper.h>

// Implemented in Go; see export_whisper.go. Declared here so the trampolines
// below can call them.
void vsWhisperLog(int level, char *text);
void vsWhisperProgress(uintptr_t handle, int progress);

// The trampolines exist because Go cannot produce a C function pointer. They
// are static definitions, which is why this preamble lives in a file without
// any //export directives -- cgo forbids definitions in a preamble that
// accompanies exported functions.

static void vs_log_trampoline(enum ggml_log_level level, const char *text, void *user_data) {
    (void) user_data;
    vsWhisperLog((int) level, (char *) text);
}

static void vs_progress_trampoline(struct whisper_context *ctx, struct whisper_state *state,
                                   int progress, void *user_data) {
    (void) ctx;
    (void) state;
    vsWhisperProgress((uintptr_t) user_data, progress);
}

static void vs_install_log_handler(void) {
    // Both, and in this order: whisper routes its own messages through
    // whisper_log_set, while the backend chatter (Metal device init, buffer
    // sizes) goes through ggml's own handler.
    whisper_log_set(vs_log_trampoline, NULL);
    ggml_log_set(vs_log_trampoline, NULL);
}

static void vs_set_progress_callback(struct whisper_full_params *p, uintptr_t handle) {
    p->progress_callback = vs_progress_trampoline;
    p->progress_callback_user_data = (void *) handle;
}
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"strings"
	"sync"
	"unsafe"
)

// installLogHandler is run once, before the first context is created, so that
// no runtime output escapes before the callback is in place.
var installLogHandler = sync.OnceFunc(func() {
	C.vs_install_log_handler()
})

type whisperSession struct {
	mu    sync.Mutex
	ctx   *C.struct_whisper_context
	model string
}

// Open loads a model into memory. The caller keeps the session and reuses it:
// this is the slow, memory-hungry step.
func Open(modelPath string) (Session, error) {
	installLogHandler()

	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	cp := C.whisper_context_default_params()
	cp.use_gpu = C.bool(true)

	ctx := C.whisper_init_from_file_with_params(cPath, cp)
	if ctx == nil {
		return nil, fmt.Errorf("load model %s: whisper could not initialise it (wrong format, or not a whisper ggml model?)", modelPath)
	}
	return &whisperSession{ctx: ctx, model: modelPath}, nil
}

func (s *whisperSession) Model() string { return s.model }

func (s *whisperSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil {
		C.whisper_free(s.ctx)
		s.ctx = nil
	}
	return nil
}

func (s *whisperSession) Transcribe(samples []float32, p Params, onProgress Progress) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ctx == nil {
		return Result{}, ErrClosed
	}
	if len(samples) == 0 {
		return Result{}, fmt.Errorf("no audio samples to transcribe")
	}

	wp := C.whisper_full_default_params(C.WHISPER_SAMPLING_GREEDY)

	// Silence the runtime's own printing. These are the fields that write
	// directly to stdout/stderr from inside whisper, bypassing the log handler.
	wp.print_progress = C.bool(false)
	wp.print_realtime = C.bool(false)
	wp.print_special = C.bool(false)
	wp.print_timestamps = C.bool(false)

	wp.translate = C.bool(p.Translate)
	if p.Threads > 0 {
		wp.n_threads = C.int(p.Threads)
	}
	if p.OffsetSec > 0 {
		wp.offset_ms = C.int(p.OffsetSec * 1000)
	}
	if p.DurationSec > 0 {
		wp.duration_ms = C.int(p.DurationSec * 1000)
	}

	// Every C string here has to outlive whisper_full, so the frees are
	// deferred to the end of the function rather than set up inline.
	lang := p.Language
	if lang == "" {
		lang = "auto"
	}
	cLang := C.CString(lang)
	defer C.free(unsafe.Pointer(cLang))
	wp.language = cLang

	if p.Prompt != "" {
		cPrompt := C.CString(p.Prompt)
		defer C.free(unsafe.Pointer(cPrompt))
		wp.initial_prompt = cPrompt
	}

	if p.VADModelPath != "" {
		cVAD := C.CString(p.VADModelPath)
		defer C.free(unsafe.Pointer(cVAD))
		wp.vad = C.bool(true)
		wp.vad_model_path = cVAD
		wp.vad_params = C.whisper_vad_default_params()
	}

	if onProgress != nil {
		h := cgo.NewHandle(onProgress)
		defer h.Delete()
		C.vs_set_progress_callback(&wp, C.uintptr_t(h))
	}

	// &samples[0] is a Go pointer handed to C for the duration of the call,
	// which cgo permits because []float32 contains no pointers of its own and
	// whisper_full does not retain the buffer.
	rc := C.whisper_full(s.ctx, wp, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples)))
	if rc != 0 {
		return Result{}, fmt.Errorf("transcription failed (whisper_full returned %d)", int(rc))
	}

	n := int(C.whisper_full_n_segments(s.ctx))
	segments := make([]Segment, 0, n)
	for i := 0; i < n; i++ {
		text := strings.TrimSpace(C.GoString(C.whisper_full_get_segment_text(s.ctx, C.int(i))))
		if text == "" {
			// Whisper emits empty segments around silence. They carry no
			// information and would fail the transcript envelope's validation.
			continue
		}
		segments = append(segments, Segment{
			// Timestamps come back in centiseconds, not milliseconds.
			Start: float64(C.whisper_full_get_segment_t0(s.ctx, C.int(i))) / 100,
			End:   float64(C.whisper_full_get_segment_t1(s.ctx, C.int(i))) / 100,
			Text:  text,
		})
	}

	detected := C.GoString(C.whisper_lang_str(C.whisper_full_lang_id(s.ctx)))
	if p.Translate {
		// The task translated into English, so that is what the text is,
		// whatever the source language was detected as.
		detected = "en"
	}

	return Result{Segments: segments, Language: detected}, nil
}
