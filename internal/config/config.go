package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/celados/vox/internal/voxerr"
)

const appDir = ".vox"

type DashScopeConfig struct {
	APIKey string `json:"api_key,omitempty"`
}

type Services struct {
	DashScope DashScopeConfig `json:"dashscope,omitempty"`
}

type Config struct {
	Services Services `json:"services"`
}

type State struct {
	LastVoice string `json:"last_voice,omitempty"`
	LastLang  string `json:"last_lang,omitempty"`
}

type AppConfig struct {
	Config Config
	State  State
	Dir    string
}

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, appDir)
}

func Load() (*AppConfig, error) {
	dir := Dir()
	os.MkdirAll(dir, 0755)
	os.MkdirAll(filepath.Join(dir, "voices"), 0755)
	os.MkdirAll(filepath.Join(dir, "cache"), 0755)

	ac := &AppConfig{Dir: dir}

	if data, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		json.Unmarshal(data, &ac.Config)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "state.json")); err == nil {
		json.Unmarshal(data, &ac.State)
	}

	return ac, nil
}

func (ac *AppConfig) SaveConfig() error {
	return writeJSON(filepath.Join(ac.Dir, "config.json"), ac.Config)
}

func (ac *AppConfig) SaveState() error {
	return writeJSON(filepath.Join(ac.Dir, "state.json"), ac.State)
}

func (ac *AppConfig) RequireAPIKey() (string, error) {
	key := ac.Config.Services.DashScope.APIKey
	if key == "" {
		return "", voxerr.New(voxerr.NotAuthenticated, "no DashScope credentials stored").
			WithHint("vox auth login dashscope")
	}
	return key, nil
}

// ClearCredentials drops every stored secret and persists the empty config.
func (ac *AppConfig) ClearCredentials() error {
	ac.Config.Services = Services{}
	return ac.SaveConfig()
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
