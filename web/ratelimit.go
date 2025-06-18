package web

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter manages rate limiting for incoming requests.
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.Mutex
}

// NewRateLimiter creates a new rate limiter manager.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

func (m *RateLimiter) getLimiter(ip string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	limiter, exists := m.limiters[ip]
	if !exists {
		// Allow 5 requests every 12 seconds.
		limiter = rate.NewLimiter(rate.Every(12*time.Second), 5)
		m.limiters[ip] = limiter
	}
	return limiter
}

// Middleware returns a middleware that enforces rate limiting.
func (m *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !m.getLimiter(ip).Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
