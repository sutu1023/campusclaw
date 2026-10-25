package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"campusclaw/internal/skill"
	"campusclaw/internal/store"
)

type skillRequest struct {
	Source string `json:"source"`
}

// listSkills returns every skill of the caller's class.
//
// @Summary      列出本班技能
// @Tags         skills
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  skillListResponse
// @Failure      401  {object}  errorBody
// @Router       /api/skills [get]
func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	skills, err := s.Store.ListSkills(r.Context(), user.ClassID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}
	if skills == nil {
		skills = []store.Skill{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

// createSkill parses a skill file and stores it.
//
// @Summary      用技能文件新建技能
// @Description  工具接受 knowledge_search 与 tool@server，去重后最多8项；在写事务内校验本班已连接的批准目录。缺失/他班服务器404，不可用/未知工具400，无部分写入，保留原文。
// @Tags         skills
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  skillRequest  true  "技能文件原文"
// @Success      201  {object}  store.Skill
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      403  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/skills [post]
func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ns, ok := decodeSkillRequest(w, r, user.ClassID)
	if !ok {
		return
	}
	created, err := s.Store.CreateSkill(r.Context(), ns)
	if !writeSkillError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// updateSkill replaces a class-scoped skill from a new file.
//
// @Summary      用技能文件改写技能
// @Description  先校验技能归属，再在同一写事务内验证 MCP 引用；断开/待确认/未发现400，缺失/他班404；失败保留原文、工具和绑定。
// @Tags         skills
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int           true  "技能编号"
// @Param        body  body  skillRequest  true  "技能文件原文"
// @Success      200  {object}  store.Skill
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      403  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/skills/{id} [put]
func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.Store.GetSkill(r.Context(), user.ClassID, id); !writeSkillError(w, err) {
		return
	}
	ns, ok := decodeSkillRequest(w, r, user.ClassID)
	if !ok {
		return
	}
	updated, err := s.Store.UpdateSkill(r.Context(), user.ClassID, id, ns)
	if !writeSkillError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// deleteSkill removes a skill and its bindings.
//
// @Summary      删除本班技能
// @Tags         skills
// @Security     BearerAuth
// @Param        id  path  int  true  "技能编号"
// @Success      204
// @Failure      400  {object}  errorBody
// @Failure      401  {object}  errorBody
// @Failure      403  {object}  errorBody
// @Failure      404  {object}  errorBody
// @Router       /api/skills/{id} [delete]
func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
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
	err := s.Store.DeleteSkill(r.Context(), user.ClassID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skill")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeSkillRequest parses and validates a skill file, mapping parse failures
// to a readable 400 for the teacher.
func decodeSkillRequest(w http.ResponseWriter, r *http.Request, classID int64) (store.NewSkill, bool) {
	var req skillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return store.NewSkill{}, false
	}
	parsed, err := skill.Parse(req.Source)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.NewSkill{}, false
	}
	return store.NewSkill{
		ClassID:     classID,
		Name:        parsed.Name,
		Trigger:     parsed.Trigger,
		Description: parsed.Description,
		Tools:       parsed.Tools,
		Source:      parsed.Source,
		Subject:     parsed.Subject,
		Enabled:     parsed.Enabled,
	}, true
}

func writeSkillError(w http.ResponseWriter, err error) bool {
	var toolError *store.SkillToolError
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, http.StatusBadRequest, "name already exists in this class")
	case errors.As(err, &toolError):
		writeError(w, http.StatusBadRequest, toolError.Error())
	case errors.Is(err, store.ErrInvalidMCP):
		writeError(w, http.StatusBadRequest, "工具引用无效或超过上限")
	default:
		writeError(w, http.StatusInternalServerError, "failed to save skill")
	}
	return false
}
