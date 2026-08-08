// Package catalog is the curated list of models voice-scribe can install.
//
// Whisper-family models trade off size, speed and language fit sharply enough
// that asking a user to pick one from Hugging Face means the tool goes unused.
// The catalog hides that: each entry carries the repository, the exact file,
// and the defaults that make the model work, so `models pull <name>` is the
// whole interaction.
//
// Every field here was verified against the Hugging Face API on 2026-08-08.
// Sizes and licenses are recorded rather than guessed; when adding an entry,
// check the upstream repository rather than copying a neighbour's values.
package catalog

import (
	"fmt"
	"sort"

	"github.com/nlink-jp/voice-scribe/internal/store"
)

// Entry is a model that can be installed by name.
type Entry struct {
	Name        string
	Kind        store.Kind
	Description string

	// Repo and File locate the weights on Hugging Face.
	Repo string
	File string

	// Language is the ISO 639-1 code a model is specialised for; empty means
	// multilingual.
	Language     string
	Quantization string
	SizeBytes    int64
	License      string
	// Default marks the entry suggested for its language when nothing is set.
	Default bool
	// Role separates the two halves of a diarization pair. It is empty for
	// every other kind.
	Role Role
}

// Role distinguishes the two models diarization needs from each other.
type Role string

const (
	RoleSegmentation Role = "segmentation"
	RoleEmbedding    Role = "embedding"
)

// URL is where the weights are fetched from.
func (e Entry) URL() string {
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", e.Repo, e.File)
}

// Model converts a catalog entry into a registry record for a downloaded file.
func (e Entry) Model(path string) store.Model {
	return store.Model{
		Name:         e.Name,
		Kind:         e.Kind,
		Path:         path,
		Language:     e.Language,
		Quantization: e.Quantization,
		SizeBytes:    e.SizeBytes,
		License:      e.License,
		Source:       e.Repo + "/" + e.File,
		Role:         string(e.Role),
	}
}

// DiarizationPair returns the default segmentation and embedding entries.
// Diarization needs both; neither is useful alone.
func DiarizationPair() (segmentation, embedding Entry, ok bool) {
	for _, e := range entries {
		if e.Kind != store.KindDiarization || !e.Default {
			continue
		}
		switch e.Role {
		case RoleSegmentation:
			segmentation = e
		case RoleEmbedding:
			embedding = e
		}
	}
	return segmentation, embedding, segmentation.Name != "" && embedding.Name != ""
}

// entries is the catalog. Quantized builds are preferred throughout: q5_0 is
// roughly half the size of the full weights at a quality difference that does
// not show up in practice, and the difference between 537 MB and 1.5 GB does.
var entries = []Entry{
	{
		Name:         "kotoba-whisper-v2.2",
		Kind:         store.KindTranscription,
		Description:  "Japanese-specialised distil-whisper. ~6x faster than large-v3 at comparable error rate.",
		Repo:         "kenrouse/kotoba-whisper-v2.2-ggml",
		File:         "kotoba-whisper-v2.2-ggml-q5_0.bin",
		Language:     "ja",
		Quantization: "q5_0",
		SizeBytes:    537819875,
		License:      "apache-2.0",
		Default:      true,
	},
	{
		Name:         "kotoba-whisper-v2.0",
		Kind:         store.KindTranscription,
		Description:  "Japanese-specialised distil-whisper, published by the model's own authors.",
		Repo:         "kotoba-tech/kotoba-whisper-v2.0-ggml",
		File:         "ggml-kotoba-whisper-v2.0-q5_0.bin",
		Language:     "ja",
		Quantization: "q5_0",
		SizeBytes:    537819875,
		License:      "apache-2.0",
	},
	{
		Name:         "large-v3-turbo",
		Kind:         store.KindTranscription,
		Description:  "Multilingual. Near large-v3 accuracy at roughly half the inference time.",
		Repo:         "ggerganov/whisper.cpp",
		File:         "ggml-large-v3-turbo-q5_0.bin",
		Quantization: "q5_0",
		SizeBytes:    574041195,
		License:      "mit",
		Default:      true,
	},
	{
		Name:         "large-v3",
		Kind:         store.KindTranscription,
		Description:  "Multilingual, highest accuracy and the slowest of the three.",
		Repo:         "ggerganov/whisper.cpp",
		File:         "ggml-large-v3-q5_0.bin",
		Quantization: "q5_0",
		SizeBytes:    1081140203,
		License:      "mit",
	},
	{
		Name:         "base",
		Kind:         store.KindTranscription,
		Description:  "Multilingual, small and fast. Accuracy is well below the large models; useful for smoke tests.",
		Repo:         "ggerganov/whisper.cpp",
		File:         "ggml-base-q5_1.bin",
		Quantization: "q5_1",
		SizeBytes:    59707625,
		License:      "mit",
	},
	{
		// Speaker diarization needs two models working together: segmentation
		// finds speech regions and speaker changes, embedding turns each region
		// into a vector that can be clustered. Neither is useful alone, so
		// `models pull` for either one tells the user about the other.
		Name:        "pyannote-segmentation-3",
		Kind:        store.KindDiarization,
		Description: "Speaker segmentation for --diarize. Pair with an embedding model.",
		Repo:        "csukuangfj/sherpa-onnx-pyannote-segmentation-3-0",
		File:        "model.onnx",
		SizeBytes:   5992913,
		// The ONNX export ships pyannote's own MIT licence, (c) 2022 CNRS. The
		// upstream Hugging Face repo is gated; this mirror is not.
		License: "mit",
		Default: true,
		Role:    RoleSegmentation,
	},
	{
		Name:        "campplus-speaker-embedding",
		Kind:        store.KindDiarization,
		Description: "Speaker embedding for --diarize. Trained on Chinese and English; voice identity transfers across languages.",
		Repo:        "csukuangfj/speaker-embedding-models",
		File:        "3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx",
		SizeBytes:   28281164,
		License:     "apache-2.0",
		Default:     true,
		Role:        RoleEmbedding,
	},
	{
		Name:        "silero-vad",
		Kind:        store.KindVAD,
		Description: "Voice-activity detection. Enables --vad, which suppresses hallucinated text over silence.",
		Repo:        "ggml-org/whisper-vad",
		File:        "ggml-silero-v5.1.2.bin",
		SizeBytes:   885098,
		License:     "mit",
	},
}

// All returns the catalog, sorted by name.
func All() []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup finds an entry by name.
func Lookup(name string) (Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// DefaultFor returns the suggested transcription model for a language, or the
// suggested multilingual one when the language has no specialist.
func DefaultFor(language string) (Entry, bool) {
	for _, e := range entries {
		if e.Kind == store.KindTranscription && e.Default && e.Language == language {
			return e, true
		}
	}
	for _, e := range entries {
		if e.Kind == store.KindTranscription && e.Default && e.Language == "" {
			return e, true
		}
	}
	return Entry{}, false
}
