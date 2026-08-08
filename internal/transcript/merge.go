package transcript

// Timed is a span of text with no language attached — the shape the runtime
// produces, before it is placed into the language-keyed envelope.
type Timed struct {
	Start float64
	End   float64
	Text  string
}

// MergeLanguage attaches a second language's text to an existing set of
// segments, matching by time.
//
// It exists because whisper's translate task is a separate decode, not an extra
// output: one run yields either the transcription or the English translation.
// Producing both means running twice, and the two runs pick their own segment
// boundaries, so the results cannot simply be zipped together.
//
// Each incoming span is attached to whichever segment it overlaps most. This is
// an approximation: differing boundaries mean a translated sentence can straddle
// two transcribed ones, so a translation may read as slightly offset from the
// original beside it. Spans that overlap nothing are dropped rather than
// guessed at, because attaching text to the wrong moment is worse than omitting
// it.
func MergeLanguage(segments []Segment, lang string, incoming []Timed) []Segment {
	if lang == "" {
		return segments
	}

	for _, in := range incoming {
		if in.Text == "" {
			continue
		}

		best := -1
		bestOverlap := 0.0
		for i, s := range segments {
			if o := overlap(s.Start, s.End, in.Start, in.End); o > bestOverlap {
				best, bestOverlap = i, o
			}
		}
		if best < 0 {
			continue
		}

		if segments[best].Text == nil {
			segments[best].Text = map[string]string{}
		}
		// Several spans can land on one segment when the translation split more
		// finely; join them rather than letting the last one win and silently
		// discard the rest.
		if existing := segments[best].Text[lang]; existing != "" {
			segments[best].Text[lang] = existing + " " + in.Text
		} else {
			segments[best].Text[lang] = in.Text
		}
	}
	return segments
}

// overlap returns the length of the intersection of two time spans, zero when
// they do not intersect.
func overlap(aStart, aEnd, bStart, bEnd float64) float64 {
	start := max(aStart, bStart)
	end := min(aEnd, bEnd)
	if end <= start {
		return 0
	}
	return end - start
}
