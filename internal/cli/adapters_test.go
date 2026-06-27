package cli

import (
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
)

func TestNewRouterAdapter(t *testing.T) {
	engine := &router.Engine{}
	adapter := NewRouterAdapter(engine)

	if adapter == nil {
		t.Error("expected non-nil adapter")
	}
	if adapter.engine != engine {
		t.Error("expected adapter.engine to be the provided engine")
	}
}

func TestRouterAdapter_DetectRoute(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:model",
			},
		},
	}
	engine := router.NewEngine(cfg)
	adapter := NewRouterAdapter(engine)

	req := router.RouteRequest{
		IsBackground: false,
		ThinkLevel:   router.ThinkNone,
		TokenCount:   1000,
		HasWebSearch: false,
		HasImages:    false,
	}

	// Just call the method - it should delegate to engine.DetectRoute
	route := adapter.DetectRoute(req)

	if route == "" {
		t.Error("expected a route to be returned")
	}

	if route != "default" {
		t.Errorf("expected route 'default', got '%s'", route)
	}
}

func TestRouterAdapter_GetTargets(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "anthropic:claude-3-5-sonnet",
			},
		},
	}
	engine := router.NewEngine(cfg)
	adapter := NewRouterAdapter(engine)

	targets := adapter.GetTargets("default")

	if targets == nil {
		t.Error("expected targets to be returned")
	}
}

func TestNewRegistryAdapter(t *testing.T) {
	realRegistry := transformer.NewRegistry()
	adapter := NewRegistryAdapter(realRegistry)

	if adapter == nil {
		t.Error("expected non-nil adapter")
	}
	if adapter.registry != realRegistry {
		t.Error("expected adapter.registry to be the provided registry")
	}
}

func TestRegistryAdapter_Get_Success(t *testing.T) {
	// Use the real transformer registry with a real transformer
	realRegistry := transformer.NewRegistry()
	realRegistry.Register(transformers.NewAnthropicTransformer())
	adapter := NewRegistryAdapter(realRegistry)

	result, err := adapter.Get("anthropic")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result == nil {
		t.Error("expected non-nil transformer")
	}

	if result.Name() != "anthropic" {
		t.Errorf("expected transformer to have name 'anthropic', got '%s'", result.Name())
	}
}

func TestRegistryAdapter_Get_NotFound(t *testing.T) {
	realRegistry := transformer.NewRegistry()
	adapter := NewRegistryAdapter(realRegistry)

	_, err := adapter.Get("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent transformer")
	}
}

func TestRegistryAdapter_Get_ReturnsTransformer(t *testing.T) {
	realRegistry := transformer.NewRegistry()
	realRegistry.Register(transformers.NewAnthropicTransformer())
	adapter := NewRegistryAdapter(realRegistry)

	result, err := adapter.Get("anthropic")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result.Name() != "anthropic" {
		t.Errorf("expected name 'anthropic', got '%s'", result.Name())
	}
}

func TestRouterAdapter_SetActiveProfile(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {
					Name:   "Fast",
					Routes: map[string]string{"default": "p:fast"},
				},
				"quality": {
					Name:   "Quality",
					Routes: map[string]string{"default": "p:quality"},
				},
			},
		},
	}
	engine := router.NewEngine(cfg)
	engine.SetActiveProfile("fast")
	adapter := NewRouterAdapter(engine)

	// Verify initial profile
	targets := adapter.GetTargets("default")
	if len(targets) == 0 || targets[0].Model != "fast" {
		t.Errorf("expected fast model, got %v", targets)
	}

	// Switch profile via adapter
	adapter.SetActiveProfile("quality")

	// Verify profile was switched on the engine
	targets = adapter.GetTargets("default")
	if len(targets) == 0 || targets[0].Model != "quality" {
		t.Errorf("expected quality model after switch, got %v", targets)
	}
}
