package proxy

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

func TestNewServer(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 8081,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if server == nil {
		t.Error("expected non-nil server")
	}
}

func TestNewServer_NilConfig(t *testing.T) {
	server, err := NewServer(nil)
	if err != nil {
		t.Fatalf("failed to create server with nil config: %v", err)
	}

	if server == nil {
		t.Error("expected non-nil server")
	}

	// Should use defaults
	if server.config.Host != "localhost" {
		t.Errorf("expected host 'localhost', got '%s'", server.config.Host)
	}
	if server.config.Port != 8081 {
		t.Errorf("expected port 8081, got %d", server.config.Port)
	}
}

func TestNewServer_DefaultMaxRequestSize(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 8081,
		// MaxRequestSize not set, should use default
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	expectedMaxSize := int64(50 * 1024 * 1024) // 50MB
	if server.config.MaxRequestSize != expectedMaxSize {
		t.Errorf("expected max request size %d, got %d", expectedMaxSize, server.config.MaxRequestSize)
	}
}

func TestDefaults(t *testing.T) {
	defaults := Defaults()

	if defaults.Host != "localhost" {
		t.Errorf("expected host 'localhost', got '%s'", defaults.Host)
	}
	if defaults.Port != 8081 {
		t.Errorf("expected port 8081, got %d", defaults.Port)
	}
	if defaults.MaxRequestSize != 50*1024*1024 {
		t.Errorf("expected max request size 50MB, got %d", defaults.MaxRequestSize)
	}
}

func TestServer_Addr(t *testing.T) {
	cfg := &ServerConfig{
		Host: "127.0.0.1",
		Port: 9999,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	expected := "127.0.0.1:9999"
	if server.Addr() != expected {
		t.Errorf("expected addr '%s', got '%s'", expected, server.Addr())
	}
}

func TestServer_IsRunning(t *testing.T) {
	server, err := NewServer(&ServerConfig{Host: "localhost", Port: 8082})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if server.IsRunning() {
		t.Error("expected server to not be running initially")
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// No sleep needed - Start() now blocks until server is ready

	if !server.IsRunning() {
		t.Error("expected server to be running after Start()")
	}

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(ctx); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	if server.IsRunning() {
		t.Error("expected server to not be running after Stop()")
	}
}

func TestServer_StartTwice(t *testing.T) {
	server, err := NewServer(&ServerConfig{Host: "localhost", Port: 8083})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop(context.Background())

	// No sleep needed - Start() now blocks until server is ready

	// Try to start again
	err = server.Start()
	if err == nil {
		t.Error("expected error when starting already running server")
	}
}

func TestServer_StopWhenNotRunning(t *testing.T) {
	server, err := NewServer(&ServerConfig{Host: "localhost", Port: 8084})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Stop should not error when server is not running
	ctx := context.Background()
	if err := server.Stop(ctx); err != nil {
		t.Errorf("expected no error when stopping non-running server, got: %v", err)
	}
}

func TestNewServer_WithCustomConfig(t *testing.T) {
	cfg := &ServerConfig{
		Host:           "custom-host",
		Port:           9090,
		MaxRequestSize: 100 * 1024 * 1024,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.config.Host != "custom-host" {
		t.Errorf("expected Host 'custom-host', got %s", server.config.Host)
	}
	if server.config.Port != 9090 {
		t.Errorf("expected Port 9090, got %d", server.config.Port)
	}
	if server.config.MaxRequestSize != 100*1024*1024 {
		t.Errorf("expected MaxRequestSize 100MB, got %d", server.config.MaxRequestSize)
	}
}

func TestNewServer_ZeroMaxRequestSize(t *testing.T) {
	cfg := &ServerConfig{
		Host:           "localhost",
		Port:           8081,
		MaxRequestSize: 0,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Should use default
	if server.config.MaxRequestSize != 50*1024*1024 {
		t.Errorf("expected default MaxRequestSize 50MB, got %d", server.config.MaxRequestSize)
	}
}

func TestServer_Setters(t *testing.T) {
	cfg := &ServerConfig{}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	router := &serverTestMockRouter{}
	server.SetRouter(router)
	if server.handler.router != router {
		t.Error("expected router to be set")
	}

	reg := &serverTestMockTransformerRegistry{}
	server.SetTransformerRegistry(reg)
	if server.handler.transformerRegistry != reg {
		t.Error("expected transformerRegistry to be set")
	}

	clients := map[string]HTTPClient{}
	server.SetProviderClients(clients)
	if server.handler.providerClients == nil {
		t.Error("expected providerClients to be set")
	}

	tracker := &serverTestMockUsageTracker{}
	server.SetUsageTracker(tracker)
	if server.usageTracker != tracker {
		t.Error("expected usageTracker to be set")
	}
	if server.handler.usageTracker != tracker {
		t.Error("expected handler usageTracker to be set")
	}

	server.SetInstanceID("test-instance")
	if server.instanceID != "test-instance" {
		t.Errorf("expected instanceID 'test-instance', got %s", server.instanceID)
	}
}

func TestServer_TimeoutConfiguration(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 0,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Start server to initialize the http.Server
	err = server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// No sleep needed - Start() now blocks until server is ready

	// The underlying http.Server should be created
	if server.server == nil {
		t.Fatal("expected server to be initialized after Start")
	}

	// Check read timeout
	if server.server.ReadTimeout != 30*time.Second {
		t.Errorf("expected ReadTimeout 30s, got %v", server.server.ReadTimeout)
	}

	// Check write timeout
	if server.server.WriteTimeout != 5*time.Minute {
		t.Errorf("expected WriteTimeout 5m, got %v", server.server.WriteTimeout)
	}

	// Clean up
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	server.Stop(ctx)
}

func TestServer_HandlerCreated(t *testing.T) {
	cfg := &ServerConfig{
		MaxRequestSize: 1024 * 1024,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server.handler == nil {
		t.Error("expected handler to be created")
	}
}

func TestServer_HandlerMaxRequestSize(t *testing.T) {
	tests := []struct {
		name           string
		maxRequestSize int64
	}{
		{"1MB", 1024 * 1024},
		{"10MB", 10 * 1024 * 1024},
		{"50MB", 50 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ServerConfig{
				MaxRequestSize: tt.maxRequestSize,
			}

			server, err := NewServer(cfg)
			if err != nil {
				t.Fatalf("NewServer failed: %v", err)
			}

			if server.handler.maxRequestSize != tt.maxRequestSize {
				t.Errorf("expected handler maxRequestSize %d, got %d",
					tt.maxRequestSize, server.handler.maxRequestSize)
			}
		})
	}
}

func TestServer_ShutdownWithUsageTracker(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 0,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Create a usage tracker that implements Shutdown
	tracker := &serverTestShutdownableTracker{
		shutdownCalled: false,
	}
	server.SetUsageTracker(tracker)

	// Start the server
	err = server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// No sleep needed - Start() now blocks until server is ready

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = server.Stop(ctx)
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	// Verify Shutdown was called
	if !tracker.shutdownCalled {
		t.Error("expected usage tracker Shutdown to be called")
	}
}

func TestServer_ConcurrentIsRunning(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 0,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Start the server
	err = server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		server.Stop(ctx)
	}()

	// Test concurrent IsRunning calls
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			if !server.IsRunning() {
				t.Error("expected server to be running")
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestServer_StartWaitsForReadiness(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 0, // Let OS pick port
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Start should return only when server is ready
	err = server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		server.Stop(ctx)
	}()

	// After Start returns, server should be immediately accepting connections
	// Verify by making an HTTP request
	addr := server.Addr()
	if addr == "localhost:0" {
		// If port is 0, we need to get the actual bound port
		// For this test, use a fixed port instead
		t.Skip("Skipping test with dynamic port - use fixed port for readiness test")
	}

	// Try to connect immediately without sleep
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/v1/models", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Server not ready after Start returned: %v", err)
	}
	defer resp.Body.Close()

	// We expect 404 or 200 depending on handler setup
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("Unexpected status code: %d", resp.StatusCode)
	}
}

func TestServer_StartWithFixedPortIsReady(t *testing.T) {
	// Use a random high port to avoid conflicts
	serverCfg := &ServerConfig{
		Host: "127.0.0.1",
		Port: 19101,
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Set a minimal config to avoid nil pointer in handler
	minimalConfig := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"test": {
				BaseURL: "http://example.com",
				APIKey:  "test-key",
				Models:  []string{"test-model"},
			},
		},
	}
	server.SetConfig(minimalConfig)

	// Start should return only when server is ready
	err = server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		server.Stop(ctx)
	}()

	// After Start returns, server should be immediately accepting connections
	// Verify by making an HTTP request WITHOUT any sleep
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:19101/v1/models", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Server not ready after Start returned: %v", err)
	}
	defer resp.Body.Close()

	// Should get a 200 OK for /v1/models
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code: %d", resp.StatusCode)
	}
}

func TestServer_PortZero(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 0, // Let OS pick port
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Addr should still return a formatted address
	addr := server.Addr()
	if addr != "localhost:0" {
		t.Errorf("expected address 'localhost:0', got '%s'", addr)
	}
}

func TestServer_ActualAddr(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 0, // Let OS pick port
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// ActualAddr should be empty before Start
	if server.ActualAddr() != "" {
		t.Errorf("expected empty ActualAddr before Start, got '%s'", server.ActualAddr())
	}

	// Start the server
	err = server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		server.Stop(ctx)
	}()

	// ActualAddr should return the OS-assigned port (not 0)
	actualAddr := server.ActualAddr()
	if actualAddr == "" {
		t.Fatal("expected non-empty ActualAddr after Start")
	}
	if actualAddr == "localhost:0" {
		t.Errorf("ActualAddr should not be 'localhost:0' after Start, got '%s'", actualAddr)
	}

	// Addr should still return the configured address
	if server.Addr() != "localhost:0" {
		t.Errorf("expected Addr 'localhost:0', got '%s'", server.Addr())
	}
}

// Helper types for testing

type serverTestMockRouter struct{}

func (m *serverTestMockRouter) DetectRoute(req router.RouteRequest) string {
	return "default"
}

func (m *serverTestMockRouter) GetTargets(routeName string) []config.RouteTarget {
	return []config.RouteTarget{{Provider: "anthropic", Model: "claude-3"}}
}

func (m *serverTestMockRouter) SetActiveProfile(profile string) {
	// No-op for mock
}

type serverTestMockTransformerRegistry struct{}

func (m *serverTestMockTransformerRegistry) Get(name string) (transformer.Transformer, error) {
	return &serverTestAnthropicTransformer{}, nil
}

type serverTestAnthropicTransformer struct{}

func (m *serverTestAnthropicTransformer) Name() string { return "anthropic" }
func (m *serverTestAnthropicTransformer) Endpoint() string { return "/v1/messages" }
func (m *serverTestAnthropicTransformer) PrepareRequest(req *anthropic.Request, baseURL, apiKey, model string) (*http.Request, error) {
	return nil, nil
}
func (m *serverTestAnthropicTransformer) ParseResponse(resp *http.Response) (*anthropic.Response, error) {
	return &anthropic.Response{}, nil
}
func (m *serverTestAnthropicTransformer) SupportsStreaming() bool {
	return false
}
func (m *serverTestAnthropicTransformer) TransformStreamEvent(event *transformer.SSEEvent) ([]transformer.SSEEvent, error) {
	return nil, nil
}
type serverTestMockUsageTracker struct{}

func (t *serverTestMockUsageTracker) Record(instanceID, route, model, profile, provider string, tokens, fallbacks int) {}

type serverTestShutdownableTracker struct {
	shutdownCalled bool
}

func (t *serverTestShutdownableTracker) Record(instanceID, route, model, profile, provider string, tokens, fallbacks int) {}

func (t *serverTestShutdownableTracker) Shutdown() {
	t.shutdownCalled = true
}

func TestServer_SetActiveProfile(t *testing.T) {
	cfg := &ServerConfig{
		Host: "localhost",
		Port: 0, // Random port
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Set up config with profiles
	appCfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast":    {Name: "Fast", Routes: map[string]string{"default": "p:fast"}},
				"quality": {Name: "Quality", Routes: map[string]string{"default": "p:quality"}},
			},
		},
	}
	server.handler.SetConfig(appCfg)

	// Set active profile before router is set
	server.SetActiveProfile("fast")
	if got := server.handler.GetActiveProfile(); got != "fast" {
		t.Errorf("expected handler profile 'fast', got '%s'", got)
	}

	// Now set the router and verify it also gets the profile
	engine := router.NewEngine(&config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast":    {Name: "Fast", Routes: map[string]string{"default": "p:fast"}},
				"quality": {Name: "Quality", Routes: map[string]string{"default": "p:quality"}},
			},
		},
	})
	server.handler.SetRouter(&routerAdapter{engine: engine})

	// Set active profile after router is set — should propagate to both
	server.SetActiveProfile("quality")
	if got := server.handler.GetActiveProfile(); got != "quality" {
		t.Errorf("expected handler profile 'quality', got '%s'", got)
	}
	if got := engine.GetActiveProfile(); got != "quality" {
		t.Errorf("expected router profile 'quality', got '%s'", got)
	}
}

func TestServer_SetAdminToken_GetAdminToken(t *testing.T) {
	cfg := &ServerConfig{Host: "localhost", Port: 8081}
	server, _ := NewServer(cfg)

	server.SetAdminToken("my-secret-token")
	if got := server.GetAdminToken(); got != "my-secret-token" {
		t.Errorf("expected 'my-secret-token', got '%s'", got)
	}
}

// Minimal adapter for router.Engine to proxy.Router interface
type routerAdapter struct {
	engine *router.Engine
}

func (a *routerAdapter) DetectRoute(req router.RouteRequest) string {
	return a.engine.DetectRoute(req)
}

func (a *routerAdapter) GetTargets(routeName string) []config.RouteTarget {
	return a.engine.GetTargets(routeName)
}

func (a *routerAdapter) SetActiveProfile(profile string) {
	a.engine.SetActiveProfile(profile)
}
