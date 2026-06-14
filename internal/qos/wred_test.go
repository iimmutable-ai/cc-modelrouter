package qos

import (
	"math"
	"testing"
)

func approxEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

func TestWREDBelowMin(t *testing.T) {
	wred := WREDConfig{MinDepth: 0.5, MaxDepth: 0.9}

	dropped, prob := wred.ShouldDrop(0.3)
	if dropped {
		t.Error("should not drop below min depth")
	}
	if prob != 0 {
		t.Errorf("expected probability 0, got %f", prob)
	}

	dropped, prob = wred.ShouldDrop(0.5)
	if dropped {
		t.Error("should not drop at exactly min depth")
	}
	if prob != 0 {
		t.Errorf("expected probability 0 at min, got %f", prob)
	}
}

func TestWREDAtMax(t *testing.T) {
	wred := WREDConfig{MinDepth: 0.5, MaxDepth: 0.9}

	dropped, prob := wred.ShouldDrop(0.9)
	if !dropped {
		t.Error("should drop at max depth")
	}
	if prob != 1.0 {
		t.Errorf("expected probability 1.0 at max, got %f", prob)
	}

	dropped, prob = wred.ShouldDrop(1.0)
	if !dropped {
		t.Error("should drop above max depth")
	}
	if prob != 1.0 {
		t.Errorf("expected probability 1.0 above max, got %f", prob)
	}
}

func TestWREDLinearProbability(t *testing.T) {
	wred := WREDConfig{MinDepth: 0.5, MaxDepth: 0.9}

	tests := []struct {
		depth    float64
		expected float64
	}{
		{0.7, 0.5},
		{0.6, 0.25},
		{0.8, 0.75},
	}

	for _, tt := range tests {
		_, prob := wred.ShouldDrop(tt.depth)
		if !approxEqual(prob, tt.expected, 0.001) {
			t.Errorf("depth %.1f: expected probability ~%f, got %f", tt.depth, tt.expected, prob)
		}
	}
}

func TestWREDEqualMinMax(t *testing.T) {
	// When min == max, the <= min check catches depthPct == MinDepth first,
	// returning (false, 0). Depths strictly above trigger the MaxDepth <= MinDepth
	// guard and return (true, 1.0).
	wred := WREDConfig{MinDepth: 0.5, MaxDepth: 0.5}

	// At exactly min==max: caught by first check, no drop
	dropped, prob := wred.ShouldDrop(0.5)
	if dropped {
		t.Error("at exactly min==max, first check (<= min) catches it, should not drop")
	}
	if prob != 0 {
		t.Errorf("expected probability 0 at exactly min==max, got %f", prob)
	}

	// Above min==max: triggers MaxDepth <= MinDepth guard, always drop
	dropped, prob = wred.ShouldDrop(0.51)
	if !dropped {
		t.Error("above min==max should always drop (MaxDepth <= MinDepth guard)")
	}
	if prob != 1.0 {
		t.Errorf("expected probability 1.0 above min==max, got %f", prob)
	}
}

func TestWREDZeroDepth(t *testing.T) {
	wred := WREDConfig{MinDepth: 0.5, MaxDepth: 0.9}

	dropped, prob := wred.ShouldDrop(0.0)
	if dropped {
		t.Error("should not drop at zero depth")
	}
	if prob != 0 {
		t.Errorf("expected probability 0 at zero depth, got %f", prob)
	}
}

func TestDefaultWREDConfig(t *testing.T) {
	cfg := DefaultWREDConfig()
	if cfg.MinDepth != 0.5 {
		t.Errorf("expected default MinDepth 0.5, got %f", cfg.MinDepth)
	}
	if cfg.MaxDepth != 0.9 {
		t.Errorf("expected default MaxDepth 0.9, got %f", cfg.MaxDepth)
	}
}

func TestWREDStatisticalDistribution(t *testing.T) {
	wred := WREDConfig{MinDepth: 0.5, MaxDepth: 0.9}

	drops := 0
	n := 10000
	for i := 0; i < n; i++ {
		dropped, _ := wred.ShouldDrop(0.7)
		if dropped {
			drops++
		}
	}

	ratio := float64(drops) / float64(n)
	if ratio < 0.45 || ratio > 0.55 {
		t.Errorf("drop ratio %.2f outside expected range [0.45, 0.55] for p=0.5", ratio)
	}
}
