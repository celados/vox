package vocab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/voxerr"
)

// deployTimeout bounds the UNDEPLOYED poll after a create.
const deployTimeout = 30 * time.Second

// Entry records one materialized list. The content hash lets a sync skip the
// network entirely when nothing changed.
type Entry struct {
	VocabularyID string    `json:"vocabulary_id"`
	ContentHash  string    `json:"content_hash"`
	SyncedAt     time.Time `json:"synced_at"`
}

// Index maps vocabulary name → target model → entry.
type Index map[string]map[string]Entry

func indexPath(voxDir string) string { return filepath.Join(Dir(voxDir), ".index.json") }

func LoadIndex(voxDir string) (Index, error) {
	data, err := os.ReadFile(indexPath(voxDir))
	if err != nil {
		if os.IsNotExist(err) {
			return Index{}, nil
		}
		return nil, err
	}
	idx := Index{}
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, nil // a corrupt index is rebuildable; never fatal
	}
	return idx, nil
}

func (idx Index) Save(voxDir string) error {
	if err := os.MkdirAll(Dir(voxDir), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath(voxDir), data, 0644)
}

func (idx Index) Get(name, model string) (Entry, bool) {
	e, ok := idx[name][model]
	return e, ok
}

func (idx Index) Set(name, model string, entry Entry) {
	if idx[name] == nil {
		idx[name] = map[string]Entry{}
	}
	idx[name][model] = entry
}

// claimedIDs returns the vocabulary_ids still backed by a local YAML file.
//
// Reconciling against the index alone would be wrong: deleting a YAML leaves its
// index entry behind, and the remote list it points at would then be claimed
// forever while occupying one of the ten account slots.
func claimedIDs(voxDir string, idx Index) map[string]bool {
	ids := map[string]bool{}
	for name, models := range idx {
		if _, err := os.Stat(Path(voxDir, name)); err != nil {
			continue
		}
		for _, e := range models {
			ids[e.VocabularyID] = true
		}
	}
	return ids
}

// forgetUnbacked drops index entries whose YAML is gone, so a pruned id is not
// resurrected as a phantom "synced" state in `vocab ls`.
func (idx Index) forgetUnbacked(voxDir string) bool {
	changed := false
	for name := range idx {
		if _, err := os.Stat(Path(voxDir, name)); err != nil {
			delete(idx, name)
			changed = true
		}
	}
	return changed
}

type SyncResult struct {
	VocabularyID string
	ContentHash  string
	Action       string // reused | created | updated
	Warnings     []string
}

// Sync makes the server-side list for (vocabulary, model) match the local YAML
// and returns the id to pass to recognition.
//
// An unchanged content hash short-circuits before any request, which is the
// common path: `vox hear --vocab X` syncs on demand and normally costs nothing.
func Sync(client *dashscope.Client, voxDir string, v *Vocabulary, model string, force bool) (*SyncResult, error) {
	words, warnings := v.Resolve(model)
	if len(words) == 0 {
		return nil, voxerr.New(voxerr.VocabNotFound, "vocabulary %q resolves to no usable words for %s", v.Name, model).
			WithHint("check %s", Tilde(v.Path))
	}
	hash := ContentHash(words)

	idx, err := LoadIndex(voxDir)
	if err != nil {
		return nil, err
	}
	result := &SyncResult{ContentHash: hash, Warnings: warnings}

	if entry, ok := idx.Get(v.Name, model); ok {
		if entry.ContentHash == hash && !force {
			result.VocabularyID, result.Action = entry.VocabularyID, "reused"
			return result, nil
		}
		if err := client.UpdateVocabulary(entry.VocabularyID, words); err != nil {
			return nil, apiError(err)
		}
		idx.Set(v.Name, model, Entry{VocabularyID: entry.VocabularyID, ContentHash: hash, SyncedAt: time.Now()})
		if err := idx.Save(voxDir); err != nil {
			return nil, err
		}
		result.VocabularyID, result.Action = entry.VocabularyID, "updated"
		return result, nil
	}

	// The account cap is shared across every model, so check before creating.
	remote, err := client.ListVocabularies()
	if err != nil {
		return nil, apiError(err)
	}
	if len(remote) >= dashscope.VocabularyQuota {
		return nil, voxerr.New(voxerr.VocabQuotaExceeded,
			"account already holds %d/%d hotword lists", len(remote), dashscope.VocabularyQuota).
			WithHint("vox vocab prune")
	}

	id, err := client.CreateVocabulary(model, prefixFor(v.Name), words)
	if err != nil {
		return nil, apiError(err)
	}
	if err := client.AwaitVocabulary(id, deployTimeout); err != nil {
		return nil, apiError(err)
	}
	idx.Set(v.Name, model, Entry{VocabularyID: id, ContentHash: hash, SyncedAt: time.Now()})
	if err := idx.Save(voxDir); err != nil {
		return nil, err
	}
	result.VocabularyID, result.Action = id, "created"
	return result, nil
}

// Prune deletes server-side lists no local vocabulary YAML claims — both lists
// vox never created and lists whose YAML has since been deleted.
func Prune(client *dashscope.Client, voxDir string, dryRun bool) ([]string, error) {
	idx, err := LoadIndex(voxDir)
	if err != nil {
		return nil, err
	}
	claimed := claimedIDs(voxDir, idx)

	remote, err := client.ListVocabularies()
	if err != nil {
		return nil, apiError(err)
	}

	var orphans []string
	for _, info := range remote {
		if claimed[info.ID] {
			continue
		}
		orphans = append(orphans, info.ID)
		if dryRun {
			continue
		}
		if err := client.DeleteVocabulary(info.ID); err != nil {
			return orphans, apiError(err)
		}
	}
	if !dryRun && idx.forgetUnbacked(voxDir) {
		if err := idx.Save(voxDir); err != nil {
			return orphans, err
		}
	}
	return orphans, nil
}

func apiError(err error) error {
	return voxerr.New(voxerr.APIError, "%s", err.Error())
}

// Describe renders the per-model sync state for the index listing.
func Describe(idx Index, name string) map[string]string {
	state := map[string]string{}
	for model, entry := range idx[name] {
		state[model] = fmt.Sprintf("%s@%s", entry.VocabularyID, entry.ContentHash)
	}
	return state
}
