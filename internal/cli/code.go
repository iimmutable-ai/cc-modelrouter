package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	"github.com/iimmutable-ai/cc-modelrouter/internal/version"
	"github.com/spf13/cobra"
)

// NewCodeCommand creates the code command.
func NewCodeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code [flags] [-- <claude-args>...]",
		Short: "Start router and launch Claude Code",
		Long: `Starts the router server and launches Claude Code with the router configured as the API endpoint.

This command automatically configures Claude Code to use the router by setting
the ANTHROPIC_BASE_URL environment variable and creating a temporary
.claude/settings.local.json file in the current project.

Flags:
  -c, --config <path>   Path to custom configuration file.
                        If not specified, searches for config in:
                        - <project>/.cc-modelrouter/config.json (project)
                        - ~/.cc-modelrouter/config.json (global)

Examples:
  # Start with default configuration
  ccrouter code

  # Start with custom config file
  ccrouter code --config /path/to/config.json

Notes:
  - The router runs in the foreground and terminates when Claude Code exits
  - Press Ctrl+C to stop both the router and Claude Code
  - The temporary settings.local.json file is cleaned up on exit

Flags:
  --log-destination <dest>  Log destination: "file", "stdout", "stderr", or a file path.
                           Overrides config file setting. Default: from config.
  --log-level <level>       Log level: "debug", "info", "warn", or "error".
                           Overrides config file setting. Default: from config.`,
		RunE:                  runCode,
		DisableFlagParsing:    true,
	}

	cmd.Flags().StringP("config", "c", "", "Path to config file")
	cmd.Flags().String("log-destination", "", "Log destination (file|stdout|stderr|path)")
	cmd.Flags().String("log-level", "", "Log level: debug, info, warn, error (default: from config)")
	cmd.Flags().IntP("port", "p", 0, "Port to listen on (default: 0 = OS picks a free port)")
	cmd.Flags().String("profile", "", "Specify which route profile to use at startup")

	return cmd
}

func runCode(cmd *cobra.Command, rawArgs []string) error {
	// --- Manual flag parsing ---
	var configPath, logDestination, logLevel, profileFlag string
	var portFlag int
	var claudeArgs []string

	knownFlagsWithValues := map[string]bool{
		"--config": true, "-c": true,
		"--log-destination": true,
		"--log-level": true,
		"--port": true, "-p": true,
		"--profile": true,
	}

	skipNext := false
	for i := 0; i < len(rawArgs); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		arg := rawArgs[i]

		// Handle --help
		if arg == "--help" || arg == "-h" {
			cmd.Usage()
			return nil
		}

		// Handle -- separator: everything after goes to Claude
		if arg == "--" {
			claudeArgs = append(claudeArgs, rawArgs[i+1:]...)
			break
		}

		// Handle --flag=value syntax
		consumed := false
		if strings.Contains(arg, "=") {
			for flag := range knownFlagsWithValues {
				if strings.HasPrefix(arg, flag+"=") {
					val := arg[len(flag)+1:]
					switch flag {
					case "--config", "-c":
						configPath = val
					case "--log-destination":
						logDestination = val
					case "--log-level":
						logLevel = val
					case "--port", "-p":
						portFlag, _ = strconv.Atoi(val)
					case "--profile":
						profileFlag = val
					}
					consumed = true
					break
				}
			}
		}
		if consumed {
			continue
		}

		if knownFlagsWithValues[arg] {
			// Consume this flag and its value
			if i+1 < len(rawArgs) {
				val := rawArgs[i+1]
				skipNext = true
				switch arg {
				case "--config", "-c":
					configPath = val
				case "--log-destination":
					logDestination = val
				case "--log-level":
					logLevel = val
				case "--port", "-p":
					portFlag, _ = strconv.Atoi(val)
				case "--profile":
					profileFlag = val
				}
			}
			continue
		}

		// Not a known flag — pass through to Claude
		claudeArgs = append(claudeArgs, arg)
	}
	// --- End manual flag parsing ---

	// Default to --permission-mode auto unless user opted into --conservative
	// or explicitly set --permission-mode themselves
	hasConservative := slices.Contains(claudeArgs, "--conservative")
	hasPermissionMode := slices.ContainsFunc(claudeArgs, func(arg string) bool {
		return strings.HasPrefix(arg, "--permission-mode")
	})

	if !hasConservative && !hasPermissionMode {
		claudeArgs = append([]string{"--permission-mode", "auto"}, claudeArgs...)
	}

	// Get working directory
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Load configuration
	var cfg *config.Config
	var configType string
	if configPath != "" {
		cfg, err = config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		configType = "custom"
	} else {
		cfg, configType, err = config.LoadWithOverride(projectRoot)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Validate and set profile if specified
	var profileName string
	if profileFlag != "" {
		if !cfg.HasProfiles() {
			return fmt.Errorf("no profiles configured in config, cannot use profile flag")
		}

		if _, ok := cfg.Router.Profiles[profileFlag]; !ok {
			availableProfiles := cfg.GetProfileNames()
			return fmt.Errorf("invalid profile '%s', available profiles: %v", profileFlag, availableProfiles)
		}

		profileName = profileFlag
		fmt.Printf("Using profile: %s\n", profileName)
	} else {
		profileName = cfg.GetDefaultProfile()
	}

	// Start the router — use port from flag (default 0 = OS picks), ignoring config's port
	instanceID := daemon.GenerateInstanceID()
	serverPort := portFlag // default 0 if flag not explicitly set

	// Apply log destination overrides from flags
	if logDestination != "" {
		cfg.Logging.Destination = logDestination
		cfg.Logging.Enabled = true // CLI flag implicitly enables logging
	}

	// Apply log level override from flag
	if logLevel != "" {
		cfg.Logging.Level = logLevel
		cfg.Logging.Enabled = true // CLI flag implicitly enables logging
	}

	// Set per-instance log file path if logging to file
	if cfg.Logging.ShouldLogToFile() && cfg.Logging.FilePath == "" {
		logPath, err := cfg.Logging.GetLogPathWithInstance(instanceID)
		if err == nil {
			cfg.Logging.FilePath = logPath
		}
	}

	// Initialize logging - IMPORTANT: Do this before starting server
	// to prevent log messages from breaking Claude Code's UI
	logCleanup, err := logging.Init(&cfg.Logging)
	if err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}
	defer logCleanup()

	// Verify logging is working by writing a test message
	logging.Infof("Logging initialized - router starting for Claude Code")

	// Start in foreground mode
	fmt.Printf("Starting ccrouter %s...\n", version.String())
	if cfg.Logging.IsEnabled() {
		if cfg.Logging.Destination == "file" || cfg.Logging.Destination == "" {
			if logPath, logErr := cfg.Logging.GetLogPath(); logErr == nil {
				fmt.Printf("Logging to: %s\n", logPath)
			}
		} else if cfg.Logging.Destination == "stdout" {
			fmt.Printf("Logging to: stdout\n")
		} else if cfg.Logging.Destination == "stderr" {
			fmt.Printf("Logging to: stderr\n")
		} else {
			// Custom path
			fmt.Printf("Logging to: %s\n", cfg.Logging.Destination)
		}
	} else {
		fmt.Printf("Logging: disabled\n")
	}

	// Create server
	serverCfg := &proxy.ServerConfig{
		Host: cfg.Server.Host,
		Port: serverPort,
	}
	server, err := proxy.NewServer(serverCfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Setup router engine
	routerEngine := router.NewEngine(cfg)
	routerEngine.SetActiveProfile(profileName)
	server.SetRouter(NewRouterAdapter(routerEngine))

	// Setup transformer registry
	registry := transformer.NewRegistry()
	// New transformers (Anthropic-centric interface)
	registry.Register(transformers.NewAnthropicTransformer())
	registry.Register(transformers.NewGLMAnthropicTransformer())
	registry.Register(transformers.NewOpenRouterTransformer())
	registry.Register(transformers.NewOpenAITransformer())
	registry.Register(transformers.NewGeminiTransformer())
	// Note: Qwen and MiniMax now use the Anthropic transformer since they are Anthropic-compatible
	// GLM providers (aliyun, bigmodel) use the GLM-specific transformer which ensures signature field handling
	// OpenRouter providers use the OpenRouter-specific transformer which preserves signature fields
	server.SetTransformerRegistry(NewRegistryAdapter(registry))

	// Setup provider clients
	clients := make(map[string]proxy.HTTPClient)
	for name, providerCfg := range cfg.Providers {
		// Validate API key is not empty or unset
		if providerCfg.APIKey == "" {
			return fmt.Errorf("provider %s: API key is empty (check environment variable)", name)
		}
		if strings.HasPrefix(providerCfg.APIKey, "${") {
			return fmt.Errorf("provider %s: API key environment variable not set: %s", name, providerCfg.APIKey)
		}

		client, err := provider.NewClient(&provider.ClientConfig{
			BaseURL:           providerCfg.BaseURL,
			APIKey:            providerCfg.APIKey,
			MaxRetries:        cfg.Router.MaxRetries,
			RetryDelay:        cfg.Router.GetRetryDelay(),
			DisableKeepAlives: providerCfg.DisableKeepAlives,
		})
		if err != nil {
			return fmt.Errorf("failed to create client for %s: %w", name, err)
		}
		clients[name] = client
	}
	server.SetProviderClients(clients)

	// Create streaming clients (no timeout for long-running SSE streams)
	streamingClients := make(map[string]proxy.HTTPClient)
	for name, providerCfg := range cfg.Providers {
		streamingClient, err := provider.NewStreamingClient(&provider.ClientConfig{
			BaseURL:           providerCfg.BaseURL,
			APIKey:            providerCfg.APIKey,
			MaxRetries:        cfg.Router.MaxRetries,
			RetryDelay:        cfg.Router.GetRetryDelay(),
			DisableKeepAlives: providerCfg.DisableKeepAlives,
		})
		if err != nil {
			return fmt.Errorf("failed to create streaming client for %s: %w", name, err)
		}
		streamingClients[name] = streamingClient
	}
	server.SetStreamingClients(streamingClients)

	server.SetConfig(cfg)

	// Initialize usage tracker
	dbPath, err := usage.DBPath()
	if err != nil {
		return fmt.Errorf("failed to get db path: %w", err)
	}

	usageDB, err := usage.InitDB(dbPath)
	if err != nil {
		return fmt.Errorf("failed to init usage db: %w", err)
	}

	tracker := usage.NewTracker(usageDB, usage.DefaultBufferSize, usage.DefaultFlushTimeout)
	server.SetUsageTracker(tracker)
	server.SetInstanceID(instanceID)

	// Generate admin token for runtime profile management
	adminToken := daemon.GenerateAdminToken()
	server.SetAdminToken(adminToken)

	// Initialize handler's active profile
	server.SetActiveProfile(profileName)

	// Start server
	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	// Get actual bound address (important when port is 0 — OS assigns a free port)
	actualAddr := server.ActualAddr()
	_, actualPort, _ := net.SplitHostPort(actualAddr)
	fmt.Printf("Router started on %s\n", actualAddr)

	// Save instance metadata with admin token and active profile
	meta := &daemon.InstanceMetadata{
		ID:           instanceID,
		Port:         cfg.Server.Port,
		PID:          os.Getpid(),
		ConfigType:   configType,
		ConfigPath:   configPath,
		ProjectRoot:  projectRoot,
		StartTime:    time.Now(),
		AdminToken:   adminToken,
		ActiveProfile: profileName,
	}
	if actualPort != "" {
		fmt.Sscanf(actualPort, "%d", &meta.Port)
	}
	if err := daemon.SaveInstance(meta); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save instance metadata: %v\n", err)
	}

	// Create temporary settings.local.json to override global settings
	// This ensures Claude Code uses the router's base URL
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.local.json")
	settingsDir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create .claude directory: %v\n", err)
	}

	settings := map[string]interface{}{}
	// Read existing settings to preserve entries from other instances
	if existing, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(existing, &settings)
	}
	if settings["env"] == nil {
		settings["env"] = map[string]interface{}{}
	}
	envMap := settings["env"].(map[string]interface{})
	envMap["ANTHROPIC_BASE_URL"] = fmt.Sprintf("http://%s", actualAddr)

	settingsData, err := json.MarshalIndent(settings, "", "  ")
	if err == nil {
		if err := os.WriteFile(settingsPath, settingsData, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write settings.local.json: %v\n", err)
		} else {
			defer func() {
				// Only delete if file still points to our proxy.
				// If another instance overwrote it, the file belongs to them.
				data, err := os.ReadFile(settingsPath)
				if err == nil {
					var s map[string]interface{}
					if json.Unmarshal(data, &s) == nil {
						if env, ok := s["env"].(map[string]interface{}); ok {
							if url, ok := env["ANTHROPIC_BASE_URL"].(string); ok && url == fmt.Sprintf("http://%s", actualAddr) {
								os.Remove(settingsPath)
							}
						}
					}
				} else if os.IsNotExist(err) {
					// File already gone, nothing to do
				} else {
					// Unexpected error, try to remove anyway
					os.Remove(settingsPath)
				}
			}()
			fmt.Printf("Created %s to route requests through the proxy\n", settingsPath)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal settings: %v\n", err)
	}

	// Create profile slash command file for easy profile switching
	profileCmdPath := filepath.Join(settingsDir, "commands", "profile.md")
	if err := createProfileSlashCommand(profileCmdPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create profile slash command: %v\n", err)
	} else {
		defer func() {
			// Only delete if no other ccrouter code instance is running in this project.
			instances, _ := daemon.ListInstances()
			activeInProject := 0
			for _, inst := range instances {
				if inst.ProjectRoot == projectRoot && daemon.IsRunning(inst) {
					activeInProject++
				}
			}
			if activeInProject == 0 {
				os.Remove(profileCmdPath)
			}
		}()
		fmt.Printf("Created %s for profile switching\n", profileCmdPath)
	}

	// Propagate settings into existing worktree directories
	propagateSettingsToWorktrees(projectRoot, actualAddr)

	// Start watching for new worktree creation during the session
	watcherCtx, watcherCancel := context.WithCancel(context.Background())
	defer watcherCancel()
	if stopWatcher, err := startWorktreeWatcher(watcherCtx, projectRoot, actualAddr); err == nil {
		defer stopWatcher()
		fmt.Println("Watching for new worktrees")
	} else {
		fmt.Fprintf(os.Stderr, "Warning: worktree watcher failed: %v\n", err)
	}

	// Set environment variable for Claude Code (as backup)
	os.Setenv("ANTHROPIC_BASE_URL", fmt.Sprintf("http://%s", actualAddr))

	// Find claude binary
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		// Stop server and return error
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Stop(ctx)
		daemon.DeleteInstance(instanceID)
		return fmt.Errorf("claude binary not found in PATH: %w", err)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Launch Claude Code
	claudeCmd := exec.Command(claudePath, claudeArgs...)
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr
	claudeCmd.Env = os.Environ()

	// Start Claude in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- claudeCmd.Run()
	}()

	// Wait for either Claude to finish or a signal
	select {
	case err := <-errChan:
		if err != nil {
			// Error exit — immediate cleanup
			fmt.Fprintf(os.Stderr, "Claude exited with error: %v\n", err)
		} else {
			// Clean exit — might be worktree switch or subagents still running.
			// Keep proxy alive while connections are active.
			fmt.Println("Claude Code exited — waiting up to 120s for active connections...")
			graceDeadline := time.After(120 * time.Second)
			ticker := time.NewTicker(2 * time.Second)
		graceLoop:
			for {
				select {
				case <-ticker.C:
					conns := server.ActiveConnections()
					if conns == 0 {
						fmt.Println("All connections closed, shutting down")
						break graceLoop
					}
					fmt.Printf("  %d active connection(s) remaining...\n", conns)
				case <-graceDeadline:
					fmt.Println("Grace period expired, shutting down")
					break graceLoop
				case sig := <-sigChan:
					fmt.Printf("\nReceived signal %v during grace period, shutting down...\n", sig)
					break graceLoop
				}
			}
			ticker.Stop()
		}
	case sig := <-sigChan:
		// Signal received — forward to Claude and shut down immediately
		fmt.Printf("\nReceived signal %v, shutting down...\n", sig)
		if claudeCmd.Process != nil {
			claudeCmd.Process.Signal(sig)
		}
	}

	// Cleanup
	cleanupWorktreeSettings(projectRoot, actualAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Stop(ctx)
	daemon.DeleteInstance(instanceID)

	fmt.Println("Router stopped")
	return nil
}

// createProfileSlashCommand creates a slash command file for profile switching.
// It uses an embedded template that discovers the running instance dynamically,
// so no hardcoded port, token, or profile list is needed.
func createProfileSlashCommand(path string) error {
	// Create commands directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := templateFS.ReadFile("templates/profile.md")
	if err != nil {
		return fmt.Errorf("failed to read embedded profile template: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}
