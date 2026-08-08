package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/nlink-jp/voice-scribe/internal/catalog"
	"github.com/nlink-jp/voice-scribe/internal/download"
	"github.com/nlink-jp/voice-scribe/internal/store"
	"github.com/spf13/cobra"
)

var modelsVerifyOpts struct {
	reconcile bool
}

var modelsVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check installed models against the catalog's hashes",
	Long: `verify hashes every installed model and compares it with what the catalog
expects.

Hash verification arrived after the first release, so models installed before it
have never been checked. This is how they get checked — without deleting and
re-downloading gigabytes that are very likely already correct. A model that
passes has its hash recorded, so the result survives into later runs.

--reconcile additionally files an entry under the catalog name whose file it
actually matches. That happens when the catalog renames something: the weights on
disk are unchanged and still correct, only the name is stale. Nothing is
downloaded and the file is not moved.`,
	Args: cobra.NoArgs,
	RunE: runModelsVerify,
}

func init() {
	modelsCmd.AddCommand(modelsVerifyCmd)
	modelsVerifyCmd.Flags().BoolVar(&modelsVerifyOpts.reconcile, "reconcile", false,
		"File entries under the catalog name their file matches")
}

func runModelsVerify(cmd *cobra.Command, args []string) error {
	rt, err := newRuntimeContext()
	if err != nil {
		return err
	}

	checks, err := rt.Store.Verify(catalog.Expectations{}, download.SHA256File)
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no models installed")
		return nil
	}

	// Settle the registry before reporting. A table printed first would show a
	// state the command is about to change, and then tell the reader to run it
	// again to see the result — which is how a fix ends up looking like a
	// half-fix.
	pending, err := settle(cmd, rt, checks)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tNOTE")
	for _, c := range checks {
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.Model.Name, c.Status, verifyNote(c))
	}
	w.Flush()

	for _, line := range pending {
		fmt.Fprintln(cmd.ErrOrStderr(), line)
	}
	return summarise(checks)
}

// settle records verified hashes and, with --reconcile, re-files entries under
// the catalog name their file matches. It updates checks in place so the table
// printed afterwards describes the registry as it now stands.
//
// It returns advice to print after the table.
func settle(cmd *cobra.Command, rt *runtimeContext, checks []store.Check) ([]string, error) {
	var advice []string
	adopted := 0

	for i := range checks {
		c := &checks[i]

		// An entry the catalog no longer knows, whose file is nonetheless a
		// catalog model. The bytes identify it; only the name is stale.
		if c.AlsoKnownAs != "" && c.AlsoKnownAs != c.Model.Name {
			if !modelsVerifyOpts.reconcile {
				advice = append(advice, fmt.Sprintf(
					"%s holds the same file as catalog model %q — run `voice-scribe models verify --reconcile` to file it under that name (nothing is downloaded)",
					c.Model.Name, c.AlsoKnownAs))
				continue
			}
			from := c.Model.Name
			if err := rt.Store.Rename(from, c.AlsoKnownAs); err != nil {
				return advice, err
			}
			if err := rt.Store.Adopt(c.AlsoKnownAs, c.Actual); err != nil {
				return advice, err
			}
			c.Model.Name = c.AlsoKnownAs
			c.Model.SHA256 = c.Actual
			c.Status = store.StatusVerified
			c.Expected = c.Actual
			c.AlsoKnownAs = ""
			advice = append(advice, fmt.Sprintf(
				"re-filed %s as %s — the file was already correct, so nothing was downloaded",
				from, c.Model.Name))
			adopted++
			continue
		}

		if c.Status == store.StatusVerified && c.Adopted {
			if err := rt.Store.Adopt(c.Model.Name, c.Actual); err != nil {
				return advice, err
			}
			adopted++
		}
	}

	if adopted > 0 {
		advice = append(advice, fmt.Sprintf("recorded the verified hash for %d model(s)", adopted))
	}
	return advice, nil
}

// verifyNote is the human-facing explanation of a check. It is the whole point
// of the command: a status word alone does not tell anyone what to do.
func verifyNote(c store.Check) string {
	switch c.Status {
	case store.StatusVerified:
		if c.Adopted {
			return "checked for the first time"
		}
		return ""
	case store.StatusMismatch:
		return fmt.Sprintf("expected %s, got %s — do not use it", short(c.Expected), short(c.Actual))
	case store.StatusMissing:
		return "the file is gone; re-pull or remove the entry"
	default:
		if c.AlsoKnownAs != "" {
			return fmt.Sprintf("not in the catalog under this name, but the file is catalog model %q", c.AlsoKnownAs)
		}
		return "nothing knows what this should hash to (imported, or dropped from the catalog)"
	}
}

// summarise fails the command when anything is actually wrong: a verification
// that always exits 0 is not a gate.
func summarise(checks []store.Check) error {
	var bad int
	for _, c := range checks {
		if c.Status == store.StatusMismatch || c.Status == store.StatusMissing {
			bad++
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d model(s) failed verification", bad)
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12] + "…"
	}
	return sha
}
