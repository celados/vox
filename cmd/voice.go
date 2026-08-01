package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/celados/vox/internal/audio"
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/ui"
)

type VoiceCmd struct {
	List   VoiceListCmd   `cmd:"" help:"List available voices"`
	Record VoiceRecordCmd `cmd:"" help:"Record and enroll a voice clone"`
	Delete VoiceDeleteCmd `cmd:"" help:"Delete a cloned voice"`
}

// --- voice list ---

type VoiceListCmd struct{}

func (c *VoiceListCmd) Run(cfg *config.AppConfig) error {
	// System voices (always available)
	ui.Info("\n%s", ui.Key("System Voices"))
	ui.Info("%s", ui.Dim("  (use with: vox say --voice <name> \"text\")"))
	for _, v := range dashscope.SystemVoices {
		ui.Info("  %-12s %s  %s", ui.Key(v.ID), ui.Dim(v.Gender), ui.Dim(v.Language))
	}

	// Cloned voices (from API)
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		ui.Info("\n%s", ui.Dim("  (login to see cloned voices)"))
		return nil
	}

	client := dashscope.NewClient(apiKey)
	voices, err := client.ListVoices(0, 50)
	if err != nil {
		ui.Warn("Failed to fetch cloned voices: %v", err)
		return nil
	}

	if len(voices) == 0 {
		ui.Info("\n%s", ui.Dim("  No cloned voices. Use: vox voice record --lang zh"))
		return nil
	}

	ui.Info("\n%s", ui.Key("Cloned Voices"))
	for _, v := range voices {
		voiceID, _ := v["voice"].(string)
		lang, _ := v["language"].(string)
		model, _ := v["target_model"].(string)
		// Extract name from voice ID: "qwen-tts-vc-<name>-voice-..."
		name := extractNameFromVoiceID(voiceID)
		ui.Info("  %-12s %s  %s  %s", ui.Key(name), ui.Dim(voiceID), ui.Dim(lang), ui.Dim(model))
	}

	return nil
}

// --- voice record ---

var sampleTexts = map[string]string{
	"zh": "今天天气真不错，适合出去走走。技术正在以前所未有的速度发展，改变着我们的生活方式。",
	"en": "The quick brown fox jumps over the lazy dog. Technology is evolving faster than ever before, reshaping how we live and work.",
	"ja": "今日はとても良い天気ですね。テクノロジーはかつてないスピードで進化しています。私たちの生活を大きく変えています。",
	"ko": "오늘 날씨가 정말 좋네요, 산책하기 딱 좋아요. 기술은 전례 없는 속도로 발전하며 우리의 생활 방식을 바꾸고 있습니다.",
	"de": "Das Wetter ist heute wirklich schön, perfekt für einen Spaziergang. Die Technologie entwickelt sich schneller als je zuvor und verändert unsere Lebensweise.",
	"fr": "Le temps est vraiment magnifique aujourd'hui, parfait pour une promenade. La technologie évolue plus vite que jamais et transforme notre façon de vivre.",
	"es": "El tiempo está muy bonito hoy, perfecto para dar un paseo. La tecnología avanza más rápido que nunca y está cambiando nuestra forma de vivir.",
	"pt": "O tempo está muito bom hoje, perfeito para um passeio. A tecnologia está avançando mais rápido do que nunca e mudando a nossa forma de viver.",
	"it": "Il tempo è davvero bello oggi, perfetto per una passeggiata. La tecnologia si evolve più velocemente che mai e sta cambiando il nostro modo di vivere.",
	"ru": "Сегодня прекрасная погода, отлично подходит для прогулки. Технологии развиваются быстрее, чем когда-либо, меняя наш образ жизни.",
	"pl": "Pogoda jest dziś naprawdę piękna, idealna na spacer. Technologia rozwija się szybciej niż kiedykolwiek, zmieniając nasz sposób życia.",
	"sv": "Vädret är riktigt fint idag, perfekt för en promenad. Tekniken utvecklas snabbare än någonsin och förändrar vårt sätt att leva.",
	"da": "Vejret er virkelig dejligt i dag, perfekt til en gåtur. Teknologien udvikler sig hurtigere end nogensinde og ændrer vores måde at leve på.",
	"fi": "Sää on tänään todella kaunis, täydellinen kävelylle. Teknologia kehittyy nopeammin kuin koskaan ja muuttaa elämäntapaamme.",
	"no": "Været er virkelig fint i dag, perfekt for en spasertur. Teknologien utvikler seg raskere enn noensinne og endrer måten vi lever på.",
	"cs": "Počasí je dnes opravdu krásné, ideální na procházku. Technologie se vyvíjejí rychleji než kdy dříve a mění náš způsob života.",
	"is": "Veðrið er virkilega fallegt í dag, fullkomið fyrir göngutúr. Tæknin þróast hraðar en nokkru sinni fyrr og breytir lífsstíl okkar.",
}

type VoiceRecordCmd struct {
	Lang     string `short:"l" help:"Language for sample text (zh, en, ja). Auto-detected from system if omitted."`
	Name     string `short:"n" help:"Name for the cloned voice"`
	Duration int    `short:"d" default:"15" help:"Recording duration in seconds (10-20s recommended)"`
	File     string `short:"f" help:"Use existing audio file instead of recording"`
}

func (c *VoiceRecordCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}

	// Resolve language
	lang := c.Lang
	if lang == "" {
		lang = detectSystemLang()
		ui.Info("%s %s", ui.Dim("language"), ui.Key(lang))
	}

	var audioData []byte

	if c.File != "" {
		// Use existing file
		audioData, err = os.ReadFile(c.File)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		ui.Info("Using audio file: %s", ui.Key(c.File))
	} else {
		// Record from microphone
		sample, ok := sampleTexts[lang]
		if !ok {
			sample = sampleTexts["en"]
		}

		ui.Info("\n%s", ui.Key("Read this aloud:"))
		ui.Info("  %s\n", sample)
		ui.Info("Recording for %ds... %s", c.Duration, ui.Dim("(speak now)"))

		recorder, err := audio.NewRecorder(audio.SampleRate, 1)
		if err != nil {
			return fmt.Errorf("init recorder: %w", err)
		}

		recorder.Start()
		time.Sleep(time.Duration(c.Duration) * time.Second)
		audioData = recorder.Stop()

		ui.Success("Recorded %d bytes", len(audioData))

		// Save locally
		wavPath := filepath.Join(cfg.Dir, "voices", fmt.Sprintf("recording-%d.wav", time.Now().Unix()))
		if err := writeWAV(wavPath, audioData); err != nil {
			ui.Warn("Failed to save local copy: %v", err)
		}
	}

	// Determine name (max 16 chars, alphanumeric + underscore only)
	name := c.Name
	if name == "" {
		name = fmt.Sprintf("vox%d", time.Now().Unix()%1e10)
	}

	// Wrap raw PCM in WAV if needed (enrollment expects audio file format)
	var wavData []byte
	if c.File != "" {
		wavData = audioData
	} else {
		wavData = wrapPCMAsWAV(audioData)
	}

	// Enroll voice
	ui.Info("Enrolling voice %s...", ui.Key(name))
	client := dashscope.NewClient(apiKey)
	voiceID, err := client.EnrollVoice(name, base64.StdEncoding.EncodeToString(wavData))
	if err != nil {
		return fmt.Errorf("enroll: %w", err)
	}

	ui.Success("Voice enrolled!")
	ui.KV("Voice ID", voiceID)
	ui.KV("Name", name)
	ui.Info("\n  Use it: %s", ui.Key(fmt.Sprintf("vox say --voice %s \"Hello!\"", voiceID)))

	// Save as last voice
	cfg.State.LastVoice = voiceID
	cfg.SaveState()

	return nil
}

func writeWAV(path string, pcm []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	wav := wrapPCMAsWAV(pcm)
	_, err = f.Write(wav)
	return err
}

func wrapPCMAsWAV(pcm []byte) []byte {
	dataLen := uint32(len(pcm))
	fileLen := dataLen + 36
	sr := uint32(audio.SampleRate)
	br := sr * 2

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

// --- voice delete ---

type VoiceDeleteCmd struct {
	VoiceID string `arg:"" help:"Voice ID to delete"`
}

func (c *VoiceDeleteCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}

	if dashscope.IsSystemVoice(c.VoiceID) {
		return fmt.Errorf("cannot delete system voice: %s", c.VoiceID)
	}

	client := dashscope.NewClient(apiKey)
	if err := client.DeleteVoice(c.VoiceID); err != nil {
		return err
	}

	ui.Success("Deleted voice: %s", c.VoiceID)

	// Clear last voice if it was this one
	if cfg.State.LastVoice == c.VoiceID {
		cfg.State.LastVoice = ""
		cfg.SaveState()
	}

	return nil
}

// extractNameFromVoiceID extracts the user-chosen name from voice ID
// e.g. "qwen-tts-vc-dio-voice-20260220..." → "dio"
func extractNameFromVoiceID(id string) string {
	// Pattern: qwen-tts-vc-<name>-voice-<timestamp>-<hash>
	const prefix = "qwen-tts-vc-"
	const marker = "-voice-"
	if !strings.HasPrefix(id, prefix) {
		return id
	}
	rest := id[len(prefix):]
	idx := strings.Index(rest, marker)
	if idx < 0 {
		return id
	}
	return rest[:idx]
}

// localeToLang maps macOS locale prefixes to language codes
var localeToLang = map[string]string{
	"zh": "zh", "en": "en", "ja": "ja", "ko": "ko",
	"de": "de", "fr": "fr", "es": "es", "pt": "pt",
	"it": "it", "ru": "ru", "pl": "pl", "sv": "sv",
	"da": "da", "fi": "fi", "nb": "no", "nn": "no",
	"cs": "cs", "is": "is",
}

// detectSystemLang returns a language code based on macOS system locale
func detectSystemLang() string {
	// macOS: defaults read -g AppleLocale → "zh_CN", "en_US", "ja_JP", etc.
	out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return "en"
	}
	locale := strings.TrimSpace(string(out))
	// Extract prefix before "_" (e.g. "zh_CN" → "zh", "nb_NO" → "nb")
	prefix := strings.SplitN(locale, "_", 2)[0]
	if lang, ok := localeToLang[prefix]; ok {
		return lang
	}
	return "en"
}

// normalizeLang converts shorthand to DashScope language names
func normalizeLang(lang string) string {
	switch strings.ToLower(lang) {
	case "zh", "chinese":
		return "Chinese"
	case "en", "english":
		return "English"
	case "ja", "japanese":
		return "Japanese"
	default:
		return lang
	}
}
