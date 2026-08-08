package engine

import (
	"strings"
	"testing"
)

// TestDescribeMatchesLinked pins the invariant that ties the two build-tag
// variants together: Describe() must agree with the Linked constant. A future
// edit that adds a runtime to one file and forgets the other fails here rather
// than at a user's first transcription.
func TestDescribeMatchesLinked(t *testing.T) {
	got := Describe()
	if got.Available != Linked {
		t.Fatalf("Describe().Available = %v, want %v (Linked)", got.Available, Linked)
	}

	if !Linked {
		if got.Runtime != "" {
			t.Errorf("Runtime = %q, want empty without a linked runtime", got.Runtime)
		}
		return
	}

	if got.Runtime == "" {
		t.Error("Runtime is empty despite a linked runtime")
	}
	if got.Capabilities == "" {
		t.Error("Capabilities is empty despite a linked runtime")
	}
}

func TestErrNoRuntimeMentionsTheFix(t *testing.T) {
	// The error reaches users who ran `make build` and wondered why nothing
	// transcribes, so it has to name the target that fixes it.
	if msg := ErrNoRuntime.Error(); !strings.Contains(msg, "make build-engine") {
		t.Errorf("ErrNoRuntime = %q, want it to name `make build-engine`", msg)
	}
}
