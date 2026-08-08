// Package config resolves voice-scribe's TOML configuration.
//
// Precedence, lowest to highest: built-in defaults, the config file,
// environment variables, command-line flags. Flags are applied by the caller —
// this package stops at the environment, because a flag's "was it set at all?"
// question belongs to the flag library.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the whole configuration surface.
type Config struct {
	DefaultModel string     `toml:"default_model"`
	ModelsDir    string     `toml:"models_dir"`
	Transcribe   Transcribe `toml:"transcribe"`
	Diarize      Diarize    `toml:"diarize"`
	MCP          MCP        `toml:"mcp"`
}

// Transcribe holds the defaults for a transcription run.
type Transcribe struct {
	Format string `toml:"format"`
	// VAD gates silent stretches through a voice-activity model, which
	// suppresses whisper's habit of hallucinating repeated phrases over
	// silence. It needs a separate VAD model installed, so it is off by
	// default rather than failing on a fresh install.
	VAD     bool `toml:"vad"`
	Threads int  `toml:"threads"`
}

// Diarize holds the defaults for speaker diarization.
type Diarize struct {
	Enabled     bool `toml:"enabled"`
	MinSpeakers int  `toml:"min_speakers"`
	MaxSpeakers int  `toml:"max_speakers"`
}

// MCP holds the defaults for the MCP server.
type MCP struct {
	// InlineThreshold is the transcript size, in bytes, at or below which the
	// text is returned to the agent inline instead of as a file path.
	InlineThreshold int `toml:"inline_threshold"`
}

// Default returns the configuration used when nothing is set anywhere.
func Default() Config {
	return Config{
		DefaultModel: "kotoba-whisper-v2.2",
		Transcribe: Transcribe{
			Format:  "json",
			VAD:     false,
			Threads: 0,
		},
		Diarize: Diarize{
			Enabled:     false,
			MinSpeakers: 1,
			MaxSpeakers: 8,
		},
		MCP: MCP{
			InlineThreshold: 8192,
		},
	}
}

// Env is the ambient state resolution depends on. It is injected so that the
// search order can be tested without reaching into the developer's real home
// directory or mutating the process environment.
type Env struct {
	Getenv  func(string) string
	Home    string
	WorkDir string
}

// OSEnv returns the real environment.
func OSEnv() Env {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	return Env{Getenv: os.Getenv, Home: home, WorkDir: wd}
}

func (e Env) get(key string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(key)
}

// SearchPaths returns the files consulted, in order, when no path is given.
//
// $VOICE_SCRIBE_CONFIG comes first and is special: if it is set the file must
// exist, because a typo in an explicitly named path should be an error rather
// than a silent fallback to defaults.
func SearchPaths(env Env) []string {
	var paths []string
	if p := env.get("VOICE_SCRIBE_CONFIG"); p != "" {
		paths = append(paths, p)
	}
	if xdg := env.get("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "voice-scribe", "config.toml"))
	} else if env.Home != "" {
		// macOS applications conventionally use ~/Library/Application Support,
		// but every tool in this org uses ~/.config, so a user configuring one
		// of them has already learned where to look.
		paths = append(paths, filepath.Join(env.Home, ".config", "voice-scribe", "config.toml"))
	}
	if env.WorkDir != "" {
		paths = append(paths, filepath.Join(env.WorkDir, "config.toml"))
	}
	return paths
}

// Load resolves the configuration.
//
// explicitPath, when non-empty, is the --config flag: it bypasses the search
// and must exist. The returned string is the file actually read, empty when
// none was found and the defaults are in effect.
func Load(explicitPath string, env Env) (Config, string, error) {
	cfg := Default()

	path, err := locate(explicitPath, env)
	if err != nil {
		return cfg, "", err
	}

	if path != "" {
		if err := decodeFile(path, &cfg); err != nil {
			return cfg, path, err
		}
	}

	if err := applyEnv(&cfg, env); err != nil {
		return cfg, path, err
	}
	if err := cfg.Validate(); err != nil {
		if path == "" {
			return cfg, path, err
		}
		return cfg, path, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, path, nil
}

func locate(explicitPath string, env Env) (string, error) {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return "", fmt.Errorf("config file %s: %w", explicitPath, err)
		}
		return explicitPath, nil
	}

	paths := SearchPaths(env)
	explicit := env.get("VOICE_SCRIBE_CONFIG")
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		} else if p == explicit {
			// Named outright via the environment: a missing file is a mistake
			// worth reporting, not a reason to fall through to the next path.
			return "", fmt.Errorf("config file %s (from $VOICE_SCRIBE_CONFIG): %w", p, err)
		}
	}
	return "", nil
}

// decodeFile reads a TOML file strictly: any key the schema does not know is an
// error. A silently ignored key is how a user ends up convinced a setting does
// nothing, and a typo is far more likely than a deliberate unknown key.
func decodeFile(path string, cfg *Config) error {
	md, err := toml.DecodeFile(path, cfg)
	if err != nil {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return fmt.Errorf("config file %s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}
	return nil
}

func applyEnv(cfg *Config, env Env) error {
	if v := env.get("VOICE_SCRIBE_DEFAULT_MODEL"); v != "" {
		cfg.DefaultModel = v
	}
	if v := env.get("VOICE_SCRIBE_MODELS_DIR"); v != "" {
		cfg.ModelsDir = v
	}
	if v := env.get("VOICE_SCRIBE_FORMAT"); v != "" {
		cfg.Transcribe.Format = v
	}
	if v := env.get("VOICE_SCRIBE_THREADS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("$VOICE_SCRIBE_THREADS=%q: not a number", v)
		}
		cfg.Transcribe.Threads = n
	}

	cfg.ModelsDir = expandHome(cfg.ModelsDir, env.Home)
	return nil
}

// expandHome resolves a leading ~ so that a config file stays portable between
// machines with different user names.
func expandHome(path, home string) string {
	if home == "" || path == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// Validate rejects a configuration that cannot produce a working run.
func (c Config) Validate() error {
	if c.Transcribe.Threads < 0 {
		return errors.New("transcribe.threads must be zero (automatic) or positive")
	}
	if c.Diarize.MinSpeakers < 1 {
		return errors.New("diarize.min_speakers must be at least 1")
	}
	if c.Diarize.MaxSpeakers < c.Diarize.MinSpeakers {
		return fmt.Errorf("diarize.max_speakers (%d) is below diarize.min_speakers (%d)",
			c.Diarize.MaxSpeakers, c.Diarize.MinSpeakers)
	}
	if c.MCP.InlineThreshold < 0 {
		return errors.New("mcp.inline_threshold must not be negative")
	}
	return nil
}
