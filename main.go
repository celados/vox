package main

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/celados/vox/cmd"
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/ui"
)

var cli struct {
	Auth  cmd.AuthCmd  `cmd:"" help:"Manage authentication"`
	Say   cmd.SayCmd   `cmd:"" help:"Speak text with TTS"`
	Hear  cmd.HearCmd  `cmd:"" help:"Transcribe speech to text"`
	Voice cmd.VoiceCmd `cmd:"" help:"Manage voice profiles"`
	Cache cmd.CacheCmd `cmd:"" help:"Manage audio cache"`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("vox"),
		kong.Description("Voice clone TTS — powered by Qwen3-TTS"),
		kong.UsageOnError(),
	)

	cfg, err := config.Load()
	if err != nil {
		ui.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	err = ctx.Run(cfg)
	ctx.FatalIfErrorf(err)
}
