package transcript

import "testing"

func seg(start, end float64, lang, text string) Segment {
	return Segment{Start: start, End: end, Speaker: SingleSpeaker, Text: map[string]string{lang: text}}
}

func japanese() []Segment {
	return []Segment{
		seg(0, 5, "ja", "こんにちは。"),
		seg(5, 12, "ja", "本日はテストです。"),
	}
}

// TestMergeLanguageAttachesByGreatestOverlap is the normal case: the
// translation pass chose slightly different boundaries, and each span still has
// to land on the right segment.
func TestMergeLanguageAttachesByGreatestOverlap(t *testing.T) {
	got := MergeLanguage(japanese(), "en", []Timed{
		{Start: 0.2, End: 4.8, Text: "Hello."},
		{Start: 4.9, End: 11.5, Text: "Today is a test."},
	})

	if got[0].Text["en"] != "Hello." {
		t.Errorf("segment 0 got %q, want the overlapping translation", got[0].Text["en"])
	}
	if got[1].Text["en"] != "Today is a test." {
		t.Errorf("segment 1 got %q, want the overlapping translation", got[1].Text["en"])
	}
	if got[0].Text["ja"] != "こんにちは。" {
		t.Error("the original text was overwritten")
	}
}

// TestMergeLanguageJoinsWhenTheTranslationSplitFiner covers the case that would
// otherwise silently discard text: two spans landing on one segment.
func TestMergeLanguageJoinsWhenTheTranslationSplitFiner(t *testing.T) {
	got := MergeLanguage([]Segment{seg(0, 10, "ja", "長い一文です。")}, "en", []Timed{
		{Start: 0, End: 4, Text: "One."},
		{Start: 4, End: 9, Text: "Two."},
	})

	if got[0].Text["en"] != "One. Two." {
		t.Errorf("got %q, want both translated fragments kept", got[0].Text["en"])
	}
}

// TestMergeLanguageDropsUnmatched: a span with no overlap has no defensible
// home, and guessing would attach text to the wrong moment.
func TestMergeLanguageDropsUnmatched(t *testing.T) {
	got := MergeLanguage([]Segment{seg(0, 5, "ja", "こんにちは。")}, "en", []Timed{
		{Start: 30, End: 35, Text: "Stray."},
	})

	if _, ok := got[0].Text["en"]; ok {
		t.Errorf("a non-overlapping span was attached anyway: %v", got[0].Text)
	}
}

func TestMergeLanguageIsANoOpWithoutUsableInput(t *testing.T) {
	for name, call := range map[string]func() []Segment{
		"nil input":      func() []Segment { return MergeLanguage(japanese(), "en", nil) },
		"empty language": func() []Segment { return MergeLanguage(japanese(), "", []Timed{{End: 5, Text: "x"}}) },
		"empty text":     func() []Segment { return MergeLanguage(japanese(), "en", []Timed{{End: 5}}) },
	} {
		t.Run(name, func(t *testing.T) {
			got := call()
			if len(got[0].Text) != 1 {
				t.Errorf("segments changed: %v", got[0].Text)
			}
		})
	}
}

// TestMergedTranscriptStillValidates ties the merge back to the envelope: a
// two-language result has to survive the checks the renderers apply.
func TestMergedTranscriptStillValidates(t *testing.T) {
	r := Result{
		Metadata: Metadata{Source: "a.m4a", Model: "m", Languages: []string{"ja", "en"}},
		Segments: MergeLanguage(japanese(), "en", []Timed{
			{Start: 0, End: 5, Text: "Hello."},
			{Start: 5, End: 12, Text: "Today is a test."},
		}),
	}
	r.Normalize()

	if err := r.Validate(); err != nil {
		t.Fatalf("merged transcript failed validation: %v", err)
	}
	files, err := Render(r, FormatSRT)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("got %d subtitle files for two languages, want 2", len(files))
	}
}

func TestOverlap(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		aS, aE, bS, bE, want float64
	}{
		{"identical", 0, 5, 0, 5, 5},
		{"partial", 0, 5, 3, 8, 2},
		{"contained", 0, 10, 2, 4, 2},
		{"touching", 0, 5, 5, 10, 0},
		{"disjoint", 10, 20, 0, 5, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := overlap(tc.aS, tc.aE, tc.bS, tc.bE); got != tc.want {
				t.Errorf("overlap = %g, want %g", got, tc.want)
			}
		})
	}
}
