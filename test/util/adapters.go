//go:build network || edge_tests || error_tests || load || cli_tests || provider_quirks || integration || integration_real

package util

import (
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
)

// RouterAdapter adapts router.Engine to proxy.RouteDetector interface.
type RouterAdapter struct {
	Engine *router.Engine
}

func (a *RouterAdapter) DetectRoute(req router.RouteRequest) string {
	return a.Engine.DetectRoute(req)
}

func (a *RouterAdapter) GetTargets(routeName string) []config.RouteTarget {
	return a.Engine.GetTargets(routeName)
}

// RegistryAdapter adapts transformer.Registry to proxy.TransformerRegistry interface.
type RegistryAdapter struct {
	Registry *transformer.Registry
}

func (a *RegistryAdapter) Get(name string) (transformer.Transformer, error) {
	return a.Registry.Get(name)
}