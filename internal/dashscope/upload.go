package dashscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
)

// The async transcription endpoint takes only URLs — no local upload, no
// base64. Model Studio's free temporary storage is the bridge: a policy grants
// a form POST to an OSS host, and the resulting oss:// URL lives 48 hours.
// https://help.aliyun.com/zh/model-studio/get-temporary-file-url
const uploadsPath = "/uploads"

type uploadPolicy struct {
	UploadHost          string `json:"upload_host"`
	UploadDir           string `json:"upload_dir"`
	OSSAccessKeyID      string `json:"oss_access_key_id"`
	Signature           string `json:"signature"`
	Policy              string `json:"policy"`
	XOSSObjectACL       string `json:"x_oss_object_acl"`
	XOSSForbidOverwrite string `json:"x_oss_forbid_overwrite"`
}

// UploadTemporary stores audio in Model Studio's scratch space and returns an
// oss:// URL. The URL expires after 48 hours, which is well beyond the life of
// one transcription task and is why nothing persists it.
func (c *Client) UploadTemporary(model, filename string, data []byte) (string, error) {
	policy, err := c.uploadPolicy(model)
	if err != nil {
		return "", err
	}

	key := policy.UploadDir + "/" + filepath.Base(filename)
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	fields := [][2]string{
		{"key", key},
		{"policy", policy.Policy},
		{"OSSAccessKeyId", policy.OSSAccessKeyID},
		{"signature", policy.Signature},
		{"x-oss-object-acl", policy.XOSSObjectACL},
		{"x-oss-forbid-overwrite", policy.XOSSForbidOverwrite},
		{"success_action_status", "200"},
	}
	for _, f := range fields {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return "", err
		}
	}
	// The file part must come last; OSS ignores fields written after it.
	part, err := form.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", policy.UploadHost, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		detail, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed: HTTP %d: %s", resp.StatusCode, string(detail))
	}
	return "oss://" + key, nil
}

func (c *Client) uploadPolicy(model string) (*uploadPolicy, error) {
	req, err := http.NewRequest("GET", httpEndpoint+uploadsPath, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("action", "getPolicy")
	q.Set("model", model)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var envelope struct {
		Data uploadPolicy `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data.UploadHost == "" {
		return nil, fmt.Errorf("upload policy has no host: %s", string(raw))
	}
	return &envelope.Data, nil
}
