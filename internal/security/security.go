package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Identity struct {
	Subject, Tenant string
	Roles           []string
	ExpiresAt       time.Time
}
type APIKeyStore struct {
	mu   sync.RWMutex
	keys map[string]Identity
}

func NewAPIKeyStore() *APIKeyStore { return &APIKeyStore{keys: map[string]Identity{}} }
func keyHash(key string) string    { h := sha256.Sum256([]byte(key)); return fmt.Sprintf("%x", h[:]) }
func (s *APIKeyStore) Put(key string, id Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[keyHash(key)] = id
}
func (s *APIKeyStore) Revoke(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, keyHash(key))
}
func (s *APIKeyStore) Authenticate(key string) (Identity, bool) {
	s.mu.RLock()
	id, ok := s.keys[keyHash(key)]
	s.mu.RUnlock()
	if !ok || (!id.ExpiresAt.IsZero() && time.Now().After(id.ExpiresAt)) {
		return Identity{}, false
	}
	return id, true
}

func (s *APIKeyStore) Save(path string) error {
	s.mu.RLock()
	b, e := json.Marshal(s.keys)
	s.mu.RUnlock()
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".apikey-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e == nil {
		e = f.Chmod(0600)
	}
	if e == nil {
		e = f.Close()
		e = os.Rename(tmp, path)
	} else {
		_ = f.Close()
	}
	return e
}
func (s *APIKeyStore) Load(path string) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	var keys map[string]Identity
	if e = json.Unmarshal(b, &keys); e != nil {
		return e
	}
	s.mu.Lock()
	s.keys = keys
	s.mu.Unlock()
	return nil
}

type JWTVerifier struct {
	mu   sync.RWMutex
	keys map[string][]byte
}

func NewJWTVerifier(secret []byte) *JWTVerifier {
	v := &JWTVerifier{keys: map[string][]byte{}}
	v.Rotate("default", secret)
	return v
}
func (v *JWTVerifier) Rotate(kid string, secret []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.keys[kid] = append([]byte(nil), secret...)
}
func (v *JWTVerifier) Verify(token string) (Identity, error) {
	p := strings.Split(token, ".")
	if len(p) != 3 {
		return Identity{}, errors.New("invalid JWT")
	}
	dec := base64.RawURLEncoding
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hb, e := dec.DecodeString(p[0])
	if e != nil {
		return Identity{}, e
	}
	if json.Unmarshal(hb, &h) != nil || h.Alg != "HS256" {
		return Identity{}, errors.New("unsupported JWT algorithm")
	}
	v.mu.RLock()
	key := v.keys[h.Kid]
	v.mu.RUnlock()
	if len(key) == 0 {
		return Identity{}, errors.New("unknown JWT key id")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(p[0] + "." + p[1]))
	sig, e := dec.DecodeString(p[2])
	if e != nil || subtle.ConstantTimeCompare(mac.Sum(nil), sig) != 1 {
		return Identity{}, errors.New("invalid JWT signature")
	}
	var c struct {
		Sub    string   `json:"sub"`
		Tenant string   `json:"tenant_id"`
		Roles  []string `json:"roles"`
		Exp    int64    `json:"exp"`
	}
	b, e := dec.DecodeString(p[1])
	if e != nil || json.Unmarshal(b, &c) != nil {
		return Identity{}, errors.New("invalid JWT claims")
	}
	if c.Exp <= 0 || time.Now().Unix() >= c.Exp {
		return Identity{}, errors.New("JWT expired")
	}
	return Identity{Subject: c.Sub, Tenant: c.Tenant, Roles: c.Roles, ExpiresAt: time.Unix(c.Exp, 0)}, nil
}

type PermissionStore struct {
	mu    sync.RWMutex
	Roles map[string]map[string]struct{} `json:"roles"`
}

func NewPermissionStore() *PermissionStore {
	return &PermissionStore{Roles: map[string]map[string]struct{}{}}
}
func (p *PermissionStore) Grant(role, resource, action string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Roles[role] == nil {
		p.Roles[role] = map[string]struct{}{}
	}
	p.Roles[role][resource+":"+action] = struct{}{}
}
func (p *PermissionStore) Allow(id Identity, resource, action string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.Roles["admin"][resource+":"+action]
	if ok {
		return true
	}
	for _, r := range id.Roles {
		if _, ok := p.Roles[r][resource+":"+action]; ok {
			return true
		}
	}
	return false
}

var secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|password|secret|token)(\s*[:=]\s*)[^,\s]+`)

func Redact(s string) string { return secretPattern.ReplaceAllString(s, "$1$2[REDACTED]") }
func SanitizePrompt(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if len(s) > 100000 {
		s = s[:100000]
	}
	return s
}

type AuditEvent struct {
	Time     time.Time `json:"time"`
	Subject  string    `json:"subject,omitempty"`
	Tenant   string    `json:"tenant,omitempty"`
	Action   string    `json:"action,omitempty"`
	Resource string    `json:"resource,omitempty"`
	Outcome  string    `json:"outcome,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}
type AuditLogger func(AuditEvent)

func Audit(logger AuditLogger, id Identity, action, resource, outcome, detail string) {
	if logger != nil {
		logger(AuditEvent{Time: time.Now().UTC(), Subject: id.Subject, Tenant: id.Tenant, Action: action, Resource: resource, Outcome: outcome, Detail: Redact(detail)})
	}
}

func ParseBearer(v string) (string, error) {
	const p = "Bearer "
	if !strings.HasPrefix(v, p) {
		return "", fmt.Errorf("missing bearer token")
	}
	t := strings.TrimSpace(strings.TrimPrefix(v, p))
	if t == "" {
		return "", errors.New("empty bearer token")
	}
	return t, nil
}

func (v *JWTVerifier) AuthenticateBearer(header string) (Identity, error) {
	token, err := ParseBearer(header)
	if err != nil {
		return Identity{}, err
	}
	return v.Verify(token)
}
