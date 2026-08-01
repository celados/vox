// Package run stores content-addressed transcription runs. A run is identified
// by sid = hash(audio, recognition args), so the same input always resolves to
// the same directory and a repeat call costs no API request.
package run

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/voxerr"
	"gopkg.in/yaml.v3"
)

// sidLength is the hex prefix kept from the sha256, git-style.
const sidLength = 12

// Args is everything that changes the transcript. Presentation choices are
// deliberately absent: exporting a run a second way must not fork it.
type Args struct {
	Model  string `yaml:"model" json:"model"`
	Format string `yaml:"format" json:"format"`
	// Vocab is the resolved content hash, never the vocabulary's name: two files
	// with identical words must not fork the run, and editing one must.
	Vocab   string   `yaml:"vocab,omitempty" json:"vocab,omitempty"`
	Lang    []string `yaml:"lang,omitempty" json:"lang,omitempty"`
	Context []string `yaml:"context,omitempty" json:"context,omitempty"`
}

type Size struct {
	Tokens   int `yaml:"tokens" json:"tokens"`
	Words    int `yaml:"words" json:"words"`
	Chars    int `yaml:"chars" json:"chars"`
	Duration int `yaml:"duration" json:"duration"` // seconds of audio
}

// Meta is the envelope: what `hear` prints and what `session ls` lists.
type Meta struct {
	SID     string    `yaml:"sid" json:"sid"`
	Source  string    `yaml:"source" json:"source"`
	Model   string    `yaml:"model" json:"model"`
	Vocab   string    `yaml:"vocab,omitempty" json:"vocab,omitempty"`
	Lang    []string  `yaml:"lang,omitempty" json:"lang,omitempty"`
	Created time.Time `yaml:"created" json:"created"`
	Path    string    `yaml:"path" json:"path"`
	Size    Size      `yaml:"size" json:"size"`
}

// Record is a run's full content, kept beside the envelope.
type Record struct {
	Meta Meta `json:"meta"`
	Args Args `json:"args"`
	// Digest is the untruncated identity. sid is a 48-bit display prefix, so a
	// cache hit is only trusted after the full digest matches — a collision
	// degrades to a miss instead of silently returning another file's transcript.
	Digest string               `json:"digest"`
	Result *dashscope.ASRResult `json:"result"`
}

type Store struct{ dir string }

func NewStore(voxDir string) *Store { return &Store{dir: filepath.Join(voxDir, "runs")} }

// Digest is the full identity of a run: the audio plus everything that changes
// the transcript.
func Digest(audio []byte, args Args) string {
	h := sha256.New()
	h.Write(audio)
	// A canonical encoding matters: a field reordering must not re-key the run.
	canonical, _ := json.Marshal(args)
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil))
}

// SID is the displayed prefix of the digest.
func SID(digest string) string { return digest[:sidLength] }

func (s *Store) Dir(sid string) string { return filepath.Join(s.dir, sid) }

// Load returns the run stored under sid only if it is the one identified by
// digest. Passing an empty digest skips the check, for lookups by id where the
// caller has no input to compare against.
func (s *Store) Load(sid, digest string) (*Record, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir(sid), "run.json"))
	if err != nil {
		return nil, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	if digest != "" && rec.Digest != digest {
		return nil, fmt.Errorf("run %s holds a different input", sid)
	}
	return &rec, nil
}

// Save publishes a run atomically: everything is written to a scratch directory
// and moved into place in one rename, so a concurrent `session ls` or `export`
// never observes a half-written run.
func (s *Store) Save(rec *Record, audio []byte, format string) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(s.dir, ".staging-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	if err := os.WriteFile(filepath.Join(staging, "audio."+format), audio, 0644); err != nil {
		return err
	}
	// meta.yaml duplicates the envelope so the directory is readable on its own,
	// without going through the CLI.
	metaYAML, err := yaml.Marshal(rec.Meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "meta.yaml"), metaYAML, 0644); err != nil {
		return err
	}
	full, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "run.json"), full, 0644); err != nil {
		return err
	}

	// A losing racer replaces an identical run, so clearing the target first is
	// safe: both writers computed the same digest from the same input.
	final := s.Dir(rec.Meta.SID)
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	return os.Rename(staging, final)
}

// sidPattern is the only shape a run directory name can take. Resolve validates
// against it before touching the filesystem: a prefix reaches os.RemoveAll, so
// "../.." must never survive as far as a path join.
var sidPattern = regexp.MustCompile(`^[0-9a-f]{1,` + strconv.Itoa(sidLength) + `}$`)

// Resolve expands a unique sid prefix. An ambiguous prefix is an error, never a
// guess.
func (s *Store) Resolve(prefix string) (string, error) {
	if !sidPattern.MatchString(prefix) {
		return "", voxerr.New(voxerr.SessionNotFound,
			"%q is not a run id: expected up to %d hex characters", prefix, sidLength)
	}
	if _, err := os.Stat(s.Dir(prefix)); err == nil {
		return prefix, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return "", voxerr.New(voxerr.SessionNotFound, "no run matching %q", prefix)
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() && sidPattern.MatchString(e.Name()) && strings.HasPrefix(e.Name(), prefix) {
			matches = append(matches, e.Name())
		}
	}
	switch len(matches) {
	case 0:
		return "", voxerr.New(voxerr.SessionNotFound, "no run matching %q", prefix).
			WithHint("vox session ls")
	case 1:
		return matches[0], nil
	default:
		return "", voxerr.New(voxerr.SessionAmbiguous, "%q matches %d runs: %s",
			prefix, len(matches), strings.Join(matches, ", "))
	}
}

// List returns run envelopes, newest first.
func (s *Store) List(sourceFilter string, limit int) ([]Meta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Meta{}, nil
		}
		return nil, err
	}

	metas := []Meta{} // never nil: an empty index still has to marshal as []
	for _, e := range entries {
		// Skip in-flight staging directories as well as stray files.
		if !e.IsDir() || !sidPattern.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name(), "meta.yaml"))
		if err != nil {
			continue
		}
		var meta Meta
		if yaml.Unmarshal(data, &meta) != nil {
			continue
		}
		if sourceFilter != "" && !sameSource(meta.Source, sourceFilter) {
			continue
		}
		metas = append(metas, meta)
	}

	sort.Slice(metas, func(i, j int) bool { return metas[i].Created.After(metas[j].Created) })
	if limit > 0 && len(metas) > limit {
		metas = metas[:limit]
	}
	return metas, nil
}

func (s *Store) Remove(sid string) error { return os.RemoveAll(s.Dir(sid)) }

func (s *Store) RemoveAll() error { return os.RemoveAll(s.dir) }

// sameSource compares by absolute path so a run found via a relative path still
// matches the one recorded from elsewhere.
func sameSource(recorded, filter string) bool {
	if recorded == filter {
		return true
	}
	a, err1 := filepath.Abs(expandHome(recorded))
	b, err2 := filepath.Abs(expandHome(filter))
	return err1 == nil && err2 == nil && a == b
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// Tilde shortens a path for display, the inverse of expandHome.
func Tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + path[len(home):]
}

// Measure sizes a transcript. Tokens are an estimate, not a tokenizer call: CJK
// glyphs bill near one token each, Latin text nearer one per four characters.
// It only has to be good enough to decide whether to fold the envelope.
func Measure(result *dashscope.ASRResult, durationSec int) Size {
	var cjk, other int
	for _, r := range result.Text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
		} else {
			other++
		}
	}
	return Size{
		Tokens:   cjk + (other+3)/4,
		Words:    len(result.Words),
		Chars:    len([]rune(result.Text)),
		Duration: durationSec,
	}
}

// Preview truncates a transcript for a folded envelope.
func Preview(text string, runes int) string {
	r := []rune(text)
	if len(r) <= runes {
		return text
	}
	return string(r[:runes]) + "…"
}
