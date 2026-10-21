package chunk

import (
	"strings"
	"testing"
)

func TestAutoRuneOffsets(t *testing.T) {
	text := strings.Repeat("这是一个中文句子。", 40) // 8*40 = 320 runes
	chunks, err := Split(text, Options{Strategy: "auto"})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	runes := []rune(text)
	for _, c := range chunks {
		if got := string(runes[c.StartOffset:c.EndOffset]); got != c.Text {
			t.Fatalf("offset mismatch for chunk %d: %q vs %q", c.Index, got, c.Text)
		}
		if c.EndOffset > len(runes) || c.StartOffset < 0 {
			t.Fatalf("offset out of range: %+v", c)
		}
	}
}

func TestAutoDoesNotLoseText(t *testing.T) {
	text := strings.Repeat("段落一。\n\n段落二。\n\n段落三。\n\n", 60)
	chunks, err := Split(text, Options{Strategy: "auto"})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	// Every rune must be covered by at least one chunk.
	runes := []rune(text)
	covered := make([]bool, len(runes))
	for _, c := range chunks {
		for i := c.StartOffset; i < c.EndOffset; i++ {
			covered[i] = true
		}
	}
	for i, ok := range covered {
		if !ok {
			t.Fatalf("rune %d (%q) not covered", i, string(runes[i]))
		}
	}
}

func TestOverlapNotSmallerThanMax(t *testing.T) {
	if _, err := Split("hello world", Options{Strategy: "custom", MaxChars: 100, Overlap: 100}); err == nil {
		t.Fatal("expected error when overlap >= max")
	}
}

func TestEmptyAndWhitespace(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n\t "} {
		chunks, err := Split(in, Options{Strategy: "auto"})
		if err != nil {
			t.Fatalf("split %q: %v", in, err)
		}
		if len(chunks) != 0 {
			t.Fatalf("expected no chunks for %q, got %d", in, len(chunks))
		}
	}
}

func TestCustomForceCut(t *testing.T) {
	text := strings.Repeat("字", 250) // no punctuation, larger than custom max 200
	chunks, err := Split(text, Options{Strategy: "custom", MaxChars: 200, Overlap: 0})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].EndOffset != 200 {
		t.Fatalf("expected forced cut at 200, got %d", chunks[0].EndOffset)
	}
}

func TestPreprocessOnlyAffectsSplitInput(t *testing.T) {
	original := "见 [文档](https://example.com/a)  与   空格"
	chunks, err := Split(original, Options{Strategy: "custom", MaxChars: 100, Overlap: 0, CollapseWS: true, StripLinks: true})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if strings.Contains(chunks[0].Text, "https://") || strings.Contains(chunks[0].Text, "   ") {
		t.Fatalf("preprocess did not apply to split input: %q", chunks[0].Text)
	}
	if original != "见 [文档](https://example.com/a)  与   空格" {
		t.Fatal("original string must not be mutated")
	}
}

func TestHierarchyKeepsHeadings(t *testing.T) {
	text := "# 第一章\n内容甲。\n## 第一节\n内容乙。\n### 小节\n内容丙。"
	chunks, err := Split(text, Options{Strategy: "hierarchy"})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 sections, got %d: %+v", len(chunks), chunks)
	}
	if !strings.HasPrefix(chunks[0].Text, "# 第一章") {
		t.Fatalf("first chunk must keep its heading: %q", chunks[0].Text)
	}
	if !strings.Contains(chunks[1].Text, "## 第一节") || !strings.Contains(chunks[2].Text, "### 小节") {
		t.Fatalf("headings must stay inside their sections: %+v", chunks)
	}
	runes := []rune(text)
	for _, c := range chunks {
		if got := string(runes[c.StartOffset:c.EndOffset]); got != c.Text {
			t.Fatalf("offset mismatch: %q vs %q", got, c.Text)
		}
	}
}
