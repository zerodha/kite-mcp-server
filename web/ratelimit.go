package web

import (
	"context"
	"hash/maphash"
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
	networks   [2]string
}

// RateLimiter manages rate limiting for incoming requests.
type RateLimiter struct {
	limiters       storage.Records[string, *limiterEntry]
	mu             sync.Mutex
	cleanupCancel  context.CancelFunc
	cleanupRunning bool
	trustedProxies []netip.Prefix
	overflow       [1024]*rate.Limiter
	seed           maphash.Seed
	networkPeers   map[string]int
}

const maxRateLimitPeers = 10000
const maxPeersPerNetwork = 256
const maxPeersPerIPv6Subnet = 32

// NewRateLimiter creates a new rate limiter manager.
// Trusted proxy CIDRs are optional. X-Forwarded-For is used only when the
// direct peer belongs to one of them, so clients cannot choose their own key.
func NewRateLimiter(trustedProxyCIDRs ...string) *RateLimiter {
	m := &RateLimiter{
		limiters:     storage.NewMemory[string, *limiterEntry](),
		seed:         maphash.MakeSeed(),
		networkPeers: make(map[string]int),
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
	ip = rateLimitKey(ip)
	networks := rateLimitNetworks(ip)

	entry, exists := m.limiters.Lookup(ip)
	if !exists {
		// Limit table occupancy per /48 and /56 so address rotation in one
		// allocation cannot consume all dedicated peer buckets. Spillover from
		// a network uses one keyed bucket, rather than draining every shard.
		if m.limiters.Len() >= maxRateLimitPeers || m.networkPeers[networks[0]] >= maxPeersPerNetwork || (networks[1] != "" && m.networkPeers[networks[1]] >= maxPeersPerIPv6Subnet) {
			index := maphash.String(m.seed, networks[0]) % uint64(len(m.overflow))
			if m.overflow[index] == nil {
				m.overflow[index] = rate.NewLimiter(rate.Every(12*time.Second), 5)
			}
			return m.overflow[index]
		}
		// Refill one request every 12 seconds, with a burst of five.
		limiter := rate.NewLimiter(rate.Every(12*time.Second), 5)
		entry = &limiterEntry{
			limiter:    limiter,
			lastAccess: time.Now(),
			networks:   networks,
		}
		m.limiters.Set(ip, entry)
		for _, key := range networks {
			if key != "" {
				m.networkPeers[key]++
			}
		}

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

func rateLimitKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}

func rateLimitNetworks(key string) [2]string {
	addr, err := netip.ParseAddr(key)
	if err != nil {
		if prefix, parseErr := netip.ParsePrefix(key); parseErr == nil {
			addr = prefix.Addr()
		} else {
			return [2]string{key}
		}
	}
	addr = addr.Unmap()
	if addr.Is6() {
		return [2]string{netip.PrefixFrom(addr, 48).Masked().String(), netip.PrefixFrom(addr, 56).Masked().String()}
	}
	return [2]string{netip.PrefixFrom(addr, 24).Masked().String()}
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
	// After a minute an idle bucket has refilled completely; forgetting it
	// cannot grant additional requests and need not retain a slot for an hour.
	const maxIdleTime = time.Minute
	const cleanupInterval = time.Minute

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
		for _, key := range m.limiters.Get(ip).networks {
			if key != "" {
				m.networkPeers[key]--
				if m.networkPeers[key] == 0 {
					delete(m.networkPeers, key)
				}
			}
		}
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
