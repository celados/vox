package dashscope

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// File transcription: vox's only recognition path. Audio is uploaded, a task is
// submitted, and the result is polled. It caps at 12 hours, segments into
// sentences natively, and reports per-word confidence.
// https://help.aliyun.com/zh/model-studio/recording-file-recognition
const (
	transcriptionPath = "/services/audio/asr/transcription"
	tasksPath         = "/tasks/"

	ModelFunASR        = "fun-asr"
	ModelQwenAudioFile = "qwen-audio-3.0-asr-flash-filetrans"

	// MaxFileSeconds is the documented cap. It is the only length limit vox has.
	MaxFileSeconds = 12 * 60 * 60
	// MaxFileBytes is the documented per-file cap.
	MaxFileBytes = 2 * 1024 * 1024 * 1024
	// DiarizationMaxSeconds is where the docs stop recommending diarization;
	// beyond it recognition may time out rather than degrade.
	DiarizationMaxSeconds = 2 * 60 * 60

	pollInterval = 5 * time.Second
)

type FileOptions struct {
	Model         string
	VocabularyID  string
	LanguageHints []string
	Diarization   bool
}

// TaskProgress reports polling state so a caller can show something during a
// minutes-long job.
type TaskProgress func(status string, elapsed time.Duration)

// TranscribeFile uploads audio, submits an async task, waits for it, and
// returns the normalized result.
func (c *Client) TranscribeFile(filename string, data []byte, opt FileOptions, progress TaskProgress) (*ASRResult, error) {
	if opt.Model == "" {
		opt.Model = ModelFunASR
	}

	fileURL, err := c.UploadTemporary(opt.Model, filename, data)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}

	params := map[string]any{}
	if opt.VocabularyID != "" {
		params["vocabulary_id"] = opt.VocabularyID
	}
	if len(opt.LanguageHints) > 0 {
		params["language_hints"] = opt.LanguageHints
	}
	if opt.Diarization {
		params["diarization_enabled"] = true
	}

	body := map[string]any{
		"model":      opt.Model,
		"input":      map[string]any{"file_urls": []string{fileURL}},
		"parameters": params,
	}

	resp, err := c.post(transcriptionPath, body,
		header{"X-DashScope-Async", "enable"},
		// Required for oss:// URLs from the temporary store to resolve.
		header{"X-DashScope-OssResourceResolve", "enable"})
	if err != nil {
		return nil, err
	}
	output, _ := resp["output"].(map[string]any)
	taskID, _ := output["task_id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("no task_id in response")
	}

	transcriptURL, err := c.awaitTask(taskID, progress)
	if err != nil {
		return nil, err
	}
	return c.fetchTranscript(transcriptURL)
}

func (c *Client) awaitTask(taskID string, progress TaskProgress) (string, error) {
	start := time.Now()
	for {
		req, err := http.NewRequest("GET", httpEndpoint+tasksPath+taskID, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}

		var task struct {
			Output struct {
				TaskStatus string `json:"task_status"`
				Message    string `json:"message"`
				Results    []struct {
					TranscriptionURL string `json:"transcription_url"`
					SubtaskStatus    string `json:"subtask_status"`
					Message          string `json:"message"`
				} `json:"results"`
			} `json:"output"`
		}
		if err := json.Unmarshal(raw, &task); err != nil {
			return "", err
		}

		status := task.Output.TaskStatus
		if progress != nil {
			progress(status, time.Since(start))
		}

		switch status {
		case "SUCCEEDED":
			if len(task.Output.Results) == 0 {
				return "", fmt.Errorf("task succeeded with no results")
			}
			result := task.Output.Results[0]
			if result.TranscriptionURL == "" {
				return "", fmt.Errorf("task succeeded without a transcript: %s", result.Message)
			}
			return result.TranscriptionURL, nil
		case "FAILED", "CANCELED":
			detail := task.Output.Message
			if detail == "" && len(task.Output.Results) > 0 {
				detail = task.Output.Results[0].Message
			}
			return "", fmt.Errorf("task %s: %s", status, detail)
		}

		time.Sleep(pollInterval)
	}
}

// fetchTranscript downloads the result document. Its URL is signed and expires
// in 24 hours, which is why the content is stored rather than the link.
func (c *Client) fetchTranscript(url string) (*ASRResult, error) {
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("transcript download: HTTP %d", resp.StatusCode)
	}

	var doc struct {
		Properties struct {
			DurationMillis int `json:"original_duration_in_milliseconds"`
		} `json:"properties"`
		Transcripts []struct {
			Text      string `json:"text"`
			Sentences []struct {
				BeginTime int    `json:"begin_time"`
				EndTime   int    `json:"end_time"`
				Text      string `json:"text"`
				SpeakerID any    `json:"speaker_id"`
				Words     []struct {
					BeginTime   int     `json:"begin_time"`
					EndTime     int     `json:"end_time"`
					Text        string  `json:"text"`
					Punctuation string  `json:"punctuation"`
					Confidence  float64 `json:"confidence"`
				} `json:"words"`
			} `json:"sentences"`
		} `json:"transcripts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Transcripts) == 0 {
		return nil, fmt.Errorf("transcript document has no channels")
	}

	// Channel 0 only: vox downmixes to mono and never requests multi-channel.
	channel := doc.Transcripts[0]
	result := &ASRResult{
		Text:        channel.Text,
		DurationSec: doc.Properties.DurationMillis / 1000,
	}
	for _, s := range channel.Sentences {
		sentence := Sentence{
			BeginTime: s.BeginTime,
			EndTime:   s.EndTime,
			Text:      s.Text,
			Speaker:   speakerLabel(s.SpeakerID),
		}
		for _, w := range s.Words {
			sentence.Words = append(sentence.Words, ASRWord{
				Text:        w.Text,
				BeginTime:   w.BeginTime,
				EndTime:     w.EndTime,
				Punctuation: w.Punctuation,
				Confidence:  w.Confidence,
			})
		}
		result.Sentences = append(result.Sentences, sentence)
	}
	return result, nil
}

// speakerLabel normalizes speaker_id, which is null when diarization is off and
// may arrive as a number or a string when it is on.
func speakerLabel(v any) string {
	switch id := v.(type) {
	case string:
		return id
	case float64:
		return fmt.Sprintf("%d", int(id))
	default:
		return ""
	}
}
