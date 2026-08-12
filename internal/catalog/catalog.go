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
	"strings"

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
	// SHA256 pins the exact bytes this catalog entry was built against.
	//
	// Size alone is not integrity — anyone who can substitute the file can keep
	// its length. It matters concretely here: whisper parses these files, and
	// upstream has already fixed a stack-buffer-overflow reachable from a
	// malformed tensor header, so a tampered model is a memory-safety problem
	// rather than merely a wrong transcript.
	SHA256  string
	License string
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
		SHA256:       e.SHA256,
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
		// Fetched from kotoba-tech, the model's own authors. A third-party
		// mirror labelled "v2.2" serves a byte-identical file (same SHA256,
		// same blob) -- so it is the same weights under a different version
		// number, and there is no reason to take the default model from an
		// unaffiliated re-upload.
		Name: "kotoba-whisper-v2.0",
		Kind: store.KindTranscription,
		// Was the Japanese default until ADR-0008 measured it against
		// large-v3-turbo on this machine: ahead on Common Voice by a margin too
		// small to call, behind on JSUT and on ReazonSpeech -- the corpus it was
		// trained on. Kept in the catalog, no longer suggested.
		Description:  "Japanese-specialised distil-whisper. Ahead of large-v3-turbo on Common Voice, behind it on JSUT and ReazonSpeech.",
		Repo:         "kotoba-tech/kotoba-whisper-v2.0-ggml",
		File:         "ggml-kotoba-whisper-v2.0-q5_0.bin",
		Language:     "ja",
		Quantization: "q5_0",
		SizeBytes:    537819875,
		SHA256:       "4a3b92192b5d3578ff854a5876213e2e27af0c2d357492c2d14271e82c303658",
		License:      "apache-2.0",
	},
	{
		Name:         "large-v3-turbo",
		Kind:         store.KindTranscription,
		Description:  "Multilingual, and the suggested model for Japanese. Near large-v3 accuracy at roughly half the inference time.",
		Repo:         "ggerganov/whisper.cpp",
		File:         "ggml-large-v3-turbo-q5_0.bin",
		Quantization: "q5_0",
		SizeBytes:    574041195,
		SHA256:       "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2",
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
		SHA256:       "d75795ecff3f83b5faa89d1900604ad8c780abd5739fae406de19f23ecd98ad1",
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
		SHA256:       "422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898",
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
		SHA256:      "220ad67ca923bef2fa91f2390c786097bf305bceb5e261d4af67b38e938e1079",
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
		SHA256:      "aa3cfc16963a10586a9393f5035d6d6b57e98d358b347f80c2a30bf4f00ceba2",
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
		SHA256:      "29940d98d42b91fbd05ce489f3ecf7c72f0a42f027e4875919a28fb4c04ea2cf",
		License:     "mit",
	},
}

// Expectations answers what a model should hash to, and which entry a hash
// belongs to. It satisfies store.Expectation, which is declared over there so
// that store does not have to import this package.
type Expectations struct{}

// HashFor returns the expected hash for a catalog entry.
func (Expectations) HashFor(name string) (string, bool) {
	e, ok := Lookup(name)
	if !ok || e.SHA256 == "" {
		return "", false
	}
	return e.SHA256, true
}

// NameForHash returns the catalog entry whose file has this hash.
//
// It is what lets an entry orphaned by a rename be recognised: the file on disk
// is unchanged, so its hash still identifies which model it actually is.
func (Expectations) NameForHash(sha string) (string, bool) {
	if sha == "" {
		return "", false
	}
	for _, e := range entries {
		if strings.EqualFold(e.SHA256, sha) {
			return e.Name, true
		}
	}
	return "", false
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
// suggested multilingual one when the language has no marked specialist.
//
// No language marks a specialist today: Japanese did until ADR-0008 measured
// the specialist against the multilingual model and the specialist lost. The
// branch stays because the question is per-language and per-model, and the next
// language may answer it the other way.
func DefaultFor(language string) (Entry, bool) { return defaultFor(entries, language) }

func defaultFor(entries []Entry, language string) (Entry, bool) {
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
