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

// Family pairs the two endpoints a caller never has to choose between. `hear`
// selects the member by duration, so `--model` names a family, not an endpoint.
type Family struct {
	Name string
	// Short is the synchronous model: 5 minutes, no native segmentation.
	Short string
	// Long is the async filetrans model: 12 hours, native sentences.
	Long string
}

var (
	FamilyFunASR = Family{Name: "fun-asr", Short: ModelFunASRFlash, Long: ModelFunASR}
	FamilyQwen   = Family{Name: "qwen-asr", Short: ModelQwenAudioASRFlash, Long: ModelQwenAudioFile}
)

// Families lists the selectable families; the first is the default.
var Families = []Family{FamilyFunASR, FamilyQwen}

func FamilyByName(name string) (Family, bool) {
	for _, f := range Families {
		if f.Name == name {
			return f, true
		}
	}
	return Family{}, false
}

func FamilyNames() []string {
	names := make([]string, 0, len(Families))
	for _, f := range Families {
		names = append(names, f.Name)
	}
	return names
}

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
	// Confidence is only reported by the async transport.
	Confidence float64 `json:"confidence,omitempty"`
}

// Sentence is the unit both transports normalize to. The async transport
// segments natively; the synchronous one returns a single sentence spanning the
// whole take, so downstream code never branches on transport.
type Sentence struct {
	BeginTime int    `json:"begin_time"`
	EndTime   int    `json:"end_time"`
	Text      string `json:"text"`
	// Speaker is set only when diarization was requested.
	Speaker string    `json:"speaker,omitempty"`
	Words   []ASRWord `json:"words,omitempty"`
}

type ASRResult struct {
	Text      string     `json:"text"`
	Sentences []Sentence `json:"sentences,omitempty"`
	// DurationSec is the billed audio duration reported by the service.
	DurationSec int `json:"duration_sec,omitempty"`
}

// Words flattens the sentences, for consumers that want a single stream.
func (r *ASRResult) Words() []ASRWord {
	var words []ASRWord
	for _, s := range r.Sentences {
		words = append(words, s.Words...)
	}
	return words
}

// Segmented reports whether the service supplied real sentence boundaries. The
// synchronous transport yields exactly one sentence covering everything, which
// is not segmentation and must not be treated as subtitle cues.
func (r *ASRResult) Segmented() bool { return len(r.Sentences) > 1 }

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
	if raw, ok := output["sentence"].(map[string]any); ok {
		// One sentence spanning the whole take — this endpoint does not segment.
		sentence := Sentence{
			BeginTime: toInt(raw["begin_time"]),
			EndTime:   toInt(raw["end_time"]),
		}
		sentence.Text, _ = raw["text"].(string)
		if text == "" {
			result.Text = sentence.Text
		}
		if words, ok := raw["words"].([]any); ok {
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
				sentence.Words = append(sentence.Words, word)
			}
		}
		if sentence.Text != "" || len(sentence.Words) > 0 {
			result.Sentences = []Sentence{sentence}
		}
	}
	if usage, ok := resp["usage"].(map[string]any); ok {
		result.DurationSec = toInt(usage["duration"])
	}

	if result.Text == "" && len(result.Sentences) == 0 {
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
