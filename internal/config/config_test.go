package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv builds an Env backed by a map, so the search order can be exercised
// without touching the developer's real home directory or the process
// environment (which would make these tests order-dependent).
func testEnv(t *testing.T, vars map[string]string) Env {
	t.Helper()
	return Env{
		Getenv:  func(k string) string { return vars[k] },
		Home:    t.TempDir(),
		WorkDir: t.TempDir(),
	}
}

func write(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsApplyWhenNoFileExists(t *testing.T) {
	cfg, path, err := Load("", testEnv(t, nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if path != "" {
		t.Errorf("reported reading %q, want no file", path)
	}
	if cfg.DefaultModel != Default().DefaultModel {
		t.Errorf("DefaultModel = %q, want the built-in default", cfg.DefaultModel)
	}
	if cfg.MCP.InlineThreshold != 8192 {
		t.Errorf("InlineThreshold = %d, want 8192", cfg.MCP.InlineThreshold)
	}
}

// TestSearchOrder pins the documented precedence. It matters because a user
// with both a home config and a ./config.toml has to be able to predict which
// one wins.
func TestSearchOrder(t *testing.T) {
	env := testEnv(t, nil)
	homeCfg := filepath.Join(env.Home, ".config", "voice-scribe", "config.toml")
	workCfg := filepath.Join(env.WorkDir, "config.toml")

	write(t, workCfg, `default_model = "from-workdir"`)
	_, path, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if path != workCfg {
		t.Fatalf("with only ./config.toml present, read %q, want %q", path, workCfg)
	}

	write(t, homeCfg, `default_model = "from-home"`)
	cfg, path, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if path != homeCfg {
		t.Errorf("read %q, want the home config to win over ./config.toml", path)
	}
	if cfg.DefaultModel != "from-home" {
		t.Errorf("DefaultModel = %q, want from-home", cfg.DefaultModel)
	}
}

func TestXDGConfigHomeWins(t *testing.T) {
	xdg := t.TempDir()
	env := testEnv(t, map[string]string{"XDG_CONFIG_HOME": xdg})
	xdgCfg := write(t, filepath.Join(xdg, "voice-scribe", "config.toml"), `default_model = "from-xdg"`)
	write(t, filepath.Join(env.Home, ".config", "voice-scribe", "config.toml"), `default_model = "from-home"`)

	cfg, path, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if path != xdgCfg {
		t.Errorf("read %q, want %q", path, xdgCfg)
	}
	if cfg.DefaultModel != "from-xdg" {
		t.Errorf("DefaultModel = %q, want from-xdg", cfg.DefaultModel)
	}
}

// TestExplicitPathsMustExist covers both ways of naming a file outright. A
// typo in either should be reported, not silently ignored in favour of
// defaults — that failure mode leaves a user convinced their settings do
// nothing.
func TestExplicitPathsMustExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")

	if _, _, err := Load(missing, testEnv(t, nil)); err == nil {
		t.Error("--config with a missing file was accepted")
	}

	env := testEnv(t, map[string]string{"VOICE_SCRIBE_CONFIG": missing})
	_, _, err := Load("", env)
	if err == nil {
		t.Fatal("$VOICE_SCRIBE_CONFIG pointing at a missing file was accepted")
	}
	if !strings.Contains(err.Error(), "VOICE_SCRIBE_CONFIG") {
		t.Errorf("error should name the environment variable, got %q", err)
	}
}

// TestUnknownKeysAreRejected is the strict-decode contract: a mistyped key must
// fail loudly rather than being dropped on the floor.
func TestUnknownKeysAreRejected(t *testing.T) {
	env := testEnv(t, nil)
	path := write(t, filepath.Join(env.WorkDir, "config.toml"), `
default_model = "x"

[transcribe]
formatt = "json"
`)

	_, _, err := Load("", env)
	if err == nil {
		t.Fatal("a config file with a misspelled key was accepted")
	}
	if !strings.Contains(err.Error(), "formatt") {
		t.Errorf("error should name the offending key, got %q", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file, got %q", err)
	}
}

func TestPartialFileKeepsDefaultsForEverythingElse(t *testing.T) {
	env := testEnv(t, nil)
	write(t, filepath.Join(env.WorkDir, "config.toml"), `
[diarize]
threshold = 0.3
`)

	cfg, _, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Diarize.Threshold != 0.3 {
		t.Errorf("Threshold = %g, want 0.3", cfg.Diarize.Threshold)
	}
	if cfg.Diarize.Enabled {
		t.Error("Enabled should stay false when a partial file does not set it")
	}
	if cfg.Transcribe.Format != "json" {
		t.Errorf("Format = %q, want the default to survive", cfg.Transcribe.Format)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	env := testEnv(t, map[string]string{
		"VOICE_SCRIBE_DEFAULT_MODEL": "from-env",
		"VOICE_SCRIBE_THREADS":       "4",
	})
	write(t, filepath.Join(env.WorkDir, "config.toml"), `
default_model = "from-file"

[transcribe]
threads = 2
`)

	cfg, _, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "from-env" {
		t.Errorf("DefaultModel = %q, want the environment to win", cfg.DefaultModel)
	}
	if cfg.Transcribe.Threads != 4 {
		t.Errorf("Threads = %d, want 4 from the environment", cfg.Transcribe.Threads)
	}
}

func TestBadThreadsEnvIsReported(t *testing.T) {
	env := testEnv(t, map[string]string{"VOICE_SCRIBE_THREADS": "many"})
	if _, _, err := Load("", env); err == nil {
		t.Error("a non-numeric $VOICE_SCRIBE_THREADS was accepted")
	}
}

// TestModelsDirExpandsHome keeps a config file portable between machines whose
// user names differ.
func TestModelsDirExpandsHome(t *testing.T) {
	env := testEnv(t, nil)
	write(t, filepath.Join(env.WorkDir, "config.toml"), `models_dir = "~/models/voice-scribe"`)

	cfg, _, err := Load("", env)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(env.Home, "models", "voice-scribe")
	if cfg.ModelsDir != want {
		t.Errorf("ModelsDir = %q, want %q", cfg.ModelsDir, want)
	}
}

func TestValidateRejectsImpossibleSettings(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"negative threads":    func(c *Config) { c.Transcribe.Threads = -1 },
		"negative threshold":  func(c *Config) { c.Diarize.Threshold = -0.1 },
		"negative mcp inline": func(c *Config) { c.MCP.InlineThreshold = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Error("Validate accepted an impossible configuration")
			}
		})
	}
}

func TestSearchPathsAreReportable(t *testing.T) {
	// `doctor` shows these to a user who cannot work out why their file is
	// being ignored, so the list must be non-empty and ordered.
	env := testEnv(t, map[string]string{"VOICE_SCRIBE_CONFIG": "/explicit.toml"})
	paths := SearchPaths(env)

	if len(paths) < 2 {
		t.Fatalf("SearchPaths returned %v, want the explicit path plus the defaults", paths)
	}
	if paths[0] != "/explicit.toml" {
		t.Errorf("paths[0] = %q, want $VOICE_SCRIBE_CONFIG first", paths[0])
	}
}
