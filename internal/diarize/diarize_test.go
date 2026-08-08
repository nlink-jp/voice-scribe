package diarize

import (
	"strings"
	"testing"
)

func TestValidateAcceptsTheUsableCombinations(t *testing.T) {
	for name, p := range map[string]Params{
		"defaults":       {},
		"pinned count":   {NumSpeakers: 3},
		"threshold only": {Threshold: 0.4},
		"threads":        {Threads: 4},
	} {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(); err != nil {
				t.Errorf("Validate rejected a usable combination: %v", err)
			}
		})
	}
}

// TestValidateRejectsACountAndAThresholdTogether is the case a user is most
// likely to hit: giving both, and quietly getting only one of them. The
// clusterer takes an exact count or a distance, never both, so saying so is
// better than silently dropping the threshold.
func TestValidateRejectsACountAndAThresholdTogether(t *testing.T) {
	err := Params{NumSpeakers: 2, Threshold: 0.4}.Validate()
	if err == nil {
		t.Fatal("Validate accepted both a pinned count and a threshold")
	}
	if !strings.Contains(err.Error(), "drop one") {
		t.Errorf("error %q should tell the user what to do about it", err)
	}
}

func TestValidateRejectsNegatives(t *testing.T) {
	for name, p := range map[string]Params{
		"negative count":     {NumSpeakers: -1},
		"negative threshold": {Threshold: -0.1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(); err == nil {
				t.Error("Validate accepted a negative value")
			}
		})
	}
}

func TestSpeakers(t *testing.T) {
	turns := []Turn{
		{Start: 0, End: 5, Speaker: 3},
		{Start: 5, End: 9, Speaker: 1},
		{Start: 9, End: 12, Speaker: 3},
	}
	if got := Speakers(turns); len(got) != 2 {
		t.Errorf("Speakers = %v, want two distinct indices", got)
	}
	if got := Speakers(nil); len(got) != 0 {
		t.Errorf("Speakers(nil) = %v, want empty", got)
	}
}

// TestRunWithoutModelsIsRejected keeps the failure at the call site rather than
// inside the runtime, where the message would be unrecognisable. It runs in
// both build configurations: without the runtime it surfaces ErrNoRuntime,
// which is equally a refusal to pretend.
func TestRunWithoutModels(t *testing.T) {
	_, err := Run([]float32{0, 0, 0}, Models{}, Params{}, nil)
	if err == nil {
		t.Fatal("Run succeeded with no models configured")
	}
}

func TestDefaultThresholdIsStated(t *testing.T) {
	// The value matters: zero means "cluster everything into one speaker",
	// which is what a zero-initialised sherpa config silently does.
	if DefaultThreshold <= 0 {
		t.Errorf("DefaultThreshold = %v; a zero threshold collapses every turn into one speaker", DefaultThreshold)
	}
}

func TestErrNoRuntimeMentionsTheFix(t *testing.T) {
	if msg := ErrNoRuntime.Error(); !strings.Contains(msg, "make build-engine") {
		t.Errorf("ErrNoRuntime = %q, want it to name `make build-engine`", msg)
	}
}
