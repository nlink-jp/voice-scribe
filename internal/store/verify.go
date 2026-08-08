package store

import "fmt"

// Status is what checking an installed model against a known hash concluded.
type Status string

const (
	// StatusVerified means the file on disk hashes to what was expected.
	StatusVerified Status = "verified"
	// StatusMismatch means it does not. Treat it as hostile until explained:
	// the bytes are not the bytes the catalog was built against.
	StatusMismatch Status = "mismatch"
	// StatusMissing means the registry points at a file that is not there.
	StatusMissing Status = "missing"
	// StatusUnknown means nothing knows what this file should hash to — an
	// imported model, or one whose catalog entry has gone away. It is not a
	// failure, but it is not an assurance either, and a list that renders it
	// the same as verified is lying by omission.
	StatusUnknown Status = "unknown"
)

// Check is the outcome of verifying one installed model.
type Check struct {
	Model  Model
	Status Status
	// Actual is the hash computed from disk, empty when the file is missing.
	Actual string
	// Expected is the hash it was checked against, empty when nothing knew one.
	Expected string
	// Adopted reports that this check recorded a hash the registry did not
	// previously hold — a model installed before verification existed, now
	// confirmed without re-downloading it.
	Adopted bool
	// AlsoKnownAs names a catalog entry whose expected hash equals this file,
	// which is how an entry orphaned by a catalog rename is recognised as the
	// same weights under a new name.
	AlsoKnownAs string
}

// Expectation supplies what a model should hash to, and lets a hash be traced
// back to a catalog entry. It is an interface so store does not depend on
// catalog (which depends on store).
type Expectation interface {
	// HashFor returns the expected hash for a catalog entry name.
	HashFor(name string) (string, bool)
	// NameForHash returns the catalog entry whose file has this hash.
	NameForHash(sha string) (string, bool)
}

// Hasher computes a file's SHA-256. Injected so the verification logic is
// testable without writing hundreds of megabytes to disk.
type Hasher func(path string) (string, error)

// Verify checks every installed model against what the catalog expects.
//
// It exists because hash verification arrived after the first release: models
// installed before it have never been checked, and without this there is no way
// to check them short of deleting and re-downloading gigabytes. A file that is
// already correct should be recognised as correct.
//
// When a check succeeds and the registry held no hash, the hash is recorded, so
// the assurance survives into later runs.
func (s *Store) Verify(exp Expectation, hash Hasher) ([]Check, error) {
	models, err := s.List()
	if err != nil {
		return nil, err
	}

	checks := make([]Check, 0, len(models))
	for _, m := range models {
		checks = append(checks, s.checkOne(m, exp, hash))
	}
	return checks, nil
}

func (s *Store) checkOne(m Model, exp Expectation, hash Hasher) Check {
	c := Check{Model: m}

	actual, err := hash(m.Path)
	if err != nil {
		c.Status = StatusMissing
		return c
	}
	c.Actual = actual

	// Prefer what the catalog says today; fall back to what the registry
	// recorded, so an entry the catalog has dropped can still be verified
	// against the hash it was installed with.
	expected, known := exp.HashFor(m.Name)
	if !known && m.SHA256 != "" {
		expected, known = m.SHA256, true
	}

	if other, ok := exp.NameForHash(actual); ok && other != m.Name {
		c.AlsoKnownAs = other
	}

	switch {
	case !known:
		c.Status = StatusUnknown
	case equalHash(actual, expected):
		c.Status = StatusVerified
		c.Expected = expected
		c.Adopted = m.SHA256 == ""
	default:
		c.Status = StatusMismatch
		c.Expected = expected
	}
	return c
}

// Adopt records a verified hash against an installed model, so a file confirmed
// once does not have to be re-hashed to be trusted later.
func (s *Store) Adopt(name, sha string) error {
	m, ok, err := s.Get(name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("model %q is not installed", name)
	}
	m.SHA256 = sha
	return s.Add(m)
}

// Rename moves an installed model to a new registry name, keeping the file
// where it is. It is how an entry orphaned by a catalog rename is reconciled
// without re-downloading weights that are already correct.
func (s *Store) Rename(from, to string) error {
	m, ok, err := s.Get(from)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("model %q is not installed", from)
	}
	if _, exists, err := s.Get(to); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("model %q is already installed; remove it first", to)
	}

	m.Name = to
	if err := s.Add(m); err != nil {
		return err
	}
	// Deregister the old name but keep the file: it is the same file.
	return s.Remove(from, false)
}

func equalHash(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
