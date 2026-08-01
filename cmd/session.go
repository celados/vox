package cmd

import (
	"fmt"
	"os"

	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/run"
	"github.com/celados/vox/internal/ui"
	"github.com/celados/vox/internal/voxerr"
	"gopkg.in/yaml.v3"
)

type SessionCmd struct {
	Ls SessionLsCmd `cmd:"" help:"List stored runs, newest first"`
	Rm SessionRmCmd `cmd:"" help:"Delete stored runs"`
}

// --- ls ---

type SessionLsCmd struct {
	File  string `short:"f" help:"Only runs whose source is this file"`
	Limit int    `short:"n" default:"20" help:"Maximum runs to list (0 for all)"`
}

func (c *SessionLsCmd) Run(cfg *config.AppConfig) error {
	metas, err := run.NewStore(cfg.Dir).List(c.File, c.Limit)
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		ui.Warn("No runs stored")
	}
	// Emitted even when empty: stdout always carries the artifact, so a caller
	// parses `[]` rather than distinguishing "no runs" from "command produced
	// nothing".
	return emitYAML(metas)
}

// --- rm ---

type SessionRmCmd struct {
	SID string `arg:"" optional:"" help:"Run id or unique prefix"`
	All bool   `help:"Delete every stored run"`
}

func (c *SessionRmCmd) Run(cfg *config.AppConfig) error {
	store := run.NewStore(cfg.Dir)

	// Refuse rather than pick: `rm abc --all` reads as "delete abc" to a caller
	// who typo'd the flag, and deleting everything instead is unrecoverable.
	if c.All && c.SID != "" {
		return voxerr.New(voxerr.InvalidUsage, "--all cannot be combined with a run id").
			WithHint("vox session rm %s  ·  vox session rm --all", c.SID)
	}
	if c.All {
		if err := store.RemoveAll(); err != nil {
			return err
		}
		ui.Success("All runs deleted")
		return nil
	}
	if c.SID == "" {
		return voxerr.New(voxerr.InvalidUsage, "no run id given").
			WithHint("vox session rm <sid>  ·  vox session rm --all")
	}

	sid, err := store.Resolve(c.SID)
	if err != nil {
		return err
	}
	if err := store.Remove(sid); err != nil {
		return err
	}
	ui.Success("Deleted %s", ui.Key(sid))
	return nil
}

// emitYAML writes the single machine-readable artifact this command produces.
func emitYAML(v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func writeOut(path, content string) error {
	if path == "" {
		fmt.Print(content)
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return voxerr.New(voxerr.IOError, "cannot write %s: %v", path, err)
	}
	return nil
}
