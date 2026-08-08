//go:build cgo_sherpa

package diarize

import (
	"os"
	"testing"

	"github.com/nlink-jp/voice-scribe/internal/audio"
)

// Opt-in end-to-end test against the real runtime and real models.
//
// It is opt-in because it needs two ONNX models installed and a recording to
// point at, neither of which belongs in the repository. Run it as:
//
//	VOICE_SCRIBE_TEST_SEGMENTATION=... VOICE_SCRIBE_TEST_EMBEDDING=... \
//	VOICE_SCRIBE_TEST_AUDIO=meeting.m4a go test -tags "cgo_whisper cgo_sherpa" ./internal/diarize/
//
// It is also the tool for diagnosing a mislabelled transcript: it prints the
// timeline sherpa actually produced, which is what separates "the clusterer got
// it wrong" from "the assignment in transcript.AssignSpeakers got it wrong".
func TestRealRuntimeDiarization(t *testing.T) {
	models := Models{
		Segmentation: os.Getenv("VOICE_SCRIBE_TEST_SEGMENTATION"),
		Embedding:    os.Getenv("VOICE_SCRIBE_TEST_EMBEDDING"),
	}
	path := os.Getenv("VOICE_SCRIBE_TEST_AUDIO")
	if models.Segmentation == "" || models.Embedding == "" || path == "" {
		t.Skip("set VOICE_SCRIBE_TEST_SEGMENTATION, VOICE_SCRIBE_TEST_EMBEDDING and VOICE_SCRIBE_TEST_AUDIO to run")
	}

	decoded, err := audio.Decode(path)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	turns, err := Run(decoded.Samples, models, Params{}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(turns) == 0 {
		t.Fatal("no speech found at all")
	}

	t.Logf("auto threshold %.2f: %d turns, %d speakers", DefaultThreshold, len(turns), len(Speakers(turns)))
	for _, turn := range turns {
		t.Logf("  %6.2f-%6.2f  speaker %d", turn.Start, turn.End, turn.Speaker)
	}

	// Sweep the clustering threshold. The turn boundaries come from the
	// segmentation model and do not move; only the grouping does. Seeing that
	// laid out is what tells a user whether --speaker-threshold can rescue a
	// recording that came back with everyone merged into one speaker.
	for _, threshold := range []float64{0.2, 0.3, 0.4, 0.5, 0.7, 0.9} {
		swept, err := Run(decoded.Samples, models, Params{Threshold: threshold}, nil)
		if err != nil {
			t.Fatalf("Run(threshold=%.2f): %v", threshold, err)
		}
		t.Logf("threshold %.2f -> %d turns, %d speakers", threshold, len(swept), len(Speakers(swept)))
	}

	// Turns must be ordered and non-overlapping in time for the assignment
	// above to be meaningful.
	for i := 1; i < len(turns); i++ {
		if turns[i].Start < turns[i-1].Start {
			t.Errorf("turn %d starts before turn %d; the timeline is not sorted", i, i-1)
		}
	}
	for _, turn := range turns {
		if turn.End <= turn.Start {
			t.Errorf("turn %v has no duration", turn)
		}
	}
}
