package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var passwordHash []byte

func SetPasswordHash(hash string) {
	passwordHash = []byte(hash)
}

func GenerateRandomPassword() string {
	return generateToken(8)
}

const (
	sessionMaxAge    = 24 * time.Hour
	maxLoginAttempts = 5
	lockoutDuration  = 15 * time.Minute
	sessionCookie    = "talaria_session"
	csrfCookie       = "talaria_csrf"
)

type session struct {
	token   string
	csrf    string
	created time.Time
}

var (
	sessions   = make(map[string]*session) // token → session
	sessionsMu sync.RWMutex
)

func generateToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func createSession() *session {
	s := &session{
		token:   generateToken(32),
		csrf:    generateToken(16),
		created: time.Now(),
	}
	sessionsMu.Lock()
	sessions[s.token] = s
	sessionsMu.Unlock()
	return s
}

func getSession(token string) *session {
	sessionsMu.RLock()
	s, ok := sessions[token]
	sessionsMu.RUnlock()
	if !ok {
		return nil
	}
	if time.Since(s.created) > sessionMaxAge {
		sessionsMu.Lock()
		delete(sessions, token)
		sessionsMu.Unlock()
		return nil
	}
	return s
}

func deleteSession(token string) {
	sessionsMu.Lock()
	delete(sessions, token)
	sessionsMu.Unlock()
}

type loginAttempt struct {
	count    int
	lastFail time.Time
}

var (
	attempts   = make(map[string]*loginAttempt) // IP → attempts
	attemptsMu sync.Mutex
)

func checkRateLimit(ip string) (remaining int, lockedUntil time.Time, allowed bool) {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()

	a, ok := attempts[ip]
	if !ok {
		return maxLoginAttempts, time.Time{}, true
	}

	if a.count >= maxLoginAttempts && time.Since(a.lastFail) > lockoutDuration {
		delete(attempts, ip)
		return maxLoginAttempts, time.Time{}, true
	}

	if a.count >= maxLoginAttempts {
		return 0, a.lastFail.Add(lockoutDuration), false
	}

	return maxLoginAttempts - a.count, time.Time{}, true
}

func recordFailedAttempt(ip string) (remaining int) {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()

	a, ok := attempts[ip]
	if !ok {
		a = &loginAttempt{}
		attempts[ip] = a
	}
	a.count++
	a.lastFail = time.Now()
	return maxLoginAttempts - a.count
}

func clearAttempts(ip string) {
	attemptsMu.Lock()
	delete(attempts, ip)
	attemptsMu.Unlock()
}

func getRealIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {

		ips := strings.Split(ip, ",")
		return strings.TrimSpace(ips[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ip := getRealIP(r)

	_, lockedUntil, allowed := checkRateLimit(ip)
	if !allowed {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":        "Too many attempts. Try again later.",
			"locked_until": lockedUntil.Unix(),
			"remaining":    0,
		})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if len(req.Password) == 0 || len(req.Password) > 72 {
		rem := recordFailedAttempt(ip)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":     "Invalid password",
			"remaining": rem,
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword(passwordHash, []byte(req.Password)); err != nil {
		rem := recordFailedAttempt(ip)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":     "Invalid password",
			"remaining": rem,
		})
		return
	}

	clearAttempts(ip)
	sess := createSession()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sess.token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    sess.csrf,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		deleteSession(c.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:   csrfCookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"authenticated": false})
		return
	}
	if s := getSession(c.Value); s != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"authenticated": true})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]interface{}{"authenticated": false})
}

func getSessionFromRequest(r *http.Request) *session {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return getSession(c.Value)
}

func isAuthenticated(r *http.Request) bool {
	return getSessionFromRequest(r) != nil
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")

		path := r.URL.Path

		if isStaticAsset(path) {
			next.ServeHTTP(w, r)
			return
		}

		session := getSessionFromRequest(r)
		if session == nil {

			if path == "/" || path == "/index.html" {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Authentication required",
			})
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			clientCSRF := r.Header.Get("X-CSRF-Token")
			if clientCSRF == "" || clientCSRF != session.csrf {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "Invalid or missing CSRF token",
				})
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isStaticAsset(path string) bool {
	for _, ext := range []string{".css", ".js", ".woff", ".woff2", ".ttf", ".ico", ".png", ".jpg", ".svg"} {
		if len(path) > len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}
	return false
}
