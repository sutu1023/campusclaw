package skill

import "strings"

// Entry is one routing candidate. Only bound and enabled skills should be
// passed in; Route additionally skips entries marked disabled.
type Entry struct {
	ID          int64
	Name        string
	Trigger     string
	Subject     string
	Description string
	Tools       []string
	Enabled     bool
}

// triggerSeparators splits a skill's 适用条件 into keywords.
var triggerSeparators = func(r rune) bool {
	switch r {
	case ',', '，', '、', ';', '；', '|':
		return true
	}
	return false
}

// Route picks at most one skill for a question using deterministic lexical
// scoring: every trigger keyword contained in the (lower-cased) question scores
// one point; the highest score wins; ties go to the smaller skill id; a zero
// score selects nothing. It never calls a model and never does semantic match.
func Route(question string, candidates []Entry) *Entry {
	q := strings.ToLower(question)
	var best *Entry
	bestScore := 0
	for i := range candidates {
		c := candidates[i]
		if !c.Enabled {
			continue
		}
		score := 0
		for _, kw := range strings.FieldsFunc(c.Trigger, triggerSeparators) {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if kw == "" {
				continue
			}
			if strings.Contains(q, kw) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		if best == nil || score > bestScore || (score == bestScore && c.ID < best.ID) {
			cp := c
			best = &cp
			bestScore = score
		}
	}
	return best
}
