package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/celados/vox/internal/audio"
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/run"
	"github.com/celados/vox/internal/ui"
	"github.com/celados/vox/internal/vocab"
	"github.com/celados/vox/internal/voxerr"
)

const (
	// foldTokens is where the envelope stops inlining the transcript. It only
	// needs to be small enough that transcribing an hour of audio does not hand
	// a caller ten thousand tokens it did not ask for.
	foldTokens = 2000
	// previewRunes is the folded envelope's excerpt length.
	previewRunes = 60
)

type HearCmd struct {
	File     string   `arg:"" help:"Audio file to transcribe"`
	Model    string   `short:"m" default:"fun-asr" enum:"fun-asr,qwen-audio-3.0-asr-flash-filetrans" help:"ASR model"`
	Vocab    string   `short:"v" help:"Vocabulary name under ~/.vox/vocabulary/"`
	Lang     []string `short:"l" help:"Language hint, e.g. zh or en (repeatable; auto-detect when unset)"`
	Speakers bool     `help:"Label speakers (recommended under 2 hours)"`
	Refresh  bool     `help:"Re-recognize and overwrite the stored run"`
}

func (c *HearCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}
	client := dashscope.NewClient(apiKey)

	audioData, format, source, err := c.read()
	if err != nil {
		return err
	}

	durationSec, probeErr := audio.Duration(source, audioData)
	if err := checkLimits(len(audioData), durationSec); err != nil {
		return err
	}
	if c.Speakers && durationSec > dashscope.DiarizationMaxSeconds {
		ui.Warn("diarization past %dh may time out rather than degrade",
			dashscope.DiarizationMaxSeconds/3600)
	}

	// The vocabulary is resolved locally first: its content hash is part of the
	// run identity, but a stored run must not cost a remote round-trip, so the
	// server-side list is only reconciled on a miss.
	var vocabulary *vocab.Vocabulary
	args := run.Args{
		Model:    c.Model,
		Format:   format,
		Lang:     normalize(c.Lang),
		Speakers: c.Speakers,
	}
	if c.Vocab != "" {
		if vocabulary, err = vocab.Load(cfg.Dir, c.Vocab); err != nil {
			return err
		}
		words, warnings := vocabulary.Resolve(c.Model)
		for _, w := range warnings {
			ui.Warn("%s", w)
		}
		args.Vocab = vocab.ContentHash(words)
	}

	store := run.NewStore(cfg.Dir)
	digest := run.Digest(audioData, args)
	sid := run.SID(digest)

	if !c.Refresh {
		if rec, err := store.Load(sid, digest); err == nil {
			ui.Info("%s", ui.Dim("stored"))
			return printEnvelope(rec)
		}
	}
	if probeErr != nil {
		ui.Warn("duration unknown (%v); the service will report it", probeErr)
	}

	var vocabularyID string
	if vocabulary != nil {
		result, err := vocab.Sync(client, cfg.Dir, vocabulary, c.Model, false)
		if err != nil {
			return err
		}
		if result.Action != "reused" {
			ui.Info("%s %s %s", ui.Dim("vocab"), ui.Key(c.Vocab), ui.Dim(result.Action))
		}
		vocabularyID = result.VocabularyID
	}

	t0 := time.Now()
	ui.Info("%s %s %s", ui.Dim("model"), ui.Key(c.Model), ui.Dim(formatSeconds(durationSec)))

	result, err := client.TranscribeFile(filepath.Base(source), audioData, dashscope.FileOptions{
		Model:         c.Model,
		VocabularyID:  vocabularyID,
		LanguageHints: args.Lang,
		Diarization:   args.Speakers,
	}, taskProgress())
	if err != nil {
		return voxerr.New(voxerr.APIError, "%s", err.Error())
	}
	ui.Info("%s %s", ui.Dim("latency"), ui.Dim(time.Since(t0).Round(time.Second).String()))

	if durationSec == 0 {
		durationSec = result.DurationSec
	}
	vocabLabel := ""
	if vocabulary != nil {
		vocabLabel = c.Vocab + "@" + args.Vocab
	}
	rec := &run.Record{
		Digest: digest,
		Meta: run.Meta{
			SID:     sid,
			Source:  run.Tilde(source),
			Model:   c.Model,
			Vocab:   vocabLabel,
			Lang:    args.Lang,
			Created: time.Now().UTC(),
			Path:    run.Tilde(store.Dir(sid)),
			Size:    run.Measure(result, durationSec),
		},
		Args:   args,
		Result: result,
	}
	if err := store.Save(rec, audioData, format); err != nil {
		return err
	}
	return printEnvelope(rec)
}

// read returns the audio bytes, its container format, and the absolute source path.
func (c *HearCmd) read() (data []byte, format, source string, err error) {
	data, err = os.ReadFile(c.File)
	if err != nil {
		return nil, "", "", voxerr.New(voxerr.AudioUnsupported, "cannot read %s: %v", c.File, err)
	}
	format = strings.TrimPrefix(strings.ToLower(filepath.Ext(c.File)), ".")
	// A whitelist, not just a non-empty check: an unsupported container reaches
	// the service as an opaque failure minutes into a task.
	if !supportedFormats[format] {
		return nil, "", "", voxerr.New(voxerr.AudioUnsupported,
			"%s is not a supported audio format", displayFormat(format, c.File)).
			WithHint("supported: %s", strings.Join(formatList(), ", "))
	}
	abs, _ := filepath.Abs(c.File)
	return data, format, abs, nil
}

// supportedFormats are the containers the recognition API accepts.
var supportedFormats = map[string]bool{
	"wav": true, "mp3": true, "opus": true, "ogg": true,
	"m4a": true, "aac": true, "flac": true, "amr": true, "wma": true,
}

func formatList() []string {
	list := make([]string, 0, len(supportedFormats))
	for f := range supportedFormats {
		list = append(list, f)
	}
	sort.Strings(list)
	return list
}

func displayFormat(format, file string) string {
	if format == "" {
		return filepath.Base(file) + " (no extension)"
	}
	return "." + format
}

// normalize drops empty entries so `-l ""` produces the same run identity as
// passing nothing — the API ignores them either way.
func normalize(values []string) []string {
	var out []string
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// checkLimits rejects audio the service cannot accept, before spending the
// upload. A duration of 0 means unknown, in which case only the size cap applies.
func checkLimits(bytes, durationSec int) error {
	if bytes > dashscope.MaxFileBytes {
		return voxerr.New(voxerr.AudioTooLarge, "audio is %.1fGB, over the %dGB limit",
			float64(bytes)/(1024*1024*1024), dashscope.MaxFileBytes/(1024*1024*1024)).
			WithHint("re-encode it, or split it")
	}
	if durationSec > dashscope.MaxFileSeconds {
		return voxerr.New(voxerr.AudioTooLarge, "audio is %s, over the %dh limit",
			formatSeconds(durationSec), dashscope.MaxFileSeconds/3600).
			WithHint("split the file into parts under %d hours", dashscope.MaxFileSeconds/3600)
	}
	return nil
}

func formatSeconds(sec int) string {
	switch {
	case sec == 0:
		return ""
	case sec >= 3600:
		return fmt.Sprintf("%dh%02dm", sec/3600, sec%3600/60)
	case sec >= 60:
		return fmt.Sprintf("%dm%02ds", sec/60, sec%60)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// taskProgress reports the job's state so a minutes-long transcription is not
// silent. Every line goes to stderr; stdout stays reserved for the envelope.
func taskProgress() dashscope.TaskProgress {
	var last string
	return func(status string, elapsed time.Duration) {
		if status == last {
			return
		}
		last = status
		ui.Info("%s %s %s", ui.Dim("task"), ui.Key(strings.ToLower(status)),
			ui.Dim(elapsed.Round(time.Second).String()))
	}
}

// printEnvelope writes the run's YAML envelope, inlining a short transcript and
// folding a long one behind an export hint.
func printEnvelope(rec *run.Record) error {
	if rec.Meta.Size.Tokens <= foldTokens {
		return emitYAML(struct {
			run.Meta `yaml:",inline"`
			Text     string `yaml:"text"`
		}{rec.Meta, rec.Result.Text})
	}
	return emitYAML(struct {
		Vox      []string `yaml:"$vox"`
		run.Meta `yaml:",inline"`
		Preview  string `yaml:"preview"`
	}{
		Vox: []string{fmt.Sprintf(
			"Transcript folded at %d tokens. Read it with `vox export %s --format md`.",
			foldTokens, rec.Meta.SID)},
		Meta:    rec.Meta,
		Preview: run.Preview(rec.Result.Text, previewRunes),
	})
}
