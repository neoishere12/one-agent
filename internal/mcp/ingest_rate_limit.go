package mcp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ingestRateLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	buckets map[string]ingestRateBucket
}

type ingestRateBucket struct {
	start time.Time
	count int
}

func newIngestRateLimiter(max int, window time.Duration) *ingestRateLimiter {
	if max <= 0 || window <= 0 {
		return nil
	}
	return &ingestRateLimiter{max: max, window: window, buckets: make(map[string]ingestRateBucket)}
}

func (l *ingestRateLimiter) Allow(key string, now time.Time) bool {
	if l == nil {
		return true
	}
	if key == "" {
		key = "unknown"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[key]
	if !ok || bucket.start.IsZero() || now.Sub(bucket.start) >= l.window {
		l.buckets[key] = ingestRateBucket{start: now, count: 1}
		return true
	}
	if bucket.count >= l.max {
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func (s *Server) allowIngest(r *http.Request) bool {
	return s.ingestLimiter.Allow(ingestClientKey(r), s.now())
}

func ingestClientKey(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if ip := headerClientIP(r.Header.Get("X-Forwarded-For")); ip != "" {
		return ip
	}
	if ip := headerClientIP(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	return remoteAddrHost(r.RemoteAddr)
}

func headerClientIP(raw string) string {
	if raw == "" {
		return ""
	}
	first := strings.TrimSpace(strings.Split(raw, ",")[0])
	if first == "" {
		return ""
	}
	return first
}

func remoteAddrHost(raw string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(raw))
	if err == nil && host != "" {
		return host
	}
	if strings.TrimSpace(raw) == "" {
		return "unknown"
	}
	return strings.TrimSpace(raw)
}
