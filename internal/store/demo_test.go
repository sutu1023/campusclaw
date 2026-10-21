package store

import (
	"context"
	"strings"
	"testing"
)

func TestSeedDemoCorpus(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	totals := map[int64]int{}
	for _, classID := range []int64{1, 2} {
		materials, total, err := st.ListMaterials(ctx, classID, 1, 100, "")
		if err != nil {
			t.Fatalf("list class %d: %v", classID, err)
		}
		if total < 2 {
			t.Fatalf("class %d must have >=2 demo materials, got %d", classID, total)
		}
		totals[classID] = total
		for _, m := range materials {
			entry, err := st.GetEntryByMaterial(ctx, classID, m.ID)
			if err != nil {
				t.Fatalf("entry for material %d: %v", m.ID, err)
			}
			if strings.TrimSpace(entry.BodyText) == "" {
				t.Fatalf("material %d must have non-empty body", m.ID)
			}
		}
	}

	// Seeding again must not add duplicate materials or entries.
	if err := st.Seed(ctx); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	for _, classID := range []int64{1, 2} {
		_, after, err := st.ListMaterials(ctx, classID, 1, 100, "")
		if err != nil {
			t.Fatalf("list after reseed: %v", err)
		}
		if after != totals[classID] {
			t.Fatalf("seed not idempotent for class %d: %d -> %d", classID, totals[classID], after)
		}
	}
}
