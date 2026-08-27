package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/voice-scribe/internal/config"
	"github.com/nlink-jp/voice-scribe/internal/mcp/tools"
	"github.com/nlink-jp/voice-scribe/internal/store"
)

// wiringContext builds a runtimeContext over a real registry in a temp dir —
// Add verifies the model file exists, so each named model gets one.
func wiringContext(t *testing.T, cfg config.Config, models ...store.Model) *mcpTranscriber {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		m.Path = filepath.Join(dir, m.Name+".bin")
		if err := os.WriteFile(m.Path, []byte("not a model"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.Add(m); err != nil {
			t.Fatal(err)
		}
	}
	return &mcpTranscriber{rt: &runtimeContext{Config: cfg, Store: st}}
}

// TestMCPHonoursTheVADRequestAndTheConfig pins the wiring ADR-0009 restored:
// the MCP path resolves VAD with the same flag-or-config expression as the
// CLI. Before it, `vad = true` in the config was silently ignored over MCP —
// while Threads from the same table was honoured — and no request field could
// turn VAD on either.
func TestMCPHonoursTheVADRequestAndTheConfig(t *testing.T) {
	vad := store.Model{Name: "silero-vad", Kind: store.KindVAD}
	var cfgOn config.Config
	cfgOn.Transcribe.VAD = true

	for name, tc := range map[string]struct {
		cfg     config.Config
		req     tools.Request
		wantVAD bool
	}{
		"requested":      {req: tools.Request{VAD: true}, wantVAD: true},
		"config default": {cfg: cfgOn, wantVAD: true},
		"neither asks":   {},
	} {
		t.Run(name, func(t *testing.T) {
			m := wiringContext(t, tc.cfg, vad)
			params, err := m.engineParams(tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if got := params.VADModelPath != ""; got != tc.wantVAD {
				t.Fatalf("VADModelPath = %q, want set: %v", params.VADModelPath, tc.wantVAD)
			}
			if tc.wantVAD && !strings.HasSuffix(params.VADModelPath, "silero-vad.bin") {
				t.Errorf("VADModelPath = %q, want the installed VAD model", params.VADModelPath)
			}
		})
	}
}

// A VAD request without the model has to fail the way classify() recognises
// as model_not_found: by mentioning `models pull`.
func TestMCPVADWithoutTheModelNamesThePull(t *testing.T) {
	m := wiringContext(t, config.Config{})
	_, err := m.engineParams(tools.Request{VAD: true})
	if err == nil {
		t.Fatal("engineParams succeeded without a VAD model installed")
	}
	if !strings.Contains(err.Error(), "models pull") {
		t.Errorf("error %q should tell the caller to run `models pull`", err)
	}
}

func TestMCPEngineParamsCarryTheRequest(t *testing.T) {
	var cfg config.Config
	cfg.Transcribe.Threads = 6
	m := wiringContext(t, cfg)

	params, err := m.engineParams(tools.Request{
		Language: "ja", Prompt: "会議", OffsetSec: 1.5, DurationSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if params.Language != "ja" || params.Prompt != "会議" || params.OffsetSec != 1.5 || params.DurationSec != 30 {
		t.Errorf("request fields lost: %+v", params)
	}
	if params.Threads != 6 {
		t.Errorf("Threads = %d, want the configured 6", params.Threads)
	}
}
