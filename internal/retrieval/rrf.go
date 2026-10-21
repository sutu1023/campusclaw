// Package retrieval implements keyword, vector and hybrid search.
package retrieval

import "sort"

// RankFuse combines ranked id lists with Reciprocal Rank Fusion: each list
// contributes 1/(k + rank) with rank starting at 1. Raw scores are never added.
// The surviving order is by descending fused score, with first-seen order as a
// stable tie-breaker.
func RankFuse(k int, lists ...[]int64) []int64 {
	if k <= 0 {
		k = 60
	}
	scores := make(map[int64]float64)
	var order []int64
	for _, list := range lists {
		for i, id := range list {
			if _, seen := scores[id]; !seen {
				order = append(order, id)
			}
			scores[id] += 1.0 / float64(k+i+1)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return scores[order[a]] > scores[order[b]]
	})
	return order
}
