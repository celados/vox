package dashscope

// Recognition result types, shared by every consumer of a transcription.

type ASRWord struct {
	Text        string  `json:"text"`
	BeginTime   int     `json:"begin_time"`
	EndTime     int     `json:"end_time"`
	Punctuation string  `json:"punctuation"`
	Confidence  float64 `json:"confidence,omitempty"`
}

// Sentence is the service's own segmentation unit: a timestamped span with
// word-level detail and, when diarization is on, a speaker label.
type Sentence struct {
	BeginTime int       `json:"begin_time"`
	EndTime   int       `json:"end_time"`
	Text      string    `json:"text"`
	Speaker   string    `json:"speaker,omitempty"`
	Words     []ASRWord `json:"words,omitempty"`
}

type ASRResult struct {
	Text      string     `json:"text"`
	Sentences []Sentence `json:"sentences,omitempty"`
	// DurationSec is the audio duration reported by the service.
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

// SuperHotwordsSupported reports whether the model honours weight=50. Only the
// Qwen-Audio-3.0 family does, so a shared vocabulary is clamped rather than
// rejected on Fun-ASR.
func SuperHotwordsSupported(model string) bool {
	return model == ModelQwenAudioFile
}
