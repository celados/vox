package dashscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	httpEndpoint   = "https://dashscope.aliyuncs.com/api/v1"
	enrollmentPath = "/services/audio/tts/customization"
)

// Client handles HTTP API calls to DashScope
type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// Validate makes the cheapest authenticated call available so `auth login` can
// reject a bad key before storing it.
func (c *Client) Validate() error {
	_, err := c.ListVoices(0, 1)
	return err
}

// EnrollVoice creates a cloned voice from audio data
func (c *Client) EnrollVoice(name string, audioBase64 string) (string, error) {
	body := map[string]any{
		"model": ModelEnrollment,
		"input": map[string]any{
			"action":       "create",
			"target_model": ModelVCRealtime,
			"preferred_name": name,
			"audio": map[string]string{
				"data": "data:audio/wav;base64," + audioBase64,
			},
		},
	}

	resp, err := c.post(enrollmentPath, body)
	if err != nil {
		return "", err
	}

	output, ok := resp["output"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected response: %v", resp)
	}
	voiceID, ok := output["voice"].(string)
	if !ok {
		return "", fmt.Errorf("no voice in response: %v", output)
	}
	return voiceID, nil
}

// ListVoices returns all enrolled custom voices
func (c *Client) ListVoices(page, pageSize int) ([]map[string]any, error) {
	body := map[string]any{
		"model": ModelEnrollment,
		"input": map[string]any{
			"action":     "list",
			"page_size":  pageSize,
			"page_index": page,
		},
	}

	resp, err := c.post(enrollmentPath, body)
	if err != nil {
		return nil, err
	}

	output, ok := resp["output"].(map[string]any)
	if !ok {
		return nil, nil
	}

	voicesRaw, ok := output["voice_list"].([]any)
	if !ok {
		return nil, nil
	}

	var voices []map[string]any
	for _, v := range voicesRaw {
		if m, ok := v.(map[string]any); ok {
			voices = append(voices, m)
		}
	}
	return voices, nil
}

// DeleteVoice removes an enrolled voice
func (c *Client) DeleteVoice(voiceID string) error {
	body := map[string]any{
		"model": ModelEnrollment,
		"input": map[string]any{
			"action": "delete",
			"voice":  voiceID,
		},
	}

	_, err := c.post(enrollmentPath, body)
	return err
}

type header struct{ key, value string }

func (c *Client) post(path string, body any, headers ...header) (map[string]any, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", httpEndpoint+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	for _, h := range headers {
		req.Header.Set(h.key, h.value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return result, nil
}
