package dashscope

import (
	"fmt"
	"time"
)

// Hotword list management.
// https://help.aliyun.com/zh/model-studio/vocabulary-http-api
const (
	customizationPath = "/services/audio/asr/customization"
	biasingModel      = "speech-biasing"

	// VocabularyQuota is the per-account cap, shared across every target model.
	VocabularyQuota = 10
)

type Hotword struct {
	Text   string `json:"text"`
	Weight int    `json:"weight"`
	Lang   string `json:"lang,omitempty"`
}

type VocabularyInfo struct {
	ID          string `json:"vocabulary_id"`
	Status      string `json:"status"` // OK | UNDEPLOYED
	TargetModel string `json:"target_model,omitempty"`
	Created     string `json:"gmt_create,omitempty"`
	Modified    string `json:"gmt_modified,omitempty"`
}

func (c *Client) CreateVocabulary(targetModel, prefix string, words []Hotword) (string, error) {
	resp, err := c.post(customizationPath, map[string]any{
		"model": biasingModel,
		"input": map[string]any{
			"action":       "create_vocabulary",
			"target_model": targetModel,
			"prefix":       prefix,
			"vocabulary":   words,
		},
	})
	if err != nil {
		return "", err
	}
	output, _ := resp["output"].(map[string]any)
	id, _ := output["vocabulary_id"].(string)
	if id == "" {
		return "", fmt.Errorf("no vocabulary_id in response")
	}
	return id, nil
}

// UpdateVocabulary replaces the list wholesale, keeping the same id. Preferred
// over delete+create so the id stays stable and the account quota does not churn.
func (c *Client) UpdateVocabulary(vocabularyID string, words []Hotword) error {
	_, err := c.post(customizationPath, map[string]any{
		"model": biasingModel,
		"input": map[string]any{
			"action":        "update_vocabulary",
			"vocabulary_id": vocabularyID,
			"vocabulary":    words,
		},
	})
	return err
}

func (c *Client) QueryVocabulary(vocabularyID string) (*VocabularyInfo, []Hotword, error) {
	resp, err := c.post(customizationPath, map[string]any{
		"model": biasingModel,
		"input": map[string]any{
			"action":        "query_vocabulary",
			"vocabulary_id": vocabularyID,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	output, ok := resp["output"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected response: missing output")
	}

	info := &VocabularyInfo{ID: vocabularyID}
	info.Status, _ = output["status"].(string)
	info.TargetModel, _ = output["target_model"].(string)
	info.Created, _ = output["gmt_create"].(string)
	info.Modified, _ = output["gmt_modified"].(string)

	var words []Hotword
	if raw, ok := output["vocabulary"].([]any); ok {
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			w := Hotword{Weight: toInt(m["weight"])}
			w.Text, _ = m["text"].(string)
			w.Lang, _ = m["lang"].(string)
			words = append(words, w)
		}
	}
	return info, words, nil
}

func (c *Client) ListVocabularies() ([]VocabularyInfo, error) {
	resp, err := c.post(customizationPath, map[string]any{
		"model": biasingModel,
		"input": map[string]any{
			"action":     "list_vocabulary",
			"page_index": 0,
			"page_size":  VocabularyQuota,
		},
	})
	if err != nil {
		return nil, err
	}
	output, _ := resp["output"].(map[string]any)
	raw, _ := output["vocabulary_list"].([]any)

	var list []VocabularyInfo
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		info := VocabularyInfo{}
		info.ID, _ = m["vocabulary_id"].(string)
		info.Status, _ = m["status"].(string)
		info.Created, _ = m["gmt_create"].(string)
		info.Modified, _ = m["gmt_modified"].(string)
		list = append(list, info)
	}
	return list, nil
}

func (c *Client) DeleteVocabulary(vocabularyID string) error {
	_, err := c.post(customizationPath, map[string]any{
		"model": biasingModel,
		"input": map[string]any{
			"action":        "delete_vocabulary",
			"vocabulary_id": vocabularyID,
		},
	})
	return err
}

// AwaitVocabulary blocks until the list is deployable. Creation has been observed
// to return OK immediately, but UNDEPLOYED is documented and a list used in that
// state simply has no effect — a silent failure worth spending a poll on.
func (c *Client) AwaitVocabulary(vocabularyID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, _, err := c.QueryVocabulary(vocabularyID)
		if err != nil {
			return err
		}
		if info.Status == "OK" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("vocabulary %s still %s after %s", vocabularyID, info.Status, timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
