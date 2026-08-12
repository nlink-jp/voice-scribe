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

// TestDefaultForPicksASpecialistThenFallsBack exercises the mechanism against a
// fixture rather than the real catalog, because no language marks a specialist
// today (ADR-0008) and an untested branch is one that stops working quietly.
func TestDefaultForPicksASpecialistThenFallsBack(t *testing.T) {
	fixture := []Entry{
		{Name: "generalist", Kind: store.KindTranscription, Default: true},
		{Name: "xx-specialist", Kind: store.KindTranscription, Language: "xx", Default: true},
		{Name: "yy-also-installed", Kind: store.KindTranscription, Language: "yy"},
	}

	for _, tc := range []struct{ language, want string }{
		{"xx", "xx-specialist"}, // a marked specialist wins
		{"yy", "generalist"},    // present but unmarked: not suggested
		{"zz", "generalist"},    // no entry at all: fall back
		{"", "generalist"},      // language unspecified
	} {
		if got, ok := defaultFor(fixture, tc.language); !ok || got.Name != tc.want {
			t.Errorf("defaultFor(%q) = %q, want %q", tc.language, got.Name, tc.want)
		}
	}

	if _, ok := defaultFor(nil, "xx"); ok {
		t.Error("invented a default out of an empty catalog")
	}
}

// TestTheSuggestedJapaneseModelIsTheMeasuredOne pins ADR-0008. The Japanese
// suggestion was a Japanese-specialised model on the reasoning that a
// specialist must be better; measuring said otherwise, and this test is what
// stops the reasoning from quietly coming back.
func TestTheSuggestedJapaneseModelIsTheMeasuredOne(t *testing.T) {
	ja, ok := DefaultFor("ja")
	if !ok {
		t.Fatal("no default for Japanese")
	}
	if ja.Name != "large-v3-turbo" {
		t.Errorf("default for ja is %q, want large-v3-turbo (ADR-0008)", ja.Name)
	}
	if _, ok := Lookup("kotoba-whisper-v2.0"); !ok {
		t.Error("kotoba-whisper left the catalog; it is no longer suggested, but --model must still reach it")
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
	if _, ok := Lookup("large-v3-turbo"); !ok {
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

// TestKotobaWhisperComesFromItsAuthors: prefer the upstream author's repository
// over any re-upload (ADR-0004). This model is no longer the Japanese default,
// but it is still shipped in the catalog, so its provenance still matters.
func TestKotobaWhisperComesFromItsAuthors(t *testing.T) {
	e, ok := Lookup("kotoba-whisper-v2.0")
	if !ok {
		t.Fatal("kotoba-whisper-v2.0 is not in the catalog")
	}
	if !strings.HasPrefix(e.Repo, "kotoba-tech/") {
		t.Errorf("kotoba-whisper comes from %q, not the model's authors", e.Repo)
	}
}
