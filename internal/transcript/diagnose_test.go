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

// spoken builds an undiarized transcript from one line of text per segment.
func spoken(lines ...string) Result {
	r := Result{Metadata: Metadata{Model: "m", Languages: []string{"ja"}}}
	for i, line := range lines {
		r.Segments = append(r.Segments, Segment{
			Start: float64(i) * 2, End: float64(i)*2 + 2, Speaker: SingleSpeaker,
			Text: map[string]string{"ja": line},
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

func repeat(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = line
	}
	return out
}

// find returns the diagnosis of the given kind, and whether it was reported.
func find(ds []Diagnosis, k Kind) (Diagnosis, bool) {
	for _, d := range ds {
		if d.Kind == k {
			return d, true
		}
	}
	return Diagnosis{}, false
}

// TestDiagnoseCatchesOverSplitting is the real case: a 39-minute recording with
// continuous music came back with 93 speakers across 624 segments, a third of
// them speaking exactly once. Well-formed, validated, and nonsense.
func TestDiagnoseCatchesOverSplitting(t *testing.T) {
	labels := manyLabels(60, 8) // 60 speakers who each say several things
	for i := range 32 {         // plus 32 who say exactly one
		labels = append(labels, SpeakerLabel(100+i))
	}

	d, flagged := find(Diagnose(diarized(labels...)), KindOverSplit)
	if !flagged {
		t.Fatal("over-splitting not flagged")
	}
	if !strings.Contains(d.Summary, "32 of them speaking once") {
		t.Errorf("summary does not report the singletons: %q", d.Summary)
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
			if d, flagged := find(Diagnose(r), KindOverSplit); flagged {
				t.Errorf("flagged a plausible cast: %s", d.Summary)
			}
		})
	}
}

// TestDiagnoseSaysNothingWithoutDiarization: an undiarized transcript has one
// nominal speaker by construction, and complaining would fire on every call.
func TestDiagnoseSaysNothingWithoutDiarization(t *testing.T) {
	r := diarized(manyLabels(50, 1)...)
	r.Metadata.Diarized = false

	if _, flagged := find(Diagnose(r), KindOverSplit); flagged {
		t.Error("flagged a transcript that was never diarized")
	}
}

func TestDiagnoseHandlesAnEmptyTranscript(t *testing.T) {
	r := Result{Metadata: Metadata{Diarized: true}}
	if ds := Diagnose(r); len(ds) != 0 {
		t.Errorf("flagged an empty transcript: %v", ds)
	}
}

// TestDiagnoseCatchesRepetitionLoops is the measured case from ADR-0008: on a
// 39-minute recording with continuous music, one model produced runs of 19, 45
// and 48 identical consecutive segments over three attempts. The audio under
// such a run is not mistranscribed — it is absent, and nothing in the result
// says so.
func TestDiagnoseCatchesRepetitionLoops(t *testing.T) {
	lines := append([]string{"最初の一文です。"}, repeat("ご視聴ありがとうございました。", 48)...)
	lines = append(lines, "続きの一文です。")

	d, flagged := find(Diagnose(spoken(lines...)), KindRepetitionLoop)
	if !flagged {
		t.Fatal("a run of 48 identical segments was not flagged")
	}
	if !strings.Contains(d.Summary, "longest run 48") {
		t.Errorf("summary does not report the run length: %q", d.Summary)
	}
	// 47 repeats of a 2-second segment: the span the loop swallowed.
	if !strings.Contains(d.Summary, "94s") {
		t.Errorf("summary does not report the swallowed audio: %q", d.Summary)
	}
	if !strings.Contains(d.Summary, "at 0:02") { // the run begins at the second segment
		t.Errorf("summary does not point at where it happened: %q", d.Summary)
	}
	if !strings.Contains(d.Advice, "--vad") || !strings.Contains(d.Advice, "--model") {
		t.Errorf("advice does not name the remedies: %q", d.Advice)
	}
}

// TestDiagnoseIsQuietOnOrdinaryRepetition: people really do repeat themselves,
// and a transcript of a conversation is full of short identical replies. Only a
// long consecutive run is a loop. The five-in-a-row case sits just above the
// longest run measured across three runs of a model that was NOT looping on the
// same audio (3), and must still stay quiet.
func TestDiagnoseIsQuietOnOrdinaryRepetition(t *testing.T) {
	for name, lines := range map[string][]string{
		"scattered agreement":     {"はい。", "そうですね。", "はい。", "なるほど。", "はい。", "ええ。", "はい。"},
		"three in a row":          {"始めます。", "はい。", "はい。", "はい。", "では次に。"},
		"five in a row":           append(repeat("はい。", 5), "では次に。"),
		"empty segments in a row": {"", "", "", "", "", "", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if d, flagged := find(Diagnose(spoken(lines...)), KindRepetitionLoop); flagged {
				t.Errorf("flagged ordinary speech: %s", d.Summary)
			}
		})
	}
}

// TestDiagnoseRepetitionCountsEveryLoop: a model that loops once usually loops
// again, and the total time lost matters more than any single run.
func TestDiagnoseRepetitionCountsEveryLoop(t *testing.T) {
	var lines []string
	lines = append(lines, repeat("A", 6)...)
	lines = append(lines, "話が戻ります。")
	lines = append(lines, repeat("B", 7)...)

	d, flagged := find(Diagnose(spoken(lines...)), KindRepetitionLoop)
	if !flagged {
		t.Fatal("two loops were not flagged")
	}
	if !strings.Contains(d.Summary, "2 repetition loop(s)") {
		t.Errorf("summary does not report both loops: %q", d.Summary)
	}
	if !strings.Contains(d.Summary, "11 repeated segments") { // 5 + 6
		t.Errorf("summary does not total the repeats: %q", d.Summary)
	}
}

// TestDiagnoseRepetitionComparesEveryLanguage: with --translate a segment
// carries two languages, and a loop repeats in both. Comparing one of them
// would make the check depend on map iteration order.
func TestDiagnoseRepetitionComparesEveryLanguage(t *testing.T) {
	r := Result{Metadata: Metadata{Model: "m", Translated: true}}
	for i := range 8 {
		text := map[string]string{"ja": "同じ文です。", "en": "The same line."}
		if i == 4 {
			text = map[string]string{"ja": "同じ文です。", "en": "A different line."}
		}
		r.Segments = append(r.Segments, Segment{
			Start: float64(i), End: float64(i) + 1, Speaker: SingleSpeaker, Text: text,
		})
	}

	// Runs of 4 and 3 once the differing English breaks the chain — under the
	// threshold, so nothing is reported even though the Japanese is identical
	// throughout.
	if d, flagged := find(Diagnose(r), KindRepetitionLoop); flagged {
		t.Errorf("a run broken by one language was still flagged: %s", d.Summary)
	}
}

// TestDiagnoseReportsBothProblemsAtOnce: the two checks are independent, and a
// bad run can hit both. Reporting only the first would hide the other.
func TestDiagnoseReportsBothProblemsAtOnce(t *testing.T) {
	r := diarized(manyLabels(50, 1)...)
	for i := range r.Segments {
		r.Segments[i].Text = map[string]string{"ja": "同じ文です。"}
	}

	ds := Diagnose(r)
	if _, ok := find(ds, KindOverSplit); !ok {
		t.Error("over-splitting not reported")
	}
	if _, ok := find(ds, KindRepetitionLoop); !ok {
		t.Error("repetition loop not reported")
	}
}
