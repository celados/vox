package cmd

import (
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/export"
	"github.com/celados/vox/internal/run"
	"github.com/celados/vox/internal/ui"
	"github.com/celados/vox/internal/voxerr"
)

type ExportCmd struct {
	SID    string `arg:"" help:"Run id or unique prefix"`
	Format string `short:"f" required:"" enum:"srt,vtt,md,txt,json" help:"Output format"`
	Output string `short:"o" help:"Write to a file instead of stdout"`
}

func (c *ExportCmd) Run(cfg *config.AppConfig) error {
	store := run.NewStore(cfg.Dir)

	sid, err := store.Resolve(c.SID)
	if err != nil {
		return err
	}
	rec, err := store.Load(sid)
	if err != nil {
		return voxerr.New(voxerr.SessionNotFound, "run %s has no stored result: %v", sid, err)
	}

	rendered, err := export.Render(rec, c.Format)
	if err != nil {
		return err
	}
	if err := writeOut(c.Output, rendered); err != nil {
		return err
	}
	if c.Output != "" {
		ui.Success("Wrote %s", ui.Key(c.Output))
	}
	return nil
}
