// SPDX-License-Identifier: AGPL-3.0-only

// Package auth implements password login, sessions, CSRF protection and roles.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/karst3nl/arbeitszeit-manager/internal/model"
	"github.com/karst3nl/arbeitszeit-manager/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type contextKey int

const (
	userKey contextKey = iota
	csrfKey
)

const (
	sessionCookie = "session"
	csrfCookie    = "csrf"
	sessionDays   = 7
)

// Auth bundles session storage and cookie settings.
type Auth struct {
	store  *store.Store
	secure bool
	limits *loginLimiter
}

// New creates an Auth instance.
func New(s *store.Store, secure bool) *Auth {
	return &Auth{store: s, secure: secure, limits: newLoginLimiter()}
}

// HashPassword hashes a password with bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// CheckPassword verifies a password against a bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (a *Auth) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionDays * 24 * 60 * 60,
	})
}

// Login authenticates a user and starts a session.
func (a *Auth) Login(w http.ResponseWriter, username, password string) (*model.User, error) {
	u, err := a.store.UserByUsername(username)
	if err != nil || !u.Active || !CheckPassword(u.PasswordHash, password) {
		return nil, store.ErrConflict
	}

	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	if err := a.store.CreateSession(u.ID, tokenHash(token), time.Now().Add(sessionDays*24*time.Hour).Unix()); err != nil {
		return nil, err
	}
	a.setSessionCookie(w, token)
	a.store.TouchLogin(u.ID)
	return u, nil
}

// Logout ends the current session.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.store.DeleteSession(tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func (a *Auth) userFromRequest(r *http.Request) *model.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := a.store.SessionUser(tokenHash(c.Value))
	if err != nil {
		return nil
	}
	return u
}

func (a *Auth) issueCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) == 64 {
		return c.Value
	}
	token, err := randomToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: a.secure, SameSite: http.SameSiteLaxMode,
		MaxAge: sessionDays * 24 * 60 * 60,
	})
	return token
}

func validCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	if err != nil || len(c.Value) != 64 {
		return false
	}
	got := r.FormValue("csrf")
	return len(got) == len(c.Value) && subtle.ConstantTimeCompare([]byte(got), []byte(c.Value)) == 1
}

// Middleware injects the current user and CSRF token into the request context
// and rejects write requests without a valid token.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csrfToken := a.issueCSRF(w, r)
		ctx := context.WithValue(r.Context(), csrfKey, csrfToken)
		if u := a.userFromRequest(r); u != nil {
			ctx = context.WithValue(ctx, userKey, u)
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if !validCSRF(r) {
				http.Error(w, "Ungültiges CSRF-Token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Require redirects anonymous visitors to the login page.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFrom(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin additionally checks the admin role.
func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r)
		if u == nil || !u.IsAdmin() {
			http.Error(w, "Keine Berechtigung", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// UserFrom returns the user stored in the request context.
func UserFrom(r *http.Request) *model.User {
	u, _ := r.Context().Value(userKey).(*model.User)
	return u
}

// CSRF returns the token stored in the request context.
func CSRF(r *http.Request) string {
	v, _ := r.Context().Value(csrfKey).(string)
	return v
}

// --- Login rate limiting ---------------------------------------------------

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]int
	until    map[string]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: map[string]int{}, until: map[string]time.Time{}}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginKey includes the account name: behind a reverse proxy every request
// comes from 127.0.0.1, so limiting by IP alone would let one user lock out
// all accounts on the LAN.
func loginKey(r *http.Request, username string) string {
	return clientIP(r) + "|" + strings.ToLower(username)
}

// LoginBlocked reports whether the client is temporarily locked out.
func (a *Auth) LoginBlocked(r *http.Request, username string) bool {
	a.limits.mu.Lock()
	defer a.limits.mu.Unlock()
	key := loginKey(r, username)
	if until, ok := a.limits.until[key]; ok {
		if time.Now().Before(until) {
			return true
		}
		delete(a.limits.until, key)
		delete(a.limits.attempts, key)
	}
	return false
}

// LoginFailed records a failed login attempt.
func (a *Auth) LoginFailed(r *http.Request, username string) {
	a.limits.mu.Lock()
	defer a.limits.mu.Unlock()
	key := loginKey(r, username)
	a.limits.attempts[key]++
	if a.limits.attempts[key] >= 3 {
		a.limits.until[key] = time.Now().Add(60 * time.Second)
	}
}

// LoginOK clears failed attempts after a successful login.
func (a *Auth) LoginOK(r *http.Request, username string) {
	a.limits.mu.Lock()
	defer a.limits.mu.Unlock()
	key := loginKey(r, username)
	delete(a.limits.attempts, key)
	delete(a.limits.until, key)
}
