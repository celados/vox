package export

import (
	"strings"
	"testing"
	"time"

	"github.com/celados/vox/internal/dashscope"
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
