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
	asrSampleRate = 16000
	// foldTokens is where the envelope stops inlining the transcript. It only
	// needs to be small enough that transcribing an hour of audio does not hand
	// a caller ten thousand tokens it did not ask for.
	foldTokens = 2000
	// previewRunes is the folded envelope's excerpt length.
	previewRunes = 60
)

type HearCmd struct {
	File     string   `arg:"" optional:"" help:"Audio file to transcribe"`
	Mic      bool     `help:"Record from the default input device instead"`
	Duration int      `short:"d" default:"5" help:"Recording duration in seconds (--mic only)"`
	Model    string   `short:"m" default:"fun-asr-flash-2026-06-15" enum:"fun-asr-flash-2026-06-15,qwen-audio-3.0-asr-flash" help:"ASR model"`
	Vocab    string   `short:"v" help:"Vocabulary name under ~/.vox/vocabulary/"`
	Lang     []string `short:"l" help:"Language hint, e.g. zh or en (repeatable; auto-detect when unset)"`
	Context  []string `short:"c" help:"Prompt context to improve recognition (repeatable)"`
	Refresh  bool     `help:"Re-recognize and overwrite the stored run"`
}

func (c *HearCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}
	if c.File == "" && !c.Mic {
		return voxerr.New(voxerr.AudioUnsupported, "no audio given").
			WithHint("vox hear <file>  ·  vox hear --mic")
	}

	client := dashscope.NewClient(apiKey)

	audioData, format, source, err := c.acquire()
	if err != nil {
		return err
	}

	durationSec, err := audio.Duration(source, audioData)
	if err != nil {
		ui.Warn("duration unknown (%v); only the size limit is enforced", err)
	}
	if err := checkLimits(len(audioData), durationSec); err != nil {
		return err
	}

	// The vocabulary is resolved locally first: its content hash is part of the
	// run identity, but a stored run must not cost a remote round-trip, so the
	// server-side list is only reconciled on a miss.
	var vocabulary *vocab.Vocabulary
	args := run.Args{
		Model:   c.Model,
		Format:  format,
		Lang:    normalize(c.Lang),
		Context: normalize(c.Context),
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
	ui.Info("%s %s", ui.Dim("model"), ui.Key(c.Model))

	result, err := client.Transcribe(audioData, dashscope.ASROptions{
		Model:         c.Model,
		Format:        format,
		SampleRate:    sampleRateFor(c.File),
		Context:       args.Context,
		VocabularyID:  vocabularyID,
		LanguageHints: args.Lang,
	})
	if err != nil {
		return voxerr.New(voxerr.APIError, "%s", err.Error())
	}
	ui.Info("%s %s", ui.Dim("latency"), ui.Dim(time.Since(t0).Round(time.Millisecond).String()))

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

// acquire returns the audio bytes, its container format, and a source label.
func (c *HearCmd) acquire() (data []byte, format, source string, err error) {
	if c.File != "" {
		data, err = os.ReadFile(c.File)
		if err != nil {
			return nil, "", "", voxerr.New(voxerr.AudioUnsupported, "cannot read %s: %v", c.File, err)
		}
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(c.File)), ".")
		// A whitelist, not just a non-empty check: the extension becomes the
		// declared format in the request, so `notes.txt` would otherwise be
		// uploaded as audio/txt and fail as an opaque server error.
		if !supportedFormats[format] {
			return nil, "", "", voxerr.New(voxerr.AudioUnsupported,
				"%s is not a supported audio format", displayFormat(format, c.File)).
				WithHint("supported: %s", strings.Join(formatList(), ", "))
		}
		abs, _ := filepath.Abs(c.File)
		return data, format, abs, nil
	}

	ui.Info("Recording for %ds... %s", c.Duration, ui.Dim("(speak now)"))
	recorder, err := audio.NewRecorder(asrSampleRate, 1)
	if err != nil {
		return nil, "", "", fmt.Errorf("init recorder: %w", err)
	}
	if err := recorder.Start(); err != nil {
		return nil, "", "", fmt.Errorf("start recording: %w", err)
	}
	time.Sleep(time.Duration(c.Duration) * time.Second)
	pcm := recorder.Stop()
	ui.Info("%s %s", ui.Dim("recorded"), ui.Dim(fmt.Sprintf("%d bytes", len(pcm))))

	return wrapPCMAsWAVWithRate(pcm, asrSampleRate), "wav", "mic", nil
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

// normalize drops empty entries so a caller passing `-c ""` produces the same
// run identity as one passing nothing — the API ignores them either way.
func normalize(values []string) []string {
	var out []string
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// checkLimits rejects oversized audio locally. The server would reject it too,
// but only after the upload — and its message says nothing about what to do.
// durationSec of 0 means unknown, in which case only the size cap applies.
func checkLimits(bytes, durationSec int) error {
	if bytes > dashscope.MaxAudioBytes {
		return voxerr.New(voxerr.AudioTooLarge, "audio is %.1fMB, over the %dMB request limit",
			float64(bytes)/(1024*1024), dashscope.MaxAudioBytes/(1024*1024)).
			WithHint("split the file, or downmix it to 16kHz mono")
	}
	if durationSec > dashscope.MaxAudioSeconds {
		return voxerr.New(voxerr.AudioTooLarge, "audio is %ds, over the %ds limit for this model",
			durationSec, dashscope.MaxAudioSeconds).
			WithHint("split the file into segments under %d minutes", dashscope.MaxAudioSeconds/60)
	}
	return nil
}

// sampleRateFor only claims a rate for audio vox recorded itself.
func sampleRateFor(file string) int {
	if file == "" {
		return asrSampleRate
	}
	return 0
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

// wrapPCMAsWAVWithRate wraps raw PCM 16-bit mono data in a WAV container at the given sample rate
func wrapPCMAsWAVWithRate(pcm []byte, sampleRate int) []byte {
	dataLen := uint32(len(pcm))
	fileLen := dataLen + 36
	sr := uint32(sampleRate)
	br := sr * 2 // 16-bit mono

	header := []byte{
		'R', 'I', 'F', 'F',
		byte(fileLen), byte(fileLen >> 8), byte(fileLen >> 16), byte(fileLen >> 24),
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		16, 0, 0, 0,
		1, 0, // PCM
		1, 0, // mono
		byte(sr), byte(sr >> 8), byte(sr >> 16), byte(sr >> 24),
		byte(br), byte(br >> 8), byte(br >> 16), byte(br >> 24),
		2, 0, // block align
		16, 0, // bits per sample
		'd', 'a', 't', 'a',
		byte(dataLen), byte(dataLen >> 8), byte(dataLen >> 16), byte(dataLen >> 24),
	}

	return append(header, pcm...)
}
