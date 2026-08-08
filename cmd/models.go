package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/nlink-jp/voice-scribe/internal/catalog"
	"github.com/nlink-jp/voice-scribe/internal/download"
	"github.com/nlink-jp/voice-scribe/internal/store"
	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage transcription and diarization models",
}

var modelsListOpts struct {
	catalogOnly bool
	all         bool
	asJSON      bool
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed models, or the catalog",
	Long: `list shows what is installed. --catalog shows what can be installed,
and --all shows both.`,
	Args: cobra.NoArgs,
	RunE: runModelsList,
}

var modelsPullCmd = &cobra.Command{
	Use:   "pull <name>",
	Short: "Download a catalog model",
	Args:  cobra.ExactArgs(1),
	RunE:  runModelsPull,
}

var modelsImportOpts struct {
	name     string
	kind     string
	language string
}

var modelsImportCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Register a model file you already have",
	Args:  cobra.ExactArgs(1),
	RunE:  runModelsImport,
}

var modelsRmOpts struct{ keepFile bool }

var modelsRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove an installed model",
	Args:  cobra.ExactArgs(1),
	RunE:  runModelsRm,
}

func init() {
	rootCmd.AddCommand(modelsCmd)
	modelsCmd.AddCommand(modelsListCmd, modelsPullCmd, modelsImportCmd, modelsRmCmd)

	lf := modelsListCmd.Flags()
	lf.BoolVar(&modelsListOpts.catalogOnly, "catalog", false, "Show the catalog instead of what is installed")
	lf.BoolVar(&modelsListOpts.all, "all", false, "Show both installed models and the catalog")
	lf.BoolVar(&modelsListOpts.asJSON, "json", false, "Emit JSON")

	imf := modelsImportCmd.Flags()
	imf.StringVar(&modelsImportOpts.name, "name", "", "Registry name (default: the file name without its extension)")
	imf.StringVar(&modelsImportOpts.kind, "kind", string(store.KindTranscription), "Model kind: transcription, vad, diarization")
	imf.StringVar(&modelsImportOpts.language, "lang", "", "ISO 639-1 code if the model is language-specific")

	modelsRmCmd.Flags().BoolVar(&modelsRmOpts.keepFile, "keep-file", false, "Deregister but leave the weights on disk")
}

// installedView and catalogView are the JSON shapes. They are separate from the
// internal types on purpose: the output is a contract with whatever reads it,
// and it should not shift because a field was renamed inside the store.
type installedView struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Language     string `json:"language,omitempty"`
	Quantization string `json:"quantization,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	License      string `json:"license,omitempty"`
	Role         string `json:"role,omitempty"`
	Path         string `json:"path"`
}

type catalogView struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Description  string `json:"description"`
	Language     string `json:"language,omitempty"`
	Quantization string `json:"quantization,omitempty"`
	SizeBytes    int64  `json:"size_bytes"`
	License      string `json:"license"`
	Role         string `json:"role,omitempty"`
	Installed    bool   `json:"installed"`
}

func runModelsList(cmd *cobra.Command, args []string) error {
	rt, err := newRuntimeContext()
	if err != nil {
		return err
	}
	installed, err := rt.Store.List()
	if err != nil {
		return err
	}

	showInstalled := !modelsListOpts.catalogOnly || modelsListOpts.all
	showCatalog := modelsListOpts.catalogOnly || modelsListOpts.all

	if modelsListOpts.asJSON {
		return emitListJSON(cmd, installed, showInstalled, showCatalog)
	}

	out := cmd.OutOrStdout()
	if showInstalled {
		if modelsListOpts.all {
			fmt.Fprintln(out, "INSTALLED")
		}
		if len(installed) == 0 {
			fmt.Fprintln(out, "no models installed (run `voice-scribe models pull kotoba-whisper-v2.2`)")
		} else {
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tKIND\tLANG\tQUANT\tSIZE\tLICENSE")
			for _, m := range installed {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					m.Name, m.Kind, dash(m.Language), dash(m.Quantization), humanBytes(m.SizeBytes), dash(m.License))
			}
			w.Flush()
		}
		if showCatalog {
			fmt.Fprintln(out)
		}
	}

	if showCatalog {
		if modelsListOpts.all {
			fmt.Fprintln(out, "CATALOG")
		}
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tKIND\tLANG\tSIZE\tLICENSE\tINSTALLED\tDESCRIPTION")
		for _, e := range catalog.All() {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				e.Name, e.Kind, dash(e.Language), humanBytes(e.SizeBytes), e.License,
				yesNo(isInstalled(installed, e.Name)), e.Description)
		}
		w.Flush()
	}
	return nil
}

// viewOf and catalogViewOf are the single definition of the JSON shapes. The
// MCP list_models tool reuses them so the two surfaces cannot disagree about
// what is installed.
func viewOf(m store.Model) installedView {
	return installedView{
		Name: m.Name, Kind: string(m.Kind), Language: m.Language,
		Quantization: m.Quantization, SizeBytes: m.SizeBytes, License: m.License,
		Role: m.Role, Path: m.Path,
	}
}

func catalogViewOf(e catalog.Entry, installed bool) catalogView {
	return catalogView{
		Name: e.Name, Kind: string(e.Kind), Description: e.Description, Language: e.Language,
		Quantization: e.Quantization, SizeBytes: e.SizeBytes, License: e.License,
		Role: string(e.Role), Installed: installed,
	}
}

func emitListJSON(cmd *cobra.Command, installed []store.Model, showInstalled, showCatalog bool) error {
	installedViews := make([]installedView, 0, len(installed))
	for _, m := range installed {
		installedViews = append(installedViews, viewOf(m))
	}

	catalogViews := make([]catalogView, 0)
	for _, e := range catalog.All() {
		catalogViews = append(catalogViews, catalogViewOf(e, isInstalled(installed, e.Name)))
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	switch {
	case showInstalled && showCatalog:
		return enc.Encode(map[string]any{"installed": installedViews, "catalog": catalogViews})
	case showCatalog:
		return enc.Encode(catalogViews)
	default:
		return enc.Encode(installedViews)
	}
}

func runModelsPull(cmd *cobra.Command, args []string) error {
	name := args[0]
	entry, ok := catalog.Lookup(name)
	if !ok {
		return fmt.Errorf("no catalog model named %q (see `voice-scribe models list --catalog`)", name)
	}

	rt, err := newRuntimeContext()
	if err != nil {
		return err
	}

	dest := filepath.Join(rt.Store.ModelsDir(), entry.File)
	if info, err := os.Stat(dest); err == nil && info.Size() == entry.SizeBytes {
		// The weights are already on disk at the right size, which happens when
		// a model was removed with --keep-file or is shared with another entry.
		// Re-registering is instant; re-downloading half a gigabyte is not.
		fmt.Fprintf(cmd.ErrOrStderr(), "%s already present, registering it\n", entry.File)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "downloading %s (%s) from %s\n", entry.Name, humanBytes(entry.SizeBytes), entry.Repo)
		progress := newProgressReporter(cmd.ErrOrStderr(), false)
		progress.stage("fetching %s", entry.File)
		err := download.Fetch(cmd.Context(), entry.URL(), dest, download.Options{
			ExpectedSize: entry.SizeBytes,
			OnProgress: func(done, total int64) {
				if total > 0 {
					progress.percent(int(done * 100 / total))
				}
			},
		})
		progress.done()
		if err != nil {
			return err
		}
	}

	if err := rt.Store.Add(entry.Model(dest)); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "installed %s (%s, %s)\n", entry.Name, entry.License, humanBytes(entry.SizeBytes))
	return nil
}

func runModelsImport(cmd *cobra.Command, args []string) error {
	path, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	kind, err := store.ParseKind(modelsImportOpts.kind)
	if err != nil {
		return err
	}

	name := modelsImportOpts.name
	if name == "" {
		base := filepath.Base(path)
		name = base[:len(base)-len(filepath.Ext(base))]
	}

	rt, err := newRuntimeContext()
	if err != nil {
		return err
	}
	if err := rt.Store.Add(store.Model{
		Name:      name,
		Kind:      kind,
		Path:      path,
		Language:  modelsImportOpts.language,
		SizeBytes: info.Size(),
		Source:    "imported",
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "registered %s -> %s\n", name, path)
	return nil
}

func runModelsRm(cmd *cobra.Command, args []string) error {
	rt, err := newRuntimeContext()
	if err != nil {
		return err
	}
	if err := rt.Store.Remove(args[0], !modelsRmOpts.keepFile); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
	return nil
}

func isInstalled(models []store.Model, name string) bool {
	for _, m := range models {
		if m.Name == name {
			return true
		}
	}
	return false
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
