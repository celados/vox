package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/ui"
	"golang.org/x/term"
)

type AuthCmd struct {
	Login  AuthLoginCmd  `cmd:"" help:"Save API credentials"`
	Logout AuthLogoutCmd `cmd:"" help:"Clear stored credentials"`
	Status AuthStatusCmd `cmd:"" help:"Show current auth status"`
}

type AuthLoginCmd struct {
	// Without the explicit name kong would derive "dash-scope".
	DashScope AuthLoginDashScopeCmd `cmd:"" name:"dashscope" help:"Login to DashScope (TTS/ASR)"`
}

// --- dashscope ---

type AuthLoginDashScopeCmd struct {
	// Optional so the key can be entered interactively and stay out of shell history.
	Token string `help:"DashScope API key (prompted when omitted)"`
}

func (c *AuthLoginDashScopeCmd) Run(cfg *config.AppConfig) error {
	token := strings.TrimSpace(c.Token)
	if token == "" {
		var err error
		token, err = promptSecret("DashScope API Key: ")
		if err != nil {
			return err
		}
	}
	if token == "" {
		return fmt.Errorf("API key is required")
	}

	ui.Info("%s", ui.Dim("validating..."))
	if err := dashscope.NewClient(token).Validate(); err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}

	cfg.Config.Services.DashScope.APIKey = token
	if err := cfg.SaveConfig(); err != nil {
		return err
	}
	ui.Success("Authenticated with %s", ui.Key("dashscope"))
	return nil
}

// --- logout ---

type AuthLogoutCmd struct{}

func (c *AuthLogoutCmd) Run(cfg *config.AppConfig) error {
	if cfg.Config.Services.DashScope.APIKey == "" {
		ui.Warn("No credentials stored")
		return nil
	}
	if err := cfg.ClearCredentials(); err != nil {
		return err
	}
	ui.Success("Credentials cleared")
	return nil
}

// --- status ---

type AuthStatusCmd struct{}

func (c *AuthStatusCmd) Run(cfg *config.AppConfig) error {
	key := cfg.Config.Services.DashScope.APIKey
	if key == "" {
		ui.Warn("No services configured")
		ui.Info("  %s", ui.Key("vox auth login dashscope"))
		return nil
	}

	ui.Success("dashscope")
	ui.KV("  API Key", maskToken(key))
	return nil
}

// promptSecret reads a secret from the terminal without echoing it.
func promptSecret(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("no terminal available — pass --token instead")
	}
	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func maskToken(t string) string {
	if len(t) < 10 {
		return "***"
	}
	return t[:6] + "..." + t[len(t)-4:]
}
