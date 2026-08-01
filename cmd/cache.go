package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/ui"
)

type CacheCmd struct {
	Status CacheStatusCmd `cmd:"" default:"withargs" help:"Show cache size and file count"`
	Clear  CacheClearCmd  `cmd:"" help:"Delete all cached audio"`
}

type CacheStatusCmd struct{}

func (c *CacheStatusCmd) Run(cfg *config.AppConfig) error {
	dir := filepath.Join(cfg.Dir, "cache")
	entries, err := os.ReadDir(dir)
	if err != nil {
		ui.Info("%s %s", ui.Dim("cache"), ui.Dim("empty"))
		return nil
	}

	var totalSize int64
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			totalSize += info.Size()
		}
	}

	ui.KV("Path", dir)
	ui.KV("Files", fmt.Sprintf("%d", len(entries)))
	ui.KV("Size", formatSize(totalSize))
	return nil
}

type CacheClearCmd struct{}

func (c *CacheClearCmd) Run(cfg *config.AppConfig) error {
	dir := filepath.Join(cfg.Dir, "cache")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		ui.Info("%s", ui.Dim("cache already empty"))
		return nil
	}

	var count int
	for _, e := range entries {
		if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
			count++
		}
	}

	ui.Success("Cleared %d cached files", count)
	return nil
}

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
