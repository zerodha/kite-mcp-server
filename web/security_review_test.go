package web

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

func TestSlash48FloodCannotStarveUnrelatedLoginPeers(t *testing.T) {
	l := NewRateLimiter()
	defer l.Shutdown()
	for i := 0; i < 65536; i++ {
		limiter := l.getLimiter(fmt.Sprintf("2001:db8:abcd:%x::1", i))
		for limiter.Allow() {
		}
	}
	if l.limiters.Len() > maxPeersPerNetwork {
		t.Fatalf("one /48 occupied %d dedicated slots", l.limiters.Len())
	}
	for key, count := range l.networkPeers {
		if count > maxPeersPerNetwork {
			t.Fatalf("network %s occupied %d slots", key, count)
		}
	}
	for i := 0; i < 200; i++ {
		if !l.getLimiter(fmt.Sprintf("198.51.100.%d", i+1)).Allow() {
			t.Fatalf("unrelated login peer %d was starved by a /48", i)
		}
	}
}

func TestNetworkSpilloverCannotDrainEveryFallbackBucket(t *testing.T) {
	l := NewRateLimiter()
	defer l.Shutdown()
	for i := 0; i < maxRateLimitPeers; i++ {
		l.getLimiter(fmt.Sprintf("peer-%d", i))
	}
	first := l.getLimiter("2001:db8:abcd::1")
	for i := 1; i < 65536; i++ {
		if l.getLimiter(fmt.Sprintf("2001:db8:abcd:%x::1", i)) != first {
			t.Fatal("one /48 acquired another overflow bucket")
		}
	}
	for range 5 {
		first.Allow()
	}
	if first.Allow() {
		t.Fatal("overflow quota was reset")
	}
	independent := false
	for i := 0; i < 200; i++ {
		next := l.getLimiter(fmt.Sprintf("198.51.%d.1", i))
		if next != first && next.Allow() {
			independent = true
			break
		}
	}
	if !independent {
		t.Fatal("one network exhausted all fallback buckets")
	}
}

func TestNetworkOccupancyIsReclaimedAfterIdleExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := NewRateLimiter()
		defer l.Shutdown()
		for i := 0; i < maxPeersPerIPv6Subnet; i++ {
			l.getLimiter(fmt.Sprintf("2001:db8:abcd:%x::1", i))
		}
		if l.networkPeers["2001:db8:abcd::/56"] != maxPeersPerIPv6Subnet {
			t.Fatal("subnet occupancy not tracked")
		}
		time.Sleep(61 * time.Second)
		l.cleanupInactive(time.Minute)
		if l.limiters.Len() != 0 || len(l.networkPeers) != 0 {
			t.Fatal("idle peers left network reservations behind")
		}
		if !l.getLimiter("2001:db8:abcd:ff::1").Allow() || l.limiters.Len() != 1 {
			t.Fatal("network could not acquire a dedicated slot after expiry")
		}
	})
}
