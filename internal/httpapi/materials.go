package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"campusclaw/internal/chunk"
	"campusclaw/internal/store"
)

// ChunkParams is the client-facing chunking configuration.
type ChunkParams struct {
	Strategy       string `json:"chunk_strategy"`
	Separator      string `json:"chunk_separator"`
	MaxChars       int    `json:"chunk_max_chars"`
	OverlapPercent int    `json:"chunk_overlap_percent"`
	CollapseWS     bool   `json:"chunk_collapse_ws"`
	StripLinks     bool   `json:"chunk_strip_links"`
}

// Options converts request parameters into chunker options.
func (p ChunkParams) Options() chunk.Options {
	overlap := 0
	if p.MaxChars > 0 {
		overlap = p.MaxChars * p.OverlapPercent / 100
	}
	return chunk.Options{
		Strategy:   p.Strategy,
		MaxChars:   p.MaxChars,
		Overlap:    overlap,
		CollapseWS: p.CollapseWS,
		StripLinks: p.StripLinks,
	}
}

// ParamsFromValues reads chunk parameters from a form.
func ParamsFromValues(v interface{ Get(string) string }) ChunkParams {
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	truthy := func(s string) bool {
		s = strings.ToLower(strings.TrimSpace(s))
		return s == "1" || s == "true" || s == "on" || s == "yes"
	}
	return ChunkParams{
		Strategy:       v.Get("chunk_strategy"),
		Separator:      v.Get("chunk_separator"),
		MaxChars:       atoi(v.Get("chunk_max_chars")),
		OverlapPercent: atoi(v.Get("chunk_overlap_percent")),
		CollapseWS:     truthy(v.Get("chunk_collapse_ws")),
		StripLinks:     truthy(v.Get("chunk_strip_links")),
	}
}

// listMaterials lists the caller's class materials.
//
// @Summary      分页列出本班材料
// @Tags         materials
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Param        title      query  string  false  "标题筛选"
// @Success      200  {object}  materialListResponse
// @Failure      401  {object}  errorBody
// @Router       /api/materials [get]
func (s *Server) listMaterials(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	title := r.URL.Query().Get("title")

	materials, total, err := s.Store.ListMaterials(r.Context(), user.ClassID, page, pageSize, title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list materials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"materials": materials,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// getMaterial returns one class-scoped material with its original text.
//
// @Summary      读取本班材料详情
// @Tags         materials
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "材料编号"
// @Success      200  {object}  materialDetailResponse
// @Failure      401  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/materials/{id} [get]
func (s *Server) getMaterial(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	material, err := s.Store.GetMaterial(r.Context(), user.ClassID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load material")
		return
	}
	entry, err := s.Store.GetEntryByMaterial(r.Context(), user.ClassID, id)
	body := ""
	if err == nil && entry != nil {
		body = entry.BodyText
	}
	writeJSON(w, http.StatusOK, map[string]any{"material": material, "body_text": body})
}

// materialVectors lists a class-scoped material's chunks and vector payload.
//
// @Summary      查看本班材料的向量记录
// @Tags         materials
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "材料编号"
// @Success      200  {object}  vectorResponse
// @Failure      401  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/materials/{id}/vectors [get]
func (s *Server) materialVectors(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Store.GetMaterial(r.Context(), user.ClassID, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	chunks, err := s.Store.ListChunksByMaterial(r.Context(), user.ClassID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chunks")
		return
	}
	out := make([]map[string]any, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, map[string]any{
			"chunk_id":         c.ID,
			"knowledge_entry_id": c.KnowledgeEntryID,
			"material_id":      c.MaterialID,
			"material_title":   c.MaterialTitle,
			"chunk_index":      c.ChunkIndex,
			"start_offset":     c.StartOffset,
			"end_offset":       c.EndOffset,
			"index_status":     c.IndexStatus,
			"embedding_model":  c.EmbeddingModel,
			"dimension":        s.EmbeddingDim,
			"payload": map[string]any{
				"class_id":           c.ClassID,
				"material_id":        c.MaterialID,
				"knowledge_entry_id": c.KnowledgeEntryID,
				"chunk_id":           c.ID,
				"chunk_index":        c.ChunkIndex,
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"chunks": out})
}

// uploadMaterial stores a teacher upload and triggers indexing.
//
// @Summary      教师上传本班材料
// @Tags         materials
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        file                   formData  file    true   "材料文件（.txt/.md）"
// @Param        title                  formData  string  false  "标题"
// @Param        chunk_strategy         formData  string  false  "切片策略：auto/custom/hierarchy"
// @Param        chunk_separator        formData  string  false  "自定义分隔符"
// @Param        chunk_max_chars        formData  int     false  "最大切片长度"
// @Param        chunk_overlap_percent  formData  int     false  "重叠比例"
// @Success      201  {object}  uploadResponse
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      403  {object}  errorBody
// @Router       /api/materials [post]
func (s *Server) uploadMaterial(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.MaxUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".txt" && ext != ".md" {
		writeError(w, http.StatusBadRequest, "unsupported file type")
		return
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read upload")
		return
	}
	bodyText := strings.TrimSpace(string(raw))
	if bodyText == "" {
		writeError(w, http.StatusBadRequest, "empty file")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}

	classID := user.ClassID
	destDir := filepath.Join(s.UploadDir, strconv.FormatInt(classID, 10))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store file")
		return
	}
	storedName, err := randomName(ext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store file")
		return
	}
	destPath := filepath.Join(destDir, storedName)
	if err := os.WriteFile(destPath, raw, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store file")
		return
	}

	materialID, _, err := s.Store.CreateMaterialWithEntry(r.Context(), classID, title, destPath, user.ID, bodyText)
	if err != nil {
		_ = os.Remove(destPath) // no orphan file when the transaction fails
		writeError(w, http.StatusInternalServerError, "failed to save material")
		return
	}

	params := ParamsFromValues(r.Form)
	count, idxErr := s.Indexer.IndexMaterial(r.Context(), classID, materialID, params.Options())
	resp := map[string]any{
		"material_id":    materialID,
		"title":          title,
		"chunk_strategy": normalizeStrategy(params.Strategy),
		"chunk_count":    count,
	}
	if idxErr != nil {
		resp["indexing"] = "failed"
	}
	writeJSON(w, http.StatusCreated, resp)
}

// reindexMaterial rebuilds a material's chunks and vectors.
//
// @Summary      重建本班某一材料的切片和向量
// @Tags         materials
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int          true   "材料编号"
// @Param        body  body  ChunkParams  false  "切片参数"
// @Success      200  {object}  reindexResponse
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      403  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/materials/{id}/reindex [post]
func (s *Server) reindexMaterial(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Store.GetMaterial(r.Context(), user.ClassID, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var params ChunkParams
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&params)
	}

	// Delete old chunks first, then their vectors, then rebuild.
	oldIDs, err := s.Store.DeleteChunksByMaterial(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear old chunks")
		return
	}
	if err := s.Indexer.Vector.Delete(r.Context(), oldIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear old vectors")
		return
	}
	count, err := s.Indexer.IndexMaterial(r.Context(), user.ClassID, id, params.Options())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reindex failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"material_id": id,
		"chunk_count": count,
	})
}

func normalizeStrategy(s string) string {
	if s == "" {
		return "auto"
	}
	return s
}

func randomName(ext string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x%s", buf, ext), nil
}
