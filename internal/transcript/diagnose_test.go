package transcript

import (
	"strings"
	"testing"
)

// diarized builds a transcript whose speaker labels follow the given pattern.
func diarized(labels ...string) Result {
	r := Result{Metadata: Metadata{Model: "m", Languages: []string{"ja"}, Diarized: true}}
	for i, l := range labels {
		r.Segments = append(r.Segments, Segment{
			Start: float64(i), End: float64(i) + 1, Speaker: l,
			Text: map[string]string{"ja": "x"},
		})
	}
	return r
}

func manyLabels(n, repeats int) []string {
	var out []string
	for i := range n {
		for range repeats {
			out = append(out, SpeakerLabel(i))
		}
	}
	return out
}

// TestDiagnoseCatchesOverSplitting is the real case: a 39-minute drama CD with
// continuous music came back with 93 speakers across 624 segments, a third of
// them speaking exactly once. Well-formed, validated, and nonsense.
func TestDiagnoseCatchesOverSplitting(t *testing.T) {
	labels := manyLabels(60, 8) // 60 speakers who each say several things
	for i := range 32 {         // plus 32 who say exactly one
		labels = append(labels, SpeakerLabel(100+i))
	}

	d, flagged := Diagnose(diarized(labels...))
	if !flagged {
		t.Fatalf("not flagged: %d speakers over %d segments", d.Speakers, d.Segments)
	}
	if d.Singletons != 32 {
		t.Errorf("Singletons = %d, want 32", d.Singletons)
	}
	if !strings.Contains(d.Advice, "raise") || !strings.Contains(d.Advice, "--speakers") {
		t.Errorf("advice does not name the remedies: %q", d.Advice)
	}
}

// TestDiagnoseIsQuietOnPlausibleCasts keeps the warning from becoming noise —
// a warning that fires on good results teaches people to ignore it.
func TestDiagnoseIsQuietOnPlausibleCasts(t *testing.T) {
	for name, r := range map[string]Result{
		"two speakers, short clip":  diarized("A", "B", "A", "B", "A", "B"),
		"one speaker":               diarized("A", "A", "A", "A"),
		"twelve-person meeting":     diarized(manyLabels(12, 20)...),
		"a crowd who all say a lot": diarized(manyLabels(30, 15)...),
	} {
		t.Run(name, func(t *testing.T) {
			if d, flagged := Diagnose(r); flagged {
				t.Errorf("flagged a plausible cast: %d speakers, %d segments, %d singletons",
					d.Speakers, d.Segments, d.Singletons)
			}
		})
	}
}

// TestDiagnoseSaysNothingWithoutDiarization: an undiarized transcript has one
// nominal speaker by construction, and complaining would fire on every call.
func TestDiagnoseSaysNothingWithoutDiarization(t *testing.T) {
	r := diarized(manyLabels(50, 1)...)
	r.Metadata.Diarized = false

	if _, flagged := Diagnose(r); flagged {
		t.Error("flagged a transcript that was never diarized")
	}
}

func TestDiagnoseHandlesAnEmptyTranscript(t *testing.T) {
	r := Result{Metadata: Metadata{Diarized: true}}
	if _, flagged := Diagnose(r); flagged {
		t.Error("flagged an empty transcript")
	}
}
