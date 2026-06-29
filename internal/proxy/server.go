// Package proxy implements the HTTP proxy server.
package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/qos"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
)

// ServerConfig represents server configuration.
type ServerConfig struct {
	Host           string
	Port           int
	MaxRequestSize int64
}

// Defaults returns default server configuration.
func Defaults() *ServerConfig {
	return &ServerConfig{
		Host:           "localhost",
		Port:           8081,
		MaxRequestSize: 50 * 1024 * 1024, // 50MB
	}
}

// Server is the HTTP proxy server.
type Server struct {
	config       *ServerConfig
	server       *http.Server
	handler      *Handler
	usageTracker UsageTracker
	instanceID   string
	mu           sync.Mutex
	running      bool
	ready        chan struct{} // Closed when server is ready to accept connections
	actualAddr   string        // Actual bound address (differs from config when port is 0)
	activeConns  atomic.Int32  // Tracks in-flight HTTP requests
	lastActivity atomic.Int64  // Unix nanos of last accepted request start
}

// NewServer creates a new proxy server.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg == nil {
		cfg = Defaults()
	}
	if cfg.MaxRequestSize == 0 {
		cfg.MaxRequestSize = Defaults().MaxRequestSize
	}

	handler := NewHandler(cfg.MaxRequestSize)

	return &Server{
		config:  cfg,
		handler: handler,
	}, nil
}

// SetRouter sets the router for the handler.
func (s *Server) SetRouter(router Router) {
	s.handler.SetRouter(router)
}

// SetTransformerRegistry sets the transformer registry.
func (s *Server) SetTransformerRegistry(reg TransformerRegistry) {
	s.handler.SetTransformerRegistry(reg)
}

// SetProviderClients sets the provider clients.
func (s *Server) SetProviderClients(clients map[string]HTTPClient) {
	s.handler.SetProviderClients(clients)
}

// SetStreamingClients sets the provider clients for streaming requests.
// These clients have no timeout and are optimized for SSE streaming.
func (s *Server) SetStreamingClients(clients map[string]HTTPClient) {
	s.handler.SetStreamingClients(clients)
}

// SetConfig sets the configuration.
func (s *Server) SetConfig(cfg *config.Config) {
	s.handler.SetConfig(cfg)
}

// SetUsageTracker sets the usage tracker.
func (s *Server) SetUsageTracker(tracker UsageTracker) {
	s.usageTracker = tracker
	s.handler.SetUsageTracker(tracker)
}

// SetInstanceID sets the instance ID.
func (s *Server) SetInstanceID(id string) {
	s.instanceID = id
	s.handler.SetInstanceID(id)
}

// SetRequestInterceptors sets the request interceptors.
func (s *Server) SetRequestInterceptors(interceptors []RequestInterceptor) {
	s.handler.SetRequestInterceptors(interceptors)
}

// SetResponseInterceptors sets the response interceptors.
func (s *Server) SetResponseInterceptors(interceptors []ResponseInterceptor) {
	s.handler.SetResponseInterceptors(interceptors)
}

// SetStreamingInterceptors sets the streaming interceptors.
func (s *Server) SetStreamingInterceptors(interceptors []StreamingResponseInterceptor) {
	s.handler.SetStreamingInterceptors(interceptors)
}

// SetAdminToken sets the admin API token for runtime management.
func (s *Server) SetAdminToken(token string) {
	s.handler.SetAdminToken(token)
}

// SetMultiUserEnabled enables or disables multi-user authentication.
func (s *Server) SetMultiUserEnabled(enabled bool) {
	s.handler.SetMultiUserEnabled(enabled)
}

// SetAuthInterceptor sets the auth interceptor for multi-user mode.
func (s *Server) SetAuthInterceptor(ai *AuthInterceptor) {
	s.handler.SetAuthInterceptor(ai)
}

// SetQoSEngine sets the QoS engine for multi-user mode.
func (s *Server) SetQoSEngine(engine *qos.QoSEngine) {
	s.handler.qosEngine = engine
}

// SetActiveProfile sets the initial active profile for the handler and router.
func (s *Server) SetActiveProfile(profile string) {
	s.handler.SetActiveProfile(profile)
	// Also set it on the router if it's already set
	if s.handler.router != nil {
		s.handler.router.SetActiveProfile(profile)
	}
}

// GetAdminToken returns the admin API token.
func (s *Server) GetAdminToken() string {
	return s.handler.GetAdminToken()
}

// AddRequestInterceptor adds a single request interceptor.
func (s *Server) AddRequestInterceptor(interceptor RequestInterceptor) {
	s.handler.AddRequestInterceptor(interceptor)
}

// AddResponseInterceptor adds a single response interceptor.
func (s *Server) AddResponseInterceptor(interceptor ResponseInterceptor) {
	s.handler.AddResponseInterceptor(interceptor)
}

// AddStreamingInterceptor adds a single streaming interceptor.
func (s *Server) AddStreamingInterceptor(interceptor StreamingResponseInterceptor) {
	s.handler.AddStreamingInterceptor(interceptor)
}

// Start starts the server and waits until it's ready to accept connections.
func (s *Server) Start() error {
	s.mu.Lock()

	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	// Create error logger that writes to our configured destination
	// Use standard log package for http.Server.ErrorLog compatibility
	errorLogWriter := logging.GetWriter()
	if errorLogWriter == nil {
		errorLogWriter = io.Discard
	}

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s, // Use Server as handler to track active connections
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long for streaming
		ErrorLog:     log.New(errorLogWriter, "", 0), // No prefix, uses our logging
	}

	// Create readiness channel before starting
	ready := make(chan struct{})
	s.ready = ready

	s.running = true
	s.mu.Unlock()

	// Create listener explicitly to know when we're ready to accept connections
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Lock()
		s.running = false
		s.ready = nil
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Store the actual bound address (differs from config when port is 0)
	s.actualAddr = listener.Addr().String()
	// Initialize lastActivity to boot time so the idle watcher doesn't see a
	// zero value (1970) and immediately trip on its first tick.
	s.lastActivity.Store(time.Now().UnixNano())

	// Launch server in goroutine
	go func() {
		// Signal readiness immediately - listener is already accepting connections
		close(ready)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Log error - in production this should use proper logging
		}
	}()

	return nil
}

// Stop stops the server.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	// Shutdown tracker if it exists
	if shutdowner, ok := s.usageTracker.(interface{ Shutdown() }); ok {
		shutdowner.Shutdown()
	}

	err := s.server.Shutdown(ctx)
	s.running = false
	s.ready = nil // Clean up readiness channel
	return err
}

// Addr returns the configured server address.
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
}

// ActualAddr returns the actual bound address after Start().
// When port 0 is used, this returns the OS-assigned port.
func (s *Server) ActualAddr() string {
	return s.actualAddr
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// ActiveConnections returns the number of currently in-flight HTTP requests.
func (s *Server) ActiveConnections() int32 {
	return s.activeConns.Load()
}

// LastActivity returns the time the most recent HTTP request was accepted.
// Safe to call concurrently with ServeHTTP.
func (s *Server) LastActivity() time.Time {
	return time.Unix(0, s.lastActivity.Load())
}

// ServeHTTP wraps the handler with connection tracking for the grace period logic.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.activeConns.Add(1)
	s.lastActivity.Store(time.Now().UnixNano())
	defer s.activeConns.Add(-1)
	s.handler.ServeHTTP(w, r)
}
