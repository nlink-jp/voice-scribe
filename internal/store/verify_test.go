package store

import (
	"errors"
	"testing"
)

// fakeExpectations stands in for the catalog.
type fakeExpectations map[string]string // name -> sha

func (f fakeExpectations) HashFor(name string) (string, bool) {
	sha, ok := f[name]
	return sha, ok
}

func (f fakeExpectations) NameForHash(sha string) (string, bool) {
	for name, s := range f {
		if equalHash(s, sha) {
			return name, true
		}
	}
	return "", false
}

// hashes maps a path to the hash the file "has", so tests do not have to write
// hundreds of megabytes to disk to exercise the logic.
func hashes(m map[string]string) Hasher {
	return func(path string) (string, error) {
		if sha, ok := m[path]; ok {
			return sha, nil
		}
		return "", errors.New("no such file")
	}
}

func byName(checks []Check) map[string]Check {
	out := map[string]Check{}
	for _, c := range checks {
		out[c.Model.Name] = c
	}
	return out
}

const (
	shaA = "aaaa000000000000000000000000000000000000000000000000000000000000"
	shaB = "bbbb000000000000000000000000000000000000000000000000000000000000"
)

// TestVerifyChecksAModelInstalledBeforeVerificationExisted is the case this
// whole command was written for: a file installed by an older version, never
// hashed, and far too large to re-download just to gain confidence in it.
func TestVerifyChecksAModelInstalledBeforeVerificationExisted(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "kotoba", Kind: KindTranscription, Language: "ja"}) // no SHA256

	checks, err := s.Verify(fakeExpectations{"kotoba": shaA}, hashes(map[string]string{m.Path: shaA}))
	if err != nil {
		t.Fatal(err)
	}

	c := byName(checks)["kotoba"]
	if c.Status != StatusVerified {
		t.Fatalf("status = %s, want verified", c.Status)
	}
	if !c.Adopted {
		t.Error("Adopted is false; the caller cannot tell this was checked for the first time")
	}
}

// TestVerifyDetectsATamperedFile: the hash on disk disagrees with the catalog.
func TestVerifyDetectsATamperedFile(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "kotoba", Kind: KindTranscription, SHA256: shaA})

	checks, err := s.Verify(fakeExpectations{"kotoba": shaA}, hashes(map[string]string{m.Path: shaB}))
	if err != nil {
		t.Fatal(err)
	}

	c := byName(checks)["kotoba"]
	if c.Status != StatusMismatch {
		t.Fatalf("status = %s, want mismatch", c.Status)
	}
	if c.Expected != shaA || c.Actual != shaB {
		t.Errorf("check does not carry both hashes: %+v", c)
	}
}

func TestVerifyReportsAMissingFile(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "gone", Kind: KindTranscription})

	checks, err := s.Verify(fakeExpectations{}, hashes(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := byName(checks)["gone"].Status; got != StatusMissing {
		t.Errorf("status = %s, want missing", got)
	}
}

// TestVerifyFallsBackToTheRecordedHash keeps an entry checkable after the
// catalog drops its name — the registry still remembers what it was installed as.
func TestVerifyFallsBackToTheRecordedHash(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "dropped", Kind: KindTranscription, SHA256: shaA})

	checks, err := s.Verify(fakeExpectations{}, hashes(map[string]string{m.Path: shaA}))
	if err != nil {
		t.Fatal(err)
	}
	if got := byName(checks)["dropped"].Status; got != StatusVerified {
		t.Errorf("status = %s, want verified against the recorded hash", got)
	}
}

// TestVerifyRecognisesARenamedCatalogEntry is how the v2.2 -> v2.0 situation is
// resolved without re-downloading: the bytes on disk still identify the model.
func TestVerifyRecognisesARenamedCatalogEntry(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "kotoba-whisper-v2.2", Kind: KindTranscription, Language: "ja"})

	checks, err := s.Verify(
		fakeExpectations{"kotoba-whisper-v2.0": shaA},
		hashes(map[string]string{m.Path: shaA}))
	if err != nil {
		t.Fatal(err)
	}

	c := byName(checks)["kotoba-whisper-v2.2"]
	if c.AlsoKnownAs != "kotoba-whisper-v2.0" {
		t.Errorf("AlsoKnownAs = %q, want the catalog name the file actually matches", c.AlsoKnownAs)
	}
	if c.Status != StatusUnknown {
		t.Errorf("status = %s: without a catalog entry or a recorded hash there is nothing to verify against", c.Status)
	}
}

// TestVerifyIsHonestAboutWhatItCannotCheck: an imported model has no expected
// hash anywhere, and saying "verified" would be a lie.
func TestVerifyIsHonestAboutWhatItCannotCheck(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "imported", Kind: KindTranscription})

	checks, err := s.Verify(fakeExpectations{}, hashes(map[string]string{m.Path: shaA}))
	if err != nil {
		t.Fatal(err)
	}
	c := byName(checks)["imported"]
	if c.Status != StatusUnknown {
		t.Errorf("status = %s, want unknown", c.Status)
	}
	if c.Expected != "" {
		t.Errorf("Expected = %q, want empty when nothing knows", c.Expected)
	}
}

func TestAdoptRecordsTheHash(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "kotoba", Kind: KindTranscription})

	if err := s.Adopt("kotoba", shaA); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Get("kotoba")
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != shaA {
		t.Errorf("SHA256 = %q, want it recorded", got.SHA256)
	}
	if err := s.Adopt("absent", shaA); err == nil {
		t.Error("Adopt accepted a model that is not installed")
	}
}

// TestRenameKeepsTheFile is the property that makes reconciliation cheap: the
// weights are already correct, only the name they are filed under is stale.
func TestRenameKeepsTheFile(t *testing.T) {
	s := newStore(t)
	m := install(t, s, Model{Name: "old", Kind: KindTranscription, Language: "ja", SHA256: shaA})

	if err := s.Rename("old", "new"); err != nil {
		t.Fatal(err)
	}

	if _, ok, _ := s.Get("old"); ok {
		t.Error("the old name is still registered")
	}
	got, ok, err := s.Get("new")
	if err != nil || !ok {
		t.Fatalf("new name not registered: %v", err)
	}
	if got.Path != m.Path {
		t.Errorf("Path = %q, want the file left where it was", got.Path)
	}
	if got.SHA256 != shaA || got.Language != "ja" {
		t.Errorf("Rename lost metadata: %+v", got)
	}
	if _, err := hashes(map[string]string{m.Path: shaA})(m.Path); err != nil {
		t.Error("the weights file was removed by a rename")
	}
}

func TestRenameRefusesToClobber(t *testing.T) {
	s := newStore(t)
	install(t, s, Model{Name: "old", Kind: KindTranscription})
	install(t, s, Model{Name: "taken", Kind: KindTranscription})

	if err := s.Rename("old", "taken"); err == nil {
		t.Error("Rename overwrote an existing entry")
	}
	if err := s.Rename("absent", "x"); err == nil {
		t.Error("Rename accepted a model that is not installed")
	}
}

func TestEqualHashIgnoresCase(t *testing.T) {
	if !equalHash("ABCDEF", "abcdef") {
		t.Error("hash comparison should be case-insensitive")
	}
	if equalHash("abc", "abcd") {
		t.Error("different-length hashes must not compare equal")
	}
}
