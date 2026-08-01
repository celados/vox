package run

import (
	"testing"

	"github.com/celados/vox/internal/dashscope"
)

func TestSIDIsStableAndArgSensitive(t *testing.T) {
	audio := []byte("fake audio bytes")
	base := Args{Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}}

	first := SID(audio, base)
	if first != SID(audio, base) {
		t.Fatal("sid is not stable for identical input")
	}
	if len(first) != sidLength {
		t.Errorf("sid length = %d, want %d", len(first), sidLength)
	}

	// Every field in Args must re-key the run.
	variants := map[string]Args{
		"model":   {Model: dashscope.ModelQwenAudioASRFlash, Lang: []string{"zh"}},
		"lang":    {Model: dashscope.ModelFunASRFlash, Lang: []string{"en"}},
		"vocab":   {Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}, Vocab: "meeting@abcd1234"},
		"context": {Model: dashscope.ModelFunASRFlash, Lang: []string{"zh"}, Context: []string{"DashScope"}},
	}
	for name, args := range variants {
		if SID(audio, args) == first {
			t.Errorf("changing %s did not change the sid", name)
		}
	}

	if SID([]byte("different audio"), base) == first {
		t.Error("changing the audio did not change the sid")
	}
}

func TestMeasureCountsCJKAndLatin(t *testing.T) {
	result := &dashscope.ASRResult{
		Text:  "你好world",
		Words: []dashscope.ASRWord{{Text: "你好"}, {Text: "world"}},
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
		if err := store.saveEmpty(sid); err != nil {
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
