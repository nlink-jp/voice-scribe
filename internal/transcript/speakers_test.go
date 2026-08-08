package transcript

import "testing"

func lines(t *testing.T, segs []Segment) []string {
	t.Helper()
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		out = append(out, s.Speaker)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func threeLines() []Segment {
	return []Segment{
		seg(0, 5, "ja", "一つ目。"),
		seg(5, 10, "ja", "二つ目。"),
		seg(10, 15, "ja", "三つ目。"),
	}
}

func TestAssignSpeakersByGreatestOverlap(t *testing.T) {
	got := AssignSpeakers(threeLines(), []SpeakerTurn{
		{Start: 0, End: 5.5, Speaker: 7},
		{Start: 5.5, End: 9.8, Speaker: 2},
		{Start: 9.8, End: 15, Speaker: 7},
	}, nil)

	if want := []string{"A", "B", "A"}; !equal(lines(t, got), want) {
		t.Errorf("got %v, want %v", lines(t, got), want)
	}
}

// TestLabelsFollowFirstAppearance is why cluster indices are remapped: sherpa
// hands back arbitrary cluster numbers, and a reader expects "A" to be whoever
// spoke first, not whoever the clusterer happened to number lowest.
func TestLabelsFollowFirstAppearance(t *testing.T) {
	got := AssignSpeakers(threeLines(), []SpeakerTurn{
		{Start: 10, End: 15, Speaker: 0},
		{Start: 0, End: 5, Speaker: 9},
		{Start: 5, End: 10, Speaker: 4},
	}, nil)

	if want := []string{"A", "B", "C"}; !equal(lines(t, got), want) {
		t.Errorf("got %v, want %v — labels should follow time, not cluster index", lines(t, got), want)
	}
}

// TestUncoveredSegmentsInheritThePreviousSpeaker: diarization drops audio it
// considers non-speech, but whisper often transcribes through it. A speaker
// appearing for one line and vanishing is the worse guess.
func TestUncoveredSegmentsInheritThePreviousSpeaker(t *testing.T) {
	got := AssignSpeakers(threeLines(), []SpeakerTurn{
		{Start: 0, End: 5, Speaker: 0},
		// nothing covers 5-10
		{Start: 10, End: 15, Speaker: 1},
	}, nil)

	if want := []string{"A", "A", "B"}; !equal(lines(t, got), want) {
		t.Errorf("got %v, want %v", lines(t, got), want)
	}
}

func TestSpeakerHintsAreAppliedInOrderOfAppearance(t *testing.T) {
	got := AssignSpeakers(threeLines(), []SpeakerTurn{
		{Start: 0, End: 5, Speaker: 3},
		{Start: 5, End: 10, Speaker: 1},
		{Start: 10, End: 15, Speaker: 3},
	}, []string{"田中", "佐藤"})

	if want := []string{"田中", "佐藤", "田中"}; !equal(lines(t, got), want) {
		t.Errorf("got %v, want %v", lines(t, got), want)
	}
}

// TestPartialHintsStillLabelTheRest: naming the two people you recognise should
// not leave the third unlabelled.
func TestPartialHintsStillLabelTheRest(t *testing.T) {
	got := AssignSpeakers(threeLines(), []SpeakerTurn{
		{Start: 0, End: 5, Speaker: 0},
		{Start: 5, End: 10, Speaker: 1},
		{Start: 10, End: 15, Speaker: 2},
	}, []string{"田中"})

	if want := []string{"田中", "B", "C"}; !equal(lines(t, got), want) {
		t.Errorf("got %v, want %v", lines(t, got), want)
	}
}

func TestAssignSpeakersIsANoOpWithoutATimeline(t *testing.T) {
	got := AssignSpeakers(threeLines(), nil, nil)
	if want := []string{"A", "A", "A"}; !equal(lines(t, got), want) {
		t.Errorf("got %v, want the segments left alone", lines(t, got))
	}
}

// TestAssignedTranscriptStillValidates ties this back to the envelope, and
// covers the property the formatters depend on: more than one speaker means the
// renderers start labelling lines.
func TestAssignedTranscriptStillValidates(t *testing.T) {
	r := Result{
		Metadata: Metadata{Source: "a.m4a", Model: "m", Languages: []string{"ja"}},
		Segments: AssignSpeakers(threeLines(), []SpeakerTurn{
			{Start: 0, End: 5, Speaker: 0},
			{Start: 5, End: 15, Speaker: 1},
		}, nil),
	}
	r.Normalize()

	if err := r.Validate(); err != nil {
		t.Fatalf("diarized transcript failed validation: %v", err)
	}
	if got := len(r.Speakers()); got != 2 {
		t.Errorf("Speakers() = %d, want 2", got)
	}
}

func TestSpeakerLabel(t *testing.T) {
	for _, tc := range []struct {
		i    int
		want string
	}{
		{0, "A"}, {1, "B"}, {25, "Z"}, {26, "AA"}, {27, "AB"}, {51, "AZ"}, {52, "BA"}, {-1, "A"},
	} {
		if got := SpeakerLabel(tc.i); got != tc.want {
			t.Errorf("SpeakerLabel(%d) = %q, want %q", tc.i, got, tc.want)
		}
	}
}
