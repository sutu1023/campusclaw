package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"campusclaw/internal/auth"
	"campusclaw/internal/chat"
	"campusclaw/internal/chunk"
	"campusclaw/internal/embed"
	"campusclaw/internal/indexer"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/store"
	"campusclaw/internal/vector"
)

const testDim = 5

// stubEmbed maps text onto a 5-dim topic space so similarity is deterministic:
// A=勾股, B=红叶, C=圆, D=青竹, Z=everything else (orthogonal to the rest).
func stubEmbed(text string) []float32 {
	switch {
	case strings.Contains(text, "勾股"):
		return []float32{1, 0, 0, 0, 0}
	case strings.Contains(text, "红叶"):
		return []float32{0, 1, 0, 0, 0}
	case strings.Contains(text, "圆"):
		return []float32{0, 0, 1, 0, 0}
	case strings.Contains(text, "青竹"):
		return []float32{0, 0, 0, 1, 0}
	default:
		return []float32{0, 0, 0, 0, 1}
	}
}

type testEnv struct {
	server        *httptest.Server
	api           *Server
	store         *store.Store
	indexer       *indexer.Indexer
	chatCalls     *int64
	chatMessages  *atomic.Pointer[[]chat.Message]
	chatFailAfter *atomic.Int64
	qdrantURL     string
	collection    string
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	qdrantURL := os.Getenv("TEST_QDRANT_URL")
	if dsn == "" || qdrantURL == "" {
		t.Skip("TEST_DB_DSN / TEST_QDRANT_URL not set; skipping HTTP integration test")
	}
	ctx := context.Background()

	st, err := store.Open(ctx, dsn, 60*time.Second)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// Child-first order preserves FK checks on every pooled connection.
	if _, err := st.DB.ExecContext(ctx, `DROP TABLE IF EXISTS mcp_tool_calls, mcp_discovery_changes, mcp_discovery_snapshots, mcp_tools, mcp_servers, mcp_conversation_usage, messages, conversations, tutor_agent_materials, tutor_agent_skills, skills, tutor_agents, knowledge_chunks, knowledge_entries, materials, sessions, users, classes`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := st.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var chatCalls int64
	var captured atomic.Pointer[[]chat.Message]
	failAfter := &atomic.Int64{}
	failAfter.Store(-1)
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		type item struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		out := struct {
			Data []item `json:"data"`
		}{}
		for i, s := range req.Input {
			out.Data = append(out.Data, item{Embedding: stubEmbed(s), Index: i})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&chatCalls, 1)
		var req struct {
			Messages []chat.Message `json:"messages"`
			Stream   bool           `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		captured.Store(&req.Messages)
		answer := "资料中未找到相关内容[9]"
		for _, m := range req.Messages {
			if m.Role == "user" && strings.Contains(m.Content, "[1] 材料") {
				answer = "依据资料[1]，勾股定理描述直角三角形三边关系。[1]"
			}
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			limit := failAfter.Load()
			for i, frag := range splitAnswer(answer) {
				if limit >= 0 && int64(i) >= limit {
					_, _ = w.Write([]byte("data: {\"error\":\"stub failure\"}\n\n"))
					flusher.Flush()
					return
				}
				payload, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": frag}}}})
				_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
				flusher.Flush()
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": answer}}},
		})
	}))

	collection := fmt.Sprintf("campusclaw_test_%d", time.Now().UnixNano())
	vec := vector.New(qdrantURL, collection, testDim)
	if err := vec.EnsureCollection(ctx); err != nil {
		t.Fatalf("ensure collection: %v", err)
	}
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, strings.TrimRight(qdrantURL, "/")+"/collections/"+collection, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
		embedSrv.Close()
		chatSrv.Close()
		_ = st.Close()
	})

	emb := embed.New(embedSrv.URL, "", "stub", testDim)
	idx := &indexer.Indexer{Store: st, Embed: emb, Vector: vec, Model: "stub"}
	ret := &retrieval.Service{Store: st, Embed: emb, Vector: vec, MinScore: 0.15, RRFK: 60}

	// Index the seeded materials so both classes have searchable chunks.
	indexSeeded(t, ctx, st, idx)

	s := &Server{
		Store:          st,
		TokenSecret:    "test-secret",
		Auth:           &auth.Handler{Store: st, TokenSecret: "test-secret", SessionTTL: time.Hour},
		Retrieval:      ret,
		Chat:           chat.New(chatSrv.URL, "", "stub"),
		Indexer:        idx,
		UploadDir:      t.TempDir(),
		MaxUploadBytes: 1 << 20,
		EmbeddingDim:   testDim,
	}
	ts := httptest.NewServer(s.Router())
	return &testEnv{server: ts, api: s, store: st, indexer: idx, chatCalls: &chatCalls,
		chatMessages: &captured, chatFailAfter: failAfter, qdrantURL: qdrantURL, collection: collection}
}

func indexSeeded(t *testing.T, ctx context.Context, st *store.Store, idx *indexer.Indexer) {
	t.Helper()
	for _, classID := range []int64{1, 2} {
		materials, _, err := st.ListMaterials(ctx, classID, 1, 100, "")
		if err != nil {
			t.Fatalf("list seeded: %v", err)
		}
		for _, m := range materials {
			if _, err := idx.IndexMaterial(ctx, classID, m.ID, chunk.Options{}); err != nil {
				t.Fatalf("index seeded material %d: %v", m.ID, err)
			}
		}
	}
}

func (e *testEnv) login(t *testing.T, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := http.Post(e.server.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Token == "" {
		t.Fatal("login returned no token")
	}
	return out.Token
}

func (e *testEnv) do(t *testing.T, method, path, token string, body []byte, contentType string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, e.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestLoginMeLogout(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "teacher_a", "CampusClaw#2026")

	resp, data := env.do(t, http.MethodGet, "/api/me", token, nil, "")
	if resp.StatusCode != http.StatusOK || !bytes.Contains(data, []byte(`"teacher"`)) {
		t.Fatalf("me: status=%d body=%s", resp.StatusCode, data)
	}

	resp, _ = env.do(t, http.MethodPost, "/api/logout", token, []byte(`{}`), "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status %d", resp.StatusCode)
	}
	resp, _ = env.do(t, http.MethodGet, "/api/me", token, nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token must be revoked after logout, got %d", resp.StatusCode)
	}
}

func TestNoTokenReturns401(t *testing.T) {
	env := setupEnv(t)
	resp, data := env.do(t, http.MethodGet, "/api/materials", "", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if bytes.Contains(data, []byte("材料")) || bytes.Contains(data, []byte("title")) {
		t.Fatalf("401 body must not leak data: %s", data)
	}
}

func TestLoginFailureIsUniform(t *testing.T) {
	env := setupEnv(t)
	unknown, _ := json.Marshal(map[string]string{"username": "nobody", "password": "x"})
	wrong, _ := json.Marshal(map[string]string{"username": "teacher_a", "password": "x"})
	resp1, body1 := env.do(t, http.MethodPost, "/api/login", "", unknown, "application/json")
	resp2, body2 := env.do(t, http.MethodPost, "/api/login", "", wrong, "application/json")
	if resp1.StatusCode != resp2.StatusCode || !bytes.Equal(body1, body2) {
		t.Fatalf("login failures must be uniform: %d/%s vs %d/%s", resp1.StatusCode, body1, resp2.StatusCode, body2)
	}
}

func TestStudentUploadForbidden(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "student_a1", "CampusClaw#2026")

	before, _, _ := env.store.ListMaterials(context.Background(), 1, 1, 100, "")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "note.md")
	_, _ = fw.Write([]byte("勾股定理"))
	_ = mw.Close()

	resp, _ := env.do(t, http.MethodPost, "/api/materials", token, buf.Bytes(), mw.FormDataContentType())
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("student upload must be 403, got %d", resp.StatusCode)
	}
	after, _, _ := env.store.ListMaterials(context.Background(), 1, 1, 100, "")
	if len(after) != len(before) {
		t.Fatalf("forbidden upload changed material count: %d -> %d", len(before), len(after))
	}
}

func TestCrossClassMaterialIs404(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "teacher_a", "CampusClaw#2026")

	classB, _, err := env.store.ListMaterials(context.Background(), 2, 1, 10, "")
	if err != nil || len(classB) == 0 {
		t.Fatalf("expected class B material: %v", err)
	}
	resp, data := env.do(t, http.MethodGet, fmt.Sprintf("/api/materials/%d", classB[0].ID), token, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-class access must be 404, got %d", resp.StatusCode)
	}
	if bytes.Contains(data, []byte("B 班")) {
		t.Fatalf("404 body must not leak class B title: %s", data)
	}
}

func (e *testEnv) uploadTeacher(t *testing.T, token, title, body string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", title)
	fw, _ := mw.CreateFormFile("file", "material.md")
	_, _ = fw.Write([]byte(body))
	_ = mw.Close()
	resp, data := e.do(t, http.MethodPost, "/api/materials", token, buf.Bytes(), mw.FormDataContentType())
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status %d body=%s", resp.StatusCode, data)
	}
}

func (e *testEnv) search(t *testing.T, token, q, mode string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"q": q, "mode": mode, "limit": 10})
	resp, data := e.do(t, http.MethodPost, "/api/search", token, body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status %d body=%s", resp.StatusCode, data)
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func TestReindexReplacesChunks(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "teacher_a", "CampusClaw#2026")
	env.uploadTeacher(t, token, "重建材料", strings.Repeat("勾股定理的证明与应用。", 30))

	materials, _, err := env.store.ListMaterials(context.Background(), 1, 1, 50, "重建材料")
	if err != nil || len(materials) == 0 {
		t.Fatalf("find uploaded material: %v", err)
	}
	materialID := materials[0].ID

	before := env.vectors(t, token, materialID)
	if len(before) == 0 {
		t.Fatal("expected initial chunks")
	}
	oldIDs := make([]int64, 0, len(before))
	for _, c := range before {
		oldIDs = append(oldIDs, c.ChunkID)
	}

	// Rebuild with a much smaller window so the chunk count must change.
	reindex, _ := json.Marshal(map[string]any{"chunk_strategy": "custom", "chunk_max_chars": 100, "chunk_overlap_percent": 0})
	resp, data := env.do(t, http.MethodPost, fmt.Sprintf("/api/materials/%d/reindex", materialID), token, reindex, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reindex status %d body=%s", resp.StatusCode, data)
	}

	after := env.vectors(t, token, materialID)
	if len(after) <= len(before) {
		t.Fatalf("expected more chunks after smaller window: %d -> %d", len(before), len(after))
	}
	newIDs := map[int64]bool{}
	for _, c := range after {
		newIDs[c.ChunkID] = true
	}
	for _, id := range oldIDs {
		if newIDs[id] {
			t.Fatalf("old chunk id %d survived reindex", id)
		}
		// The old Qdrant point must be gone.
		if env.qdrantPointExists(t, id) {
			t.Fatalf("old qdrant point %d was not deleted", id)
		}
	}
}

type vectorChunk struct {
	ChunkID     int64 `json:"chunk_id"`
	ChunkIndex  int   `json:"chunk_index"`
	StartOffset int   `json:"start_offset"`
	EndOffset   int   `json:"end_offset"`
}

func (e *testEnv) vectors(t *testing.T, token string, materialID int64) []vectorChunk {
	t.Helper()
	resp, data := e.do(t, http.MethodGet, fmt.Sprintf("/api/materials/%d/vectors", materialID), token, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("vectors status %d body=%s", resp.StatusCode, data)
	}
	var out struct {
		Chunks []vectorChunk `json:"chunks"`
	}
	_ = json.Unmarshal(data, &out)
	return out.Chunks
}

func (e *testEnv) qdrantPointExists(t *testing.T, id int64) bool {
	t.Helper()
	url := fmt.Sprintf("%s/collections/%s/points/%d", strings.TrimRight(e.qdrantURL, "/"), e.collection, id)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("qdrant get point: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestUploadAndThreeSearchModes(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "teacher_a", "CampusClaw#2026")
	env.uploadTeacher(t, token, "A 班补充勾股材料", "勾股定理的证明与应用。")

	for _, mode := range []string{"keyword", "vector", "hybrid"} {
		out := env.search(t, token, "勾股", mode)
		hits, _ := out["hits"].([]any)
		if len(hits) == 0 {
			t.Fatalf("mode %s returned no hits: %v", mode, out)
		}
		first, _ := hits[0].(map[string]any)
		for _, field := range []string{"chunk_id", "material_title", "chunk_index", "start_offset", "end_offset", "snippet"} {
			if _, ok := first[field]; !ok {
				t.Fatalf("mode %s hit missing %s: %v", mode, field, first)
			}
		}
	}
}

func TestSearchClassIsolation(t *testing.T) {
	env := setupEnv(t)
	tokenA := env.login(t, "student_a1", "CampusClaw#2026")

	// A class-A session querying a class-B-only term, with a class_id injected
	// into the body, must still see nothing.
	body, _ := json.Marshal(map[string]any{"q": "红叶", "mode": "hybrid", "class_id": 2, "limit": 10})
	resp, data := env.do(t, http.MethodPost, "/api/search", tokenA, body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status %d", resp.StatusCode)
	}
	var out struct {
		Hits   []any  `json:"hits"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(data, &out)
	if len(out.Hits) != 0 {
		t.Fatalf("cross-class search leaked hits: %s", data)
	}
}

func TestAskWithAndWithoutEvidence(t *testing.T) {
	env := setupEnv(t)
	token := env.login(t, "student_a1", "CampusClaw#2026")
	before := atomic.LoadInt64(env.chatCalls)

	withEvidence, _ := json.Marshal(map[string]any{"q": "勾股"})
	resp, data := env.do(t, http.MethodPost, "/api/ask", token, withEvidence, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ask status %d body=%s", resp.StatusCode, data)
	}
	var ok struct {
		Answer    string `json:"answer"`
		Citations []struct {
			Marker  string `json:"marker"`
			ChunkID int64  `json:"chunk_id"`
		} `json:"citations"`
	}
	_ = json.Unmarshal(data, &ok)
	if len(ok.Citations) == 0 || ok.Citations[0].Marker != "[1]" {
		t.Fatalf("expected [1] citation, got %s", data)
	}
	if atomic.LoadInt64(env.chatCalls) <= before {
		t.Fatal("chat gateway should have been called with evidence")
	}

	callsAfterEvidence := atomic.LoadInt64(env.chatCalls)
	noEvidence, _ := json.Marshal(map[string]any{"q": "今天天气如何"})
	resp, data = env.do(t, http.MethodPost, "/api/ask", token, noEvidence, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ask status %d", resp.StatusCode)
	}
	var no struct {
		Status    string `json:"status"`
		Citations []any  `json:"citations"`
	}
	_ = json.Unmarshal(data, &no)
	if len(no.Citations) != 0 {
		t.Fatalf("no-evidence ask must have empty citations: %s", data)
	}
	if atomic.LoadInt64(env.chatCalls) != callsAfterEvidence {
		t.Fatal("no-evidence ask must not call the chat gateway")
	}
}
