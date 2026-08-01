package dashscope

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Non-realtime ASR models sharing the multimodal-generation endpoint and schema.
// https://help.aliyun.com/zh/model-studio/non-real-time-speech-recognition-for-fun-asr-flash
const (
	ModelFunASRFlash       = "fun-asr-flash-2026-06-15"
	ModelQwenAudioASRFlash = "qwen-audio-3.0-asr-flash"

	multimodalGenPath = "/services/aigc/multimodal-generation/generation"

	// MaxAudioBytes is the endpoint's per-request cap. base64 inflates by ~4/3,
	// so callers check raw bytes and fail before paying for the encode + upload.
	MaxAudioBytes = 10 * 1024 * 1024
	// MaxAudioSeconds is the documented duration cap for this non-realtime path.
	MaxAudioSeconds = 300
)

// ASRModels lists the selectable model IDs; the first is the default.
var ASRModels = []string{ModelFunASRFlash, ModelQwenAudioASRFlash}

// SuperHotwordsSupported reports whether the model honours weight=50. Fun-ASR
// does not, so a shared vocabulary is clamped rather than rejected.
func SuperHotwordsSupported(model string) bool {
	return model == ModelQwenAudioASRFlash
}

type ASROptions struct {
	Model  string
	Format string // wav, mp3, opus, ... — required by the API, not sniffed from the bytes
	// SampleRate is optional; 0 omits it.
	SampleRate int
	// Context carries prior turns or domain word lists (input_text). The API keeps
	// the most recent 5 entries and truncates past 400 chars per round.
	Context []string
	// VocabularyID references a precompiled hotword list. It must have been
	// created with target_model equal to Model — a mismatch is not an error
	// server-side, the hotwords just silently do nothing.
	VocabularyID string
	// LanguageHints are BCP-47-ish codes ("zh", "en"). Qwen honours up to 4,
	// Fun-ASR only the first. Empty means auto-detect.
	LanguageHints []string
}

type ASRWord struct {
	Text        string `json:"text"`
	BeginTime   int    `json:"begin_time"`
	EndTime     int    `json:"end_time"`
	Punctuation string `json:"punctuation"`
}

type ASRResult struct {
	Text  string    `json:"text"`
	Words []ASRWord `json:"words,omitempty"`
	// DurationSec is the billed audio duration reported by the service.
	DurationSec int `json:"duration_sec,omitempty"`
}

// Transcribe runs non-streaming recognition over a complete audio file.
func (c *Client) Transcribe(audioData []byte, opt ASROptions) (*ASRResult, error) {
	if opt.Model == "" {
		opt.Model = ModelFunASRFlash
	}
	if opt.Format == "" {
		return nil, fmt.Errorf("audio format is required")
	}

	audioURI := "data:" + mimeForFormat(opt.Format) + ";base64," + base64.StdEncoding.EncodeToString(audioData)

	// Context messages must precede the audio; the audio message must come last.
	messages := make([]map[string]any, 0, len(opt.Context)+1)
	for _, text := range opt.Context {
		if text == "" {
			continue
		}
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "input_text", "text": text}},
		})
	}
	messages = append(messages, map[string]any{
		"role": "user",
		"content": []map[string]any{
			{"type": "input_audio", "input_audio": map[string]any{"data": audioURI}},
		},
	})

	params := map[string]any{"format": opt.Format}
	if opt.SampleRate > 0 {
		params["sample_rate"] = fmt.Sprint(opt.SampleRate)
	}
	if opt.VocabularyID != "" {
		params["vocabulary_id"] = opt.VocabularyID
	}
	if len(opt.LanguageHints) > 0 {
		params["language_hints"] = opt.LanguageHints
	}

	body := map[string]any{
		"model":      opt.Model,
		"input":      map[string]any{"messages": messages},
		"parameters": params,
	}

	resp, err := c.post(multimodalGenPath, body, header{"X-DashScope-SSE", "disable"})
	if err != nil {
		return nil, err
	}

	output, ok := resp["output"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing output")
	}
	text, _ := output["text"].(string)

	result := &ASRResult{Text: text}
	if sentence, ok := output["sentence"].(map[string]any); ok {
		if text == "" {
			result.Text, _ = sentence["text"].(string)
		}
		if words, ok := sentence["words"].([]any); ok {
			for _, w := range words {
				m, ok := w.(map[string]any)
				if !ok {
					continue
				}
				word := ASRWord{}
				word.Text, _ = m["text"].(string)
				word.Punctuation, _ = m["punctuation"].(string)
				word.BeginTime = toInt(m["begin_time"])
				word.EndTime = toInt(m["end_time"])
				result.Words = append(result.Words, word)
			}
		}
	}
	if usage, ok := resp["usage"].(map[string]any); ok {
		result.DurationSec = toInt(usage["duration"])
	}

	if result.Text == "" && len(result.Words) == 0 {
		return nil, fmt.Errorf("unexpected response: no transcript in output")
	}
	return result, nil
}

func mimeForFormat(format string) string {
	switch strings.ToLower(format) {
	case "mp3":
		return "audio/mpeg"
	case "opus", "ogg":
		return "audio/ogg"
	case "m4a", "mp4", "aac":
		return "audio/mp4"
	default:
		return "audio/" + strings.ToLower(format)
	}
}

func toInt(v any) int {
	f, ok := v.(float64) // encoding/json decodes every number into float64
	if !ok {
		return 0
	}
	return int(f)
}
