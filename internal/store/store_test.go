package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// install writes a placeholder weights file and registers it.
func install(t *testing.T, s *Store, m Model) Model {
	t.Helper()
	if m.Path == "" {
		m.Path = filepath.Join(s.ModelsDir(), m.Name+".bin")
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path, []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(m); err != nil {
		t.Fatalf("Add(%s): %v", m.Name, err)
	}
	return m
}

func TestAddListGetRoundTrip(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "kotoba", Kind: KindTranscription, Language: "ja"})

	models, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("List returned %d models, want 1", len(models))
	}

	got, ok, err := s.Get("kotoba")
	if err != nil || !ok {
		t.Fatalf("Get: %v (found=%v)", err, ok)
	}
	if got.Language != "ja" {
		t.Errorf("Language = %q, want ja", got.Language)
	}
}

// TestRegistrySurvivesReopen is the point of persisting at all: a second
// process must see what the first one installed.
func TestRegistrySurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	first, err := New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	install(t, first, Model{Name: "base", Kind: KindTranscription})

	second, err := New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	models, err := second.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Name != "base" {
		t.Errorf("reopened store sees %v, want the installed model", models)
	}
}

func TestAddRejectsUnusableEntries(t *testing.T) {
	s := newStore(t)
	real := filepath.Join(t.TempDir(), "m.bin")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, m := range map[string]Model{
		"no name":       {Kind: KindTranscription, Path: real},
		"no kind":       {Name: "m", Path: real},
		"relative path": {Name: "m", Kind: KindTranscription, Path: "models/m.bin"},
		"missing file":  {Name: "m", Kind: KindTranscription, Path: filepath.Join(t.TempDir(), "absent.bin")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := s.Add(m); err == nil {
				t.Error("Add accepted an unusable entry")
			}
		})
	}
}

func TestAddReplacesSameName(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "m", Kind: KindTranscription, Quantization: "q5_0"})
	install(t, s, Model{Name: "m", Kind: KindTranscription, Quantization: "q8_0"})

	models, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d entries, want the second install to replace the first", len(models))
	}
	if models[0].Quantization != "q8_0" {
		t.Errorf("Quantization = %q, want the replacement", models[0].Quantization)
	}
}

func TestRemove(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "m", Kind: KindTranscription})

	if err := s.Remove("m", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(m.Path); err != nil {
		t.Error("Remove without deleteFile removed the weights from disk")
	}
	if err := s.Remove("m", false); err == nil {
		t.Error("removing an absent model was accepted")
	}
}

func TestRemoveDeletesWeightsWhenAsked(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "m", Kind: KindTranscription})

	if err := s.Remove("m", true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(m.Path); !os.IsNotExist(err) {
		t.Error("weights still on disk after Remove(deleteFile=true)")
	}
}

// TestResolvePrefersLanguageSpecialist covers the choice a user should not have
// to make: with both a Japanese model and a multilingual one installed,
// transcribing Japanese should use the Japanese one.
func TestResolvePrefersLanguageSpecialist(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "large-v3-turbo", Kind: KindTranscription})
	install(t, s, Model{Name: "kotoba", Kind: KindTranscription, Language: "ja"})

	got, err := s.Resolve("", "", "ja")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "kotoba" {
		t.Errorf("resolved %q for Japanese, want the ja specialist", got.Name)
	}

	got, err = s.Resolve("", "", "fr")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "large-v3-turbo" {
		t.Errorf("resolved %q for French, want the multilingual model", got.Name)
	}
}

func TestResolveHonoursExplicitAndPreferred(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "a", Kind: KindTranscription, Language: "ja"})
	install(t, s, Model{Name: "b", Kind: KindTranscription})

	got, err := s.Resolve("b", "a", "ja")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "b" {
		t.Errorf("explicit choice lost: resolved %q, want b", got.Name)
	}

	got, err = s.Resolve("", "b", "ja")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "b" {
		t.Errorf("configured default lost: resolved %q, want b", got.Name)
	}
}

// TestResolveRefusesTheWrongKind stops a VAD model being loaded as a
// transcriber, which otherwise fails deep inside the runtime.
func TestResolveRefusesTheWrongKind(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "silero-vad", Kind: KindVAD})

	_, err := s.Resolve("silero-vad", "", "")
	if err == nil {
		t.Fatal("Resolve accepted a VAD model for transcription")
	}
	if !strings.Contains(err.Error(), "vad") {
		t.Errorf("error should name the actual kind, got %q", err)
	}
}

// TestResolveErrorsPointAtTheFix: a fresh install has no models, and the error
// is the only place the user learns what to do about it.
func TestResolveErrorsPointAtTheFix(t *testing.T) {
	s := newStore(t)

	_, err := s.Resolve("", "", "ja")
	if err == nil {
		t.Fatal("Resolve succeeded with an empty registry")
	}
	if !strings.Contains(err.Error(), "models pull") {
		t.Errorf("error %q should tell the user to pull a model", err)
	}

	install(t, s, Model{Name: "a", Kind: KindTranscription})
	_, err = s.Resolve("nonesuch", "", "")
	if err == nil {
		t.Fatal("Resolve accepted an uninstalled name")
	}
	if !strings.Contains(err.Error(), "models pull nonesuch") {
		t.Errorf("error %q should name the command that installs it", err)
	}
}

func TestParseKind(t *testing.T) {
	for _, k := range Kinds() {
		if got, err := ParseKind(string(k)); err != nil || got != k {
			t.Errorf("ParseKind(%q) = %v, %v", k, got, err)
		}
	}
	if _, err := ParseKind("embedding"); err == nil {
		t.Error("ParseKind accepted an unknown kind")
	}
}

func TestDefaultDataDirHonoursXDG(t *testing.T) {
	withXDG := DefaultDataDir(func(string) string { return "/xdg" }, "/home/u")
	if withXDG != filepath.Join("/xdg", "voice-scribe") {
		t.Errorf("DefaultDataDir with XDG_DATA_HOME = %q", withXDG)
	}
	withoutXDG := DefaultDataDir(func(string) string { return "" }, "/home/u")
	if withoutXDG != filepath.Join("/home/u", ".local", "share", "voice-scribe") {
		t.Errorf("DefaultDataDir without XDG_DATA_HOME = %q", withoutXDG)
	}
}

// TestModelsDirIsIndependentOfTheRegistry pins the property that lets a user
// move gigabytes of weights to another disk: the registry stays put and every
// entry carries its own absolute path.
func TestModelsDirIsIndependentOfTheRegistry(t *testing.T) {
	data := t.TempDir()
	elsewhere := t.TempDir()

	s, err := New(data, elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if s.ModelsDir() != elsewhere {
		t.Fatalf("ModelsDir = %q, want %q", s.ModelsDir(), elsewhere)
	}
	install(t, s, Model{Name: "m", Kind: KindTranscription})

	if _, err := os.Stat(filepath.Join(data, "registry.json")); err != nil {
		t.Errorf("registry should stay in the data directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "m.bin")); err != nil {
		t.Errorf("weights should land in the models directory: %v", err)
	}
}
