package usage

import (
	"testing"
	"time"
)

func TestAggregateSummary(t *testing.T) {
	records := []*Record{
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 100, Fallbacks: 0, Timestamp: time.Now()},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "bigmodel", Route: "/think", Model: "m2", Tokens: 200, Fallbacks: 1, Timestamp: time.Now()},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/ultrathink", Model: "m1", Tokens: 300, Fallbacks: 0, Timestamp: time.Now()},
	}

	summary := AggregateSummary(records)

	if summary.TotalRequests != 3 {
		t.Errorf("requests = %d, want 3", summary.TotalRequests)
	}
	if summary.TotalTokens != 600 {
		t.Errorf("tokens = %d, want 600", summary.TotalTokens)
	}
	if summary.TotalFallbacks != 1 {
		t.Errorf("fallbacks = %d, want 1", summary.TotalFallbacks)
	}
}

func TestAggregateByRoute(t *testing.T) {
	records := []*Record{
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 100, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "bigmodel", Route: "/think", Model: "m2", Tokens: 200, Fallbacks: 1},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/ultrathink", Model: "m1", Tokens: 300, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 50, Fallbacks: 0},
	}

	byRoute := AggregateByRoute(records)

	// Should have 2 composite keys (openrouter + bigmodel /think merged)
	if len(byRoute) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(byRoute))
	}

	// Check cost-saving//think (openrouter + bigmodel merged)
	think := byRoute["cost-saving//think"]
	if think == nil {
		t.Fatal("expected cost-saving//think key to exist")
	}
	if think.Requests != 3 {
		t.Errorf("cost-saving//think requests = %d, want 3", think.Requests)
	}
	if think.Tokens != 350 {
		t.Errorf("cost-saving//think tokens = %d, want 350", think.Tokens)
	}
	if think.Profile != "cost-saving" {
		t.Errorf("cost-saving//think profile = %s, want cost-saving", think.Profile)
	}
	if think.Route != "/think" {
		t.Errorf("Route field = %s, want /think", think.Route)
	}
	if think.Fallbacks != 1 {
		t.Errorf("cost-saving//think fallbacks = %d, want 1", think.Fallbacks)
	}

	// Check cost-saving//ultrathink
	ultra := byRoute["cost-saving//ultrathink"]
	if ultra == nil {
		t.Fatal("expected cost-saving//ultrathink key to exist")
	}
	if ultra.Requests != 1 {
		t.Errorf("cost-saving//ultrathink requests = %d, want 1", ultra.Requests)
	}
	if ultra.Tokens != 300 {
		t.Errorf("cost-saving//ultrathink tokens = %d, want 300", ultra.Tokens)
	}
}

func TestAggregateByModel(t *testing.T) {
	records := []*Record{
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 100, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "bigmodel", Route: "/think", Model: "m2", Tokens: 200, Fallbacks: 1},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/ultrathink", Model: "m1", Tokens: 300, Fallbacks: 0},
	}

	byModel := AggregateByModel(records)

	// Should have 2 composite keys
	if len(byModel) != 2 {
		t.Fatalf("expected 2 models, got %d", len(byModel))
	}

	// Check cost-saving/openrouter.m1
	m1 := byModel["cost-saving/openrouter.m1"]
	if m1 == nil {
		t.Fatal("expected cost-saving/openrouter.m1 key to exist")
	}
	if m1.Requests != 2 {
		t.Errorf("cost-saving/openrouter.m1 requests = %d, want 2", m1.Requests)
	}
	if m1.Tokens != 400 {
		t.Errorf("cost-saving/openrouter.m1 tokens = %d, want 400", m1.Tokens)
	}
	if m1.Profile != "cost-saving" {
		t.Errorf("cost-saving/openrouter.m1 profile = %s, want cost-saving", m1.Profile)
	}

	// Check cost-saving/bigmodel.m2
	m2 := byModel["cost-saving/bigmodel.m2"]
	if m2 == nil {
		t.Fatal("expected cost-saving/bigmodel.m2 key to exist")
	}
	if m2.Requests != 1 {
		t.Errorf("cost-saving/bigmodel.m2 requests = %d, want 1", m2.Requests)
	}
	if m2.Tokens != 200 {
		t.Errorf("cost-saving/bigmodel.m2 tokens = %d, want 200", m2.Tokens)
	}
}

func TestAggregateByRoute_LegacyEmptyProvider(t *testing.T) {
	// Records with empty provider and profile (legacy/migrated) should still work
	records := []*Record{
		{InstanceID: "inst1", Profile: "", Provider: "", Route: "/think", Model: "m1", Tokens: 100, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "", Provider: "", Route: "/think", Model: "m1", Tokens: 200, Fallbacks: 0},
	}

	byRoute := AggregateByRoute(records)

	if len(byRoute) != 1 {
		t.Fatalf("expected 1 route, got %d", len(byRoute))
	}

	stats := byRoute["default//think"]
	if stats == nil {
		t.Fatal("expected default//think key for legacy records")
	}
	if stats.Requests != 2 {
		t.Errorf("requests = %d, want 2", stats.Requests)
	}
	if stats.Tokens != 300 {
		t.Errorf("tokens = %d, want 300", stats.Tokens)
	}
	if stats.Profile != "default" {
		t.Errorf("profile = %s, want default", stats.Profile)
	}
}

func TestAggregateByRoute_SeparateProfiles(t *testing.T) {
	// Same provider+route but different profiles should produce separate entries
	records := []*Record{
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 100, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 50, Fallbacks: 1},
		{InstanceID: "inst1", Profile: "performance", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 200, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "performance", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 150, Fallbacks: 2},
	}

	byRoute := AggregateByRoute(records)

	if len(byRoute) != 2 {
		t.Fatalf("expected 2 routes (separate profiles), got %d", len(byRoute))
	}

	costSaving := byRoute["cost-saving//think"]
	if costSaving == nil {
		t.Fatal("expected cost-saving//think key")
	}
	if costSaving.Requests != 2 {
		t.Errorf("cost-saving requests = %d, want 2", costSaving.Requests)
	}
	if costSaving.Tokens != 150 {
		t.Errorf("cost-saving tokens = %d, want 150", costSaving.Tokens)
	}
	if costSaving.Fallbacks != 1 {
		t.Errorf("cost-saving fallbacks = %d, want 1", costSaving.Fallbacks)
	}

	perf := byRoute["performance//think"]
	if perf == nil {
		t.Fatal("expected performance//think key")
	}
	if perf.Requests != 2 {
		t.Errorf("performance requests = %d, want 2", perf.Requests)
	}
	if perf.Tokens != 350 {
		t.Errorf("performance tokens = %d, want 350", perf.Tokens)
	}
	if perf.Fallbacks != 2 {
		t.Errorf("performance fallbacks = %d, want 2", perf.Fallbacks)
	}
}

func TestAggregateByModel_SeparateProfiles(t *testing.T) {
	// Same provider+model but different profiles should produce separate entries
	records := []*Record{
		{InstanceID: "inst1", Profile: "cost-saving", Provider: "openrouter", Route: "/think", Model: "claude-sonnet-4", Tokens: 500, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "performance", Provider: "openrouter", Route: "/think", Model: "claude-sonnet-4", Tokens: 800, Fallbacks: 1},
	}

	byModel := AggregateByModel(records)

	if len(byModel) != 2 {
		t.Fatalf("expected 2 models (separate profiles), got %d", len(byModel))
	}

	cs := byModel["cost-saving/openrouter.claude-sonnet-4"]
	if cs == nil {
		t.Fatal("expected cost-saving/openrouter.claude-sonnet-4 key")
	}
	if cs.Requests != 1 || cs.Tokens != 500 {
		t.Errorf("cost-saving: requests=%d tokens=%d, want 1 500", cs.Requests, cs.Tokens)
	}

	pf := byModel["performance/openrouter.claude-sonnet-4"]
	if pf == nil {
		t.Fatal("expected performance/openrouter.claude-sonnet-4 key")
	}
	if pf.Requests != 1 || pf.Tokens != 800 {
		t.Errorf("performance: requests=%d tokens=%d, want 1 800", pf.Requests, pf.Tokens)
	}
}

func TestAggregateByRoute_EmptyProfileDefaults(t *testing.T) {
	// Empty profile records should be grouped under "default"
	records := []*Record{
		{InstanceID: "inst1", Profile: "", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 100, Fallbacks: 0},
		{InstanceID: "inst1", Profile: "default", Provider: "openrouter", Route: "/think", Model: "m1", Tokens: 200, Fallbacks: 0},
	}

	byRoute := AggregateByRoute(records)

	// Both should be grouped together
	if len(byRoute) != 1 {
		t.Fatalf("expected 1 route, got %d", len(byRoute))
	}

	stats := byRoute["default//think"]
	if stats == nil {
		t.Fatal("expected default//think key")
	}
	if stats.Requests != 2 {
		t.Errorf("requests = %d, want 2", stats.Requests)
	}
	if stats.Tokens != 300 {
		t.Errorf("tokens = %d, want 300", stats.Tokens)
	}
}
