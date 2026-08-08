package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nlink-jp/voice-scribe/internal/audio"
	"github.com/nlink-jp/voice-scribe/internal/diarize"
	"github.com/nlink-jp/voice-scribe/internal/engine"
	"github.com/nlink-jp/voice-scribe/internal/store"
	"github.com/nlink-jp/voice-scribe/internal/transcript"
	"github.com/spf13/cobra"
)

var transcribeOpts struct {
	model     string
	language  string
	translate bool
	prompt    string
	threads   int
	offset    float64
	duration  float64
	format    string
	output    string
	vad       bool
	quiet     bool

	diarize          bool
	speakers         int
	speakerThreshold float64
	speakerHints     []string
}

var transcribeCmd = &cobra.Command{
	Use:   "transcribe <file>",
	Short: "Transcribe an audio or video file",
	Long: `transcribe converts speech in an audio or video file into text with
timestamps, entirely on this machine.

Any container macOS can read works — m4a, mp3, wav, mp4, mov and so on.
Containers AVFoundation does not know (mkv, webm) are rejected with a message
saying so; convert them first.

Output goes to stdout as JSON unless -o or -f says otherwise.`,
	Args: cobra.ExactArgs(1),
	RunE: runTranscribe,
}

func init() {
	rootCmd.AddCommand(transcribeCmd)

	f := transcribeCmd.Flags()
	f.StringVarP(&transcribeOpts.model, "model", "m", "", "Model to use (default: the configured one, or a match for --lang)")
	f.StringVar(&transcribeOpts.language, "lang", "", "Input language as an ISO 639-1 code (default: detect)")
	f.BoolVar(&transcribeOpts.translate, "translate", false, "Also produce an English translation")
	f.StringVar(&transcribeOpts.prompt, "prompt", "", "Bias the decoder's vocabulary, e.g. proper nouns and jargon")
	f.IntVar(&transcribeOpts.threads, "threads", 0, "Inference threads (0 = automatic)")
	f.Float64Var(&transcribeOpts.offset, "offset", 0, "Start this many seconds into the audio")
	f.Float64Var(&transcribeOpts.duration, "duration", 0, "Transcribe only this many seconds (0 = to the end)")
	f.StringVarP(&transcribeOpts.format, "format", "f", "", "Output format: json, text, md, srt, vtt")
	f.StringVarP(&transcribeOpts.output, "output-file", "o", "", "Write to this file instead of stdout")
	f.BoolVar(&transcribeOpts.vad, "vad", false, "Gate silence through the VAD model (needs `models pull silero-vad`)")
	f.BoolVarP(&transcribeOpts.quiet, "quiet", "q", false, "Suppress progress output on stderr")

	f.BoolVar(&transcribeOpts.diarize, "diarize", false, "Label who is speaking (needs the diarization models)")
	f.IntVar(&transcribeOpts.speakers, "speakers", 0, "Pin the speaker count when it is known (0 = work it out)")
	f.Float64Var(&transcribeOpts.speakerThreshold, "speaker-threshold", 0,
		"Clustering distance when the count is unknown; lower splits more readily (0 = default)")
	f.StringSliceVar(&transcribeOpts.speakerHints, "speaker-hint", nil, "Names to use instead of A/B/C, in order of first appearance")
}

func runTranscribe(cmd *cobra.Command, args []string) error {
	rt, err := newRuntimeContext()
	if err != nil {
		return err
	}

	formatName := transcribeOpts.format
	if formatName == "" {
		formatName = rt.Config.Transcribe.Format
	}
	format, err := transcript.ParseFormat(formatName)
	if err != nil {
		return err
	}

	threads := transcribeOpts.threads
	if threads == 0 {
		threads = rt.Config.Transcribe.Threads
	}

	model, err := rt.Store.Resolve(transcribeOpts.model, rt.Config.DefaultModel, transcribeOpts.language)
	if err != nil {
		return err
	}

	vadPath, err := resolveVAD(rt, transcribeOpts.vad || rt.Config.Transcribe.VAD)
	if err != nil {
		return err
	}

	// Progress and runtime chatter go to stderr, never stdout: stdout may be a
	// pipe carrying the transcript, and `voice-scribe mcp` puts JSON-RPC there.
	progress := newProgressReporter(cmd.ErrOrStderr(), transcribeOpts.quiet)
	engine.SetLogHandler(func(level engine.LogLevel, text string) {
		if !transcribeOpts.quiet && level >= engine.LogWarn {
			fmt.Fprintln(cmd.ErrOrStderr(), text)
		}
	})

	source := args[0]
	progress.stage("decoding %s", filepath.Base(source))
	decoded, err := audio.Decode(source)
	if err != nil {
		return err
	}

	progress.stage("loading %s", model.Name)
	session, err := engine.Open(model.Path)
	if err != nil {
		return err
	}
	defer session.Close()

	params := engine.Params{
		Language:     transcribeOpts.language,
		Prompt:       transcribeOpts.prompt,
		Threads:      threads,
		OffsetSec:    transcribeOpts.offset,
		DurationSec:  transcribeOpts.duration,
		VADModelPath: vadPath,
	}

	progress.stage("transcribing %s of audio", formatDuration(decoded.Duration))
	started := time.Now()
	result, err := session.Transcribe(decoded.Samples, params, progress.percent)
	if err != nil {
		return err
	}

	// Whisper's translate task is a separate decode rather than an extra
	// output, so producing the original alongside an English translation means
	// running the audio through twice. That is why --translate roughly doubles
	// the time, and why the two passes have to be merged by timestamp.
	var translated []transcript.Timed
	if transcribeOpts.translate {
		progress.stage("translating")
		second, err := session.Transcribe(decoded.Samples, withTranslate(params), progress.percent)
		if err != nil {
			return err
		}
		for _, s := range second.Segments {
			translated = append(translated, transcript.Timed{Start: s.Start, End: s.End, Text: s.Text})
		}
	}
	// Diarization is a second pass over the same samples with a different pair
	// of models. It runs after transcription so that a failure here does not
	// throw away work that already succeeded.
	var turns []transcript.SpeakerTurn
	if transcribeOpts.diarize || rt.Config.Diarize.Enabled {
		turns, err = runDiarization(rt, decoded, progress)
		if err != nil {
			return err
		}
	}
	elapsed := time.Since(started)
	progress.done()

	out := assembleTranscript(assembly{
		Source:       source,
		Model:        model,
		Decoded:      decoded,
		Result:       result,
		Translated:   translated,
		Turns:        turns,
		SpeakerHints: transcribeOpts.speakerHints,
		Elapsed:      elapsed,
		Translate:    transcribeOpts.translate,
	})
	if err := out.Validate(); err != nil {
		return fmt.Errorf("%w — the audio may be silent, or in a language the model does not handle", err)
	}

	files, err := transcript.Render(out, format)
	if err != nil {
		return err
	}
	return writeOutput(cmd, files, transcribeOpts.output)
}

// resolveVAD turns the --vad request into a model path, or explains what is
// missing. Silently transcribing without the gating the user asked for would
// leave them believing hallucinations were suppressed when they were not.
func resolveVAD(rt *runtimeContext, want bool) (string, error) {
	if !want {
		return "", nil
	}
	models, err := rt.Store.List()
	if err != nil {
		return "", err
	}
	for _, m := range models {
		if m.Kind == store.KindVAD {
			return m.Path, nil
		}
	}
	return "", fmt.Errorf("--vad needs a VAD model: run `voice-scribe models pull silero-vad`")
}

func runDiarization(rt *runtimeContext, decoded audio.Audio, progress *progressReporter) ([]transcript.SpeakerTurn, error) {
	segmentation, embedding, err := rt.Store.ResolveDiarization()
	if err != nil {
		return nil, err
	}

	params := diarize.Params{
		NumSpeakers: transcribeOpts.speakers,
		Threshold:   transcribeOpts.speakerThreshold,
		Threads:     transcribeOpts.threads,
	}
	// The configured threshold applies only when the user gave neither a
	// threshold nor a pinned count; otherwise a config file would silently
	// override an explicit flag.
	if params.Threshold == 0 && params.NumSpeakers == 0 {
		params.Threshold = rt.Config.Diarize.Threshold
	}

	progress.stage("identifying speakers")
	found, err := diarize.Run(decoded.Samples, diarize.Models{
		Segmentation: segmentation.Path,
		Embedding:    embedding.Path,
	}, params, progress.percent)
	if err != nil {
		return nil, err
	}

	turns := make([]transcript.SpeakerTurn, 0, len(found))
	for _, t := range found {
		turns = append(turns, transcript.SpeakerTurn{Start: t.Start, End: t.End, Speaker: t.Speaker})
	}
	return turns, nil
}

// withTranslate returns the parameters for the English pass. The prompt is
// dropped: it biases the decoder towards vocabulary in the source language,
// which pulls the translation back towards it.
func withTranslate(p engine.Params) engine.Params {
	p.Translate = true
	p.Prompt = ""
	return p
}

// assembly is everything needed to turn engine output into a transcript. It is
// a struct rather than a long parameter list because both the CLI and the MCP
// server build one, and neither should be reading the other's flag variables.
type assembly struct {
	Source       string
	Model        store.Model
	Decoded      audio.Audio
	Result       engine.Result
	Translated   []transcript.Timed
	Turns        []transcript.SpeakerTurn
	SpeakerHints []string
	Elapsed      time.Duration
	Translate    bool
}

func assembleTranscript(a assembly) transcript.Result {
	source, model, decoded, result := a.Source, a.Model, a.Decoded, a.Result
	translated, turns := a.Translated, a.Turns
	elapsed := a.Elapsed

	language := result.Language
	if language == "" {
		language = "und"
	}

	segments := make([]transcript.Segment, 0, len(result.Segments))
	for _, s := range result.Segments {
		segments = append(segments, transcript.Segment{
			Start:   s.Start,
			End:     s.End,
			Speaker: transcript.SingleSpeaker,
			Text:    map[string]string{language: s.Text},
		})
	}

	languages := []string{language}
	if len(translated) > 0 && language != "en" {
		segments = transcript.MergeLanguage(segments, "en", translated)
		languages = append(languages, "en")
	}
	if len(turns) > 0 {
		segments = transcript.AssignSpeakers(segments, turns, a.SpeakerHints)
	}

	duration := decoded.Duration
	rtf := 0.0
	if duration > 0 {
		rtf = elapsed.Seconds() / duration
	}

	out := transcript.Result{
		Metadata: transcript.Metadata{
			Source:          filepath.Base(source),
			Model:           model.Name,
			DurationSeconds: &duration,
			Languages:       languages,
			DroppedSegments: 0,
			Engine:          "whisper.cpp",
			Translated:      a.Translate,
			Diarized:        len(turns) > 0,
			SpeakerHints:    a.SpeakerHints,
			RealTimeFactor:  &rtf,
		},
		Segments: segments,
	}
	out.Normalize()
	return out
}

// writeOutput sends rendered files to stdout or to disk. When a format produced
// several files — subtitles for a translated transcript — the language tag is
// inserted before the extension, matching gem-transcribe's naming.
func writeOutput(cmd *cobra.Command, files []transcript.File, dest string) error {
	if dest == "" {
		for _, f := range files {
			if f.Suffix != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "# %s\n", strings.TrimPrefix(f.Suffix, "."))
			}
			fmt.Fprint(cmd.OutOrStdout(), f.Content)
		}
		return nil
	}

	for _, f := range files {
		path := dest
		if f.Suffix != "" {
			ext := filepath.Ext(dest)
			path = strings.TrimSuffix(dest, ext) + f.Suffix + ext
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "wrote", path)
	}
	return nil
}

func formatDuration(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	return d.Round(time.Second).String()
}
