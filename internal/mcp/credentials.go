// Package mcp implements CampusClaw's bounded HTTP JSON-RPC tool boundary.
package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	toolNameRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	serverNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	tokenEnvRE   = regexp.MustCompile(`^MCP_(?:[A-Z0-9_]+_)?TOKEN$`)
	bearerRE     = regexp.MustCompile(`^[A-Za-z0-9._~+/\-]+=*$`)
	hostnameRE   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
)

var (
	ErrUnapproved  = errors.New("MCP target is not approved for this class and server")
	ErrCredentials = errors.New("MCP credential is not configured or invalid")
)

func ValidToolName(name string) bool   { return toolNameRE.MatchString(name) }
func ValidServerName(name string) bool { return serverNameRE.MatchString(name) }

// NormalizeOrigin accepts an origin, not a general-purpose URL. It does not
// grant access to private networks: Registry must approve the exact result.
// Errors deliberately never echo the submitted URL (it might contain a secret).
func NormalizeOrigin(raw string) (string, error) {
	invalid := func() (string, error) {
		return "", errors.New("MCP Base URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if raw == "" || len(raw) > 2048 || strings.TrimSpace(raw) != raw || strings.Contains(raw, "#") {
		return invalid()
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return invalid()
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return invalid()
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || len(host) > 253 || (net.ParseIP(host) == nil && !hostnameRE.MatchString(host)) {
		return invalid()
	}
	port := u.Port()
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return invalid()
		}
		port = strconv.Itoa(p)
	} else if strings.HasSuffix(u.Host, ":") {
		return invalid()
	}
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, nil
}

// Binding is deployment configuration. TokenEnv is an identifier, never a
// token. HTTP requests cannot supply or alter this deployment allowlist.
type Binding struct {
	ClassID    int64  `json:"class_id"`
	ServerName string `json:"server_name"`
	BaseURL    string `json:"base_url"`
	TokenEnv   string `json:"token_env"`
}

type registryKey struct {
	classID int64
	name    string
	origin  string
}

// Registry is immutable after construction. Only approved environment keys can
// ever be looked up, and no lookup occurs until the whole target matches.
type Registry struct {
	bindings map[registryKey]Binding
	getenv   func(string) string
}

func NewRegistry(raw string, getenv func(string) string) (*Registry, error) {
	r := &Registry{bindings: make(map[registryKey]Binding), getenv: getenv}
	if strings.TrimSpace(raw) == "" {
		return r, nil
	}
	if len(raw) > 64*1024 || getenv == nil {
		return nil, errors.New("invalid MCP_CREDENTIAL_BINDINGS configuration")
	}
	var bindings []Binding
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&bindings); err != nil || d.Decode(new(any)) != io.EOF || bytes.Equal(bytes.TrimSpace([]byte(raw)), []byte("null")) || len(bindings) > 64 {
		return nil, errors.New("MCP_CREDENTIAL_BINDINGS must be a bounded JSON array of approved bindings")
	}
	usedEnv := map[string]bool{}
	for i, b := range bindings {
		origin, err := NormalizeOrigin(b.BaseURL)
		if err != nil || b.ClassID <= 0 || !ValidServerName(b.ServerName) || !tokenEnvRE.MatchString(b.TokenEnv) {
			return nil, fmt.Errorf("invalid MCP credential binding at index %d", i)
		}
		b.BaseURL = origin
		key := registryKey{b.ClassID, b.ServerName, origin}
		if _, exists := r.bindings[key]; exists || usedEnv[b.TokenEnv] {
			return nil, errors.New("MCP bindings must be unique and use independent credential variables")
		}
		r.bindings[key] = b
		usedEnv[b.TokenEnv] = true
	}
	return r, nil
}

func (r *Registry) lookup(classID int64, name, origin string) (Binding, error) {
	normalized, err := NormalizeOrigin(origin)
	if err != nil || r == nil {
		return Binding{}, ErrUnapproved
	}
	b, ok := r.bindings[registryKey{classID, name, normalized}]
	if !ok {
		return Binding{}, ErrUnapproved
	}
	return b, nil
}

// Reference returns a stable non-secret identity for the exact deployment
// binding. It grants no permission by itself; Resolve must run again at use.
func (r *Registry) Reference(classID int64, name, origin string) (string, error) {
	b, err := r.lookup(classID, name, origin)
	if err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(b)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Target cannot be constructed with arbitrary credentials outside this package.
// Its private fields and formatting methods keep it out of JSON and logs.
type Target struct {
	origin string
	token  string
}

func (t Target) String() string   { return "MCP target (credential redacted)" }
func (t Target) GoString() string { return t.String() }
func (t Target) BaseURL() string  { return t.origin }

// ContainsCredential checks for an exact credential echo without exposing it.
func (t Target) ContainsCredential(text string) bool {
	return t.token != "" && strings.Contains(text, t.token)
}

func (r *Registry) Resolve(classID int64, name, origin string) (Target, error) {
	b, err := r.lookup(classID, name, origin)
	if err != nil {
		return Target{}, err
	}
	token := r.getenv(b.TokenEnv)
	if !ValidBearerToken(token) {
		return Target{}, ErrCredentials
	}
	return Target{origin: b.BaseURL, token: token}, nil
}

func (r *Registry) HasToken(classID int64, name, origin string) bool {
	_, err := r.Resolve(classID, name, origin)
	return err == nil
}

// ValidBearerToken implements the RFC 6750 token character set and prevents
// header injection. A missing value never enables anonymous access.
func ValidBearerToken(token string) bool {
	return len(token) > 0 && len(token) <= 4096 && bearerRE.MatchString(token)
}
