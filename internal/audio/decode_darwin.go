//go:build darwin

// Package audio decodes any container macOS can read into the raw PCM that
// whisper.cpp requires.
//
// The decoding goes through AVFoundation rather than an ffmpeg subprocess. That
// keeps the release a single self-contained binary — the property that would
// otherwise be lost the moment a user without ffmpeg on their PATH tries to
// transcribe an m4a. The cost is this Objective-C bridge, and the limit is that
// containers AVFoundation does not know (mkv, webm) are rejected outright
// rather than silently handled.
package audio

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework CoreMedia -framework Foundation
#include <stdlib.h>
#include "decode.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// SampleRate is the rate every decode produces. whisper's mel front-end is
// trained at this rate, so it is a requirement rather than a preference.
const SampleRate = C.VS_SAMPLE_RATE

// Errors a caller can act on, rather than only report.
var (
	// ErrNoAudioTrack means the file opened but carries no audio — a silent
	// screen recording, most often.
	ErrNoAudioTrack = errors.New("file has no audio track")
	// ErrUnsupportedFormat means AVFoundation cannot decode this container or
	// codec. mkv and webm land here; the fix is to remux, and the message says so.
	ErrUnsupportedFormat = errors.New("unsupported container or codec")
)

// Audio is decoded PCM ready for the runtime.
type Audio struct {
	// Samples is 16 kHz mono 32-bit float PCM.
	Samples []float32
	// Duration is the audio length in seconds, derived from the sample count.
	Duration float64
}

// Slice returns the portion starting at offsetSec and running for durationSec.
// A durationSec of zero (or past the end) runs to the end.
//
// It exists so that a request to work on part of a recording actually costs
// part of the work. Whisper takes an offset of its own, but the diarization
// runtime does not: without this, transcribing thirty seconds of a forty-minute
// file still means computing speaker embeddings over the whole forty minutes.
//
// The result shares the underlying array; callers must not mutate it.
func (a Audio) Slice(offsetSec, durationSec float64) Audio {
	if offsetSec <= 0 && durationSec <= 0 {
		return a
	}

	start := int(offsetSec * SampleRate)
	if start < 0 {
		start = 0
	}
	if start > len(a.Samples) {
		start = len(a.Samples)
	}

	end := len(a.Samples)
	if durationSec > 0 {
		if n := start + int(durationSec*SampleRate); n < end {
			end = n
		}
	}

	out := a.Samples[start:end]
	return Audio{Samples: out, Duration: float64(len(out)) / float64(SampleRate)}
}

// Decode reads path and returns 16 kHz mono float PCM.
func Decode(path string) (Audio, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var (
		cSamples *C.float
		cCount   C.int64_t
		cErr     *C.char
	)

	code := C.vs_decode(cPath, &cSamples, &cCount, &cErr)
	if code != 0 {
		msg := "unknown decoding failure"
		if cErr != nil {
			msg = C.GoString(cErr)
			C.vs_free(unsafe.Pointer(cErr))
		}
		return Audio{}, classify(int(code), msg)
	}
	defer C.vs_free(unsafe.Pointer(cSamples))

	count := int(cCount)
	// Copy out of the C heap: the Go slice must not outlive the buffer, and
	// handing a Go-visible pointer into C-allocated memory to the rest of the
	// program is how a use-after-free gets written six months from now.
	samples := make([]float32, count)
	copy(samples, unsafe.Slice((*float32)(unsafe.Pointer(cSamples)), count))

	return Audio{
		Samples:  samples,
		Duration: float64(count) / float64(SampleRate),
	}, nil
}

func classify(code int, msg string) error {
	switch code {
	case 2:
		return fmt.Errorf("%w: %s", ErrNoAudioTrack, msg)
	case 3:
		return fmt.Errorf("%w: %s (convert it first, e.g. `ffmpeg -i in.mkv out.m4a`)",
			ErrUnsupportedFormat, msg)
	default:
		return errors.New(msg)
	}
}
