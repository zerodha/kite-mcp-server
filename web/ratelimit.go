package web

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/zerodha/kite-mcp-server/internal/storage"
	"golang.org/x/time/rate"
)

// limiterEntry holds a rate limiter and its last access time
type limiterEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// RateLimiter manages rate limiting for incoming requests.
type RateLimiter struct {
	limiters       storage.Records[string, *limiterEntry]
	mu             sync.Mutex
	cleanupCancel  context.CancelFunc
	cleanupRunning bool
	trustedProxies []netip.Prefix
	overflow       *rate.Limiter
}

const maxRateLimitPeers = 10000

// NewRateLimiter creates a new rate limiter manager.
// Trusted proxy CIDRs are optional. X-Forwarded-For is used only when the
// direct peer belongs to one of them, so clients cannot choose their own key.
func NewRateLimiter(trustedProxyCIDRs ...string) *RateLimiter {
	m := &RateLimiter{
		limiters: storage.NewMemory[string, *limiterEntry](),
		overflow: rate.NewLimiter(rate.Every(12*time.Second), 5),
	}
	for _, cidr := range trustedProxyCIDRs {
		if prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr)); err == nil {
			m.trustedProxies = append(m.trustedProxies, prefix)
		}
	}
	return m
}

func (m *RateLimiter) clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(ip)
	if err != nil || !m.isTrustedProxy(peer) {
		return ip
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(hops[i])
		if parsed, err := netip.ParseAddr(candidate); err == nil && !m.isTrustedProxy(parsed) {
			return parsed.String()
		}
	}
	return ip
}

func (m *RateLimiter) isTrustedProxy(peer netip.Addr) bool {
	for _, prefix := range m.trustedProxies {
		if prefix.Contains(peer) {
			return true
		}
	}
	return false
}

func (m *RateLimiter) getLimiter(ip string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.limiters.Lookup(ip)
	if !exists {
		// Share a bounded overflow bucket instead of allocating indefinitely or
		// evicting active peers and resetting their rate limits.
		if m.limiters.Len() >= maxRateLimitPeers {
			return m.overflow
		}
		// Allow 5 requests every 12 seconds.
		limiter := rate.NewLimiter(rate.Every(12*time.Second), 5)
		entry = &limiterEntry{
			limiter:    limiter,
			lastAccess: time.Now(),
		}
		m.limiters.Set(ip, entry)

		// Start cleanup goroutine if not already running
		if !m.cleanupRunning {
			m.startCleanup()
		}
	} else {
		// Update last access time
		entry.lastAccess = time.Now()
	}

	return entry.limiter
}

// Shutdown stops background cleanup when the HTTP server is closed.
func (m *RateLimiter) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cleanupCancel != nil {
		m.cleanupCancel()
		m.cleanupCancel = nil
	}
	m.cleanupRunning = false
}

// Middleware returns a middleware that enforces rate limiting.
func (m *RateLimiter) Middleware(next http.Handler) http.Handler {
	return m.MiddlewareWithRejection(next, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	})
}

// MiddlewareWithRejection lets browser routes render their own rate-limit page.
func (m *RateLimiter) MiddlewareWithRejection(next http.Handler, reject http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := m.clientIP(r)
		if !m.getLimiter(ip).Allow() {
			w.Header().Set("Retry-After", "12")
			reject(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// startCleanup starts the background cleanup goroutine
func (m *RateLimiter) startCleanup() {
	if m.cleanupRunning {
		return
	}

	m.cleanupRunning = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cleanupCancel = cancel

	go m.cleanupRoutine(ctx)
}

// cleanupRoutine periodically removes inactive rate limiters
func (m *RateLimiter) cleanupRoutine(ctx context.Context) {
	// Clean up limiters that haven't been used for 1 hour
	const maxIdleTime = time.Hour
	const cleanupInterval = 30 * time.Minute

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanupInactive(maxIdleTime)
		}
	}
}

// cleanupInactive removes rate limiters that haven't been used for the specified duration
func (m *RateLimiter) cleanupInactive(maxIdleTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	toDelete := make([]string, 0)

	for ip, entry := range m.limiters.All() {
		if now.Sub(entry.lastAccess) > maxIdleTime {
			toDelete = append(toDelete, ip)
		}
	}

	for _, ip := range toDelete {
		m.limiters.Delete(ip)
	}

	// If no limiters left, stop the cleanup routine to save resources
	if m.limiters.Len() == 0 {
		if m.cleanupCancel != nil {
			m.cleanupCancel()
			m.cleanupRunning = false
		}
	}
}
