// Package chunk splits a knowledge entry's body text into retrievable slices.
//
// All offsets are rune offsets into the text actually handed to the splitter.
// When preprocessing (whitespace folding / link stripping) is enabled the input
// is rewritten first, so offsets refer to the preprocessed text, never to the
// original body_text (which is always preserved in MySQL).
package chunk

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Options controls the split strategy.
type Options struct {
	Strategy   string // auto | custom | hierarchy
	MaxChars   int    // custom: 100..2000
	Overlap    int    // custom: absolute rune overlap
	CollapseWS bool
	StripLinks bool
}

// Chunk is one slice with rune offsets.
type Chunk struct {
	Index       int
	StartOffset int
	EndOffset   int
	Text        string
}

// Defaults for the automatic window strategy.
const (
	DefaultMaxChars = 800
	DefaultOverlap  = 80

	MinCustomMaxChars = 100
	MaxCustomMaxChars = 2000
)

// ErrInvalidOverlap is returned when overlap is not smaller than the window.
var ErrInvalidOverlap = errors.New("overlap must be smaller than max chars")

var (
	whitespaceRE = regexp.MustCompile(`\s+`)
	mdLinkRE     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	urlRE        = regexp.MustCompile(`https?://\S+`)
)

// Split slices text according to opts.
func Split(text string, opts Options) ([]Chunk, error) {
	strategy := opts.Strategy
	if strategy == "" {
		strategy = "auto"
	}
	processed := preprocess(text, opts)

	switch strategy {
	case "auto":
		return window(processed, DefaultMaxChars, DefaultOverlap)
	case "custom":
		max := opts.MaxChars
		if max < MinCustomMaxChars || max > MaxCustomMaxChars {
			max = DefaultMaxChars
		}
		overlap := opts.Overlap
		if overlap < 0 {
			overlap = 0
		}
		return window(processed, max, overlap)
	case "hierarchy":
		return splitHierarchy(processed, DefaultMaxChars, DefaultOverlap)
	default:
		return nil, fmt.Errorf("unknown chunk strategy %q", strategy)
	}
}

func preprocess(text string, opts Options) string {
	if opts.CollapseWS {
		text = whitespaceRE.ReplaceAllString(text, " ")
	}
	if opts.StripLinks {
		text = mdLinkRE.ReplaceAllString(text, "$1")
		text = urlRE.ReplaceAllString(text, "")
	}
	return text
}

// window cuts text into overlapping windows of at most max runes, preferring to
// break at a newline or sentence end. Empty / whitespace-only input yields no
// chunks.
func window(text string, max, overlap int) ([]Chunk, error) {
	if overlap >= max {
		return nil, ErrInvalidOverlap
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	runes := []rune(text)
	n := len(runes)

	var chunks []Chunk
	start := 0
	idx := 0
	for start < n {
		end := start + max
		if end > n {
			end = n
		}
		if end < n {
			if cut := findBreak(runes, start+max/2, end); cut > start {
				end = cut
			}
		}
		chunks = append(chunks, Chunk{
			Index:       idx,
			StartOffset: start,
			EndOffset:   end,
			Text:        string(runes[start:end]),
		})
		idx++
		if end >= n {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks, nil
}

// findBreak scans backwards for the nearest newline or sentence end. It never
// returns below min.
func findBreak(runes []rune, min, max int) int {
	if min < 0 {
		min = 0
	}
	for i := max - 1; i >= min; i-- {
		switch runes[i] {
		case '\n', '。', '！', '？', '.', '!', '?':
			return i + 1
		}
	}
	return max
}

// splitHierarchy splits at Markdown headings (#, ##, ###) keeping the heading
// inside its section; overlong sections fall back to the window strategy.
func splitHierarchy(text string, max, overlap int) ([]Chunk, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	runes := []rune(text)
	headings := headingOffsets(text)

	var chunks []Chunk
	idx := 0
	appendSection := func(start, end int) error {
		if start >= end {
			return nil
		}
		if end-start <= max {
			chunks = append(chunks, Chunk{
				Index: idx, StartOffset: start, EndOffset: end, Text: string(runes[start:end]),
			})
			idx++
			return nil
		}
		sub, err := window(string(runes[start:end]), max, overlap)
		if err != nil {
			return err
		}
		for _, s := range sub {
			chunks = append(chunks, Chunk{
				Index:       idx,
				StartOffset: start + s.StartOffset,
				EndOffset:   start + s.EndOffset,
				Text:        s.Text,
			})
			idx++
		}
		return nil
	}

	prev := 0
	for _, h := range headings {
		if h > prev {
			if err := appendSection(prev, h); err != nil {
				return nil, err
			}
		}
		if h > prev {
			prev = h
		}
	}
	if err := appendSection(prev, len(runes)); err != nil {
		return nil, err
	}
	return chunks, nil
}

func headingOffsets(text string) []int {
	var offs []int
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSuffix(line, "\n")
		if isHeading([]rune(trimmed)) {
			offs = append(offs, offset)
		}
		offset += len([]rune(line))
	}
	return offs
}

func isHeading(line []rune) bool {
	if len(line) < 2 || line[0] != '#' {
		return false
	}
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n < 1 || n > 3 {
		return false
	}
	return n < len(line) && (line[n] == ' ' || line[n] == '\t')
}
