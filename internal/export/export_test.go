package export

import (
	"strings"
	"testing"
	"time"

	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/run"
	"gopkg.in/yaml.v3"
)

// word builds a word whose timing is caller-controlled, in milliseconds.
func word(text, punct string, begin, end int) dashscope.ASRWord {
	return dashscope.ASRWord{Text: text, Punctuation: punct, BeginTime: begin, EndTime: end}
}

func TestSegmentSplitsOnTerminator(t *testing.T) {
	cues := Segment([]dashscope.ASRWord{
		word("你好", "", 0, 500),
		word("世界", "。", 500, 1000),
		word("再见", "。", 1200, 1700),
	})
	if len(cues) != 2 {
		t.Fatalf("want 2 cues, got %d: %+v", len(cues), cues)
	}
	if cues[0].Text != "你好世界。" {
		t.Errorf("first cue = %q", cues[0].Text)
	}
	if cues[0].Begin.Milliseconds() != 0 || cues[0].End.Milliseconds() != 1000 {
		t.Errorf("first cue timing = %v..%v", cues[0].Begin, cues[0].End)
	}
	if cues[1].Index != 2 {
		t.Errorf("cue index = %d, want 2", cues[1].Index)
	}
}

// A silence longer than gapSplit ends a cue even with no punctuation, which is
// the only segmentation signal for speech the model punctuates sparsely.
func TestSegmentSplitsOnGap(t *testing.T) {
	cues := Segment([]dashscope.ASRWord{
		word("前半句", "", 0, 500),
		word("后半句", "", 1500, 2000),
	})
	if len(cues) != 2 {
		t.Fatalf("want 2 cues, got %d: %+v", len(cues), cues)
	}
}

func TestSegmentKeepsShortClausesTogether(t *testing.T) {
	// A separator below half the cue budget must not split.
	cues := Segment([]dashscope.ASRWord{
		word("好", "，", 0, 200),
		word("的", "。", 200, 400),
	})
	if len(cues) != 1 {
		t.Fatalf("want 1 cue, got %d: %+v", len(cues), cues)
	}
}

func TestSegmentEmpty(t *testing.T) {
	if cues := Segment(nil); len(cues) != 0 {
		t.Fatalf("want no cues, got %+v", cues)
	}
}

func TestTimeFormats(t *testing.T) {
	d := time.Hour + 2*time.Minute + 3*time.Second + 45*time.Millisecond
	if got := srtTime(d); got != "01:02:03,045" {
		t.Errorf("srtTime = %q", got)
	}
	if got := vttTime(d); got != "01:02:03.045" {
		t.Errorf("vttTime = %q", got)
	}
}

func TestRenderVTTHeader(t *testing.T) {
	out := renderVTT([]Cue{{Index: 1, End: time.Second, Text: "x"}})
	if !strings.HasPrefix(out, "WEBVTT\n\n") {
		t.Errorf("vtt output = %q", out)
	}
}

// Regression: a run with text but no word timings used to render an empty
// subtitle file and a markdown document containing only frontmatter.
func TestRendersWithoutWordTimings(t *testing.T) {
	rec := &run.Record{
		Meta:   run.Meta{SID: "abc", Size: run.Size{Duration: 12}},
		Result: &dashscope.ASRResult{Text: "整段文本，没有词级时间戳。"},
	}

	srt, err := Render(rec, "srt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(srt, "整段文本") {
		t.Errorf("srt lost the transcript: %q", srt)
	}
	if !strings.Contains(srt, "00:00:12,000") {
		t.Errorf("srt must span the run duration: %q", srt)
	}

	md, err := Render(rec, "md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "整段文本") {
		t.Errorf("markdown lost the transcript: %q", md)
	}
}

// Regression: out-of-order or zero-length word timings produced cues whose end
// preceded their begin, which players reject.
func TestSegmentRepairsTimeline(t *testing.T) {
	cues := Segment([]dashscope.ASRWord{
		word("后", "。", 1000, 1500),
		word("先", "。", 500, 700), // arrives late but claims an earlier start
		word("零", "。", 2000, 2000),
	})
	prev := time.Duration(-1)
	for _, cue := range cues {
		if cue.End < cue.Begin {
			t.Errorf("cue %d: end %v precedes begin %v", cue.Index, cue.End, cue.Begin)
		}
		if cue.End == cue.Begin {
			t.Errorf("cue %d is zero-length", cue.Index)
		}
		if cue.Begin < prev {
			t.Errorf("cue %d starts before the previous cue ended", cue.Index)
		}
		prev = cue.End
	}
}

// Regression: the budget was checked after appending, so one long word could
// carry a cue far past the limits.
func TestSegmentKeepsCuesWithinBudget(t *testing.T) {
	var words []dashscope.ASRWord
	for i := 0; i < 40; i++ {
		words = append(words, word("字", "", i*100, i*100+100))
	}
	for _, cue := range Segment(words) {
		if runes := len([]rune(cue.Text)); runes > maxCueRunes {
			t.Errorf("cue %d holds %d runes, over the %d budget", cue.Index, runes, maxCueRunes)
		}
	}
}

// A YAML-significant character in the source path must not corrupt the
// frontmatter of an exported markdown transcript.
func TestMarkdownFrontmatterIsValidYAML(t *testing.T) {
	rec := &run.Record{
		Meta:   run.Meta{SID: "abc", Source: "/tmp/a: b #1.wav", Model: "m"},
		Result: &dashscope.ASRResult{Text: "x。"},
	}
	md := renderMarkdown(rec)

	_, body, found := strings.Cut(strings.TrimPrefix(md, "---\n"), "---\n")
	if !found {
		t.Fatalf("no frontmatter delimiters: %q", md)
	}
	head, _, _ := strings.Cut(strings.TrimPrefix(md, "---\n"), "---\n")

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(head), &parsed); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v\n%s", err, head)
	}
	if parsed["source"] != "/tmp/a: b #1.wav" {
		t.Errorf("source round-tripped as %v", parsed["source"])
	}
	if !strings.Contains(body, "x。") {
		t.Errorf("body = %q", body)
	}
}
