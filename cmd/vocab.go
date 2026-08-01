package cmd

import (
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/ui"
	"github.com/celados/vox/internal/vocab"
)

type VocabCmd struct {
	Ls    VocabLsCmd    `cmd:"" help:"List vocabularies and their sync state"`
	Sync  VocabSyncCmd  `cmd:"" help:"Push local YAML to the server for a target model"`
	Prune VocabPruneCmd `cmd:"" help:"Delete server-side lists no local vocabulary claims"`
}

// --- ls ---

type VocabLsCmd struct{}

// vocabEntry is the index record. It carries the path because there is no
// `vocab show` or `vocab path` — an agent reads and writes the YAML directly.
type vocabEntry struct {
	Name   string            `yaml:"name"`
	Path   string            `yaml:"path"`
	Words  int               `yaml:"words"`
	Lang   string            `yaml:"lang,omitempty"`
	Synced map[string]string `yaml:"synced,omitempty"`
}

func (c *VocabLsCmd) Run(cfg *config.AppConfig) error {
	vocabularies, err := vocab.List(cfg.Dir)
	if err != nil {
		return err
	}
	if len(vocabularies) == 0 {
		ui.Warn("No vocabularies")
		ui.Info("  write %s", ui.Key(vocab.Tilde(vocab.Path(cfg.Dir, "<name>"))))
		return nil
	}

	index, err := vocab.LoadIndex(cfg.Dir)
	if err != nil {
		return err
	}

	entries := make([]vocabEntry, 0, len(vocabularies))
	for _, v := range vocabularies {
		entries = append(entries, vocabEntry{
			Name:   v.Name,
			Path:   vocab.Tilde(v.Path),
			Words:  len(v.File.Words),
			Lang:   v.File.Lang,
			Synced: vocab.Describe(index, v.Name),
		})
	}

	// The account cap is shared across models, so surface usage next to the list.
	if key, err := cfg.RequireAPIKey(); err == nil {
		if remote, err := dashscope.NewClient(key).ListVocabularies(); err == nil {
			ui.Info("%s %d/%d", ui.Dim("remote lists"), len(remote), dashscope.VocabularyQuota)
		}
	}
	return emitYAML(entries)
}

// --- sync ---

type VocabSyncCmd struct {
	Name  string `arg:"" optional:"" help:"Vocabulary name"`
	All   bool   `help:"Sync every local vocabulary"`
	Model string `short:"m" default:"fun-asr-flash-2026-06-15" enum:"fun-asr-flash-2026-06-15,qwen-audio-3.0-asr-flash" help:"Target model"`
	Force bool   `help:"Push even when the content hash is unchanged"`
}

func (c *VocabSyncCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}
	client := dashscope.NewClient(apiKey)

	var targets []*vocab.Vocabulary
	if c.All {
		if targets, err = vocab.List(cfg.Dir); err != nil {
			return err
		}
	} else {
		v, err := vocab.Load(cfg.Dir, c.Name)
		if err != nil {
			return err
		}
		targets = []*vocab.Vocabulary{v}
	}

	for _, v := range targets {
		result, err := vocab.Sync(client, cfg.Dir, v, c.Model, c.Force)
		if err != nil {
			return err
		}
		for _, w := range result.Warnings {
			ui.Warn("%s: %s", v.Name, w)
		}
		ui.Success("%s %s %s", ui.Key(v.Name), ui.Dim(result.Action), ui.Dim(result.VocabularyID))
	}
	return nil
}

// --- prune ---

type VocabPruneCmd struct {
	DryRun bool `help:"Report orphans without deleting them"`
}

func (c *VocabPruneCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}

	orphans, err := vocab.Prune(dashscope.NewClient(apiKey), cfg.Dir, c.DryRun)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		ui.Success("No orphaned lists")
		return nil
	}
	verb := "Deleted"
	if c.DryRun {
		verb = "Would delete"
	}
	for _, id := range orphans {
		ui.Info("%s %s", ui.Dim(verb), ui.Key(id))
	}
	return nil
}
