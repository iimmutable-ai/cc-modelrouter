// Package qos provides quality-of-service with priority queuing and WRED.
package qos

import "math/rand"

// WREDConfig controls the Weighted Random Early Detection parameters.
type WREDConfig struct {
	MinDepth float64 // Queue depth percentage where dropping begins (default 0.5)
	MaxDepth float64 // Queue depth percentage where dropping is certain (default 0.9)
}

// DefaultWREDConfig returns default WRED parameters.
func DefaultWREDConfig() WREDConfig {
	return WREDConfig{
		MinDepth: 0.5,
		MaxDepth: 0.9,
	}
}

// ShouldDrop returns true if the request should be dropped based on WRED.
// depthPct is the current queue depth as a fraction (0.0 to 1.0+).
// Returns (shouldDrop, dropProbability).
func (w *WREDConfig) ShouldDrop(depthPct float64) (bool, float64) {
	if depthPct <= w.MinDepth {
		return false, 0
	}
	if depthPct >= w.MaxDepth || w.MaxDepth <= w.MinDepth {
		return true, 1.0
	}

	prob := (depthPct - w.MinDepth) / (w.MaxDepth - w.MinDepth)
	if rand.Float64() < prob {
		return true, prob
	}
	return false, prob
}
