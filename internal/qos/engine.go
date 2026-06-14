package qos

import (
	"context"
	"fmt"
	"math"
	"sync"
)

const (
	// DefaultGlobalMaxConc is the default global max concurrency when not configured.
	DefaultGlobalMaxConc = 100

	// DefaultQueueCapacity is the default per-group queue buffer size.
	DefaultQueueCapacity = 100
)

// GroupConfig defines a group's QoS parameters.
type GroupConfig struct {
	Name           string
	PriorityWeight float64
	MaxConcurrency int
}

// GroupState tracks the runtime state of a group.
type GroupState struct {
	Name            string
	PriorityWeight  float64
	GuaranteedShare int // ceil(globalMax * weight)
	Active          int  // currently in-flight
	Queued          int  // waiting in queue
	Queue           chan struct{} // buffered channel for queue slots
}

// QoSEngine provides admission control with priority queuing and WRED.
type QoSEngine struct {
	mu         sync.RWMutex
	globalMax  int
	wred       WREDConfig
	groups     map[string]*GroupState
	pctTracker *ProviderCapacityTracker
}

// NewQoSEngine creates a new QoS engine with the given configuration.
func NewQoSEngine(globalMax int, wredCfg WREDConfig, groupCfgs []GroupConfig) *QoSEngine {
	if globalMax <= 0 {
		globalMax = DefaultGlobalMaxConc
	}
	if wredCfg.MinDepth == 0 && wredCfg.MaxDepth == 0 {
		wredCfg = DefaultWREDConfig()
	}

	e := &QoSEngine{
		globalMax:  globalMax,
		wred:       wredCfg,
		groups:     make(map[string]*GroupState),
		pctTracker: NewProviderCapacityTracker(),
	}

	// Calculate guaranteed shares
	totalWeight := 0.0
	for _, gc := range groupCfgs {
		totalWeight += gc.PriorityWeight
	}

	for _, gc := range groupCfgs {
		share := 1
		if totalWeight > 0 {
			share = int(math.Ceil(float64(globalMax) * (gc.PriorityWeight / totalWeight)))
		}
		queueCap := DefaultQueueCapacity
		if gc.MaxConcurrency > 0 {
			queueCap = gc.MaxConcurrency * 2
		}
		e.groups[gc.Name] = &GroupState{
			Name:            gc.Name,
			PriorityWeight:  gc.PriorityWeight,
			GuaranteedShare: share,
			Queue:           make(chan struct{}, queueCap),
		}
	}

	return e
}

// Admit attempts to admit a request for the given group.
// Returns nil if admitted, or an error if rejected (429) or queued.
// If admitted, caller must call Release(groupName) when done.
func (e *QoSEngine) Admit(ctx context.Context, groupName string) error {
	e.mu.Lock()
	gs, ok := e.groups[groupName]
	if !ok {
		e.mu.Unlock()
		return nil // Unknown group — admit without QoS
	}

	// Check if within guaranteed share
	if gs.Active < gs.GuaranteedShare {
		gs.Active++
		e.mu.Unlock()
		return nil
	}

	// Check if idle capacity exists from other groups
	totalActive := 0
	for _, g := range e.groups {
		totalActive += g.Active
	}

	// Global cap is min of configured globalMax and provider capacity
	effectiveCap := e.globalMax
	pctCap := e.pctTracker.TotalCapacity()
	if pctCap > 0 && pctCap < effectiveCap {
		effectiveCap = pctCap
	}

	if totalActive < effectiveCap {
		gs.Active++
		e.mu.Unlock()
		return nil
	}

	// No capacity — enter queue with WRED check
	queueDepthPct := float64(len(gs.Queue)+gs.Active) / float64(cap(gs.Queue)+gs.GuaranteedShare)
	shouldDrop, _ := e.wred.ShouldDrop(queueDepthPct)
	if shouldDrop {
		e.mu.Unlock()
		return fmt.Errorf("server overloaded: queue full for group %s", groupName)
	}

	// Enqueue
	gs.Queued++
	e.mu.Unlock()

	// Wait for a slot or context cancellation
	select {
	case gs.Queue <- struct{}{}:
		e.mu.Lock()
		gs.Queued--
		gs.Active++
		e.mu.Unlock()
		return nil
	case <-ctx.Done():
		e.mu.Lock()
		// Remove from queue if still there
		select {
		case <-gs.Queue:
			gs.Queued--
		default:
		}
		e.mu.Unlock()
		return ctx.Err()
	}
}

// Release frees a slot for the given group and admits the next queued request.
func (e *QoSEngine) Release(groupName string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	gs, ok := e.groups[groupName]
	if !ok {
		return
	}
	if gs.Active > 0 {
		gs.Active--
	}

	// Drain one queued request
	select {
	case <-gs.Queue:
		gs.Active++
	default:
	}
}

// ProviderTracker returns the embedded provider capacity tracker.
func (e *QoSEngine) ProviderTracker() *ProviderCapacityTracker {
	return e.pctTracker
}

// GetStats returns a snapshot of QoS stats for the admin API.
func (e *QoSEngine) GetStats() map[string]struct {
	Active          int
	Queued          int
	GuaranteedShare int
	PriorityWeight  float64
} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	stats := make(map[string]struct {
		Active          int
		Queued          int
		GuaranteedShare int
		PriorityWeight  float64
	})
	for name, gs := range e.groups {
		stats[name] = struct {
			Active          int
			Queued          int
			GuaranteedShare int
			PriorityWeight  float64
		}{
			Active:          gs.Active,
			Queued:          gs.Queued,
			GuaranteedShare: gs.GuaranteedShare,
			PriorityWeight:  gs.PriorityWeight,
		}
	}
	return stats
}

// GlobalMax returns the configured global max concurrency.
func (e *QoSEngine) GlobalMax() int {
	return e.globalMax
}
