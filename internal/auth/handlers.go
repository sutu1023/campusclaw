package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"campusclaw/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// Handler serves the login / logout / me endpoints.
type Handler struct {
	Store      *store.Store
	TokenSecret string
	SessionTTL  time.Duration
}

// Register mounts the auth routes on the given mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/login", h.Login)
	mux.Handle("POST /api/logout", Middleware(h.Store, h.TokenSecret)(http.HandlerFunc(h.Logout)))
	mux.Handle("GET /api/me", Middleware(h.Store, h.TokenSecret)(http.HandlerFunc(h.Me)))
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// OpenAPI response shapes for the auth endpoints (kept local so swag can
// resolve them without importing the httpapi package).
type authUserBody struct {
	ID          int64  `json:"id" example:"1"`
	Username    string `json:"username" example:"teacher_a"`
	DisplayName string `json:"display_name" example:"教师 A"`
	Role        string `json:"role" example:"teacher"`
	ClassID     int64  `json:"class_id" example:"1"`
}

type authLoginResponse struct {
	Token string       `json:"token"`
	User  authUserBody `json:"user"`
}

type authMeResponse struct {
	User authUserBody `json:"user"`
}

type authOKResponse struct {
	Status string `json:"status" example:"ok"`
}

type authErrorBody struct {
	Error string `json:"error" example:"invalid credentials"`
}

type userResponse struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	ClassID     int64  `json:"class_id"`
}

func toUserResponse(u *store.User) userResponse {
	return userResponse{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role, ClassID: u.ClassID}
}

// Login validates credentials, revokes any previous session and issues a new
// opaque token via the response body. All failures share one status and body.
//
// @Summary      登录并签发访问令牌
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body  loginRequest  true  "登录凭据"
// @Success      200  {object}  authLoginResponse
// @Failure      400  {object}  authErrorBody
// @Failure      401  {object}  authErrorBody
// @Router       /api/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	user, err := h.Store.GetUserByUsername(r.Context(), req.Username)
	if err != nil || user == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	// Session fixation defense: drop any existing session before issuing a new token.
	if err := h.Store.DeleteSessionsForUser(r.Context(), user.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	token, err := NewToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	expires := time.Now().Add(h.SessionTTL)
	if err := h.Store.CreateSession(r.Context(), HashToken(h.TokenSecret, token), user.ID, expires); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": toUserResponse(user)})
}

// Logout revokes the presented token immediately.
//
// @Summary      登出并吊销当前令牌
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  authOKResponse
// @Router       /api/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if token, ok := BearerToken(r); ok {
		_ = h.Store.DeleteSession(r.Context(), HashToken(h.TokenSecret, token))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Me returns the current user, letting a SPA validate a stored token.
//
// @Summary      读取当前登录用户
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  authMeResponse
// @Failure      401  {object}  authErrorBody
// @Router       /api/me [get]
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserResponse(u)})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
