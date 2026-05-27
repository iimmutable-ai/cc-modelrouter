package router

import (
	"sync"
	"testing"

	"github.com/iimmutable/cc-modelrouter/internal/config"
)

func TestRouteDetection(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default":     "bigmodel:glm-4.7;openrouter:claude-sonnet-4.5",
				"background":  "bigmodel:glm-4.5-air",
				"longContext": "bigmodel:glm-4.7;gemini:gemini-2.5-pro",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name     string
		req      RouteRequest
		expected string
	}{
		{
			name:     "default route",
			req:      RouteRequest{},
			expected: "default",
		},
		{
			name:     "background route",
			req:      RouteRequest{IsBackground: true},
			expected: "background",
		},
		{
			name:     "long context route",
			req:      RouteRequest{TokenCount: 70000},
			expected: "longContext",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := engine.DetectRoute(tt.req)
			if route != tt.expected {
				t.Errorf("expected route %s, got %s", tt.expected, route)
			}
		})
	}
}

func TestThinkLevelDetection(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default":    "bigmodel:glm-4.7",
				"think":      "openrouter:claude-sonnet-4.5",
				"thinkMore":  "openrouter:claude-sonnet-4.5",
				"ultrathink": "openrouter:claude-opus-4.5",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name     string
		req      RouteRequest
		expected string
	}{
		{
			name:     "no thinking",
			req:      RouteRequest{ThinkLevel: ThinkNone},
			expected: "default",
		},
		{
			name:     "basic thinking",
			req:      RouteRequest{ThinkLevel: ThinkBasic},
			expected: "think",
		},
		{
			name:     "middle thinking",
			req:      RouteRequest{ThinkLevel: ThinkMiddle},
			expected: "thinkMore",
		},
		{
			name:     "highest thinking",
			req:      RouteRequest{ThinkLevel: ThinkHighest},
			expected: "ultrathink",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := engine.DetectRoute(tt.req)
			if route != tt.expected {
				t.Errorf("expected route %s, got %s", tt.expected, route)
			}
		})
	}
}

func TestThinkLevelFallback(t *testing.T) {
	// Test fallback when only "think" route is configured
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "bigmodel:glm-4.7",
				"think":   "openrouter:claude-sonnet-4.5",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name     string
		req      RouteRequest
		expected string
	}{
		{
			name:     "basic thinking uses think route",
			req:      RouteRequest{ThinkLevel: ThinkBasic},
			expected: "think",
		},
		{
			name:     "middle thinking falls back to think",
			req:      RouteRequest{ThinkLevel: ThinkMiddle},
			expected: "think",
		},
		{
			name:     "highest thinking falls back to think",
			req:      RouteRequest{ThinkLevel: ThinkHighest},
			expected: "think",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := engine.DetectRoute(tt.req)
			if route != tt.expected {
				t.Errorf("expected route %s, got %s", tt.expected, route)
			}
		})
	}
}

func TestThinkLevelPartialConfig(t *testing.T) {
	// Test when thinkMore is configured but ultrathink is not
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default":   "bigmodel:glm-4.7",
				"think":     "openrouter:claude-sonnet-4.5",
				"thinkMore": "openrouter:claude-sonnet-4.5",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name     string
		req      RouteRequest
		expected string
	}{
		{
			name:     "basic thinking uses think route",
			req:      RouteRequest{ThinkLevel: ThinkBasic},
			expected: "think",
		},
		{
			name:     "middle thinking uses thinkMore",
			req:      RouteRequest{ThinkLevel: ThinkMiddle},
			expected: "thinkMore",
		},
		{
			name:     "highest thinking falls back to thinkMore",
			req:      RouteRequest{ThinkLevel: ThinkHighest},
			expected: "thinkMore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := engine.DetectRoute(tt.req)
			if route != tt.expected {
				t.Errorf("expected route %s, got %s", tt.expected, route)
			}
		})
	}
}

func TestRoutePriority(t *testing.T) {
	// Test that route detection follows correct priority order:
	// background > ultrathink > thinkMore > think > image > webSearch > longContext > default
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default":     "provider:default-model",
				"background":  "provider:background-model",
				"think":       "provider:think-model",
				"thinkMore":   "provider:thinkmore-model",
				"ultrathink":  "provider:ultrathink-model",
				"image":       "provider:image-model",
				"webSearch":   "provider:websearch-model",
				"longContext": "provider:longcontext-model",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name     string
		req      RouteRequest
		expected string
	}{
		{
			name:     "background takes priority over think",
			req:      RouteRequest{IsBackground: true, ThinkLevel: ThinkHighest},
			expected: "background",
		},
		{
			name:     "background takes priority over image",
			req:      RouteRequest{IsBackground: true, HasImages: true},
			expected: "background",
		},
		{
			name:     "highest think takes priority over image",
			req:      RouteRequest{ThinkLevel: ThinkHighest, HasImages: true},
			expected: "ultrathink",
		},
		{
			name:     "basic think takes priority over image",
			req:      RouteRequest{ThinkLevel: ThinkBasic, HasImages: true},
			expected: "think",
		},
		{
			name:     "image takes priority over webSearch",
			req:      RouteRequest{HasImages: true, HasWebSearch: true},
			expected: "image",
		},
		{
			name:     "webSearch takes priority over longContext",
			req:      RouteRequest{HasWebSearch: true, TokenCount: 100000},
			expected: "webSearch",
		},
		{
			name:     "longContext when no other flags",
			req:      RouteRequest{TokenCount: 100000},
			expected: "longContext",
		},
		{
			name:     "default when no flags",
			req:      RouteRequest{},
			expected: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := engine.DetectRoute(tt.req)
			if route != tt.expected {
				t.Errorf("expected route %s, got %s", tt.expected, route)
			}
		})
	}
}

func TestThinkLevelNoRouteConfigured(t *testing.T) {
	// Test when no thinking routes are configured - should fall back to default
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:default-model",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name     string
		req      RouteRequest
		expected string
	}{
		{
			name:     "basic thinking falls back to default",
			req:      RouteRequest{ThinkLevel: ThinkBasic},
			expected: "default",
		},
		{
			name:     "middle thinking falls back to default",
			req:      RouteRequest{ThinkLevel: ThinkMiddle},
			expected: "default",
		},
		{
			name:     "highest thinking falls back to default",
			req:      RouteRequest{ThinkLevel: ThinkHighest},
			expected: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := engine.DetectRoute(tt.req)
			if route != tt.expected {
				t.Errorf("expected route %s, got %s", tt.expected, route)
			}
		})
	}
}

func TestGetTargets(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default":    "provider1:model1;provider2:model2",
				"think":      "provider3:model3",
				"ultrathink": "provider4:model4",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name          string
		routeName     string
		expectedCount int
		firstProvider string
		firstModel    string
	}{
		{
			name:          "default route with multiple targets",
			routeName:     "default",
			expectedCount: 2,
			firstProvider: "provider1",
			firstModel:    "model1",
		},
		{
			name:          "think route with single target",
			routeName:     "think",
			expectedCount: 1,
			firstProvider: "provider3",
			firstModel:    "model3",
		},
		{
			name:          "unknown route falls back to default",
			routeName:     "unknown",
			expectedCount: 2,
			firstProvider: "provider1",
			firstModel:    "model1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := engine.GetTargets(tt.routeName)
			if len(targets) != tt.expectedCount {
				t.Errorf("expected %d targets, got %d", tt.expectedCount, len(targets))
			}
			if len(targets) > 0 {
				if targets[0].Provider != tt.firstProvider {
					t.Errorf("expected first provider %s, got %s", tt.firstProvider, targets[0].Provider)
				}
				if targets[0].Model != tt.firstModel {
					t.Errorf("expected first model %s, got %s", tt.firstModel, targets[0].Model)
				}
			}
		})
	}
}

func TestProfileBasedRouting(t *testing.T) {
	// Test routing with profiles configured
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-default", // Legacy routes
				"think":   "provider:legacy-think",
			},
			Profiles: map[string]config.ProfileConfig{
				"fast": {
					Name:        "Fast",
					Description: "Fast models",
					Routes: map[string]string{
						"default": "provider:fast-default",
						"think":   "provider:fast-think",
					},
				},
				"quality": {
					Name:        "Quality",
					Description: "Quality models",
					Routes: map[string]string{
						"default":    "provider:quality-default",
						"think":      "provider:quality-think",
						"ultrathink": "provider:quality-ultrathink",
					},
				},
			},
		},
	}

	engine := NewEngine(cfg)
	engine.SetActiveProfile("quality")

	tests := []struct {
		name          string
		routeName     string
		expectedModel string
	}{
		{
			name:          "default uses quality profile",
			routeName:     "default",
			expectedModel: "quality-default",
		},
		{
			name:          "think uses quality profile",
			routeName:     "think",
			expectedModel: "quality-think",
		},
		{
			name:          "ultrathink uses quality profile",
			routeName:     "ultrathink",
			expectedModel: "quality-ultrathink",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := engine.GetTargets(tt.routeName)
			if len(targets) == 0 {
				t.Fatal("expected at least one target")
			}
			if targets[0].Model != tt.expectedModel {
				t.Errorf("expected model %s, got %s", tt.expectedModel, targets[0].Model)
			}
		})
	}
}

func TestProfileSwitching(t *testing.T) {
	// Test that switching profiles changes the active routes
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-default",
			},
			Profiles: map[string]config.ProfileConfig{
				"fast": {
					Name: "Fast",
					Routes: map[string]string{
						"default": "provider:fast-default",
					},
				},
				"quality": {
					Name: "Quality",
					Routes: map[string]string{
						"default": "provider:quality-default",
					},
				},
			},
		},
	}

	engine := NewEngine(cfg)
	engine.SetActiveProfile("fast")

	// Initially uses fast profile
	targets := engine.GetTargets("default")
	if len(targets) == 0 || targets[0].Model != "fast-default" {
		t.Errorf("expected fast-default model initially, got %v", targets)
	}

	// Switch to quality profile
	engine.SetActiveProfile("quality")

	// Now should use quality profile
	targets = engine.GetTargets("default")
	if len(targets) == 0 || targets[0].Model != "quality-default" {
		t.Errorf("expected quality-default model after switch, got %v", targets)
	}
}

func TestProfileBasedRouting_ThreadSafe(t *testing.T) {
	// Test that concurrent access to routes is thread-safe when mutex is set
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-default",
			},
			Profiles: map[string]config.ProfileConfig{
				"profile1": {
					Name: "Profile 1",
					Routes: map[string]string{
						"default": "provider:model1",
					},
				},
				"profile2": {
					Name: "Profile 2",
					Routes: map[string]string{
						"default": "provider:model2",
					},
				},
			},
		},
	}

	engine := NewEngine(cfg)
	engine.SetActiveProfile("profile1")

	// Create a mutex and set it
	mu := &sync.RWMutex{}
	engine.SetConfigMutex(mu)

	// Run concurrent reads and profile switches
	done := make(chan bool)
	var readErrors []error
	var switchErrors []error

	// Concurrent readers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				targets := engine.GetTargets("default")
				if len(targets) == 0 {
					readErrors = append(readErrors, nil)
				}
			}
			done <- true
		}()
	}

	// Concurrent profile switches
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				mu.Lock()
				if engine.GetActiveProfile() == "profile1" {
					engine.SetActiveProfile("profile2")
				} else {
					engine.SetActiveProfile("profile1")
				}
				mu.Unlock()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	if len(readErrors) > 0 {
		t.Errorf("got %d read errors during concurrent access", len(readErrors))
	}
	if len(switchErrors) > 0 {
		t.Errorf("got %d switch errors during concurrent access", len(switchErrors))
	}
}

func TestProfileFallbackToLegacy(t *testing.T) {
	// Test that when no profiles are configured, legacy routes are used
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-default",
				"think":   "provider:legacy-think",
			},
		},
		Profiles: map[string]config.ProfileConfig{}, // Empty profiles
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name          string
		routeName     string
		expectedModel string
	}{
		{
			name:          "default uses legacy",
			routeName:     "default",
			expectedModel: "legacy-default",
		},
		{
			name:          "think uses legacy",
			routeName:     "think",
			expectedModel: "legacy-think",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := engine.GetTargets(tt.routeName)
			if len(targets) == 0 {
				t.Fatal("expected at least one target")
			}
			if targets[0].Model != tt.expectedModel {
				t.Errorf("expected model %s, got %s", tt.expectedModel, targets[0].Model)
			}
		})
	}
}

func TestReviewAndSubagentRoutes(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default":  "provider:default-model",
				"review":   "provider:review-model",
				"subagent": "provider:subagent-model",
			},
		},
	}

	engine := NewEngine(cfg)

	tests := []struct {
		name     string
		req      RouteRequest
		expected string
	}{
		{
			name:     "subagent route",
			req:      RouteRequest{IsSubagent: true},
			expected: "subagent",
		},
		{
			name:     "review route",
			req:      RouteRequest{IsReview: true},
			expected: "review",
		},
		{
			name:     "subagent takes priority over review",
			req:      RouteRequest{IsSubagent: true, IsReview: true},
			expected: "subagent",
		},
		{
			name:     "subagent takes priority over think",
			req:      RouteRequest{IsSubagent: true, ThinkLevel: ThinkHighest},
			expected: "subagent",
		},
		{
			name:     "review takes priority over think",
			req:      RouteRequest{IsReview: true, ThinkLevel: ThinkHighest},
			expected: "review",
		},
		{
			name:     "review falls back to default when not configured",
			req:      RouteRequest{IsReview: true},
			expected: "default",
		},
		{
			name:     "subagent falls back to default when not configured",
			req:      RouteRequest{IsSubagent: true},
			expected: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For fallback tests, use a config without the specific route
			var testEngine *Engine
			if tt.name == "review falls back to default when not configured" ||
				tt.name == "subagent falls back to default when not configured" {
				testCfg := &config.Config{
					Router: config.RouterConfig{
						Routes: map[string]string{
							"default": "provider:default-model",
						},
					},
				}
				testEngine = NewEngine(testCfg)
			} else {
				testEngine = engine
			}

			route := testEngine.DetectRoute(tt.req)
			if route != tt.expected {
				t.Errorf("expected route %s, got %s", tt.expected, route)
			}
		})
	}
}
