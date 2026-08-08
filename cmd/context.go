package cmd

import (
	"fmt"

	"github.com/nlink-jp/voice-scribe/internal/config"
	"github.com/nlink-jp/voice-scribe/internal/store"
)

// runtimeContext is the resolved configuration and registry a command works
// against. Building it is deferred to command execution rather than done in
// init(), so that `--help` and `--version` never touch the filesystem.
type runtimeContext struct {
	Config     config.Config
	ConfigFile string
	Store      *store.Store
}

func newRuntimeContext() (*runtimeContext, error) {
	env := config.OSEnv()

	cfg, file, err := config.Load(configPath, env)
	if err != nil {
		return nil, err
	}

	dataDir := store.DefaultDataDir(env.Getenv, env.Home)
	st, err := store.New(dataDir, cfg.ModelsDir)
	if err != nil {
		return nil, err
	}

	return &runtimeContext{Config: cfg, ConfigFile: file, Store: st}, nil
}

// humanBytes renders a size the way a download progress line should read.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
