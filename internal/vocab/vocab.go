// Package vocab owns hotword lists. The YAML files under ~/.vox/vocabulary are
// the source of truth; the server-side lists are a materialization of them, one
// per target model, reconciled by content hash.
//
// Model-specific API constraints live here, never in the YAML: an agent editing
// a vocabulary should not have to know any model's rules.
package vocab

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/voxerr"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultWeight is the value Alibaba recommends when none is given.
	DefaultWeight = 4
	// SuperWeight is the "super hotword" magic value; Qwen only.
	SuperWeight = 50
	// MaxWords is the smallest documented per-list cap across our models.
	MaxWords = 2000
	// MaxSuperWords caps weight=50 entries.
	MaxSuperWords = 50
)

// File is the on-disk YAML shape.
type File struct {
	Lang          string          `yaml:"lang,omitempty"`
	DefaultWeight int             `yaml:"default_weight,omitempty"`
	Words         map[string]*int `yaml:"words"`
	// Models is the escape hatch for genuine per-model intent. Merged over the
	// base, model block wins. Most files omit it.
	Models map[string]struct {
		Words map[string]*int `yaml:"words"`
	} `yaml:"models,omitempty"`
}

type Vocabulary struct {
	Name string
	Path string
	File File
}

func Dir(voxDir string) string { return filepath.Join(voxDir, "vocabulary") }

func Path(voxDir, name string) string { return filepath.Join(Dir(voxDir), name+".yaml") }

func Load(voxDir, name string) (*Vocabulary, error) {
	path := Path(voxDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, voxerr.New(voxerr.VocabNotFound, "no vocabulary %q", name).
				WithHint("write %s, then rerun", Tilde(path))
		}
		return nil, err
	}
	// Strict decoding: this file is hand- and agent-edited, and a typo like
	// `word:` for `words:` would otherwise resolve to an empty vocabulary that
	// silently changes nothing about recognition.
	var f File
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&f); err != nil && err != io.EOF {
		return nil, voxerr.New(voxerr.VocabNotFound, "vocabulary %q is not valid: %v", name, err).
			WithHint("check %s", Tilde(path))
	}
	if f.DefaultWeight != 0 && !validWeight(f.DefaultWeight) {
		return nil, voxerr.New(voxerr.VocabNotFound,
			"vocabulary %q has default_weight %d; expected 1-5", name, f.DefaultWeight).
			WithHint("check %s", Tilde(path))
	}
	return &Vocabulary{Name: name, Path: path, File: f}, nil
}

// Names lists every vocabulary file, valid or not. Callers decide how to report
// an unreadable one; skipping it here would hide a broken source of truth.
func Names(voxDir string) ([]string, error) {
	entries, err := os.ReadDir(Dir(voxDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".yaml")
		// Skip directories, non-YAML files, and the dotfiles vox owns.
		if e.IsDir() || name == e.Name() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// List loads every vocabulary, failing on the first unreadable one.
func List(voxDir string) ([]*Vocabulary, error) {
	names, err := Names(voxDir)
	if err != nil {
		return nil, err
	}
	out := make([]*Vocabulary, 0, len(names))
	for _, name := range names {
		v, err := Load(voxDir, name)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Resolve renders the vocabulary for one target model: base words merged with
// that model's block, API constraints applied. Warnings describe what was
// clamped or dropped and are meant for stderr.
func (v *Vocabulary) Resolve(model string) (words []dashscope.Hotword, warnings []string) {
	merged := make(map[string]*int, len(v.File.Words))
	for word, weight := range v.File.Words {
		merged[word] = weight
	}
	if block, ok := v.File.Models[model]; ok {
		for word, weight := range block.Words {
			merged[word] = weight
		}
	}

	defaultWeight := v.File.DefaultWeight
	if defaultWeight == 0 {
		defaultWeight = DefaultWeight
	}
	lang := langFor(model, v.File.Lang)
	if v.File.Lang != "" && lang == "" {
		warnings = append(warnings, fmt.Sprintf("lang %q is not supported by %s; letting the model auto-detect", v.File.Lang, model))
	}

	// Sorted so the content hash is stable across map iteration order.
	names := make([]string, 0, len(merged))
	for word := range merged {
		names = append(names, word)
	}
	sort.Strings(names)

	superCount, clamped, oversized := 0, 0, 0
	for _, name := range names {
		word := strings.TrimSpace(name)
		if word == "" {
			warnings = append(warnings, "dropped a blank word entry")
			continue
		}
		weight := defaultWeight
		if merged[name] != nil {
			weight = *merged[name]
		}
		if weight == SuperWeight {
			if !dashscope.SuperHotwordsSupported(model) {
				weight = 5
				clamped++
			} else if superCount >= MaxSuperWords {
				weight = 5
				clamped++
			} else {
				superCount++
			}
		}
		if !validWeight(weight) {
			warnings = append(warnings, fmt.Sprintf("%q: weight %d out of range, using %d", word, weight, DefaultWeight))
			weight = DefaultWeight
		}
		if !validLength(word) {
			oversized++
			continue
		}
		words = append(words, dashscope.Hotword{Text: word, Weight: weight, Lang: lang})
	}

	if clamped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d super hotword(s) clamped to weight 5 for %s", clamped, model))
	}
	if oversized > 0 {
		warnings = append(warnings, fmt.Sprintf("%d word(s) dropped: over the length limit", oversized))
	}
	if len(words) > MaxWords {
		warnings = append(warnings, fmt.Sprintf("%d words exceed the %d cap; keeping the first %d", len(words), MaxWords, MaxWords))
		words = words[:MaxWords]
	}
	return words, warnings
}

// ContentHash fingerprints the resolved list. Two vocabularies with the same
// hash need no server round-trip.
func ContentHash(words []dashscope.Hotword) string {
	canonical, _ := json.Marshal(words)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:8]
}

// validWeight is the documented range, plus the super-hotword magic value.
func validWeight(w int) bool { return (w >= 1 && w <= 5) || w == SuperWeight }

// validLength enforces the documented word limits: at most 15 characters when
// any non-ASCII is present, at most 7 space-separated segments when pure ASCII.
func validLength(word string) bool {
	runes := []rune(word)
	if len(runes) == 0 || len(strings.Fields(word)) == 0 {
		return false
	}
	for _, r := range runes {
		if r > 127 {
			return len(runes) <= 15
		}
	}
	return len(strings.Fields(word)) <= 7
}

// funASRLangs is the vocabulary API's accepted set for Fun-ASR targets; other
// codes are dropped so the list still deploys.
var funASRLangs = map[string]bool{"zh": true, "en": true, "ja": true}

func langFor(model, lang string) string {
	if lang == "" {
		return ""
	}
	if model == dashscope.ModelFunASR && !funASRLangs[lang] {
		return ""
	}
	return lang
}

// prefixPattern matches the API's constraint: lowercase alphanumerics, ≤10 chars.
var prefixPattern = regexp.MustCompile(`[^a-z0-9]`)

func prefixFor(name string) string {
	p := prefixPattern.ReplaceAllString(strings.ToLower(name), "")
	if p == "" {
		p = "vox"
	}
	if len(p) > 10 {
		p = p[:10]
	}
	return p
}

func Tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + path[len(home):]
}
