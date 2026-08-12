// Command tcspike answers the one question the transcribe.cpp CLI cannot:
// can Go drive its C API directly, when the project ships no Go binding?
//
// It links libtranscribe.a statically (the libraries and frameworks below are
// transcribed from build/install/lib/transcribe-link.json, which the build
// emits precisely so a consumer does not hand-maintain this list) and runs one
// model over one 16 kHz mono WAV, printing a JSON object on stdout with both
// the transcript and the speaker segments.
//
// stdout carries the JSON and nothing else -- the property voice-scribe's MCP
// mode depends on -- so a run doubles as a stdout-purity check.
//
// Throwaway evaluation code for ADR-0007. Not part of the shipped binary.
package main

/*
#cgo CFLAGS: -I${SRCDIR}/third_party/transcribe.cpp/build/install/include
#cgo LDFLAGS: ${SRCDIR}/third_party/transcribe.cpp/build/install/lib/libtranscribe.a
#cgo LDFLAGS: ${SRCDIR}/third_party/transcribe.cpp/build/install/lib/libggml.a
#cgo LDFLAGS: ${SRCDIR}/third_party/transcribe.cpp/build/install/lib/libggml-cpu.a
#cgo LDFLAGS: ${SRCDIR}/third_party/transcribe.cpp/build/install/lib/libggml-metal.a
#cgo LDFLAGS: ${SRCDIR}/third_party/transcribe.cpp/build/install/lib/libggml-base.a
#cgo LDFLAGS: -framework Accelerate -framework Foundation -framework Metal -framework MetalKit
#cgo LDFLAGS: -lc++ -lm

#include <stdlib.h>
#include "transcribe.h"

// The log callback has to be a C function pointer; a no-op silences the
// library without touching either standard stream.
static void gospike_silence(transcribe_log_level level, const char * text, void * userdata) {
    (void) level; (void) text; (void) userdata;
}
static void gospike_mute(void) { transcribe_log_set(gospike_silence, NULL); }
*/
import "C"

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
	"unsafe"
)

type speaker struct {
	T0MS      int64    `json:"t0_ms"`
	T1MS      int64    `json:"t1_ms"`
	SpeakerID int32    `json:"speaker_id"`
	P         *float64 `json:"p"` // nil when the family reports NaN ("not produced")
}

type result struct {
	Model string `json:"model"`
	Arch  string `json:"arch"`

	// The capability block is the point of the -caps mode: whether a model
	// can stand in for kotoba-whisper inside voice-scribe is decided here,
	// not by its CER. An engine that cannot time-stamp a segment cannot feed
	// the gem-transcribe-compatible envelope, however accurate it is.
	License     string   `json:"license"`
	Timestamps  string   `json:"max_timestamps"`
	MaxAudioSec float64  `json:"max_audio_sec"` // 0 = chunked internally / unbounded
	Diarization bool     `json:"supports_diarization"`
	Streaming   bool     `json:"supports_streaming"`
	Translate   bool     `json:"supports_translate"`
	LangDetect  bool     `json:"supports_language_detect"`
	NLanguages  int      `json:"n_languages"`
	Japanese    bool     `json:"advertises_ja"`
	Backend     string   `json:"backend,omitempty"`
	Languages   []string `json:"languages,omitempty"`

	Language string    `json:"language,omitempty"`
	Text     string    `json:"text,omitempty"`
	Speakers []speaker `json:"speakers,omitempty"`
	LoadMS   float64   `json:"load_ms"`
	RunMS    float64   `json:"run_ms,omitempty"`
}

func main() {
	args := os.Args[1:]
	capsOnly := len(args) > 0 && args[0] == "-caps"
	if capsOnly {
		args = args[1:]
	}
	if (capsOnly && len(args) != 1) || (!capsOnly && len(args) != 2) {
		fmt.Fprintf(os.Stderr, "usage: %s MODEL.gguf AUDIO.wav\n       %s -caps MODEL.gguf\n",
			os.Args[0], os.Args[0])
		os.Exit(2)
	}
	wav := ""
	if !capsOnly {
		wav = args[1]
	}
	if err := run(args[0], wav); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func timestampKind(k C.transcribe_timestamp_kind) string {
	switch k {
	case C.TRANSCRIBE_TIMESTAMPS_NONE:
		return "none"
	case C.TRANSCRIBE_TIMESTAMPS_SEGMENT:
		return "segment"
	case C.TRANSCRIBE_TIMESTAMPS_WORD:
		return "word"
	case C.TRANSCRIBE_TIMESTAMPS_TOKEN:
		return "token"
	}
	return "auto"
}

func run(modelPath, wavPath string) error {
	C.gospike_mute()

	var pcm []float32
	if wavPath != "" {
		var err error
		if pcm, err = readWAV(wavPath); err != nil {
			return err
		}
	}

	if st := C.transcribe_init_backends_default(); st != C.TRANSCRIBE_OK {
		return statusErr("init_backends", st)
	}

	var lp C.struct_transcribe_model_load_params
	C.transcribe_model_load_params_init(&lp)
	var sp C.struct_transcribe_session_params
	C.transcribe_session_params_init(&sp)

	cModel := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cModel))

	var sess *C.struct_transcribe_session
	t0 := time.Now()
	if st := C.transcribe_open(cModel, &lp, &sp, &sess); st != C.TRANSCRIBE_OK {
		return statusErr("open", st)
	}
	loadMS := float64(time.Since(t0).Microseconds()) / 1000
	defer C.transcribe_close(sess)

	model := C.transcribe_get_model(sess)
	out := result{
		Model:   modelPath,
		Arch:    C.GoString(C.transcribe_model_arch_string(model)),
		Backend: C.GoString(C.transcribe_model_backend(model)),
		LoadMS:  loadMS,
		// The capability bit, not a guess from the file name: this is how a
		// caller decides whether to offer diarization for a given model.
		Diarization: bool(C.transcribe_model_supports(model, C.TRANSCRIBE_FEATURE_DIARIZATION)),
	}

	// Capabilities decide the run params, not the caller's assumptions: a
	// diarizer advertises no languages at all and rejects a language hint
	// with UNSUPPORTED_LANGUAGE.
	var caps C.struct_transcribe_capabilities
	C.transcribe_capabilities_init(&caps)
	if st := C.transcribe_model_get_capabilities(model, &caps); st != C.TRANSCRIBE_OK {
		return statusErr("get_capabilities", st)
	}
	langs := goStringSlice(caps.languages, int(caps.n_languages))
	out.NLanguages = len(langs)
	out.Japanese = contains(langs, "ja") || contains(langs, "ja-JP")
	out.Streaming = bool(caps.supports_streaming)
	out.Translate = bool(caps.supports_translate)
	out.LangDetect = bool(caps.supports_language_detect)
	out.Timestamps = timestampKind(caps.max_timestamp_kind)
	out.MaxAudioSec = float64(caps.max_audio_ms) / 1000
	out.License = C.GoString(C.transcribe_model_meta_val_str(model, C.CString("general.license")))

	if wavPath == "" { // -caps: report and stop, no inference
		out.Languages = langs
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	var rp C.struct_transcribe_run_params
	C.transcribe_run_params_init(&rp)
	// Gate the hint on MEMBERSHIP, not on the count: the Sortformer diarizer
	// advertises exactly one language ("en") and rejects every hint with
	// UNSUPPORTED_LANGUAGE, so `n_languages > 0` is not the question --
	// "is my language in the list" is.
	if wantLang := "ja"; contains(langs, wantLang) {
		lang := C.CString(wantLang)
		defer C.free(unsafe.Pointer(lang))
		rp.language = lang
		out.Language = wantLang
	}

	t0 = time.Now()
	if st := C.transcribe_run(sess, (*C.float)(&pcm[0]), C.int(len(pcm)), &rp); st != C.TRANSCRIBE_OK {
		return statusErr("run", st)
	}
	out.RunMS = float64(time.Since(t0).Microseconds()) / 1000
	out.Text = C.GoString(C.transcribe_full_text(sess))

	n := int(C.transcribe_n_speaker_segments(sess))
	for i := 0; i < n; i++ {
		var seg C.struct_transcribe_speaker_segment
		C.transcribe_speaker_segment_init(&seg)
		if st := C.transcribe_get_speaker_segment(sess, C.int(i), &seg); st != C.TRANSCRIBE_OK {
			return statusErr("get_speaker_segment", st)
		}
		s := speaker{
			T0MS: int64(seg.t0_ms), T1MS: int64(seg.t1_ms),
			SpeakerID: int32(seg.speaker_id),
		}
		if p := float64(seg.p); !math.IsNaN(p) {
			s.P = &p
		}
		out.Speakers = append(out.Speakers, s)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// goStringSlice copies a C `const char * const *` array of n entries.
func goStringSlice(arr **C.char, n int) []string {
	if arr == nil || n <= 0 {
		return nil
	}
	cs := unsafe.Slice(arr, n)
	out := make([]string, n)
	for i := range cs {
		out[i] = C.GoString(cs[i])
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

func statusErr(where string, st C.transcribe_status) error {
	return fmt.Errorf("%s: %s (%d)", where, C.GoString(C.transcribe_status_string(C.int(st))), int(st))
}

// readWAV reads the 16 kHz mono 16-bit PCM that transcribe.cpp requires and
// returns float32 samples in [-1, 1].
func readWAV(path string) ([]float32, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 44 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}
	var data []byte
	var channels, bits int
	var rate uint32
	for off := 12; off+8 <= len(raw); {
		id := string(raw[off : off+4])
		size := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		body := off + 8
		if body+size > len(raw) {
			size = len(raw) - body
		}
		switch id {
		case "fmt ":
			channels = int(binary.LittleEndian.Uint16(raw[body+2 : body+4]))
			rate = binary.LittleEndian.Uint32(raw[body+4 : body+8])
			bits = int(binary.LittleEndian.Uint16(raw[body+14 : body+16]))
		case "data":
			data = raw[body : body+size]
		}
		off = body + size + size%2
	}
	if channels != 1 || rate != 16000 || bits != 16 {
		return nil, fmt.Errorf("need 16 kHz mono 16-bit, got %d Hz %dch %d-bit", rate, channels, bits)
	}
	pcm := make([]float32, len(data)/2)
	for i := range pcm {
		pcm[i] = float32(int16(binary.LittleEndian.Uint16(data[2*i:]))) / 32768
	}
	return pcm, nil
}
