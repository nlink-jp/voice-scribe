package cmd

import (
	"context"
	"path/filepath"
	"time"

	"github.com/nlink-jp/voice-scribe/internal/audio"
	"github.com/nlink-jp/voice-scribe/internal/catalog"
	"github.com/nlink-jp/voice-scribe/internal/config"
	"github.com/nlink-jp/voice-scribe/internal/diarize"
	"github.com/nlink-jp/voice-scribe/internal/engine"
	"github.com/nlink-jp/voice-scribe/internal/mcp/tools"
	"github.com/nlink-jp/voice-scribe/internal/store"
	"github.com/nlink-jp/voice-scribe/internal/transcript"
)

// defaultWorkspaceRoot is where workspaces live when the agent does not prepare
// one of its own. It sits beside the models rather than in the config
// directory: these are working files, sometimes large ones.
func defaultWorkspaceRoot() string {
	env := config.OSEnv()
	return filepath.Join(store.DefaultDataDir(env.Getenv, env.Home), "mcp-workspaces")
}

// mcpTranscriber adapts the CLI's transcription path to the MCP tool interface.
//
// It deliberately opens and closes a session per call rather than keeping a
// model resident. Two transcriptions never overlap — the job manager has one
// worker — so the only thing residency would buy is skipping a reload between
// calls, at the cost of holding half a gigabyte indefinitely in a server that
// may sit idle for hours. Making it resident is a change to consider once
// there is a measurement saying the reload matters.
type mcpTranscriber struct {
	rt *runtimeContext
}

func newMCPTranscriber(rt *runtimeContext) tools.Transcriber {
	return &mcpTranscriber{rt: rt}
}

func (m *mcpTranscriber) Transcribe(ctx context.Context, req tools.Request, report func(float64, string)) (transcript.Result, error) {
	model, err := m.rt.Store.Resolve(req.Model, m.rt.Config.DefaultModel, req.Language)
	if err != nil {
		return transcript.Result{}, err
	}

	report(0, "decoding")
	decoded, err := audio.Decode(req.Audio)
	if err != nil {
		return transcript.Result{}, err
	}

	report(0.05, "loading "+model.Name)
	session, err := engine.Open(model.Path)
	if err != nil {
		return transcript.Result{}, err
	}
	defer session.Close()

	params := engine.Params{
		Language:    req.Language,
		Prompt:      req.Prompt,
		Threads:     m.rt.Config.Transcribe.Threads,
		OffsetSec:   req.OffsetSec,
		DurationSec: req.DurationSec,
	}

	started := time.Now()
	report(0.1, "transcribing")
	result, err := session.Transcribe(decoded.Samples, params, func(pct int) {
		// Transcription owns the middle of the bar; decoding and diarization
		// take the ends.
		report(0.1+float64(pct)/100*0.6, "transcribing")
	})
	if err != nil {
		return transcript.Result{}, err
	}

	var translated []transcript.Timed
	if req.Translate {
		report(0.7, "translating")
		second, err := session.Transcribe(decoded.Samples, withTranslate(params), nil)
		if err != nil {
			return transcript.Result{}, err
		}
		for _, s := range second.Segments {
			translated = append(translated, transcript.Timed{Start: s.Start, End: s.End, Text: s.Text})
		}
	}

	var turns []transcript.SpeakerTurn
	if req.Diarize {
		report(0.85, "identifying speakers")
		turns, err = m.diarize(decoded, req)
		if err != nil {
			return transcript.Result{}, err
		}
	}
	report(0.98, "writing")

	return assembleTranscript(assembly{
		Source:       req.Audio,
		Model:        model,
		Decoded:      decoded,
		Result:       result,
		Translated:   translated,
		Turns:        turns,
		SpeakerHints: req.SpeakerHints,
		Elapsed:      time.Since(started),
		Translate:    req.Translate,
	}), nil
}

func (m *mcpTranscriber) diarize(decoded audio.Audio, req tools.Request) ([]transcript.SpeakerTurn, error) {
	segmentation, embedding, err := m.rt.Store.ResolveDiarization()
	if err != nil {
		return nil, err
	}

	threshold := req.SpeakerThreshold
	if threshold == 0 && req.Speakers == 0 {
		threshold = m.rt.Config.Diarize.Threshold
	}

	found, err := diarize.Run(decoded.Samples, diarize.Models{
		Segmentation: segmentation.Path,
		Embedding:    embedding.Path,
	}, diarize.Params{
		NumSpeakers: req.Speakers,
		Threshold:   threshold,
		Threads:     m.rt.Config.Transcribe.Threads,
	}, nil)
	if err != nil {
		return nil, err
	}

	turns := make([]transcript.SpeakerTurn, 0, len(found))
	for _, t := range found {
		turns = append(turns, transcript.SpeakerTurn{Start: t.Start, End: t.End, Speaker: t.Speaker})
	}
	return turns, nil
}

// listModelsView backs the list_models tool with the same views that back
// `models list --json`, so the two never disagree about what is installed.
func listModelsView(rt *runtimeContext, scope string) (any, error) {
	installed, err := rt.Store.List()
	if err != nil {
		return nil, err
	}

	installedViews := make([]installedView, 0, len(installed))
	for _, m := range installed {
		installedViews = append(installedViews, viewOf(m))
	}
	catalogViews := make([]catalogView, 0)
	for _, e := range catalog.All() {
		catalogViews = append(catalogViews, catalogViewOf(e, isInstalled(installed, e.Name)))
	}

	switch scope {
	case "catalog":
		return catalogViews, nil
	case "all":
		return map[string]any{"installed": installedViews, "catalog": catalogViews}, nil
	default:
		return installedViews, nil
	}
}
