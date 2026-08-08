// Package diarize answers "who spoke when" for a piece of audio.
//
// It is a second native runtime alongside whisper.cpp: sherpa-onnx running a
// pyannote segmentation model and a speaker-embedding model on ONNX Runtime.
// whisper cannot do this — its tinydiarize is limited to two English speakers —
// and the alternative was to leave the local tool strictly weaker than its cloud
// counterpart, which infers speakers as part of transcription.
//
// The runtime is linked only under the `cgo_sherpa` build tag, separately from
// whisper's `cgo_whisper`. Keeping the tags apart means the transcription
// binary still builds when the ONNX Runtime archive cannot be fetched — which
// has already happened once, when upstream's pinned hash stopped matching the
// published asset.
package diarize

import (
	"errors"
	"fmt"
)

// ErrNoRuntime is returned when the binary was built without `cgo_sherpa`.
var ErrNoRuntime = errors.New("diarization runtime not linked; build with `make build-engine`")

// DefaultThreshold is the clustering distance used when the speaker count is
// unknown. Lower splits more readily, higher merges more readily.
//
// It is stated here rather than left to the runtime because sherpa-onnx has no
// defaults getter for its diarization config: a zero-initialised struct means a
// threshold of zero, which collapses every turn into one speaker.
const DefaultThreshold = 0.5

// Minimum durations, in seconds, for a speech region to be kept and for a gap
// to end one. Same reason as above: these have no library-supplied defaults
// reachable from the C API.
const (
	defaultMinDurationOn  = 0.3
	defaultMinDurationOff = 0.5
)

// Models are the two ONNX files diarization needs. Segmentation finds speech
// regions and speaker changes; embedding turns each region into a vector that
// can be clustered, which is what actually groups regions into speakers.
type Models struct {
	Segmentation string
	Embedding    string
}

// Params control one diarization run.
type Params struct {
	// NumSpeakers pins the speaker count when it is known. Zero means cluster
	// by distance instead, which is the honest default: a recording's speaker
	// count is rarely known in advance, and guessing wrong forces real speakers
	// to merge or splits one across two labels.
	NumSpeakers int
	// Threshold is the clustering distance used when the count is unknown.
	// Zero takes DefaultThreshold. Lower splits more readily, higher merges.
	//
	// There is deliberately no min/max speaker bound: sherpa-onnx's clusterer
	// takes either an exact count or a distance, and nothing in between.
	// Exposing bounds it cannot honour would be a flag that does nothing.
	Threshold float64
	// Threads is the inference thread count; zero lets the runtime decide.
	Threads int
}

// Turn is one continuous stretch of speech attributed to one speaker.
type Turn struct {
	Start   float64
	End     float64
	Speaker int
}

// Progress reports completion as a percentage.
type Progress func(percent int)

// Validate rejects parameter combinations that cannot produce a sensible run.
func (p Params) Validate() error {
	if p.NumSpeakers < 0 {
		return errors.New("speaker count must not be negative")
	}
	if p.Threshold < 0 {
		return errors.New("clustering threshold must not be negative")
	}
	if p.NumSpeakers > 0 && p.Threshold > 0 {
		return fmt.Errorf("a pinned speaker count (%d) and a clustering threshold (%g) cannot both apply; the count wins, so drop one",
			p.NumSpeakers, p.Threshold)
	}
	return nil
}

// Speakers returns the distinct speaker indices present in a timeline.
func Speakers(turns []Turn) []int {
	seen := map[int]bool{}
	var out []int
	for _, t := range turns {
		if !seen[t.Speaker] {
			seen[t.Speaker] = true
			out = append(out, t.Speaker)
		}
	}
	return out
}
