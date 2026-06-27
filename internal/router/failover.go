package router

import (
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// Target represents a route target.
type Target interface {
	Provider() string
	Model() string
}

// routeTarget wraps config.RouteTarget to implement Target interface.
type routeTarget struct {
	config.RouteTarget
}

func (r routeTarget) Provider() string { return r.RouteTarget.Provider }
func (r routeTarget) Model() string    { return r.RouteTarget.Model }

// Failover manages sequential failover with looping.
type Failover struct {
	targets     []Target
	current     int
	attempts    int
	maxAttempts int
}

// NewFailover creates a new failover manager from config route targets.
func NewFailover(targets []config.RouteTarget) *Failover {
	t := make([]Target, len(targets))
	for i, rt := range targets {
		t[i] = routeTarget{rt}
	}

	return &Failover{
		targets:     t,
		current:     0,
		attempts:    0,
		maxAttempts: len(targets) * 2, // 2x loop
	}
}

// NewFailoverFromTargets creates a new failover manager from Target interface slice.
func NewFailoverFromTargets(targets []Target) *Failover {
	return &Failover{
		targets:     targets,
		current:     0,
		attempts:    0,
		maxAttempts: len(targets) * 2, // 2x loop
	}
}

// Next returns the next target in the sequence.
// Returns nil if max attempts reached.
func (f *Failover) Next() Target {
	if f.attempts >= f.maxAttempts {
		return nil
	}

	target := f.targets[f.current]
	f.current = (f.current + 1) % len(f.targets)
	f.attempts++

	return target
}

// HasMore returns true if there are more attempts available.
func (f *Failover) HasMore() bool {
	return f.attempts < f.maxAttempts
}

// MaxAttempts returns the maximum number of attempts.
func (f *Failover) MaxAttempts() int {
	return f.maxAttempts
}

// Attempts returns the current attempt count.
func (f *Failover) Attempts() int {
	return f.attempts
}
