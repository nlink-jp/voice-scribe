package engine

// sampleRate is whisper's fixed input rate. The runtime accepts nothing else,
// which is what lets this file convert between seconds and sample counts
// without asking the caller. session_whisper.go pins it against the C header
// at compile time.
const sampleRate = 16000

// vadWindow makes VAD and an offset/duration request compose on the original
// timeline. Upstream applies them in the wrong order for that: whisper_full
// runs VAD first and hands whisper_full_with_state a buffer with the silence
// removed, so offset_ms counts seconds into the *compressed* audio — the run
// silently covers a different stretch than the one asked for (ADR-0009).
//
// So when both are in play, the window is cut here instead: the samples are
// sliced to the requested stretch, the offset and duration handed to whisper
// are zeroed so VAD sees only the window, and the returned shift is what the
// caller must add to every timestamp to land back on the original timeline.
//
// Without a VAD model, or without a window, everything passes through
// untouched and whisper's own offset handling applies as before.
func vadWindow(samples []float32, p Params) ([]float32, Params, float64) {
	if p.VADModelPath == "" || (p.OffsetSec <= 0 && p.DurationSec <= 0) {
		return samples, p, 0
	}

	start := 0
	shift := 0.0
	if p.OffsetSec > 0 {
		start = int(p.OffsetSec * sampleRate)
		shift = p.OffsetSec
		if start > len(samples) {
			start = len(samples)
		}
	}

	end := len(samples)
	if p.DurationSec > 0 {
		if n := start + int(p.DurationSec*sampleRate); n < end {
			end = n
		}
	}

	p.OffsetSec, p.DurationSec = 0, 0
	return samples[start:end], p, shift
}
