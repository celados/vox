// Package export renders a stored run. Cue segmentation happens here because
// the recognition API does not segment: a multi-sentence take comes back as one
// sentence spanning the whole file, both non-streaming and over SSE. The only
// segmentation signal available is per-word punctuation and inter-word gaps.
package export

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/run"
)

// Cue conventions. Fixed rather than exposed as flags: these are subtitle
// typesetting norms, not per-call decisions.
const (
	maxCueRunes    = 28
	maxCueDuration = 6 * time.Second
	// gapSplit is the inter-word silence that ends a cue even mid-clause.
	gapSplit = 400 * time.Millisecond
)

// terminators end a cue outright; separators end one only if it is long enough.
const (
	terminators = "。！？!?…"
	separators  = "，、；：,;:"
)

type Cue struct {
	Index int
	Begin time.Duration
	End   time.Duration
	Text  string
}

// Segment groups words into subtitle cues.
func Segment(words []dashscope.ASRWord) []Cue {
	var cues []Cue
	var buf strings.Builder
	var begin, end time.Duration
	var runeCount int
	started := false

	flush := func() {
		text := strings.TrimSpace(buf.String())
		if text == "" {
			buf.Reset()
			started = false
			runeCount = 0
			return
		}
		cues = append(cues, Cue{Index: len(cues) + 1, Begin: begin, End: end, Text: text})
		buf.Reset()
		started = false
		runeCount = 0
	}

	for i, w := range words {
		wordBegin := time.Duration(w.BeginTime) * time.Millisecond
		wordEnd := time.Duration(w.EndTime) * time.Millisecond

		// A long silence before this word closes the pending cue.
		if started && wordBegin-end >= gapSplit {
			flush()
		}
		if !started {
			begin, started = wordBegin, true
		}
		buf.WriteString(w.Text)
		buf.WriteString(w.Punctuation)
		runeCount += len([]rune(w.Text))
		end = wordEnd

		last := i == len(words)-1
		switch {
		case last:
			flush()
		case strings.ContainsAny(w.Punctuation, terminators):
			flush()
		case strings.ContainsAny(w.Punctuation, separators) && runeCount >= maxCueRunes/2:
			flush()
		case runeCount >= maxCueRunes:
			flush()
		case end-begin >= maxCueDuration:
			flush()
		}
	}
	if started {
		flush()
	}
	return cues
}

// Render produces the requested format. Formats are the whole contract; adding
// one here is a surface change.
func Render(rec *run.Record, format string) (string, error) {
	switch format {
	case "txt":
		return rec.Result.Text + "\n", nil
	case "json":
		data, err := json.MarshalIndent(rec, "", "  ")
		return string(data) + "\n", err
	case "md":
		return renderMarkdown(rec), nil
	case "srt":
		return renderSRT(Segment(rec.Result.Words)), nil
	case "vtt":
		return renderVTT(Segment(rec.Result.Words)), nil
	default:
		return "", fmt.Errorf("unknown format %q", format)
	}
}

func renderMarkdown(rec *run.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntype: Transcript\nsid: %s\nsource: %s\nmodel: %s\n", rec.Meta.SID, rec.Meta.Source, rec.Meta.Model)
	if rec.Meta.Vocab != "" {
		fmt.Fprintf(&b, "vocab: %s\n", rec.Meta.Vocab)
	}
	fmt.Fprintf(&b, "duration: %s\ngenerated: { by: vox, at: %s }\n---\n\n",
		formatDuration(time.Duration(rec.Meta.Size.Duration)*time.Second),
		rec.Meta.Created.UTC().Format(time.RFC3339))

	// Paragraphs follow the same cue segmentation, merged until a sentence ends,
	// so prose stays readable instead of one wall of text.
	var para strings.Builder
	for _, cue := range Segment(rec.Result.Words) {
		para.WriteString(cue.Text)
		if strings.ContainsAny(lastRune(cue.Text), terminators) {
			b.WriteString(para.String())
			b.WriteString("\n\n")
			para.Reset()
		}
	}
	if para.Len() > 0 {
		b.WriteString(para.String())
		b.WriteString("\n")
	}
	return b.String()
}

func renderSRT(cues []Cue) string {
	var b strings.Builder
	for _, cue := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n",
			cue.Index, srtTime(cue.Begin), srtTime(cue.End), cue.Text)
	}
	return b.String()
}

func renderVTT(cues []Cue) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, cue := range cues {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n",
			vttTime(cue.Begin), vttTime(cue.End), cue.Text)
	}
	return b.String()
}

func srtTime(d time.Duration) string {
	h, m, s, ms := splitDuration(d)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func vttTime(d time.Duration) string {
	h, m, s, ms := splitDuration(d)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func splitDuration(d time.Duration) (h, m, s, ms int) {
	total := int(d.Milliseconds())
	return total / 3600000, total / 60000 % 60, total / 1000 % 60, total % 1000
}

func formatDuration(d time.Duration) string {
	h, m, s, _ := splitDuration(d)
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func lastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}
