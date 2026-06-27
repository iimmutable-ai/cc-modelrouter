package cli

import (
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
)

// RouterAdapter adapts router.Engine to proxy.Router interface.
type RouterAdapter struct {
	engine *router.Engine
}

// NewRouterAdapter creates a new router adapter.
func NewRouterAdapter(engine *router.Engine) *RouterAdapter {
	return &RouterAdapter{engine: engine}
}

// DetectRoute implements proxy.Router.
func (a *RouterAdapter) DetectRoute(req router.RouteRequest) string {
	return a.engine.DetectRoute(req)
}

// GetTargets implements proxy.Router.
func (a *RouterAdapter) GetTargets(routeName string) []config.RouteTarget {
	return a.engine.GetTargets(routeName)
}

// SetActiveProfile implements proxy.Router.
func (a *RouterAdapter) SetActiveProfile(profile string) {
	a.engine.SetActiveProfile(profile)
}

// RegistryAdapter adapts transformer.Registry to proxy.TransformerRegistry interface.
type RegistryAdapter struct {
	registry *transformer.Registry
}

// NewRegistryAdapter creates a new registry adapter.
func NewRegistryAdapter(registry *transformer.Registry) *RegistryAdapter {
	return &RegistryAdapter{registry: registry}
}

// Get implements proxy.TransformerRegistry.
func (a *RegistryAdapter) Get(name string) (transformer.Transformer, error) {
	return a.registry.Get(name)
}
