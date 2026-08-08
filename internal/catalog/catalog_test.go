package catalog

import (
	"strings"
	"testing"

	"github.com/nlink-jp/voice-scribe/internal/store"
)

// TestEveryEntryIsComplete guards the way a catalog rots: an entry added by
// copying a neighbour and half-edited. Every field here is load-bearing —
// a missing size defeats the truncation check, a missing license means the
// tool cannot tell the user what they are installing.
func TestEveryEntryIsComplete(t *testing.T) {
	for _, e := range All() {
		t.Run(e.Name, func(t *testing.T) {
			if e.Kind == "" {
				t.Error("Kind is empty")
			}
			if e.Repo == "" || e.File == "" {
				t.Errorf("Repo/File incomplete: %q %q", e.Repo, e.File)
			}
			if e.Description == "" {
				t.Error("Description is empty; it is what `models list --catalog` shows")
			}
			if e.SizeBytes <= 0 {
				t.Error("SizeBytes is unset, so a truncated download cannot be detected")
			}
			if e.License == "" {
				t.Error("License is empty")
			}
			if !strings.HasPrefix(e.URL(), "https://huggingface.co/"+e.Repo+"/resolve/main/") {
				t.Errorf("URL = %q, which does not point into the declared repo", e.URL())
			}
		})
	}
}

func TestNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range All() {
		if seen[e.Name] {
			t.Errorf("duplicate catalog name %q", e.Name)
		}
		seen[e.Name] = true
	}
}

// TestDefaultForPicksASpecialistThenFallsBack is the behaviour that makes
// `models pull` work without the user knowing which model suits their language.
func TestDefaultForPicksASpecialistThenFallsBack(t *testing.T) {
	ja, ok := DefaultFor("ja")
	if !ok {
		t.Fatal("no default for Japanese")
	}
	if ja.Language != "ja" {
		t.Errorf("default for ja is %q (language %q), want a Japanese specialist", ja.Name, ja.Language)
	}

	fr, ok := DefaultFor("fr")
	if !ok {
		t.Fatal("no default for French")
	}
	if fr.Language != "" {
		t.Errorf("default for fr is %q (language %q), want a multilingual model", fr.Name, fr.Language)
	}

	none, ok := DefaultFor("")
	if !ok || none.Language != "" {
		t.Errorf("default for an unspecified language = %q, want a multilingual model", none.Name)
	}
}

// TestExactlyOneDefaultPerLanguage keeps DefaultFor deterministic: two entries
// flagged Default for the same language would make the answer depend on
// declaration order.
func TestExactlyOneDefaultPerLanguage(t *testing.T) {
	counts := map[string]int{}
	for _, e := range All() {
		if e.Kind == store.KindTranscription && e.Default {
			counts[e.Language]++
		}
	}
	for lang, n := range counts {
		if n != 1 {
			t.Errorf("language %q has %d default models, want exactly 1", lang, n)
		}
	}
	if counts[""] != 1 {
		t.Error("there must be exactly one default multilingual model")
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("kotoba-whisper-v2.0"); !ok {
		t.Error("the documented default model is not in the catalog")
	}
	if _, ok := Lookup("silero-vad"); !ok {
		t.Error("the VAD model --vad needs is not in the catalog")
	}
	if _, ok := Lookup("nonesuch"); ok {
		t.Error("Lookup invented an entry")
	}
}

// TestModelCarriesCatalogFactsIntoTheRegistry keeps `models list` able to show
// what a model is without re-consulting the catalog, which matters for models
// installed from a catalog entry that later changes.
func TestModelCarriesCatalogFactsIntoTheRegistry(t *testing.T) {
	e, _ := Lookup("kotoba-whisper-v2.0")
	m := e.Model("/models/k.bin")

	if m.Name != e.Name || m.Kind != e.Kind || m.Language != e.Language {
		t.Errorf("Model() lost identity: %+v", m)
	}
	if m.License != e.License || m.SizeBytes != e.SizeBytes {
		t.Errorf("Model() lost provenance: %+v", m)
	}
	if !strings.Contains(m.Source, e.Repo) {
		t.Errorf("Source = %q, want it to record the upstream repo", m.Source)
	}
}

// TestEveryEntryPinsAHash is the supply-chain gate. An entry without one is
// installed on size alone, which anyone able to substitute the file can
// preserve — and these files are parsed by a runtime that has already had a
// stack-buffer-overflow reachable from a malformed header.
func TestEveryEntryPinsAHash(t *testing.T) {
	for _, e := range All() {
		if len(e.SHA256) != 64 {
			t.Errorf("%s: SHA256 = %q, want a 64-character hex digest", e.Name, e.SHA256)
			continue
		}
		if strings.Trim(e.SHA256, "0123456789abcdef") != "" {
			t.Errorf("%s: SHA256 %q is not lowercase hex", e.Name, e.SHA256)
		}
	}
}

// TestNoTwoEntriesShipTheSameBytes: two names for one file is a menu that lies
// about the choice on offer. It was a real defect — a third-party mirror
// labelled "v2.2" served a file byte-identical to the authors' "v2.0".
func TestNoTwoEntriesShipTheSameBytes(t *testing.T) {
	seen := map[string]string{}
	for _, e := range All() {
		if prior, dup := seen[e.SHA256]; dup {
			t.Errorf("%s and %s are the same file (%s); offer one of them", prior, e.Name, e.SHA256)
		}
		seen[e.SHA256] = e.Name
	}
}

// TestTheDefaultJapaneseModelComesFromItsAuthors: for the model installed by
// default, prefer the upstream author's repository over any re-upload.
func TestTheDefaultJapaneseModelComesFromItsAuthors(t *testing.T) {
	e, ok := DefaultFor("ja")
	if !ok {
		t.Fatal("no default Japanese model")
	}
	if !strings.HasPrefix(e.Repo, "kotoba-tech/") {
		t.Errorf("the default Japanese model comes from %q, not the model's authors", e.Repo)
	}
}
