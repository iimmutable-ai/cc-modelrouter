package qos

import (
	"sync"
	"time"
)

const (
	// InitialProviderLimit is the starting concurrency limit per provider.
	InitialProviderLimit = 50

	// ProviderFloor is the minimum concurrency limit per provider.
	ProviderFloor = 1

	// AIMDWindowSize is the sliding window duration for tracking 429s.
	AIMDWindowSize = 60 * time.Second

	// AIMDSuccessInterval is the number of successes between additive increases.
	AIMDSuccessInterval = 10
)

// ProviderCapacityTracker tracks per-provider concurrency limits using AIMD.
type ProviderCapacityTracker struct {
	mu         sync.RWMutex
	limits     map[int]int32       // current limit per provider index
	claims     map[int]int32       // current in-flight count per provider
	hist429    map[int][]time.Time // recent 429 timestamps per provider
	successCnt map[int]int         // success count since last increase
}

// NewProviderCapacityTracker creates a new tracker.
func NewProviderCapacityTracker() *ProviderCapacityTracker {
	return &ProviderCapacityTracker{
		limits:     make(map[int]int32),
		claims:     make(map[int]int32),
		hist429:    make(map[int][]time.Time),
		successCnt: make(map[int]int),
	}
}

// RegisterProvider adds a provider with the initial limit.
func (t *ProviderCapacityTracker) RegisterProvider(providerIdx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.limits[providerIdx]; !ok {
		t.limits[providerIdx] = InitialProviderLimit
	}
}

// Acquire attempts to claim one slot for the provider. Returns false if at capacity.
func (t *ProviderCapacityTracker) Acquire(providerIdx int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	limit, ok := t.limits[providerIdx]
	if !ok {
		limit = InitialProviderLimit
		t.limits[providerIdx] = limit
	}

	if t.claims[providerIdx] >= limit {
		return false
	}
	t.claims[providerIdx]++
	return true
}

// Release frees one slot for the provider.
func (t *ProviderCapacityTracker) Release(providerIdx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.claims[providerIdx] > 0 {
		t.claims[providerIdx]--
	}
}

// Record429 records a 429 response and adjusts the provider limit downward.
func (t *ProviderCapacityTracker) Record429(providerIdx int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	// Prune old entries
	window := now.Add(-AIMDWindowSize)
	var recent []time.Time
	for _, ts := range t.hist429[providerIdx] {
		if ts.After(window) {
			recent = append(recent, ts)
		}
	}
	recent = append(recent, now)
	t.hist429[providerIdx] = recent

	limit := t.limits[providerIdx]

	// Multiplicative decrease: halve if 2+ recent 429s, reduce 20% if 1
	if len(recent) >= 2 {
		limit = limit / 2
	} else {
		limit = limit * 4 / 5 // 80% = -20%
	}
	if limit < ProviderFloor {
		limit = ProviderFloor
	}
	t.limits[providerIdx] = limit
}

// RecordSuccess records a successful response and may increment the limit.
func (t *ProviderCapacityTracker) RecordSuccess(providerIdx int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.successCnt[providerIdx]++
	if t.successCnt[providerIdx] >= AIMDSuccessInterval {
		t.successCnt[providerIdx] = 0

		// Only increase if no recent 429s
		now := time.Now()
		window := now.Add(-AIMDWindowSize)
		recent429 := false
		for _, ts := range t.hist429[providerIdx] {
			if ts.After(window) {
				recent429 = true
				break
			}
		}
		if !recent429 {
			t.limits[providerIdx]++
		}
	}
}

// GetLimit returns the current concurrency limit for a provider.
func (t *ProviderCapacityTracker) GetLimit(providerIdx int) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return int(t.limits[providerIdx])
}

// GetClaim returns the current in-flight count for a provider.
func (t *ProviderCapacityTracker) GetClaim(providerIdx int) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return int(t.claims[providerIdx])
}

// TotalCapacity returns the sum of all provider limits.
func (t *ProviderCapacityTracker) TotalCapacity() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := 0
	for _, limit := range t.limits {
		total += int(limit)
	}
	return total
}

// TotalClaims returns the sum of all provider claims.
func (t *ProviderCapacityTracker) TotalClaims() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := 0
	for _, claim := range t.claims {
		total += int(claim)
	}
	return total
}

// ResetProvider resets a provider's limit to the initial default.
func (t *ProviderCapacityTracker) ResetProvider(providerIdx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.limits[providerIdx] = InitialProviderLimit
	t.claims[providerIdx] = 0
	t.hist429[providerIdx] = nil
	t.successCnt[providerIdx] = 0
}

// GetProviderStats returns a snapshot of all provider stats for the admin API.
func (t *ProviderCapacityTracker) GetProviderStats() map[int]struct {
	Limit  int
	Claims int
} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	stats := make(map[int]struct{ Limit, Claims int })
	for idx := range t.limits {
		stats[idx] = struct{ Limit, Claims int }{
			Limit:  int(t.limits[idx]),
			Claims: int(t.claims[idx]),
		}
	}
	return stats
}
