package cmd

import (
	"fmt"

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
	// Error reports a file that exists but cannot be used, so a broken
	// vocabulary is visible in the index rather than silently absent.
	Error string `yaml:"error,omitempty"`
}

// vocabIndex is the whole artifact: the quota belongs in it, not on stderr,
// because deciding whether another vocabulary fits is exactly what a caller
// reads this for.
type vocabIndex struct {
	Quota        string       `yaml:"quota"`
	Vocabularies []vocabEntry `yaml:"vocabularies"`
}

func (c *VocabLsCmd) Run(cfg *config.AppConfig) error {
	names, err := vocab.Names(cfg.Dir)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		ui.Warn("No vocabularies")
		ui.Info("  write %s", ui.Key(vocab.Tilde(vocab.Path(cfg.Dir, "<name>"))))
	}

	index, err := vocab.LoadIndex(cfg.Dir)
	if err != nil {
		return err
	}

	entries := make([]vocabEntry, 0, len(names))
	for _, name := range names {
		entry := vocabEntry{Name: name, Path: vocab.Tilde(vocab.Path(cfg.Dir, name))}
		v, err := vocab.Load(cfg.Dir, name)
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.Words = len(v.File.Words)
			entry.Lang = v.File.Lang
			entry.Synced = vocab.Describe(index, name)
		}
		entries = append(entries, entry)
	}

	return emitYAML(vocabIndex{Quota: quotaUsage(cfg), Vocabularies: entries})
}

// quotaUsage reports the shared account cap, or why it is unknown — an
// unreachable API must not silently look like "plenty of room".
func quotaUsage(cfg *config.AppConfig) string {
	key, err := cfg.RequireAPIKey()
	if err != nil {
		return "unknown (not authenticated)"
	}
	remote, err := dashscope.NewClient(key).ListVocabularies()
	if err != nil {
		return "unknown (" + err.Error() + ")"
	}
	return fmt.Sprintf("%d/%d", len(remote), dashscope.VocabularyQuota)
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
