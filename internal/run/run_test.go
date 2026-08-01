package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/celados/vox/internal/dashscope"
)

func TestSIDIsStableAndArgSensitive(t *testing.T) {
	audio := []byte("fake audio bytes")
	base := Args{Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}}

	first := SID(Digest(audio, base))
	if first != SID(Digest(audio, base)) {
		t.Fatal("sid is not stable for identical input")
	}
	if len(first) != sidLength {
		t.Errorf("sid length = %d, want %d", len(first), sidLength)
	}

	// Every field in Args must re-key the run.
	variants := map[string]Args{
		"model":    {Model: dashscope.ModelQwenAudioASRFlash, Lang: []string{"zh"}},
		"lang":     {Model: dashscope.ModelFunASRFlash, Lang: []string{"en"}},
		"vocab":    {Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}, Vocab: "abcd1234"},
		"speakers": {Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}, Speakers: true},
		"context":  {Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}, Context: []string{"DashScope"}},
		"format":   {Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}, Format: "mp3"},
	}
	for name, args := range variants {
		if SID(Digest(audio, args)) == first {
			t.Errorf("changing %s did not change the sid", name)
		}
	}

	if SID(Digest([]byte("different audio"), base)) == first {
		t.Error("changing the audio did not change the sid")
	}
}

func TestMeasureCountsCJKAndLatin(t *testing.T) {
	result := &dashscope.ASRResult{
		Text: "你好world",
		Sentences: []dashscope.Sentence{{
			Words: []dashscope.ASRWord{{Text: "你好"}, {Text: "world"}},
		}},
	}
	size := Measure(result, 42)

	// 2 CJK glyphs at ~1 token each, plus 5 Latin characters at ~1 per 4.
	if size.Tokens != 4 {
		t.Errorf("tokens = %d, want 4", size.Tokens)
	}
	if size.Words != 2 {
		t.Errorf("words = %d, want 2", size.Words)
	}
	if size.Chars != 7 {
		t.Errorf("chars = %d, want 7", size.Chars)
	}
	if size.Duration != 42 {
		t.Errorf("duration = %d, want 42", size.Duration)
	}
}

func TestPreviewTruncatesOnRunes(t *testing.T) {
	if got := Preview("你好世界", 10); got != "你好世界" {
		t.Errorf("short text was altered: %q", got)
	}
	if got := Preview("你好世界", 2); got != "你好…" {
		t.Errorf("preview = %q, want 你好…", got)
	}
}

func TestResolveRejectsAmbiguousPrefix(t *testing.T) {
	store := &Store{dir: t.TempDir()}
	for _, sid := range []string{"abc111111111", "abc222222222"} {
		if err := os.MkdirAll(store.Dir(sid), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.Resolve("abc"); err == nil {
		t.Fatal("an ambiguous prefix must not resolve")
	}
	if _, err := store.Resolve("abc1"); err != nil {
		t.Fatalf("a unique prefix must resolve: %v", err)
	}
	if _, err := store.Resolve("zzz"); err == nil {
		t.Fatal("a missing prefix must not resolve")
	}
}

// Regression: Resolve feeds os.RemoveAll. Before validation, `session rm ..`
// resolved to ~/.vox and `../..` to the home directory.
func TestResolveRejectsPathTraversal(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	if err := os.MkdirAll(store.dir, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(home, "config.json")
	if err := os.WriteFile(sentinel, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, prefix := range []string{"..", "../..", "../../..", "/etc", "a/b", ".", "ABCDEF", "zzz"} {
		if _, err := store.Resolve(prefix); err == nil {
			t.Errorf("Resolve(%q) must not resolve", prefix)
		}
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel outside runs/ was disturbed: %v", err)
	}
}

// A truncated sid could in principle collide. The full digest is what decides a
// cache hit, so a collision degrades to a miss instead of returning the wrong
// transcript.
func TestLoadRejectsDigestMismatch(t *testing.T) {
	store := NewStore(t.TempDir())
	rec := &Record{
		Digest: "digest-of-input-A",
		Meta:   Meta{SID: "aaaaaaaaaaaa"},
		Result: &dashscope.ASRResult{Text: "transcript A"},
	}
	if err := store.Save(rec, []byte("audio"), "wav"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load("aaaaaaaaaaaa", "digest-of-input-B"); err == nil {
		t.Error("a different input must not be served from a colliding sid")
	}
	got, err := store.Load("aaaaaaaaaaaa", "digest-of-input-A")
	if err != nil {
		t.Fatalf("matching digest must load: %v", err)
	}
	if got.Result.Text != "transcript A" {
		t.Errorf("text = %q", got.Result.Text)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	store := NewStore(t.TempDir())
	rec := &Record{Digest: "d", Meta: Meta{SID: "bbbbbbbbbbbb"}, Result: &dashscope.ASRResult{Text: "x"}}
	if err := store.Save(rec, []byte("audio"), "wav"); err != nil {
		t.Fatal(err)
	}

	// No staging directory may survive, and none may show up in the index.
	metas, err := store.List("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].SID != "bbbbbbbbbbbb" {
		t.Fatalf("index = %+v", metas)
	}
}

func TestListReturnsEmptySliceNotNil(t *testing.T) {
	metas, err := NewStore(t.TempDir()).List("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if metas == nil {
		t.Error("an empty index must marshal as [], not null")
	}
}
