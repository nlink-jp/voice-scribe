package engine

import (
	"fmt"
	"sync"
)

// Params control one transcription run.
type Params struct {
	// Language is an ISO 639-1 code. Empty means detect it from the audio.
	Language string
	// Translate also produces English, using whisper's translate task. Whisper
	// can only translate *into* English, so this is a boolean rather than a
	// target language.
	Translate bool
	// Prompt biases the decoder's vocabulary — proper nouns, jargon, spellings
	// the model would otherwise guess at. Whisper calls it the initial prompt.
	Prompt string
	// Threads is the inference thread count; zero lets the runtime decide.
	Threads int
	// OffsetSec and DurationSec restrict the run to a slice of the audio.
	// DurationSec of zero means "to the end".
	OffsetSec   float64
	DurationSec float64
	// VADModelPath enables voice-activity gating, which suppresses whisper's
	// habit of hallucinating repeated phrases over silence. Empty disables it;
	// the feature needs a model file of its own, which is why this is a path
	// rather than a boolean.
	VADModelPath string
}

// Segment is one span of transcribed speech, in seconds from the start.
type Segment struct {
	Start float64
	End   float64
	Text  string
}

// Result is what one transcription run produced.
type Result struct {
	Segments []Segment
	// Language is the code the runtime actually used, which is the detected
	// one when Params.Language was empty.
	Language string
}

// Progress reports completion as a percentage. It is called from a runtime
// thread, so an implementation must not block for long.
type Progress func(percent int)

// Session is a loaded model. Loading is the expensive part of a transcription —
// seconds, and gigabytes of memory — so a session is meant to be kept and
// reused across files rather than opened per call.
//
// Implementations are safe for concurrent use, but transcriptions on one
// session are serialised: whisper keeps decoding state on the context.
type Session interface {
	Transcribe(samples []float32, p Params, onProgress Progress) (Result, error)
	// Model returns the path the session was opened from.
	Model() string
	Close() error
}

// LogLevel mirrors ggml's log levels.
type LogLevel int

const (
	LogNone LogLevel = iota
	LogDebug
	LogInfo
	LogWarn
	LogError
	LogCont
)

var (
	logMu      sync.RWMutex
	logHandler func(LogLevel, string)
)

// SetLogHandler routes the runtime's log output to fn.
//
// This exists to keep the runtime's chatter off stdout. `voice-scribe mcp`
// speaks JSON-RPC there, and a library writing to stdout corrupts the protocol
// — a failure this org has already shipped once, in image-forge v0.9.0. The
// current whisper.cpp writes to stderr, but that is an observation about
// today's upstream rather than a contract, so the callback is installed
// unconditionally and the destination is decided here.
//
// fn may be nil, which discards the output. It is called from runtime threads.
func SetLogHandler(fn func(LogLevel, string)) {
	logMu.Lock()
	logHandler = fn
	logMu.Unlock()
}

// dispatchLog is called by the cgo trampoline.
func dispatchLog(level LogLevel, text string) {
	logMu.RLock()
	fn := logHandler
	logMu.RUnlock()
	if fn != nil {
		fn(level, text)
	}
}

// ErrClosed reports use of a session after Close.
var ErrClosed = fmt.Errorf("session is closed")
