package cmd

import (
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/ui"
)

type AuthCmd struct {
	Login  AuthLoginCmd  `cmd:"" help:"Save API credentials"`
	Status AuthStatusCmd `cmd:"" help:"Show current auth status"`
}

type AuthLoginCmd struct {
	DashScope AuthLoginDashScopeCmd `cmd:"" help:"Login to DashScope (TTS/ASR)"`
	Slack     AuthLoginSlackCmd     `cmd:"" help:"Login to Slack (listen mode)"`
}

// --- dashscope ---

type AuthLoginDashScopeCmd struct {
	Token string `required:"" help:"DashScope API key"`
}

func (c *AuthLoginDashScopeCmd) Run(cfg *config.AppConfig) error {
	cfg.Config.Services.DashScope.APIKey = c.Token
	if err := cfg.SaveConfig(); err != nil {
		return err
	}
	ui.Success("Authenticated with %s", ui.Key("dashscope"))
	return nil
}

// --- slack ---

type AuthLoginSlackCmd struct {
	BotToken string `required:"" help:"Slack Bot Token (xoxb-...)"`
	AppToken string `required:"" help:"Slack App-Level Token (xapp-...)"`
}

func (c *AuthLoginSlackCmd) Run(cfg *config.AppConfig) error {
	cfg.Config.Services.Slack.BotToken = c.BotToken
	cfg.Config.Services.Slack.AppToken = c.AppToken
	if err := cfg.SaveConfig(); err != nil {
		return err
	}
	ui.Success("Authenticated with %s", ui.Key("slack"))
	return nil
}

// --- status ---

type AuthStatusCmd struct{}

func (c *AuthStatusCmd) Run(cfg *config.AppConfig) error {
	any := false

	if key := cfg.Config.Services.DashScope.APIKey; key != "" {
		any = true
		ui.Success("dashscope")
		ui.KV("  API Key", maskToken(key))
	}

	if s := cfg.Config.Services.Slack; s.BotToken != "" {
		any = true
		ui.Success("slack")
		ui.KV("  Bot Token", maskToken(s.BotToken))
		ui.KV("  App Token", maskToken(s.AppToken))
	}

	if !any {
		ui.Warn("No services configured")
		ui.Info("  %s", ui.Key("vox auth login dashscope --token <key>"))
		ui.Info("  %s", ui.Key("vox auth login slack --bot-token <token> --app-token <token>"))
	}

	return nil
}

func maskToken(t string) string {
	if len(t) < 10 {
		return "***"
	}
	return t[:6] + "..." + t[len(t)-4:]
}
