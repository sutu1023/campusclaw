package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"campusclaw/internal/chunk"
	"campusclaw/internal/embed"
	"campusclaw/internal/indexer"
)

func TestExamplesEndpoint(t *testing.T) {
	env := setupEnv(t)

	resp, _ := env.do(t, http.MethodGet, "/api/examples", "", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("examples must require auth, got %d", resp.StatusCode)
	}

	token := env.login(t, "student_a1", "CampusClaw#2026")
	resp, data := env.do(t, http.MethodGet, "/api/examples", token, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("examples status %d body=%s", resp.StatusCode, data)
	}
	var out struct {
		Examples []struct {
			Text       string `json:"text"`
			NoEvidence bool   `json:"no_evidence"`
		} `json:"examples"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode examples: %v", err)
	}
	if len(out.Examples) < 3 {
		t.Fatalf("expected >=3 examples, got %d", len(out.Examples))
	}
	noEvidence := 0
	for _, e := range out.Examples {
		if e.Text == "" {
			t.Fatal("example text must not be empty")
		}
		if e.NoEvidence {
			noEvidence++
		}
	}
	if noEvidence != 1 {
		t.Fatalf("expected exactly 1 no_evidence example, got %d", noEvidence)
	}
}

func TestDemoCorpusSearchModes(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "student_a1", "CampusClaw#2026")

	cases := []struct{ mode, q string }{
		{"keyword", "勾股"},
		{"vector", "勾股定理的内容是什么？"},
		{"hybrid", "分数的意义是什么？"},
	}
	for _, c := range cases {
		out := env.search(t, token, c.q, c.mode)
		hits, _ := out["hits"].([]any)
		if len(hits) == 0 {
			t.Fatalf("mode %s query %q returned no hits: %v", c.mode, c.q, out)
		}
	}
}

func TestDemoClassIsolation(t *testing.T) {
	env := setupEnv(t)
	tokenA := env.login(t, "student_a1", "CampusClaw#2026")

	for _, q := range []string{"红叶", "圆周率"} {
		out := env.search(t, tokenA, q, "hybrid")
		if hits, _ := out["hits"].([]any); len(hits) != 0 {
			t.Fatalf("class-A session must not see class-B content via %q: %v", q, out)
		}
	}
}

func TestAskExamplesWithAndWithoutEvidence(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "student_a1", "CampusClaw#2026")

	before := atomic.LoadInt64(env.chatCalls)
	body, _ := json.Marshal(map[string]any{"q": "课堂提问有哪些有效技巧？"})
	resp, data := env.do(t, http.MethodPost, "/api/ask", token, body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ask status %d body=%s", resp.StatusCode, data)
	}
	var ok struct {
		Citations []any `json:"citations"`
	}
	_ = json.Unmarshal(data, &ok)
	if len(ok.Citations) == 0 {
		t.Fatalf("answerable example must produce citations: %s", data)
	}
	if atomic.LoadInt64(env.chatCalls) <= before {
		t.Fatal("answerable ask must call the chat gateway")
	}

	callsAfter := atomic.LoadInt64(env.chatCalls)
	ne, _ := json.Marshal(map[string]any{"q": "今天天气怎么样？"})
	resp, data = env.do(t, http.MethodPost, "/api/ask", token, ne, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no-evidence ask status %d", resp.StatusCode)
	}
	var no struct {
		Status    string `json:"status"`
		Citations []any  `json:"citations"`
	}
	_ = json.Unmarshal(data, &no)
	if no.Status != "no_evidence" || len(no.Citations) != 0 {
		t.Fatalf("expected no_evidence with empty citations: %s", data)
	}
	if atomic.LoadInt64(env.chatCalls) != callsAfter {
		t.Fatal("no-evidence ask must not call the chat gateway")
	}
}

func TestEnsureMaterialIsIdempotent(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()

	materials, _, err := env.store.ListMaterials(ctx, 1, 1, 50, "A 班-勾股定理教案")
	if err != nil || len(materials) == 0 {
		t.Fatalf("find demo material: %v", err)
	}
	id := materials[0].ID

	old, err := env.store.DeleteChunksByMaterial(ctx, id)
	if err != nil {
		t.Fatalf("delete chunks: %v", err)
	}
	if err := env.indexer.Vector.Delete(ctx, old); err != nil {
		t.Fatalf("delete vectors: %v", err)
	}

	if _, err := env.indexer.EnsureMaterial(ctx, 1, id, chunk.Options{}); err != nil {
		t.Fatalf("ensure material: %v", err)
	}
	first, _ := env.store.ListChunksByMaterial(ctx, 1, id)
	if len(first) == 0 {
		t.Fatal("expected chunks after ensure")
	}
	for _, c := range first {
		if c.IndexStatus != "ready" {
			t.Fatalf("expected ready chunk, got %s", c.IndexStatus)
		}
	}

	if _, err := env.indexer.EnsureMaterial(ctx, 1, id, chunk.Options{}); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	second, _ := env.store.ListChunksByMaterial(ctx, 1, id)
	if len(second) != len(first) {
		t.Fatalf("EnsureMaterial duplicated chunks: %d -> %d", len(first), len(second))
	}
}

func TestIndexDegradesWhenGatewayUnavailable(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()

	materials, _, err := env.store.ListMaterials(ctx, 1, 1, 50, "A 班-分数教学教研笔记")
	if err != nil || len(materials) == 0 {
		t.Fatalf("find demo material: %v", err)
	}
	id := materials[0].ID
	old, _ := env.store.DeleteChunksByMaterial(ctx, id)
	_ = env.indexer.Vector.Delete(ctx, old)

	// A gateway-less indexer must fail without duplicating or dropping chunks.
	broken := &indexer.Indexer{
		Store:  env.store,
		Embed:  embed.New("", "", "", testDim),
		Vector: env.indexer.Vector,
		Model:  "stub",
	}
	if _, err := broken.EnsureMaterial(ctx, 1, id, chunk.Options{}); err == nil {
		t.Fatal("expected indexing to fail without a configured gateway")
	}
	views, _ := env.store.ListChunksByMaterial(ctx, 1, id)
	if len(views) == 0 {
		t.Fatal("chunks must be retained when embedding fails")
	}
	for _, v := range views {
		if v.IndexStatus != "failed" {
			t.Fatalf("expected failed chunk, got %s", v.IndexStatus)
		}
	}

	// Recovery: the working indexer (backfill path) turns them ready.
	if _, err := env.indexer.EnsureMaterial(ctx, 1, id, chunk.Options{}); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	views, _ = env.store.ListChunksByMaterial(ctx, 1, id)
	for _, v := range views {
		if v.IndexStatus != "ready" {
			t.Fatalf("expected ready after recovery, got %s", v.IndexStatus)
		}
	}
}
