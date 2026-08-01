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
	"gopkg.in/yaml.v3"
)

// Cue conventions. Fixed rather than exposed as flags: these are subtitle
// typesetting norms, not per-call decisions.
const (
	maxCueRunes    = 28
	maxCueDuration = 6 * time.Second
	// gapSplit is the inter-word silence that ends a cue even mid-clause.
	gapSplit = 400 * time.Millisecond
	// minWordMillis gives a zero-length word enough span to be a valid cue.
	minWordMillis = 40
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
	words = sanitize(words)

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
		// Close before appending too, so one long word cannot silently blow past
		// the budget for the words already buffered.
		if started && (runeCount+len([]rune(w.Text)) > maxCueRunes || wordEnd-begin > maxCueDuration) {
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

// sanitize makes the timeline monotonic and non-degenerate. Word timings come
// from the service; a cue whose end precedes its begin produces a subtitle file
// that players reject outright, so it is repaired here rather than emitted.
func sanitize(words []dashscope.ASRWord) []dashscope.ASRWord {
	out := make([]dashscope.ASRWord, 0, len(words))
	prevEnd := 0
	for _, w := range words {
		if strings.TrimSpace(w.Text) == "" && w.Punctuation == "" {
			continue
		}
		if w.BeginTime < prevEnd {
			w.BeginTime = prevEnd
		}
		if w.EndTime <= w.BeginTime {
			w.EndTime = w.BeginTime + minWordMillis
		}
		prevEnd = w.EndTime
		out = append(out, w)
	}
	return out
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
		return renderSRT(cues(rec)), nil
	case "vtt":
		return renderVTT(cues(rec)), nil
	default:
		return "", fmt.Errorf("unknown format %q", format)
	}
}

// cues falls back to a single whole-run cue when the service returned text
// without word timings — an unsegmented subtitle still beats an empty file.
func cues(rec *run.Record) []Cue {
	if segmented := Segment(rec.Result.Words); len(segmented) > 0 {
		return segmented
	}
	if strings.TrimSpace(rec.Result.Text) == "" {
		return nil
	}
	return []Cue{{
		Index: 1,
		Begin: 0,
		End:   time.Duration(rec.Meta.Size.Duration) * time.Second,
		Text:  rec.Result.Text,
	}}
}

// frontmatter is marshalled rather than formatted: a source path containing a
// colon or a hash would otherwise produce a document that is not valid YAML.
type frontmatter struct {
	Type      string            `yaml:"type"`
	SID       string            `yaml:"sid"`
	Source    string            `yaml:"source"`
	Model     string            `yaml:"model"`
	Vocab     string            `yaml:"vocab,omitempty"`
	Duration  string            `yaml:"duration"`
	Generated map[string]string `yaml:"generated"`
}

func renderMarkdown(rec *run.Record) string {
	head, err := yaml.Marshal(frontmatter{
		Type:     "Transcript",
		SID:      rec.Meta.SID,
		Source:   rec.Meta.Source,
		Model:    rec.Meta.Model,
		Vocab:    rec.Meta.Vocab,
		Duration: formatDuration(time.Duration(rec.Meta.Size.Duration) * time.Second),
		Generated: map[string]string{
			"by": "vox",
			"at": rec.Meta.Created.UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return rec.Result.Text + "\n"
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(head)
	b.WriteString("---\n\n")

	segmented := Segment(rec.Result.Words)
	if len(segmented) == 0 {
		// No word timings: the transcript is still the point of the document.
		b.WriteString(strings.TrimSpace(rec.Result.Text))
		b.WriteString("\n")
		return b.String()
	}

	// Paragraphs follow the same cue segmentation, merged until a sentence ends,
	// so prose stays readable instead of one wall of text.
	var para strings.Builder
	for _, cue := range segmented {
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
