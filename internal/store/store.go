// Package store keeps the registry of installed models.
//
// The registry lives with the tool's data, while the weights themselves may sit
// anywhere — models run to hundreds of megabytes each, so a user with a small
// boot volume needs to be able to move them without losing track of what is
// installed. Every registry entry therefore records an absolute path rather
// than assuming a layout.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Kind separates models that are not interchangeable. Handing a VAD model to
// the transcriber fails deep inside the runtime with an unhelpful message, so
// the mismatch is caught here instead.
type Kind string

const (
	KindTranscription Kind = "transcription"
	KindVAD           Kind = "vad"
	// KindDiarization is reserved for the speaker-segmentation and embedding
	// models Phase 2a adds.
	KindDiarization Kind = "diarization"
)

// Kinds lists every valid kind, for help text and validation.
func Kinds() []Kind { return []Kind{KindTranscription, KindVAD, KindDiarization} }

// ParseKind resolves a user-supplied kind.
func ParseKind(s string) (Kind, error) {
	for _, k := range Kinds() {
		if string(k) == s {
			return k, nil
		}
	}
	names := make([]string, 0, len(Kinds()))
	for _, k := range Kinds() {
		names = append(names, string(k))
	}
	return "", fmt.Errorf("unknown model kind %q (want one of: %s)", s, strings.Join(names, ", "))
}

// Model is one installed model.
type Model struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Path is absolute, so that moving the models directory does not orphan
	// entries installed before the move.
	Path string `json:"path"`
	// Language is the ISO 639-1 code a model is specialised for, empty when it
	// is multilingual. Used to pick a default model for a requested language.
	Language     string `json:"language,omitempty"`
	Quantization string `json:"quantization,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	License      string `json:"license,omitempty"`
	Source       string `json:"source,omitempty"`
}

// Multilingual reports whether the model handles languages other than one.
func (m Model) Multilingual() bool { return m.Language == "" }

// Store is the on-disk registry.
type Store struct {
	mu        sync.Mutex
	dataDir   string
	modelsDir string
}

type registry struct {
	Version int     `json:"version"`
	Models  []Model `json:"models"`
}

const registryVersion = 1

// New opens (and creates, if needed) the registry.
//
// modelsDir may be empty, in which case models are placed under the data
// directory. It only affects where new downloads land: existing entries keep
// the absolute path they were registered with.
func New(dataDir, modelsDir string) (*Store, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("data directory is empty")
	}
	if modelsDir == "" {
		modelsDir = filepath.Join(dataDir, "models")
	}

	abs, err := filepath.Abs(modelsDir)
	if err != nil {
		return nil, fmt.Errorf("resolve models directory: %w", err)
	}
	return &Store{dataDir: dataDir, modelsDir: abs}, nil
}

// DefaultDataDir returns where the registry lives, honouring XDG.
func DefaultDataDir(getenv func(string) string, home string) string {
	if xdg := getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "voice-scribe")
	}
	return filepath.Join(home, ".local", "share", "voice-scribe")
}

// ModelsDir is where new downloads are written.
func (s *Store) ModelsDir() string { return s.modelsDir }

func (s *Store) registryPath() string { return filepath.Join(s.dataDir, "registry.json") }

func (s *Store) load() (registry, error) {
	var reg registry
	b, err := os.ReadFile(s.registryPath())
	if os.IsNotExist(err) {
		return registry{Version: registryVersion}, nil
	}
	if err != nil {
		return reg, fmt.Errorf("read registry: %w", err)
	}
	if err := json.Unmarshal(b, &reg); err != nil {
		return reg, fmt.Errorf("registry %s is corrupt: %w", s.registryPath(), err)
	}
	return reg, nil
}

func (s *Store) save(reg registry) error {
	reg.Version = registryVersion
	sort.Slice(reg.Models, func(i, j int) bool { return reg.Models[i].Name < reg.Models[j].Name })

	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}

	// Write to a sibling and rename: a crash mid-write would otherwise leave a
	// truncated registry, losing the record of every installed model.
	tmp := s.registryPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	if err := os.Rename(tmp, s.registryPath()); err != nil {
		return fmt.Errorf("replace registry: %w", err)
	}
	return nil
}

// List returns the installed models, sorted by name.
func (s *Store) List() ([]Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reg, err := s.load()
	if err != nil {
		return nil, err
	}
	return reg.Models, nil
}

// Get returns one model by name.
func (s *Store) Get(name string) (Model, bool, error) {
	models, err := s.List()
	if err != nil {
		return Model{}, false, err
	}
	for _, m := range models {
		if m.Name == name {
			return m, true, nil
		}
	}
	return Model{}, false, nil
}

// Add registers a model, replacing any existing entry with the same name.
func (s *Store) Add(m Model) error {
	if m.Name == "" {
		return fmt.Errorf("model name is empty")
	}
	if m.Kind == "" {
		return fmt.Errorf("model %s: kind is empty", m.Name)
	}
	if !filepath.IsAbs(m.Path) {
		return fmt.Errorf("model %s: path %q is not absolute", m.Name, m.Path)
	}
	if _, err := os.Stat(m.Path); err != nil {
		return fmt.Errorf("model %s: %w", m.Name, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	reg, err := s.load()
	if err != nil {
		return err
	}
	replaced := false
	for i := range reg.Models {
		if reg.Models[i].Name == m.Name {
			reg.Models[i] = m
			replaced = true
			break
		}
	}
	if !replaced {
		reg.Models = append(reg.Models, m)
	}
	return s.save(reg)
}

// Remove deregisters a model. deleteFile also removes the weights from disk.
func (s *Store) Remove(name string, deleteFile bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	reg, err := s.load()
	if err != nil {
		return err
	}

	kept := reg.Models[:0]
	var removed *Model
	for _, m := range reg.Models {
		if m.Name == name {
			cp := m
			removed = &cp
			continue
		}
		kept = append(kept, m)
	}
	if removed == nil {
		return fmt.Errorf("model %q is not installed", name)
	}
	reg.Models = kept

	if err := s.save(reg); err != nil {
		return err
	}
	if deleteFile {
		// Deregistering succeeded, so a failure to unlink is worth reporting
		// but must not look like the removal did not happen.
		if err := os.Remove(removed.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("deregistered %s but could not delete %s: %w", name, removed.Path, err)
		}
	}
	return nil
}

// Resolve picks the model to use for a run.
//
// An explicit name wins. Otherwise the preferred name is used if it is
// installed, and failing that any installed transcription model specialised for
// the requested language, and failing that any multilingual one. The fallback
// chain exists so that a fresh install with one model works without
// configuration, rather than insisting the user name it.
func (s *Store) Resolve(explicit, preferred, language string) (Model, error) {
	models, err := s.List()
	if err != nil {
		return Model{}, err
	}

	if explicit != "" {
		for _, m := range models {
			if m.Name == explicit {
				if m.Kind != KindTranscription {
					return Model{}, fmt.Errorf("model %q is a %s model, not a transcription model", explicit, m.Kind)
				}
				return m, nil
			}
		}
		return Model{}, fmt.Errorf("model %q is not installed (run `voice-scribe models pull %s`)", explicit, explicit)
	}

	var transcription []Model
	for _, m := range models {
		if m.Kind == KindTranscription {
			transcription = append(transcription, m)
		}
	}
	if len(transcription) == 0 {
		return Model{}, fmt.Errorf("no transcription model is installed (run `voice-scribe models pull kotoba-whisper-v2.2`)")
	}

	if preferred != "" {
		for _, m := range transcription {
			if m.Name == preferred {
				return m, nil
			}
		}
	}
	if language != "" {
		for _, m := range transcription {
			if m.Language == language {
				return m, nil
			}
		}
	}
	for _, m := range transcription {
		if m.Multilingual() {
			return m, nil
		}
	}
	return transcription[0], nil
}
