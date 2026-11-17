// SPDX-License-Identifier: GPL-3.0-only

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"netrave/internal/access"
	"netrave/internal/config"
	"netrave/internal/database"
	"netrave/internal/permcatalog"
)

const (
	CookieName   = "netrave_session"
	argonTime    = 2
	argonMem     = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	maxFails     = 5
	lockout      = 5 * time.Minute
)

var b64 = base64.RawStdEncoding

// RoleRef - a brief reference to a user role (for the user card).
type RoleRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// User - a system user.
type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt int64     `json:"created_at"`
	Roles     []RoleRef `json:"roles"`
	RolePerms []string  `json:"role_perms"`
}

// Session - an active session.
type Session struct {
	ID   string
	User User
	CSRF string
}

// HashPassword returns an Argon2id password hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMem, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMem, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword verifies the password against the hash in constant time.
func VerifyPassword(pw, enc string) bool {
	p := strings.Split(enc, "$")
	if len(p) != 6 || p[1] != "argon2id" {
		return false
	}
	var m, t, th uint32
	if _, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &m, &t, &th); err != nil {
		return false
	}
	if m == 0 || m > 1<<20 || t == 0 || t > 10 || th == 0 || th > 16 {
		return false
	}
	salt, err := b64.DecodeString(p[4])
	if err != nil {
		return false
	}
	want, err := b64.DecodeString(p[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, uint8(th), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyHash is used to equalize response time for an unknown user.
var DummyHash, _ = HashPassword("netrave-dummy-password")

type failState struct {
	n     int
	until time.Time
}

// Service - user and session handling.
type Service struct {
	db    *database.DB
	cfg   *config.Config
	mu    sync.Mutex
	fails map[string]*failState
}

// UseConfig connects settings (session lifetime) from the configuration file.
func (s *Service) UseConfig(c *config.Config) { s.cfg = c }

func New(db *database.DB) *Service {
	return &Service{db: db, fails: map[string]*failState{}}
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// ClientIP returns the client address (proxy headers are not trusted).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// SameOrigin checks that the Origin header (if present) matches the request host.
func SameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

// ---- password brute-force limiting ----

func (s *Service) Allow(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fails[ip]
	if f == nil || f.n < maxFails {
		return true
	}
	if time.Now().After(f.until) {
		delete(s.fails, ip)
		return true
	}
	return false
}

func (s *Service) Fail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fails[ip]
	if f == nil {
		f = &failState{}
		s.fails[ip] = f
	}
	f.n++
	if f.n >= maxFails {
		f.until = time.Now().Add(lockout)
	}
}

func (s *Service) Reset(ip string) {
	s.mu.Lock()
	delete(s.fails, ip)
	s.mu.Unlock()
}

// ---- users ----

// FindByUsername returns the user and their password hash.
func (s *Service) FindByUsername(name string) (*User, string, error) {
	var u User
	var hash string
	var dis int
	err := s.db.QueryRow(`SELECT id,username,role,disabled,created_at,password_hash FROM users WHERE username=?`, name).
		Scan(&u.ID, &u.Username, &u.Role, &dis, &u.CreatedAt, &hash)
	if err != nil {
		return nil, "", err
	}
	u.Disabled = dis != 0
	if err := FillRoles(s.db, &u); err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

// BootstrapAdmin creates an admin user with a random password if there are no users yet.
func (s *Service) BootstrapAdmin() (password string, created bool, err error) {
	var n int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return
	}
	if n > 0 {
		return "", false, nil
	}
	password, err = randToken(12)
	if err != nil {
		return
	}
	hash, err := HashPassword(password)
	if err != nil {
		return "", false, err
	}
	_, err = s.db.Exec(`INSERT INTO users(username,password_hash,role) VALUES('admin',?,'admin')`, hash)
	if err != nil {
		return "", false, err
	}
	return password, true, nil
}

// ---- sessions ----

func (s *Service) TTL() time.Duration {
	return s.cfg.SessionTTL()
}

func (s *Service) CreateSession(userID int64, ip string) (token, csrf string, exp time.Time, err error) {
	if token, err = randToken(32); err != nil {
		return
	}
	if csrf, err = randToken(24); err != nil {
		return
	}
	now := time.Now()
	exp = now.Add(s.TTL())
	_, err = s.db.Exec(`INSERT INTO sessions(id,user_id,csrf,ip,created_at,expires_at) VALUES(?,?,?,?,?,?)`,
		hashToken(token), userID, csrf, ip, now.Unix(), exp.Unix())
	return
}

func (s *Service) Lookup(token string) (*Session, error) {
	var ss Session
	var dis int
	err := s.db.QueryRow(`SELECT s.id, s.csrf, u.id, u.username, u.role, u.disabled, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = ? AND s.expires_at > ?`, hashToken(token), time.Now().Unix()).
		Scan(&ss.ID, &ss.CSRF, &ss.User.ID, &ss.User.Username, &ss.User.Role, &dis, &ss.User.CreatedAt)
	if err != nil {
		return nil, err
	}
	if dis != 0 {
		return nil, sql.ErrNoRows
	}
	if err := FillRoles(s.db, &ss.User); err != nil {
		return nil, err
	}
	return &ss, nil
}

// FillRoles fills the user's roles and the union of their permissions from the permission cache.
// The cache is derived from users/roles/role_permissions/user_roles and is reset when they change.
func FillRoles(db *database.DB, u *User) error {
	snap, err := access.UserPermissions(db, u.ID)
	if err != nil {
		return err
	}
	u.Roles = make([]RoleRef, 0, len(snap.Roles))
	for _, r := range snap.Roles {
		u.Roles = append(u.Roles, RoleRef{ID: r.ID, Name: r.Name})
	}
	u.RolePerms = make([]string, len(snap.Permissions))
	copy(u.RolePerms, snap.Permissions)
	return nil
}

func (s *Service) DeleteSession(id string) {
	var userID int64
	_ = s.db.QueryRow(`SELECT user_id FROM sessions WHERE id=?`, id).Scan(&userID)
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	if userID != 0 {
		access.InvalidateUserPermissions(s.db, userID)
	}
}

// DeleteUserSessions deletes all of the user's sessions except exceptID (may be empty).
func (s *Service) DeleteUserSessions(userID int64, exceptID string) {
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE user_id=? AND id<>?`, userID, exceptID)
	access.InvalidateUserPermissions(s.db, userID)
}

// Cleanup deletes expired sessions and stale limiter records.
func (s *Service) Cleanup() {
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	s.mu.Lock()
	for ip, f := range s.fails {
		if f.n >= maxFails && time.Now().After(f.until) {
			delete(s.fails, ip)
		}
	}
	s.mu.Unlock()
}

// ---- middleware ----

type ctxKey struct{}

// FromContext returns the session placed there by the Require middleware.
func FromContext(ctx context.Context) *Session {
	s, _ := ctx.Value(ctxKey{}).(*Session)
	return s
}

func deny(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ClearCookie deletes the session cookie.
func ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// Require requires an authenticated user; for mutating requests it checks CSRF and Origin.
func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil || c.Value == "" {
			deny(w, http.StatusUnauthorized, "требуется авторизация")
			return
		}
		ss, err := s.Lookup(c.Value)
		if err != nil {
			ClearCookie(w, r)
			deny(w, http.StatusUnauthorized, "сессия недействительна")
			return
		}
		if !safeMethod(r.Method) {
			tok := r.Header.Get("X-CSRF-Token")
			if !SameOrigin(r) || subtle.ConstantTimeCompare([]byte(tok), []byte(ss.CSRF)) != 1 {
				deny(w, http.StatusForbidden, "CSRF-проверка не пройдена")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, ss)))
	})
}

// RequireAdmin requires the administrator role.
func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return s.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()).User.Role != "admin" {
			deny(w, http.StatusForbidden, "доступно только администратору")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// Can reports whether the user has a permission. Role permissions are the union of all roles' permissions.
// "Access to administrative sections" (admin.access) opens existing global sections,
// but does not make the user an administrator and does not allow managing roles or users.
func (u User) Can(p string) bool {
	if u.Role == "admin" {
		return true
	}
	for _, x := range u.RolePerms {
		if x == p {
			return true
		}
	}
	if isGlobalSection(p) {
		for _, x := range u.RolePerms {
			if x == permcatalog.AdminAccess {
				return true
			}
		}
	}
	return false
}

// HasRolePerm reports whether the permission is granted by at least one of the user's roles.
func (u User) HasRolePerm(p string) bool {
	for _, x := range u.RolePerms {
		if x == p {
			return true
		}
	}
	return false
}

func isGlobalSection(p string) bool {
	switch p {
	case permcatalog.SectionDashboard, permcatalog.SectionLogs, permcatalog.SectionTemplates, permcatalog.SectionImages:
		return true
	}
	return false
}

// RequirePerm allows only users with the specified permission (administrators - always).
func (s *Service) RequirePerm(perm string, next http.Handler) http.Handler {
	return s.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !FromContext(r.Context()).User.Can(perm) {
			deny(w, http.StatusForbidden, "нет права: "+perm)
			return
		}
		next.ServeHTTP(w, r)
	}))
}
