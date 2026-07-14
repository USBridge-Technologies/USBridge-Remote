package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// deriveKey returns a 32-byte AES-256 key derived from the raw master secret via SHA-256.
func deriveKey(secret []byte) []byte {
	h := sha256.Sum256(secret)
	return h[:]
}

// CalculateHMAC computes the HMAC-SHA256 signature for the given request components.
// message = METHOD + PATH + TIMESTAMP_STR + BODY
func CalculateHMAC(method, path, timestamp, body string, secret []byte) string {
	message := fmt.Sprintf("%s%s%s%s", method, path, timestamp, body)
	mac := hmac.New(sha256.New, deriveKey(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// GenerateMasterKey generates a cryptographically random 32-byte master key
// and returns it as a hex string.
func GenerateMasterKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ipRateLimiter tracks per-IP rate limiters.
type ipRateLimiter struct {
	mu  sync.RWMutex
	ips map[string]*rate.Limiter
	r   rate.Limit
	b   int
}

func newIPRateLimiter(r rate.Limit, b int) *ipRateLimiter {
	return &ipRateLimiter{ips: make(map[string]*rate.Limiter), r: r, b: b}
}

func (l *ipRateLimiter) get(ip string) *rate.Limiter {
	l.mu.RLock()
	lim, ok := l.ips[ip]
	l.mu.RUnlock()
	if ok {
		return lim
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim, ok = l.ips[ip]; ok {
		return lim
	}
	lim = rate.NewLimiter(l.r, l.b)
	l.ips[ip] = lim
	return lim
}

// SecurityMiddleware holds rate limiters and the master secret.
type SecurityMiddleware struct {
	realtimeLimiter *ipRateLimiter
	pollingLimiter  *ipRateLimiter
	authLimiter     *ipRateLimiter
	pinLimiter      *ipRateLimiter
	masterKey       []byte
}

// NewSecurityMiddleware creates middleware with the given master key (raw bytes).
func NewSecurityMiddleware(masterKey []byte) *SecurityMiddleware {
	return &SecurityMiddleware{
		realtimeLimiter: newIPRateLimiter(rate.Limit(100), 200),
		pollingLimiter:  newIPRateLimiter(rate.Limit(20), 40),
		authLimiter:     newIPRateLimiter(rate.Limit(3), 5),
		pinLimiter:      newIPRateLimiter(rate.Limit(0.5), 3),
		masterKey:       masterKey,
	}
}

func (m *SecurityMiddleware) clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.Trim(r.RemoteAddr, "[]")
	}
	return host
}

// publicPaths are exempt from HMAC verification.
func isPublicPath(path string) bool {
	return path == "/api/healthz" ||
		path == "/api/moonlight/pin" ||
		strings.HasPrefix(path, "/api/auth/qr")
}

// syncPath is auth-by-payload (AES-GCM), not HMAC.
func isSyncPath(path string) bool {
	return path == "/api/auth/sync"
}

// verifyHMAC checks the X-Auth-Signature / X-Auth-Timestamp headers.
// Returns false and writes an HTTP error if verification fails.
func (m *SecurityMiddleware) verifyHMAC(w http.ResponseWriter, r *http.Request, body []byte) bool {
	sig := r.Header.Get("X-Auth-Signature")
	tsStr := r.Header.Get("X-Auth-Timestamp")

	if sig == "" || tsStr == "" || len(m.masterKey) == 0 {
		log.Printf("[security] unauthorized from %s: missing signature or master key not set", m.clientIP(r))
		http.Error(w, "Unauthorized: signature required", http.StatusUnauthorized)
		return false
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		http.Error(w, "Unauthorized: invalid timestamp", http.StatusUnauthorized)
		return false
	}

	now := time.Now().Unix()
	if now-ts > 60 || ts-now > 60 {
		log.Printf("[security] unauthorized from %s: timestamp expired (skew=%ds)", m.clientIP(r), now-ts)
		http.Error(w, "Unauthorized: timestamp expired", http.StatusUnauthorized)
		return false
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "multipart/") {
			http.Error(w, "Unsupported Media Type: expected application/json", http.StatusUnsupportedMediaType)
			return false
		}
	}

	expected := CalculateHMAC(r.Method, r.URL.RequestURI(), tsStr, string(body), m.masterKey)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		log.Printf("[security] unauthorized from %s: signature mismatch path=%s", m.clientIP(r), r.URL.Path)
		http.Error(w, "Unauthorized: invalid signature", http.StatusUnauthorized)
		return false
	}
	return true
}

// LimitRealtime wraps a handler with per-IP rate limiting + HMAC verification.
// Use for high-frequency endpoints (mouse/keyboard WebSocket).
func (m *SecurityMiddleware) LimitRealtime(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.realtimeLimiter.get(m.clientIP(r)).Allow() {
			writeRateLimitJSON(w)
			return
		}
		if !isPublicPath(r.URL.Path) && !isSyncPath(r.URL.Path) {
			body := readBody(r)
			if !m.verifyHMAC(w, r, body) {
				return
			}
		}
		next(w, r)
	}
}

// LimitPolling wraps a handler with polling-rate limiting + HMAC verification.
func (m *SecurityMiddleware) LimitPolling(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.pollingLimiter.get(m.clientIP(r)).Allow() {
			writeRateLimitJSON(w)
			return
		}
		body := readBody(r)
		if !m.verifyHMAC(w, r, body) {
			return
		}
		next(w, r)
	}
}

// LimitSync rate-limits the /api/auth/sync endpoint only (no HMAC — payload is AES-GCM).
func (m *SecurityMiddleware) LimitSync(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.authLimiter.get(m.clientIP(r)).Allow() {
			writeRateLimitJSON(w)
			return
		}
		next(w, r)
	}
}

// Public wraps a public endpoint (no auth, but still rate-limited via polling limiter).
func (m *SecurityMiddleware) Public(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.pollingLimiter.get(m.clientIP(r)).Allow() {
			writeRateLimitJSON(w)
			return
		}
		next(w, r)
	}
}

// OptionalVerify rate-limits specifically for PIN entry, and verifies the HMAC signature when
// present. usbridge_client submits the Moonlight pairing PIN unsigned before it
// has received the master key (pre-pair), and signed afterwards (post-pair) —
// this accepts both while still rejecting a forged signature.
func (m *SecurityMiddleware) OptionalVerify(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.pinLimiter.get(m.clientIP(r)).Allow() {
			writeRateLimitJSON(w)
			return
		}
		if r.Header.Get("X-Auth-Signature") != "" || r.Header.Get("X-Auth-Timestamp") != "" {
			body := readBody(r)
			if !m.verifyHMAC(w, r, body) {
				return
			}
		}
		next(w, r)
	}
}

func readBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))
	return body
}

func writeRateLimitJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"success":false,"error":"rate_limit","details":"too many requests"}`))
}
