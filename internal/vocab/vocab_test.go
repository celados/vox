package vocab

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/voxerr"
)

func weight(n int) *int { return &n }

func fixture() *Vocabulary {
	return &Vocabulary{Name: "meeting", File: File{
		Lang:          "zh",
		DefaultWeight: 4,
		Words:         map[string]*int{"百炼": weight(5), "赛德克巴莱": nil},
	}}
}

func TestResolveAppliesDefaultWeight(t *testing.T) {
	words, _ := fixture().Resolve(dashscope.ModelFunASRFlash)
	byText := map[string]int{}
	for _, w := range words {
		byText[w.Text] = w.Weight
	}
	if byText["百炼"] != 5 {
		t.Errorf("explicit weight = %d, want 5", byText["百炼"])
	}
	if byText["赛德克巴莱"] != 4 {
		t.Errorf("default weight = %d, want 4", byText["赛德克巴莱"])
	}
}

// Super hotwords are Qwen-only. A shared vocabulary must degrade rather than
// fail, since the YAML is meant to be model-agnostic.
func TestResolveClampsSuperWeightOnFunASR(t *testing.T) {
	v := fixture()
	v.File.Words["Qwen"] = weight(SuperWeight)

	words, warnings := v.Resolve(dashscope.ModelFunASRFlash)
	for _, w := range words {
		if w.Text == "Qwen" && w.Weight != 5 {
			t.Errorf("clamped weight = %d, want 5", w.Weight)
		}
	}
	if len(warnings) == 0 {
		t.Error("clamping must warn")
	}

	words, _ = v.Resolve(dashscope.ModelQwenAudioASRFlash)
	for _, w := range words {
		if w.Text == "Qwen" && w.Weight != SuperWeight {
			t.Errorf("qwen weight = %d, want %d", w.Weight, SuperWeight)
		}
	}
}

func TestResolveModelBlockOverridesBase(t *testing.T) {
	v := fixture()
	v.File.Models = map[string]struct {
		Words map[string]*int `yaml:"words"`
	}{
		dashscope.ModelFunASRFlash: {Words: map[string]*int{"百炼": weight(2), "声网": weight(5)}},
	}

	words, _ := v.Resolve(dashscope.ModelFunASRFlash)
	byText := map[string]int{}
	for _, w := range words {
		byText[w.Text] = w.Weight
	}
	if byText["百炼"] != 2 {
		t.Errorf("override = %d, want 2", byText["百炼"])
	}
	if byText["声网"] != 5 {
		t.Error("model block must contribute new words")
	}

	// The base is untouched for other models.
	words, _ = v.Resolve(dashscope.ModelQwenAudioASRFlash)
	for _, w := range words {
		if w.Text == "声网" {
			t.Error("model block leaked into another model")
		}
	}
}

func TestResolveDropsOversizedWords(t *testing.T) {
	v := fixture()
	v.File.Words["这是一个非常长的热词超过了十五个字符的限制"] = weight(4)
	v.File.Words["one two three four five six seven eight"] = weight(4)

	words, warnings := v.Resolve(dashscope.ModelFunASRFlash)
	for _, w := range words {
		if len([]rune(w.Text)) > 15 {
			t.Errorf("oversized word survived: %q", w.Text)
		}
	}
	if len(warnings) == 0 {
		t.Error("dropping words must warn")
	}
}

// Fun-ASR's vocabulary API accepts only zh/en/ja; an unsupported code is dropped
// so the list still deploys instead of erroring.
func TestResolveDropsUnsupportedLang(t *testing.T) {
	v := fixture()
	v.File.Lang = "de"

	words, warnings := v.Resolve(dashscope.ModelFunASRFlash)
	for _, w := range words {
		if w.Lang != "" {
			t.Errorf("lang = %q, want empty", w.Lang)
		}
	}
	if len(warnings) == 0 {
		t.Error("dropping a lang must warn")
	}

	words, _ = v.Resolve(dashscope.ModelQwenAudioASRFlash)
	if len(words) > 0 && words[0].Lang != "de" {
		t.Errorf("qwen lang = %q, want de", words[0].Lang)
	}
}

// The content hash keys both the sync short-circuit and the run's sid, so map
// iteration order must never change it.
func TestContentHashIsStable(t *testing.T) {
	first, _ := fixture().Resolve(dashscope.ModelFunASRFlash)
	for i := 0; i < 20; i++ {
		next, _ := fixture().Resolve(dashscope.ModelFunASRFlash)
		if ContentHash(first) != ContentHash(next) {
			t.Fatal("content hash is not stable across resolves")
		}
	}
}

func TestContentHashChangesWithContent(t *testing.T) {
	base, _ := fixture().Resolve(dashscope.ModelFunASRFlash)

	v := fixture()
	v.File.Words["百炼"] = weight(3)
	changed, _ := v.Resolve(dashscope.ModelFunASRFlash)

	if ContentHash(base) == ContentHash(changed) {
		t.Error("editing a weight must change the content hash")
	}
}

func TestPrefixFor(t *testing.T) {
	cases := map[string]string{
		"meeting":               "meeting",
		"Dev-Notes_2026":        "devnotes20",
		"中文":                    "vox",
		"averyveryverylongname": "averyveryv",
	}
	for in, want := range cases {
		if got := prefixFor(in); got != want {
			t.Errorf("prefixFor(%q) = %q, want %q", in, got, want)
		}
	}
}

// Regression: pruning against the index alone leaks the remote list of a deleted
// vocabulary, which then holds one of the ten account slots forever.
func TestClaimedIDsIgnoresDeletedYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir, "kept"), []byte("words: {a: 4}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx := Index{
		"kept":    {dashscope.ModelFunASRFlash: Entry{VocabularyID: "vocab-kept"}},
		"deleted": {dashscope.ModelFunASRFlash: Entry{VocabularyID: "vocab-deleted"}},
	}

	claimed := claimedIDs(dir, idx)
	if !claimed["vocab-kept"] {
		t.Error("a backed vocabulary must stay claimed")
	}
	if claimed["vocab-deleted"] {
		t.Error("a vocabulary whose YAML is gone must not stay claimed")
	}

	if !idx.forgetUnbacked(dir) {
		t.Error("forgetUnbacked must report the change")
	}
	if _, ok := idx["deleted"]; ok {
		t.Error("stale index entry survived")
	}
	if _, ok := idx["kept"]; !ok {
		t.Error("live index entry was dropped")
	}
}

// Regression: a corrupt index used to be reported as empty, which made `prune`
// consider nothing claimed and delete every hotword list on the account.
func TestLoadIndexFailsClosedOnCorruption(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(dir), ".index.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := LoadIndex(dir)
	if err == nil {
		t.Fatalf("a corrupt index must be an error, got %v", idx)
	}
	var e *voxerr.Error
	if !errors.As(err, &e) || e.Code != voxerr.VocabIndexCorrupt {
		t.Errorf("code = %v, want %s", err, voxerr.VocabIndexCorrupt)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0755); err != nil {
		t.Fatal(err)
	}
	// `word:` instead of `words:` would otherwise resolve to an empty vocabulary
	// that changes nothing about recognition, with no signal to the user.
	if err := os.WriteFile(Path(dir, "typo"), []byte("word:\n  百炼: 5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "typo"); err == nil {
		t.Error("an unknown field must be rejected")
	}
}

func TestLoadRejectsInvalidDefaultWeight(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir, "bad"), []byte("default_weight: 99\nwords:\n  x: \n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "bad"); err == nil {
		t.Error("an out-of-range default_weight must be rejected, not silently sent")
	}
}

func TestResolveDropsBlankWords(t *testing.T) {
	v := fixture()
	v.File.Words["   "] = weight(4)

	words, _ := v.Resolve(dashscope.ModelFunASRFlash)
	for _, w := range words {
		if strings.TrimSpace(w.Text) == "" {
			t.Error("a blank word reached the request")
		}
	}
}
