// Package router handles request routing and model selection.
package router

import (
	"sync"

	"github.com/iimmutable/cc-modelrouter/internal/config"
)

const (
	LongContextThreshold = 60000

	// Thinking level thresholds based on budget_tokens
	ThinkLevelNone    = 0
	ThinkLevelBasic   = 4000  // "think"
	ThinkLevelMiddle  = 10000 // "think hard", "think more", "megathink"
	ThinkLevelHighest = 32000 // "ultrathink", "think harder"
)

// ThinkLevel represents the thinking intensity level.
type ThinkLevel int

const (
	ThinkNone ThinkLevel = iota
	ThinkBasic            // ~4K tokens: "think"
	ThinkMiddle           // ~10K tokens: "think hard", "think more"
	ThinkHighest          // ~32K tokens: "ultrathink"
)

// RouteRequest contains information for route detection.
type RouteRequest struct {
	IsBackground bool
	ThinkLevel   ThinkLevel
	TokenCount   int
	HasWebSearch bool
	HasImages    bool
	IsReview     bool // Detected review task
	IsSubagent   bool // Detected subagent task
}

// Engine handles route detection and target selection.
type Engine struct {
	config        *config.Config
	configMu      *sync.RWMutex // Reference to handler's mutex for thread-safe access
	activeProfile string        // Runtime state - current active profile (not from config)
}

// NewEngine creates a new router engine.
func NewEngine(cfg *config.Config) *Engine {
	return &Engine{config: cfg}
}

// SetConfigMutex sets the mutex for thread-safe config access.
func (e *Engine) SetConfigMutex(mu *sync.RWMutex) {
	e.configMu = mu
}

// SetActiveProfile sets the active profile for route selection.
func (e *Engine) SetActiveProfile(profile string) {
	e.activeProfile = profile
}

// GetActiveProfile returns the current active profile.
func (e *Engine) GetActiveProfile() string {
	return e.activeProfile
}

// getRoutes returns the active routes (thread-safe if mutex is set).
func (e *Engine) getRoutes() map[string]string {
	if e.configMu != nil {
		e.configMu.RLock()
		defer e.configMu.RUnlock()
	}
	return e.config.GetActiveRoutes(e.activeProfile)
}

// DetectRoute determines which route to use based on request characteristics.
func (e *Engine) DetectRoute(req RouteRequest) string {
	routes := e.getRoutes()

	// Priority order for route detection
	switch {
	case req.IsBackground:
		if route, ok := routes["background"]; ok && route != "" {
			return "background"
		}

	case req.IsSubagent:
		// Subagent tasks - high priority after background
		if route, ok := routes["subagent"]; ok && route != "" {
			return "subagent"
		}

	case req.IsReview:
		// Review tasks
		if route, ok := routes["review"]; ok && route != "" {
			return "review"
		}

	case req.ThinkLevel >= ThinkHighest:
		// Try ultrathink first for highest level
		if route, ok := routes["ultrathink"]; ok && route != "" {
			return "ultrathink"
		}
		// Fall back to thinkMore if ultrathink not configured
		if route, ok := routes["thinkMore"]; ok && route != "" {
			return "thinkMore"
		}
		// Fall back to think
		if route, ok := routes["think"]; ok && route != "" {
			return "think"
		}

	case req.ThinkLevel >= ThinkMiddle:
		// Try thinkMore for middle level
		if route, ok := routes["thinkMore"]; ok && route != "" {
			return "thinkMore"
		}
		// Fall back to think
		if route, ok := routes["think"]; ok && route != "" {
			return "think"
		}

	case req.ThinkLevel >= ThinkBasic:
		if route, ok := routes["think"]; ok && route != "" {
			return "think"
		}

	case req.HasImages:
		if route, ok := routes["image"]; ok && route != "" {
			return "image"
		}

	case req.HasWebSearch:
		if route, ok := routes["webSearch"]; ok && route != "" {
			return "webSearch"
		}

	case req.TokenCount > LongContextThreshold:
		if route, ok := routes["longContext"]; ok && route != "" {
			return "longContext"
		}
	}

	return "default"
}

// GetTargets returns the route targets for a given route name.
func (e *Engine) GetTargets(routeName string) []config.RouteTarget {
	routes := e.getRoutes()
	route, ok := routes[routeName]
	if !ok {
		route = routes["default"]
	}
	return config.ParseRoute(route)
}
