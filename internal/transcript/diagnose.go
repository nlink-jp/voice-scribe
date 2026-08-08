package transcript

// Diagnosis reports that a speaker assignment does not look like a real cast.
//
// Diarization can fail in a way that produces a perfectly well-formed result:
// every segment carries a label, the JSON validates, nothing errors. Continuous
// background music is the usual cause — the embedding model sees music mixed
// with voice, so the same person's embeddings scatter and the clusterer splits
// them. Handing that back without comment presents nonsense as an answer.
type Diagnosis struct {
	Speakers   int
	Segments   int
	Singletons int
	// Advice is what to do about it, in the caller's own vocabulary.
	Advice string
}

// diagnosisThresholds are deliberately loose. The aim is to catch a result that
// is obviously not a cast of people, not to police a plausible one.
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
)

// Diagnose reports whether the speaker labels look like over-splitting.
//
// It says nothing when diarization did not run: an undiarized transcript has
// one nominal speaker by construction, and complaining about that would be
// noise on every single call.
func Diagnose(r Result) (Diagnosis, bool) {
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

	d := Diagnosis{Speakers: len(counts), Segments: len(r.Segments), Singletons: singletons}
	if d.Speakers < minSuspiciousSpeakers {
		return Diagnosis{}, false
	}
	if singletons < d.Speakers/singletonShare && d.Speakers <= d.Segments/segmentShare {
		return Diagnosis{}, false
	}

	// The advice is the opposite of the under-splitting case, which is why it
	// is worth stating rather than leaving to the reader: a threshold is
	// lowered to split more, so it is raised to split less.
	d.Advice = "this many speakers usually means diarization over-split rather than that this many people spoke — " +
		"common with continuous background music. Pin the count with --speakers, or raise --speaker-threshold above " +
		"the default 0.5 to merge more readily."
	return d, true
}
