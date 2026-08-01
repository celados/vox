package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/celados/vox/internal/audio"
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/ui"
)

const asrSampleRate = 16000

type HearCmd struct {
	File     string   `short:"f" help:"Transcribe an existing audio file instead of recording"`
	Duration int      `short:"d" default:"5" help:"Recording duration in seconds"`
	Model    string   `short:"m" default:"qwen-audio-3.0-asr-flash" enum:"qwen-audio-3.0-asr-flash,fun-asr-flash-2026-06-15" help:"ASR model"`
	Context  []string `short:"c" help:"Text context to improve recognition, e.g. domain terms (repeatable)"`
	Lang     []string `short:"l" help:"Language hint, e.g. zh or en (repeatable; auto-detect when unset)"`
	Hotword  []string `help:"Instant hotword as word=weight, weight 1-5 or 50 (repeatable, qwen only)"`
	JSON     bool     `help:"Emit the full result with word-level timestamps as JSON"`
	NoCache  bool     `help:"Skip transcription cache"`
}

func (c *HearCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}

	vocabulary, err := parseHotwords(c.Hotword)
	if err != nil {
		return err
	}

	opt := dashscope.ASROptions{
		Model:         c.Model,
		Format:        "wav",
		SampleRate:    asrSampleRate,
		Context:       c.Context,
		Vocabulary:    vocabulary,
		LanguageHints: c.Lang,
	}

	var audioData []byte
	var cacheKey string

	if c.File != "" {
		audioData, err = os.ReadFile(c.File)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		// The API takes the container format as a parameter rather than sniffing it.
		if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(c.File)), "."); ext != "" {
			opt.Format = ext
		}
		// Sample rate is only known for audio we recorded ourselves.
		opt.SampleRate = 0
		ui.Info("%s %s", ui.Dim("file"), ui.Key(c.File))

		cacheKey = asrCacheKey(audioData, opt)
		if !c.NoCache {
			if cached, err := os.ReadFile(asrCachePath(cfg.Dir, cacheKey)); err == nil {
				ui.Info("%s", ui.Dim("cached"))
				return emitResult(cached, c.JSON)
			}
		}
	} else {
		ui.Info("Recording for %ds... %s", c.Duration, ui.Dim("(speak now)"))

		recorder, err := audio.NewRecorder(asrSampleRate, 1)
		if err != nil {
			return fmt.Errorf("init recorder: %w", err)
		}
		if err := recorder.Start(); err != nil {
			return fmt.Errorf("start recording: %w", err)
		}
		time.Sleep(time.Duration(c.Duration) * time.Second)
		pcm := recorder.Stop()

		ui.Info("%s %s", ui.Dim("recorded"), ui.Dim(fmt.Sprintf("%d bytes", len(pcm))))
		audioData = wrapPCMAsWAVWithRate(pcm, asrSampleRate)
	}

	t0 := time.Now()
	ui.Info("%s %s", ui.Dim("model"), ui.Key(c.Model))

	result, err := dashscope.NewClient(apiKey).Transcribe(audioData, opt)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}

	ui.Info("%s %s", ui.Dim("latency"), ui.Dim(time.Since(t0).Round(time.Millisecond).String()))

	// Cache the whole result, not just the text, so --json stays cacheable too.
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if cacheKey != "" && !c.NoCache {
		os.WriteFile(asrCachePath(cfg.Dir, cacheKey), encoded, 0644)
	}

	return emitResult(encoded, c.JSON)
}

func emitResult(encoded []byte, asJSON bool) error {
	var result dashscope.ASRResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return err
	}
	if asJSON {
		pretty, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(pretty))
		return nil
	}
	// Plain transcript on stdout so it stays pipeable.
	fmt.Println(result.Text)
	return nil
}

func parseHotwords(raw []string) (map[string]int, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	vocabulary := make(map[string]int, len(raw))
	for _, item := range raw {
		word, weightStr, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("hotword %q must be word=weight", item)
		}
		weight, err := strconv.Atoi(weightStr)
		if err != nil {
			return nil, fmt.Errorf("hotword %q: weight must be a number", item)
		}
		// Weight is [1,5], or the magic 50 for a "super" hotword.
		if (weight < 1 || weight > 5) && weight != 50 {
			return nil, fmt.Errorf("hotword %q: weight must be 1-5, or 50 for a super hotword", item)
		}
		vocabulary[word] = weight
	}
	return vocabulary, nil
}

// asrCacheKey covers everything that changes the transcript, so switching model
// or hints does not serve a stale result.
func asrCacheKey(audioData []byte, opt dashscope.ASROptions) string {
	h := sha256.New()
	h.Write(audioData)
	fingerprint, _ := json.Marshal(opt)
	h.Write(fingerprint)
	return hex.EncodeToString(h.Sum(nil))
}

func asrCachePath(dir, key string) string {
	return filepath.Join(dir, "cache", "asr-"+key+".json")
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
		2, 0,  // block align
		16, 0, // bits per sample
		'd', 'a', 't', 'a',
		byte(dataLen), byte(dataLen >> 8), byte(dataLen >> 16), byte(dataLen >> 24),
	}

	return append(header, pcm...)
}
