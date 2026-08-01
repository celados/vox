package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/celados/vox/cmd"
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/ui"
	"github.com/celados/vox/internal/voxerr"
	"gopkg.in/yaml.v3"
)

var cli struct {
	Auth    cmd.AuthCmd    `cmd:"" help:"Manage authentication"`
	Hear    cmd.HearCmd    `cmd:"" help:"Transcribe audio into a run"`
	Session cmd.SessionCmd `cmd:"" help:"Inspect and delete stored runs"`
	Export  cmd.ExportCmd  `cmd:"" help:"Render a stored run"`
	Vocab   cmd.VocabCmd   `cmd:"" help:"Manage hotword vocabularies"`
	Say     cmd.SayCmd     `cmd:"" help:"Speak text with TTS"`
	Voice   cmd.VoiceCmd   `cmd:"" help:"Manage voice profiles"`
	Cache   cmd.CacheCmd   `cmd:"" help:"Manage TTS audio cache"`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("vox"),
		kong.Description("Agent-facing speech to text and text to speech"),
		kong.UsageOnError(),
	)

	cfg, err := config.Load()
	if err != nil {
		ui.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	if err := ctx.Run(cfg); err != nil {
		os.Exit(report(err))
	}
}

// report renders a failure as one YAML document on stderr so an agent can branch
// on the code instead of parsing prose.
func report(err error) int {
	var e *voxerr.Error
	if !errors.As(err, &e) {
		e = voxerr.New(voxerr.APIError, "%s", err.Error())
	}
	data, marshalErr := yaml.Marshal(e)
	if marshalErr != nil {
		ui.Error("%v", err)
		return voxerr.ExitUsage
	}
	fmt.Fprint(os.Stderr, string(data))
	return e.ExitCode()
}
