package engine

import "testing"

// ramp returns sec seconds of samples whose value is their index, so a test
// can tell exactly which stretch of the original audio a slice came from.
func ramp(sec float64) []float32 {
	s := make([]float32, int(sec*sampleRate))
	for i := range s {
		s[i] = float32(i)
	}
	return s
}

func TestVADWindowCutsTheRequestedStretch(t *testing.T) {
	in := ramp(10)
	p := Params{VADModelPath: "vad.bin", OffsetSec: 2, DurationSec: 3}

	out, adjusted, shift := vadWindow(in, p)

	if want := 3 * sampleRate; len(out) != want {
		t.Fatalf("window holds %d samples, want %d", len(out), want)
	}
	if out[0] != float32(2*sampleRate) {
		t.Errorf("window starts at sample %v, want %v — the cut is off the requested offset", out[0], 2*sampleRate)
	}
	if shift != 2 {
		t.Errorf("shift = %v, want the offset (2) so timestamps land back on the original timeline", shift)
	}
	// Whisper must not apply the offset a second time, and VAD must see only
	// the window — both mean handing it zeroes.
	if adjusted.OffsetSec != 0 || adjusted.DurationSec != 0 {
		t.Errorf("params still carry offset/duration (%v/%v), want both zeroed", adjusted.OffsetSec, adjusted.DurationSec)
	}
	if adjusted.VADModelPath != "vad.bin" {
		t.Errorf("VADModelPath lost: %q", adjusted.VADModelPath)
	}
}

func TestVADWindowDurationOnly(t *testing.T) {
	out, adjusted, shift := vadWindow(ramp(10), Params{VADModelPath: "vad.bin", DurationSec: 4})

	if want := 4 * sampleRate; len(out) != want {
		t.Fatalf("window holds %d samples, want %d", len(out), want)
	}
	if out[0] != 0 || shift != 0 {
		t.Errorf("duration-only window should start at the beginning with no shift; got first sample %v, shift %v", out[0], shift)
	}
	if adjusted.DurationSec != 0 {
		t.Errorf("DurationSec = %v, want 0", adjusted.DurationSec)
	}
}

func TestVADWindowClampsToTheEnd(t *testing.T) {
	out, _, shift := vadWindow(ramp(10), Params{VADModelPath: "vad.bin", OffsetSec: 8, DurationSec: 60})

	if want := 2 * sampleRate; len(out) != want {
		t.Errorf("window holds %d samples, want the %d that exist past the offset", len(out), want)
	}
	if shift != 8 {
		t.Errorf("shift = %v, want 8", shift)
	}
}

func TestVADWindowPastTheEndComesBackEmpty(t *testing.T) {
	// The session turns an empty window into an error naming the offset,
	// instead of whisper's misleading "no speech" path (ADR-0009).
	out, _, _ := vadWindow(ramp(10), Params{VADModelPath: "vad.bin", OffsetSec: 11})
	if len(out) != 0 {
		t.Errorf("window past the end holds %d samples, want none", len(out))
	}
}

func TestVADWindowLeavesOtherRunsAlone(t *testing.T) {
	in := ramp(1)

	for name, p := range map[string]Params{
		"no VAD":    {OffsetSec: 2, DurationSec: 3},
		"no window": {VADModelPath: "vad.bin"},
	} {
		out, adjusted, shift := vadWindow(in, p)
		if len(out) != len(in) || shift != 0 || adjusted != p {
			t.Errorf("%s: vadWindow interfered (len %d→%d, shift %v, params %+v→%+v)",
				name, len(in), len(out), shift, p, adjusted)
		}
	}
}
