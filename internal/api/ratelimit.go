package api

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// rateLimiter is an in-memory token-bucket limiter keyed by arbitrary string
// (client IP or user ID). No external dependencies; buckets idle for more
// than rateLimiterIdle are swept once the map grows, so memory stays bounded
// behind an internet-facing deployment.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	capacity float64 // burst size
	perMinute float64 // sustained refill rate, tokens per minute
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

const (
	rateLimiterIdle      = 10 * time.Minute
	rateLimiterSweepSize = 4096
)

func newRateLimiter(capacity int, perMinute int) *rateLimiter {
	return &rateLimiter{
		buckets:   map[string]*rateBucket{},
		capacity:  float64(capacity),
		perMinute: float64(perMinute),
	}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.buckets) > rateLimiterSweepSize {
		for k, b := range l.buckets {
			if now.Sub(b.last) > rateLimiterIdle {
				delete(l.buckets, k)
			}
		}
	}

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &rateBucket{tokens: l.capacity, last: now}
		l.buckets[key] = bucket
	}
	elapsed := now.Sub(bucket.last).Minutes()
	bucket.tokens += elapsed * l.perMinute
	if bucket.tokens > l.capacity {
		bucket.tokens = l.capacity
	}
	bucket.last = now

	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// clientKeyForRateLimit identifies the caller for rate limiting. Forwarded
// headers are only trusted when the TCP peer is loopback — the supported
// exposure path is cloudflared running on the same host, which connects from
// 127.0.0.1 and sets CF-Connecting-IP to the real client address. On a direct
// connection the client controls its own headers, so a spoofable value would
// let an attacker rotate keys to bypass the limit; only the socket address is
// trustworthy there.
func clientKeyForRateLimit(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return host
	}
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if first != "" {
			return first
		}
	}
	return host
}

// maxAuthBodyBytes caps the unauthenticated auth endpoints (login, register,
// invitation validate/accept) whose payloads are a few fields at most.
const maxAuthBodyBytes int64 = 16 << 10 // 16 KB

// withJSONBodyLimit wraps a handler with http.MaxBytesReader so oversized
// bodies fail instead of being parsed. PocketBase's global 32 MB limit still
// applies to the content-bearing endpoints (chapters, glossaries, epubs).
func withJSONBodyLimit(limit int64, next func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if e.Request.Body != nil {
			e.Request.Body = http.MaxBytesReader(e.Response, e.Request.Body, limit)
		}
		return next(e)
	}
}

// withIPRateLimit wraps a handler with a per-client-IP limiter. Rejected
// requests get a 429 with Retry-After: 60.
func withIPRateLimit(limiter *rateLimiter, next func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if !limiter.allow(clientKeyForRateLimit(e.Request)) {
			slog.Warn("rate limit exceeded", "path", e.Request.URL.Path)
			e.Response.Header().Set("Retry-After", strconv.Itoa(60))
			return writeV1Error(e, http.StatusTooManyRequests, "rate_limited", "too many requests, try again later")
		}
		return next(e)
	}
}
