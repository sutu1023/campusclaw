package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"campusclaw/internal/mcp"
)

type server struct{ tokenHash [sha256.Size]byte }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func newHandler(token string) (http.Handler, error) {
	if !mcp.ValidBearerToken(token) {
		return nil, errors.New("MCP_AUTH_TOKEN is required and must be a valid Bearer token")
	}
	s := &server{tokenHash: sha256.Sum256([]byte(token))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /mcp", s.serveRPC)
	return mux, nil
}

func (s *server) serveRPC(w http.ResponseWriter, r *http.Request) {
	// Authenticate before parsing any attacker-controlled JSON. Comparing fixed
	// length digests avoids leaking token length through the comparison path.
	token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	gotHash := sha256.Sum256([]byte(token))
	if !bearer || len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare(gotHash[:], s.tokenHash[:]) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcp.MaxResponseBytes))
	if err != nil || !json.Valid(body) {
		writeError(w, nil, -32700, "Invalid JSON request")
		return
	}
	var req request
	if json.Unmarshal(body, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
		writeError(w, nil, -32600, "Invalid JSON-RPC request")
		return
	}
	if req.Method == "notifications/initialized" {
		if len(req.ID) != 0 || !emptyParams(req.Params) {
			writeError(w, req.ID, -32600, "Invalid initialized notification")
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	// JSON-RPC notifications have no response, even for unknown methods. This
	// stateless profile supports no other notification or server-push feature.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string                          `json:"protocolVersion"`
			Capabilities    map[string]json.RawMessage      `json:"capabilities"`
			ClientInfo      *struct{ Name, Version string } `json:"clientInfo"`
		}
		if !decodeObject(req.Params, &params) || params.ProtocolVersion != mcp.ProtocolVersion || params.Capabilities == nil || params.ClientInfo == nil || params.ClientInfo.Name == "" || params.ClientInfo.Version == "" {
			writeError(w, req.ID, -32602, "Expected compatible protocol version, capabilities and clientInfo")
			return
		}
		writeResult(w, req.ID, map[string]any{
			"protocolVersion": mcp.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "campusclaw-teaching-tools", "version": "1"},
		})
	case "tools/list":
		if !emptyParams(req.Params) {
			writeError(w, req.ID, -32602, "Expected empty tools/list parameters")
			return
		}
		writeResult(w, req.ID, map[string]any{"tools": teachingTools()})
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if !decodeObject(req.Params, &params) {
			writeError(w, req.ID, -32602, "Expected a tool name and arguments object")
			return
		}
		result, err := callTeachingTool(params.Name, params.Arguments)
		if err != nil {
			writeError(w, req.ID, -32602, "Unknown tool or invalid query (1-500 non-blank characters, no extra properties)")
			return
		}
		writeResult(w, req.ID, result)
	default:
		writeError(w, req.ID, -32601, "Method not found")
	}
}

func validID(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return true
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number:
		return true
	default:
		return false
	}
}

func decodeObject(raw json.RawMessage, dest any) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(dest) == nil && d.Decode(new(any)) == io.EOF
}

func emptyParams(raw json.RawMessage) bool {
	return len(raw) == 0 || decodeObject(raw, &struct{}{})
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeResponse(w, response{JSONRPC: "2.0", ID: id, Error: &rpcError{code, message}})
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeResponse(w, response{JSONRPC: "2.0", ID: id, Result: result})
}

func writeResponse(w http.ResponseWriter, resp response) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}
