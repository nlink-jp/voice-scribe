package transcript

import (
	"fmt"
	"sort"
	"strings"
)

// Diagnosis reports a transcript that is well-formed but probably wrong.
//
// Both failure modes caught here share a shape: every field is populated, the
// JSON validates, nothing errors, and the result is nonsense. Handing that back
// without comment presents nonsense as an answer.
type Diagnosis struct {
	// Kind names the check, so a caller can filter or count without parsing
	// prose.
	Kind Kind
	// Summary is what was observed; Advice is what to do about it.
	Summary string
	Advice  string
}

// Kind identifies a diagnosis.
type Kind string

const (
	// KindOverSplit: diarization produced more speakers than a real cast.
	KindOverSplit Kind = "over-split-speakers"
	// KindRepetitionLoop: the decoder repeated one line instead of decoding.
	KindRepetitionLoop Kind = "repetition-loop"
)

func (d Diagnosis) String() string { return d.Summary + " " + d.Advice }

// diagnosisThresholds are deliberately loose. The aim is to catch a result that
// is obviously wrong, not to police a plausible one.
const (
	// Below this many speakers, nothing is said: a genuine meeting can have a
	// dozen participants, and a short clip can legitimately have a speaker who
	// says one thing.
	minSuspiciousSpeakers = 8
	// A cast where a large share of "people" speak exactly once is the
	// signature of over-splitting, not of a crowd.
	singletonShare = 4 // i.e. a quarter
	// So is a speaker count that approaches the segment count.
	segmentShare = 4
	// A run of identical consecutive segments this long is a decoder loop, not
	// speech. Measured on a 39-minute recording with continuous background
	// music, three runs per model (ADR-0008): the looping model hit runs of
	// 19, 45 and 48, and the one that did not loop never exceeded 3. The same
	// input does not give the same transcript twice, so the threshold sits in
	// the gap between two ranges rather than next to a single observation.
	minLoopRun = 6
)

// Diagnose returns every problem worth mentioning about a finished transcript,
// in the order the checks are defined. An empty slice means nothing looked
// wrong -- which is the common case, and why callers must not treat a
// diagnosis as an error.
func Diagnose(r Result) []Diagnosis {
	var out []Diagnosis
	if d, ok := diagnoseSpeakers(r); ok {
		out = append(out, d)
	}
	if d, ok := diagnoseRepetition(r); ok {
		out = append(out, d)
	}
	return out
}

// diagnoseSpeakers reports whether the speaker labels look like over-splitting.
//
// It says nothing when diarization did not run: an undiarized transcript has
// one nominal speaker by construction, and complaining about that would be
// noise on every single call.
//
// Continuous background music is the usual cause -- the embedding model sees
// music mixed with voice, so one person's embeddings scatter and the clusterer
// splits them.
func diagnoseSpeakers(r Result) (Diagnosis, bool) {
	if !r.Metadata.Diarized || len(r.Segments) == 0 {
		return Diagnosis{}, false
	}

	counts := map[string]int{}
	for _, s := range r.Segments {
		counts[s.Speaker]++
	}

	singletons := 0
	for _, n := range counts {
		if n == 1 {
			singletons++
		}
	}

	speakers, segments := len(counts), len(r.Segments)
	if speakers < minSuspiciousSpeakers {
		return Diagnosis{}, false
	}
	if singletons < speakers/singletonShare && speakers <= segments/segmentShare {
		return Diagnosis{}, false
	}

	return Diagnosis{
		Kind: KindOverSplit,
		Summary: fmt.Sprintf("%d speakers across %d segments (%d of them speaking once).",
			speakers, segments, singletons),
		// The advice is the opposite of the under-splitting case, which is why
		// it is worth stating rather than leaving to the reader: a threshold is
		// lowered to split more, so it is raised to split less.
		Advice: "this many speakers usually means diarization over-split rather than that this many people spoke — " +
			"common with continuous background music. Pin the count with --speakers, or raise --speaker-threshold above " +
			"the default 0.5 to merge more readily.",
	}, true
}

// diagnoseRepetition reports whether the decoder fell into a repetition loop.
//
// Whisper emits the same line over and over when it loses the thread, most
// often over music or noise rather than over speech: the audio in that stretch
// is not transcribed at all, and the transcript says nothing about it. Callers
// see well-formed segments with plausible timestamps.
func diagnoseRepetition(r Result) (Diagnosis, bool) {
	longest, at, loops, repeats, seconds := 0, 0.0, 0, 0, 0.0

	for i := 0; i < len(r.Segments); {
		key := fingerprint(r.Segments[i])
		j := i
		for j+1 < len(r.Segments) && fingerprint(r.Segments[j+1]) == key {
			j++
		}
		if run := j - i + 1; key != "" && run >= minLoopRun {
			loops++
			// The first utterance of a run may be real; the repeats are the
			// waste, and their span is the audio the run swallowed.
			repeats += run - 1
			seconds += r.Segments[j].End - r.Segments[i+1].Start
			if run > longest {
				longest, at = run, r.Segments[i].Start
			}
		}
		i = j + 1
	}

	if loops == 0 {
		return Diagnosis{}, false
	}

	return Diagnosis{
		Kind: KindRepetitionLoop,
		Summary: fmt.Sprintf("%d repetition loop(s): %d repeated segments covering %.0fs of audio, longest run %d at %s.",
			loops, repeats, seconds, longest, timecode(at)),
		Advice: "the decoder repeated one line instead of transcribing — that audio is missing from the transcript, " +
			"not mistranscribed. It usually happens over music or noise. Gate non-speech with --vad " +
			"(needs `models pull silero-vad`), or try another model with --model.",
	}, true
}

// fingerprint is the comparable content of a segment: its text in every
// language, in a stable order. Whitespace-only text returns "" so that silent
// or empty segments never count as a loop.
func fingerprint(s Segment) string {
	langs := make([]string, 0, len(s.Text))
	for lang := range s.Text {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	var b strings.Builder
	empty := true
	for _, lang := range langs {
		text := strings.TrimSpace(s.Text[lang])
		if text != "" {
			empty = false
		}
		b.WriteString(lang)
		b.WriteByte('=')
		b.WriteString(text)
		b.WriteByte('\n')
	}
	if empty {
		return ""
	}
	return b.String()
}

// timecode renders seconds as mm:ss so the warning points at a place in the
// audio the reader can actually go and listen to.
func timecode(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%d:%02d", int(sec)/60, int(sec)%60)
}
