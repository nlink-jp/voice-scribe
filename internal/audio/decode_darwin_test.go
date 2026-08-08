//go:build darwin

package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeWAV builds a 16-bit PCM WAV by hand. Generating the fixture rather than
// committing one keeps the repository free of binary blobs and, more usefully,
// makes the expected result derivable: we know exactly what went in, so the
// test can assert that resampling and downmixing actually happened.
func writeWAV(t *testing.T, path string, seconds float64, rate, channels int, freq float64) {
	t.Helper()

	frames := int(seconds * float64(rate))
	data := new(bytes.Buffer)
	for i := 0; i < frames; i++ {
		v := math.Sin(2 * math.Pi * freq * float64(i) / float64(rate))
		sample := int16(v * 0.8 * math.MaxInt16)
		for c := 0; c < channels; c++ {
			binary.Write(data, binary.LittleEndian, sample)
		}
	}

	var out bytes.Buffer
	dataLen := uint32(data.Len())
	blockAlign := uint16(channels * 2)
	byteRate := uint32(rate) * uint32(blockAlign)

	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(36+dataLen))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	binary.Write(&out, binary.LittleEndian, uint32(16))
	binary.Write(&out, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&out, binary.LittleEndian, uint16(channels))
	binary.Write(&out, binary.LittleEndian, uint32(rate))
	binary.Write(&out, binary.LittleEndian, byteRate)
	binary.Write(&out, binary.LittleEndian, blockAlign)
	binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	binary.Write(&out, binary.LittleEndian, dataLen)
	out.Write(data.Bytes())

	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func peak(samples []float32) float64 {
	var max float64
	for _, s := range samples {
		if a := math.Abs(float64(s)); a > max {
			max = a
		}
	}
	return max
}

// TestDecodeResamplesAndDownmixes is the decoder's whole job in one assertion:
// whatever went in, what comes out is 16 kHz mono. The input here is 44.1 kHz
// stereo, so a decoder that merely copied bytes through would fail both checks.
func TestDecodeResamplesAndDownmixes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, path, 2.0, 44100, 2, 440)

	got, err := Decode(path)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	wantSamples := 2 * SampleRate
	if diff := math.Abs(float64(len(got.Samples) - wantSamples)); diff > float64(SampleRate)*0.05 {
		t.Errorf("got %d samples, want about %d (16 kHz mono for 2 seconds)", len(got.Samples), wantSamples)
	}
	if math.Abs(got.Duration-2.0) > 0.1 {
		t.Errorf("Duration = %g, want about 2.0", got.Duration)
	}
	// Measured 2026-08-08: a 0.8-amplitude tone duplicated across both channels
	// comes back at about 1.13, i.e. 0.8 x sqrt(2) — AVFoundation downmixes
	// with equal power, not by averaging, so correlated stereo can exceed unity.
	// That is left alone rather than clamped: whisper computes a log-mel
	// spectrogram from these floats and tolerates it, whereas normalising would
	// silently change levels between files. The range below is wide enough to
	// admit it and narrow enough to catch silence or a scaling mistake.
	if p := peak(got.Samples); p < 0.3 || p > 2.0 {
		t.Errorf("peak amplitude %g is outside the plausible range for a 0.8 tone", p)
	}
}

func TestDecodeAcceptsMonoAtTheTargetRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "already16k.wav")
	writeWAV(t, path, 1.0, SampleRate, 1, 220)

	got, err := Decode(path)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if diff := math.Abs(float64(len(got.Samples) - SampleRate)); diff > float64(SampleRate)*0.05 {
		t.Errorf("got %d samples, want about %d", len(got.Samples), SampleRate)
	}
}

// TestDecodeCompressedContainer covers the case the whole AVFoundation decision
// was made for: an m4a, which is what a phone or a meeting recorder produces.
func TestDecodeCompressedContainer(t *testing.T) {
	afconvert, err := exec.LookPath("afconvert")
	if err != nil {
		t.Skip("afconvert not available to build the fixture")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "tone.wav")
	dst := filepath.Join(dir, "tone.m4a")
	writeWAV(t, src, 1.5, 44100, 2, 440)

	if out, err := exec.Command(afconvert, "-f", "m4af", "-d", "aac", src, dst).CombinedOutput(); err != nil {
		t.Skipf("afconvert could not build the fixture: %v: %s", err, out)
	}

	got, err := Decode(dst)
	if err != nil {
		t.Fatalf("Decode(m4a): %v", err)
	}
	if math.Abs(got.Duration-1.5) > 0.2 {
		t.Errorf("Duration = %g, want about 1.5", got.Duration)
	}
	if p := peak(got.Samples); p < 0.2 {
		t.Errorf("peak amplitude %g: the m4a decoded to silence", p)
	}
}

func TestDecodeMissingFile(t *testing.T) {
	_, err := Decode(filepath.Join(t.TempDir(), "absent.wav"))
	if err == nil {
		t.Fatal("Decode accepted a missing file")
	}
}

// TestDecodeRejectsNonAudio pins that an unreadable input fails with a typed
// error the CLI can turn into advice, rather than a generic message.
func TestDecodeRejectsNonAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("this is not audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Decode(path)
	if err == nil {
		t.Fatal("Decode accepted a text file")
	}
	if !errors.Is(err, ErrUnsupportedFormat) && !errors.Is(err, ErrNoAudioTrack) {
		t.Errorf("error = %v, want it classified as unsupported or track-less", err)
	}
}

// TestUnsupportedFormatSuggestsAFix keeps the remux advice attached to the
// error: "unsupported container" alone leaves a user with an mkv stuck.
func TestUnsupportedFormatSuggestsAFix(t *testing.T) {
	err := classify(3, "cannot read this container")
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("classify(3) = %v, want ErrUnsupportedFormat", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("ffmpeg")) {
		t.Errorf("error %q should tell the user how to convert the file", err)
	}
}
