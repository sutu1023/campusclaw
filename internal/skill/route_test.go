package skill

import "testing"

func entry(id int64, trigger string) Entry {
	return Entry{ID: id, Name: "s", Trigger: trigger, Enabled: true}
}

func TestRouteHighestScore(t *testing.T) {
	candidates := []Entry{
		entry(1, "方程,去括号"),
		entry(2, "方程,去括号,分数"),
	}
	got := Route("这道方程要先去括号，再处理分数通分", candidates)
	if got == nil || got.ID != 2 {
		t.Fatalf("expected skill 2 (higher score), got %+v", got)
	}
}

func TestRouteTieBreaksToSmallerID(t *testing.T) {
	candidates := []Entry{
		entry(5, "方程"),
		entry(3, "方程"),
	}
	got := Route("方程怎么解", candidates)
	if got == nil || got.ID != 3 {
		t.Fatalf("expected smaller id 3 on tie, got %+v", got)
	}
}

func TestRouteZeroScoreSelectsNothing(t *testing.T) {
	candidates := []Entry{entry(1, "方程,去括号")}
	if got := Route("今天天气怎么样", candidates); got != nil {
		t.Fatalf("expected nil for zero score, got %+v", got)
	}
}

func TestRouteCaseFolding(t *testing.T) {
	candidates := []Entry{entry(1, "Equation")}
	if got := Route("How to solve an equation?", candidates); got == nil || got.ID != 1 {
		t.Fatalf("case folding failed, got %+v", got)
	}
}

func TestRouteSeparators(t *testing.T) {
	candidates := []Entry{entry(1, "方程，去括号、分数;通分；面积|行程")}
	// Any one of the separated keywords must be enough to score.
	for _, q := range []string{"通分怎么做", "行程问题", "面积单位"} {
		if got := Route(q, candidates); got == nil {
			t.Fatalf("separator keyword not matched for %q", q)
		}
	}
}

func TestRouteSkipsDisabled(t *testing.T) {
	candidates := []Entry{
		{ID: 1, Trigger: "方程", Enabled: false},
		entry(2, "方程"),
	}
	got := Route("方程", candidates)
	if got == nil || got.ID != 2 {
		t.Fatalf("disabled candidate must be skipped, got %+v", got)
	}
}

func TestRouteEmptyCandidates(t *testing.T) {
	if got := Route("方程", nil); got != nil {
		t.Fatalf("expected nil for no candidates, got %+v", got)
	}
}
