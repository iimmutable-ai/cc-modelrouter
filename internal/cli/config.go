package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/configwizard"
	"github.com/spf13/cobra"
	tea "github.com/charmbracelet/bubbletea"
)

// NewConfigCommand creates the config command - launches the TUI wizard.
func NewConfigCommand() *cobra.Command {
	var shellExport bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Interactive configuration wizard",
		Long: `Launch an interactive terminal UI for managing ccrouter configuration.

This command opens a menu-driven interface where you can:
  - Manage API providers (add, edit, test connectivity)
  - Configure routing rules
  - Set server host and port
  - Configure logging settings
  - View current configuration

Use "ccrouter config" to launch the wizard.

Note: This replaces the old "show", "path", and "init" subcommands.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if shellExport {
				return runShellExport()
			}
			return runConfigWizard(cmd, args)
		},
	}

	cmd.Flags().BoolVar(&shellExport, "shell-export", false, "Print shell export commands (for eval)")
	return cmd
}

func runConfigWizard(cmd *cobra.Command, args []string) error {
	// Get config path
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	configPath := config.ProjectConfigPath(projectRoot)

	// Try to load existing config, or create default
	var cfg *config.Config
	if _, err := os.Stat(configPath); err == nil {
		cfg, err = config.LoadRaw(configPath)
		if err != nil {
			// If load fails, use default config
			cfg = config.Defaults()
		}
	} else {
		// Try global config
		globalPath := config.GlobalConfigPath()
		if _, err := os.Stat(globalPath); err == nil {
			cfg, err = config.LoadRaw(globalPath)
			if err != nil {
				cfg = config.Defaults()
			}
			configPath = globalPath
		} else {
			// No config exists, create default
			cfg = config.Defaults()
		}
	}

	// Create wizard model
	model := configwizard.NewWizardModel(cfg, configPath)

	// Run the Bubble Tea program
	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("failed to run wizard: %w", err)
	}

	// After TUI exits, print source hint if API keys were saved
	if wm, ok := finalModel.(*configwizard.WizardModel); ok {
		keys := wm.ResolvedKeys()
		if len(keys) > 0 {
			shellCfg, err := configwizard.GetShellConfig()
			if err == nil {
				envPath, err := shellCfg.WriteEnvFile(keys)
				if err == nil {
					fmt.Fprintf(os.Stderr, "\nTo apply API keys in current shell: source %s\n", envPath)
					fmt.Fprintf(os.Stderr, "Or for future sessions: eval \"$(ccrouter config --shell-export)\"\n")
				}
			}
		}
	}

	return nil
}

// runShellExport prints export commands to stdout, suitable for eval.
func runShellExport() error {
	home, _ := os.UserHomeDir()
	envPath := filepath.Join(home, ".cc-modelrouter", "shell_env.sh")

	// Try to read the pre-generated env file first
	if data, err := os.ReadFile(envPath); err == nil && len(data) > 0 {
		// Print only the export lines (skip comment lines)
		for _, line := range splitLines(string(data)) {
			trimmed := trimSpace(line)
			if len(trimmed) > 0 && trimmed[0] != '#' {
				fmt.Println(trimmed)
			}
		}
		return nil
	}

	// Fallback: resolve from config
	return printExportsFromConfig()
}

func printExportsFromConfig() error {
	globalPath := config.GlobalConfigPath()
	cfg, err := config.LoadRaw(globalPath)
	if err != nil {
		// Try project config
		projectRoot, err2 := os.Getwd()
		if err2 != nil {
			return nil
		}
		projectPath := config.ProjectConfigPath(projectRoot)
		cfg, err = config.LoadRaw(projectPath)
		if err != nil {
			return nil
		}
	}

	if cfg == nil {
		return nil
	}

	for name, pCfg := range cfg.Providers {
		key := os.ExpandEnv(pCfg.APIKey)
		if key != "" {
			fmt.Println(configwizard.GenerateExportLine(name, key))
		}
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
