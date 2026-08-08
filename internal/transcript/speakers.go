package transcript

import "sort"

// SpeakerTurn is a stretch of audio attributed to one speaker by diarization.
// Speaker is a cluster index, which carries no meaning of its own — cluster 3
// is not "the third person to talk", it is just the label the clusterer used.
type SpeakerTurn struct {
	Start   float64
	End     float64
	Speaker int
}

// AssignSpeakers labels transcript segments from a diarization timeline.
//
// The two come from independent models with independent notions of where a
// boundary falls, so a segment is attributed to whichever speaker's turns cover
// most of it. A segment that no turn overlaps — diarization discards audio it
// considers non-speech, and whisper sometimes transcribes through it — inherits
// the previous segment's speaker, because a conversation continuing is a better
// guess than a speaker appearing for one line and vanishing.
//
// names, when non-empty, replaces the generated letters. It is matched in order
// of first appearance, so the first person to speak gets the first name given.
func AssignSpeakers(segments []Segment, turns []SpeakerTurn, names []string) []Segment {
	if len(segments) == 0 || len(turns) == 0 {
		return segments
	}

	// Cluster indices are arbitrary, but a reader expects "A" to be whoever
	// spoke first. Remap by first appearance in time.
	ordered := append([]SpeakerTurn(nil), turns...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })

	label := map[int]string{}
	for _, t := range ordered {
		if _, seen := label[t.Speaker]; !seen {
			label[t.Speaker] = nameAt(names, len(label))
		}
	}

	previous := ""
	for i := range segments {
		best, bestOverlap := -1, 0.0
		for _, t := range turns {
			if o := overlap(segments[i].Start, segments[i].End, t.Start, t.End); o > bestOverlap {
				best, bestOverlap = t.Speaker, o
			}
		}

		switch {
		case best >= 0:
			segments[i].Speaker = label[best]
			previous = segments[i].Speaker
		case previous != "":
			segments[i].Speaker = previous
		default:
			segments[i].Speaker = nameAt(names, 0)
		}
	}
	return segments
}

// nameAt returns the caller-supplied name for the i-th speaker, falling back to
// a generated letter. Falling back per-index rather than all-or-nothing means a
// user who names the two people they recognise still gets labels for the third.
func nameAt(names []string, i int) string {
	if i < len(names) && names[i] != "" {
		return names[i]
	}
	return SpeakerLabel(i)
}

// SpeakerLabel turns a zero-based index into A, B, ... Z, AA, AB, ...
func SpeakerLabel(i int) string {
	if i < 0 {
		i = 0
	}
	label := ""
	for {
		label = string(rune('A'+i%26)) + label
		i = i/26 - 1
		if i < 0 {
			return label
		}
	}
}
