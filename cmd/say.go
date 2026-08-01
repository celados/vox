package cmd

import (
	"context"
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

type SayCmd struct {
	Text     string  `arg:"" help:"Text to speak"`
	Voice    string  `short:"v" help:"Voice ID (system name or cloned voice ID)"`
	Lang     string  `short:"l" default:"auto" help:"Language hint (auto, Chinese, English, Japanese, ...)"`
	Instruct string  `short:"i" help:"Voice style instruction (e.g. 'warm and expressive, moderate pace')"`
	Speed    float64 `short:"s" default:"1.0" help:"Speech rate (0.5-2.0)"`
	Output   string  `short:"o" help:"Save audio to file instead of playing"`
	NoCache  bool    `help:"Skip audio cache"`
}

func (c *SayCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}

	// Resolve voice
	voice := c.Voice
	if voice == "" {
		voice = cfg.State.LastVoice
	}
	if voice == "" {
		voice = "Cherry" // default system voice
	}

	// Pick model based on voice type and instruct mode
	model := dashscope.ModelForVoice(voice)
	if c.Instruct != "" && dashscope.IsSystemVoice(voice) {
		model = dashscope.ModelInstructRealtime
	}

	// Check cache
	cacheKey := fmt.Sprintf("%s:%s:%s:%s:%s:%.1f", model, voice, c.Lang, c.Instruct, c.Text, c.Speed)
	cacheHash := sha256.Sum256([]byte(cacheKey))
	hashStr := hex.EncodeToString(cacheHash[:])
	cachePath := filepath.Join(cfg.Dir, "cache", hashStr+".opus")

	if !c.NoCache {
		if opusData, err := os.ReadFile(cachePath); err == nil {
			ui.Info("%s %s", ui.Dim("cached"), ui.Dim(voice))
			pcmData, err := audio.DecodeOpusToPCM(opusData)
			if err != nil {
				// Fallback: try legacy .pcm cache
				legacyPath := filepath.Join(cfg.Dir, "cache", hashStr+".pcm")
				if pcmData, err = os.ReadFile(legacyPath); err != nil {
					return fmt.Errorf("decode cache: %w", err)
				}
			}
			return playPCM(pcmData, c.Output)
		}
		// Fallback: try legacy .pcm cache
		legacyPath := filepath.Join(cfg.Dir, "cache", hashStr+".pcm")
		if data, err := os.ReadFile(legacyPath); err == nil {
			ui.Info("%s %s", ui.Dim("cached"), ui.Dim(voice))
			return playPCM(data, c.Output)
		}
	}

	// Stream from API
	ui.Info("%s %s %s", ui.Dim("voice"), ui.Key(voice), ui.Dim("("+model+")"))

	client := dashscope.NewRealtimeClient(apiKey)
	player := audio.NewStreamPlayer()
	collector := &audio.PCMCollector{}

	t0 := time.Now()
	var firstChunk bool

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	opts := dashscope.TTSOptions{
		Model:      model,
		Voice:      voice,
		Text:       c.Text,
		Lang:       c.Lang,
		Instruct:   c.Instruct,
		SpeechRate: c.Speed,
	}
	err = client.StreamTTS(ctx, opts, func(pcm []byte) {
		if !firstChunk {
			firstChunk = true
			ui.Info("%s %s", ui.Dim("first audio"), ui.Dim(time.Since(t0).Round(time.Millisecond).String()))
		}
		player.Write(pcm)
		collector.Write(pcm)
	})

	player.Close()

	if err != nil {
		return fmt.Errorf("TTS stream: %w", err)
	}

	// Cache the result as opus
	if !c.NoCache && len(collector.Bytes()) > 0 {
		if opusData, err := audio.EncodePCMToOpus(collector.Bytes()); err == nil {
			os.WriteFile(cachePath, opusData, 0644)
		} else {
			// Fallback to raw PCM if ffmpeg unavailable
			os.WriteFile(filepath.Join(cfg.Dir, "cache", hashStr+".pcm"), collector.Bytes(), 0644)
		}
	}

	// Save output file if requested
	if c.Output != "" {
		if err := writePCMAsWAV(c.Output, collector.Bytes()); err != nil {
			return fmt.Errorf("save: %w", err)
		}
		ui.Success("Saved to %s", c.Output)
	}

	// Update state
	cfg.State.LastVoice = voice
	if c.Lang != "auto" {
		cfg.State.LastLang = c.Lang
	}
	cfg.SaveState()

	return nil
}

func playPCM(data []byte, outputPath string) error {
	player := audio.NewStreamPlayer()
	player.Write(data)
	player.Close()

	if outputPath != "" {
		return writePCMAsWAV(outputPath, data)
	}
	return nil
}

func writePCMAsWAV(path string, pcm []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write WAV header
	dataLen := uint32(len(pcm))
	fileLen := dataLen + 36
	sampleRate := uint32(audio.SampleRate)
	byteRate := sampleRate * 2 // 16-bit mono = 2 bytes per sample
	header := []byte{
		'R', 'I', 'F', 'F',
		byte(fileLen), byte(fileLen >> 8), byte(fileLen >> 16), byte(fileLen >> 24),
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		16, 0, 0, 0, // chunk size
		1, 0, // PCM format
		1, 0, // mono
		byte(sampleRate), byte(sampleRate >> 8), byte(sampleRate >> 16), byte(sampleRate >> 24),
		byte(byteRate), byte(byteRate >> 8), byte(byteRate >> 16), byte(byteRate >> 24),
		2, 0, // block align
		16, 0, // bits per sample
		'd', 'a', 't', 'a',
		byte(dataLen), byte(dataLen >> 8), byte(dataLen >> 16), byte(dataLen >> 24),
	}

	if _, err := f.Write(header); err != nil {
		return err
	}
	_, err = f.Write(pcm)
	return err
}
