package retrieval

import "testing"

func TestFilterBySubject(t *testing.T) {
	hits := []Hit{
		{MaterialTitle: "A 班-数学自学笔记"},
		{MaterialTitle: "A 班-语文自学笔记"},
		{MaterialTitle: "课堂提问技巧"},
	}

	math := FilterBySubject(hits, "数学")
	if len(math) != 1 || math[0].MaterialTitle != "A 班-数学自学笔记" {
		t.Fatalf("expected only the math material, got %+v", math)
	}

	none := FilterBySubject(hits, "英语")
	if len(none) != 0 {
		t.Fatalf("expected no matches, got %+v", none)
	}

	all := FilterBySubject(hits, "")
	if len(all) != len(hits) {
		t.Fatalf("empty subject must not filter, got %d", len(all))
	}
}
