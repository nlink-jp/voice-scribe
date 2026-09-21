package cmd

import (
	"testing"

	"github.com/nlink-jp/voice-scribe/internal/catalog"
	"github.com/nlink-jp/voice-scribe/internal/store"
)

// A registry record keeps the licence that was current when the model was
// pulled. v0.4.5 corrected two catalog licences that had been read from the
// conversion repo rather than from the weights, and an already-installed copy
// went on reporting the old terms -- the correction reached only people who had
// not pulled yet. These tests watch the field a user reads, not the catalog.

func TestInstalledListingTakesTheLicenceFromTheCatalog(t *testing.T) {
	e, ok := catalog.Lookup("base")
	if !ok {
		t.Fatal("catalog has no entry named base")
	}
	stale := store.Model{
		Name: "base", Kind: store.KindTranscription, Path: "/models/base.bin",
		License: "mit", // what the catalog said before v0.4.5
	}
	if e.License == stale.License {
		t.Fatalf("this test needs the catalog to disagree with the stored value; both are %q", e.License)
	}

	v := viewOf(stale)
	if v.License != e.License {
		t.Errorf("installed view reports %q, catalog says %q -- the correction does not reach an installed model",
			v.License, e.License)
	}
	if v.WeightsRepo != e.WeightsRepo {
		t.Errorf("WeightsRepo = %q, want %q: a licence is reported without saying where it was read from",
			v.WeightsRepo, e.WeightsRepo)
	}
}

func TestAModelOutsideTheCatalogKeepsItsRecordedLicence(t *testing.T) {
	m := store.Model{
		Name: "retired-model", Kind: store.KindTranscription, Path: "/models/x.bin",
		License: "apache-2.0",
	}
	if _, ok := catalog.Lookup(m.Name); ok {
		t.Fatalf("%s is in the catalog; this test needs a name that is not", m.Name)
	}

	v := viewOf(m)
	if v.License != "apache-2.0" {
		t.Errorf("License = %q, want the recorded apache-2.0: there is nothing else to report", v.License)
	}
	if v.WeightsRepo != "" {
		t.Errorf("WeightsRepo = %q, want empty: nothing records where that licence came from", v.WeightsRepo)
	}
}
