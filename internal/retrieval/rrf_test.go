package retrieval

import "testing"

func TestRankFuseOrdersByFusedScore(t *testing.T) {
	// 5 appears in both lists and must rank first.
	keyword := []int64{1, 5, 3}
	vector := []int64{5, 2, 9}

	got := RankFuse(60, keyword, vector)
	if len(got) != 5 {
		t.Fatalf("expected 5 unique ids, got %d: %v", len(got), got)
	}
	if got[0] != 5 {
		t.Fatalf("both-list id must rank first, got %v", got)
	}
	// 1 and 2 are each in one list at rank 1/2; keyword rank1 beats vector rank2.
	if got[1] != 1 {
		t.Fatalf("expected id 1 second (rank 1 in keyword), got %v", got)
	}
}

func TestRankFuseUsesRanksNotScores(t *testing.T) {
	// An id present in two lists must outrank a rank-1 id from a single list:
	// RRF fuses ranks, it never adds raw similarity scores.
	got := RankFuse(60, []int64{1, 2}, []int64{2})
	if got[0] != 2 {
		t.Fatalf("overlap must win under RRF, got %v", got)
	}
}

func TestRankFuseMissingListContributesNothing(t *testing.T) {
	got := RankFuse(60, []int64{4, 2}, nil)
	if len(got) != 2 || got[0] != 4 || got[1] != 2 {
		t.Fatalf("order must follow the single non-empty list, got %v", got)
	}
}
