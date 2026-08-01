package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/celados/vox/internal/audio"
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/ui"
)

const asrSampleRate = 16000

type HearCmd struct {
	File     string `short:"f" help:"Transcribe an existing audio file instead of recording"`
	Duration int    `short:"d" default:"5" help:"Recording duration in seconds"`
	Context  string `short:"c" help:"Text context to improve recognition (e.g. domain terms)"`
	NoCache  bool   `help:"Skip transcription cache"`
}

func (c *HearCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}

	var wavData []byte
	var cacheKey string

	if c.File != "" {
		wavData, err = os.ReadFile(c.File)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		ui.Info("%s %s", ui.Dim("file"), ui.Key(c.File))

		// Cache key = hash of file content + context
		h := sha256.New()
		h.Write(wavData)
		h.Write([]byte(":" + c.Context))
		cacheKey = hex.EncodeToString(h.Sum(nil))

		// Check cache
		if !c.NoCache {
			cachePath := filepath.Join(cfg.Dir, "cache", "asr-"+cacheKey+".txt")
			if cached, err := os.ReadFile(cachePath); err == nil {
				ui.Info("%s", ui.Dim("cached"))
				fmt.Println(string(cached))
				return nil
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

		wavData = wrapPCMAsWAVWithRate(pcm, asrSampleRate)
	}

	// Transcribe
	t0 := time.Now()
	ui.Info("%s %s", ui.Dim("model"), ui.Key(dashscope.ModelASRFlash))

	client := dashscope.NewClient(apiKey)
	result, err := client.Transcribe(wavData, c.Context)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}

	elapsed := time.Since(t0).Round(time.Millisecond)
	ui.Info("%s %s", ui.Dim("latency"), ui.Dim(elapsed.String()))

	// Cache the result for file-based transcription
	if cacheKey != "" && !c.NoCache && result.Text != "" {
		cachePath := filepath.Join(cfg.Dir, "cache", "asr-"+cacheKey+".txt")
		os.WriteFile(cachePath, []byte(result.Text), 0644)
	}

	// Output transcription to stdout (so it can be piped)
	fmt.Println(result.Text)

	return nil
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
