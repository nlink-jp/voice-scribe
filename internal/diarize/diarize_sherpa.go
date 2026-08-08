//go:build cgo_sherpa

package diarize

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/sherpa-onnx

// Static archives, dependents first. sherpa-onnx's C API sits on its core,
// which pulls in the kaldi and fst pieces, and everything ultimately resolves
// against ONNX Runtime.
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libsherpa-onnx-c-api.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libsherpa-onnx-core.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libsherpa-onnx-kaldifst-core.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libsherpa-onnx-fstfar.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libsherpa-onnx-fst.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libkaldi-decoder-core.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libkaldi-native-fbank-core.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libkissfft-float.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/lib/libssentencepiece_core.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/sherpa-onnx/build/_deps/onnxruntime-src/lib/libonnxruntime.a
// libc++ is requested here even though the whisper engine also asks for it.
// Dropping it to silence the linker's duplicate-library warning breaks any
// build that links this package without that one -- `go test ./internal/diarize`
// most obviously. A package declares what it needs; the warning is the cost.
#cgo LDFLAGS: -framework Foundation -framework CoreML -lc++

#include <stdlib.h>
#include <stdint.h>
#include "sherpa-onnx/c-api/c-api.h"

// Implemented in Go; see export_sherpa.go.
int32_t vsDiarizeProgress(int32_t processed, int32_t total, void *arg);

// Trampoline: Go cannot produce a C function pointer. Static definition, which
// is why this file carries no //export directives of its own.
static int32_t vs_diarize_progress_trampoline(int32_t processed, int32_t total, void *arg) {
    return vsDiarizeProgress(processed, total, arg);
}

static const SherpaOnnxOfflineSpeakerDiarizationResult *vs_diarize_process(
        const SherpaOnnxOfflineSpeakerDiarization *sd, const float *samples,
        int32_t n, uintptr_t handle, int32_t want_progress) {
    if (!want_progress) {
        return SherpaOnnxOfflineSpeakerDiarizationProcess(sd, samples, n);
    }
    return SherpaOnnxOfflineSpeakerDiarizationProcessWithCallback(
        sd, samples, n, vs_diarize_progress_trampoline, (void *) handle);
}
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"unsafe"
)

// Linked reports whether the diarization runtime is compiled into this binary.
const Linked = true

// SampleRate is what the segmentation model expects. It matches the decoder's
// output, which is why no resampling happens between the two.
const SampleRate = 16000

// Run performs speaker diarization over 16 kHz mono float samples.
func Run(samples []float32, m Models, p Params, onProgress Progress) ([]Turn, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if m.Segmentation == "" || m.Embedding == "" {
		return nil, fmt.Errorf("diarization needs both a segmentation and an embedding model")
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("no audio samples to diarize")
	}

	cSeg := C.CString(m.Segmentation)
	defer C.free(unsafe.Pointer(cSeg))
	cEmb := C.CString(m.Embedding)
	defer C.free(unsafe.Pointer(cEmb))
	cProvider := C.CString("cpu")
	defer C.free(unsafe.Pointer(cProvider))

	threads := C.int32_t(1)
	if p.Threads > 0 {
		threads = C.int32_t(p.Threads)
	}

	// Unlike whisper, sherpa-onnx's C API exposes no defaults getter for this
	// config, so a zero-initialised struct is not "use the sensible values" --
	// it is a threshold of 0 and no minimum durations, which clusters every
	// turn into a single speaker. The defaults below are the ones the library
	// documents; they must be written out explicitly.
	var cfg C.SherpaOnnxOfflineSpeakerDiarizationConfig
	cfg.segmentation.pyannote.model = cSeg
	cfg.segmentation.num_threads = threads
	cfg.segmentation.provider = cProvider
	cfg.embedding.model = cEmb
	cfg.embedding.num_threads = threads
	cfg.embedding.provider = cProvider
	cfg.min_duration_on = C.float(defaultMinDurationOn)
	cfg.min_duration_off = C.float(defaultMinDurationOff)

	if p.NumSpeakers > 0 {
		cfg.clustering.num_clusters = C.int32_t(p.NumSpeakers)
	} else {
		cfg.clustering.num_clusters = -1
		threshold := p.Threshold
		if threshold <= 0 {
			threshold = DefaultThreshold
		}
		cfg.clustering.threshold = C.float(threshold)
	}

	sd := C.SherpaOnnxCreateOfflineSpeakerDiarization(&cfg)
	if sd == nil {
		return nil, fmt.Errorf("could not load the diarization models (segmentation %s, embedding %s)",
			m.Segmentation, m.Embedding)
	}
	defer C.SherpaOnnxDestroyOfflineSpeakerDiarization(sd)

	if rate := int(C.SherpaOnnxOfflineSpeakerDiarizationGetSampleRate(sd)); rate != SampleRate {
		return nil, fmt.Errorf("the segmentation model expects %d Hz audio, but the decoder produces %d Hz",
			rate, SampleRate)
	}

	wantProgress := C.int32_t(0)
	var handle cgo.Handle
	if onProgress != nil {
		handle = cgo.NewHandle(onProgress)
		defer handle.Delete()
		wantProgress = 1
	}

	// &samples[0] is a Go pointer handed to C for the duration of the call,
	// which cgo permits because []float32 holds no pointers of its own and the
	// runtime does not retain the buffer.
	res := C.vs_diarize_process(sd, (*C.float)(unsafe.Pointer(&samples[0])),
		C.int32_t(len(samples)), C.uintptr_t(handle), wantProgress)
	if res == nil {
		return nil, fmt.Errorf("diarization failed")
	}
	defer C.SherpaOnnxOfflineSpeakerDiarizationDestroyResult(res)

	n := int(C.SherpaOnnxOfflineSpeakerDiarizationResultGetNumSegments(res))
	if n == 0 {
		return nil, nil
	}

	segments := C.SherpaOnnxOfflineSpeakerDiarizationResultSortByStartTime(res)
	if segments == nil {
		return nil, fmt.Errorf("diarization produced %d segments but returned none", n)
	}
	defer C.SherpaOnnxOfflineSpeakerDiarizationDestroySegment(segments)

	view := unsafe.Slice((*C.SherpaOnnxOfflineSpeakerDiarizationSegment)(unsafe.Pointer(segments)), n)
	turns := make([]Turn, 0, n)
	for _, s := range view {
		turns = append(turns, Turn{
			Start:   float64(s.start),
			End:     float64(s.end),
			Speaker: int(s.speaker),
		})
	}
	return turns, nil
}
