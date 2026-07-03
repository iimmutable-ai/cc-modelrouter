// Package cli implements the command-line interface.
package cli

import (
	"fmt"
	"os"

	"github.com/iimmutable-ai/cc-modelrouter/internal/version"
	"github.com/spf13/cobra"
)

// Version is the application version.
var Version = version.String()

// NewRootCommand creates the root command.
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ccrouter",
		Short:   "Claude Code Model Router",
		Long: `Claude Code Model Router (ccrouter) is a proxy server that routes API requests
to multiple LLM providers based on configurable routing rules.

It enables intelligent model selection, request transformation, and usage tracking
for Claude Code and other Anthropic API clients.

Available Commands:
  code    Start router and launch Claude Code
  start   Start the router server
  stop    Stop a router instance
  restart Restart a router instance
  status  Show router instance status
  clean   Remove stale instance files
  config  Show or manage configuration
  logs    Show instance logs
  monitor Live usage monitor with terminal UI
  profile Manage route profiles
  setup  Guided setup commands (e.g. public-facing HTTPS server)

Use "ccrouter [command] --help" for more information about a command.`,
		Version: Version,
	}

	cmd.AddCommand(NewCodeCommand())
	cmd.AddCommand(NewStartCommand())
	cmd.AddCommand(NewStopCommand())
	cmd.AddCommand(NewRestartCommand())
	cmd.AddCommand(NewStatusCommand())
	cmd.AddCommand(NewCleanCommand())
	cmd.AddCommand(NewConfigCommand())
	cmd.AddCommand(NewLogsCommand())
	cmd.AddCommand(NewMonitorCommand())
	cmd.AddCommand(NewProfileCommand())
	cmd.AddCommand(NewKeysCommand())
	cmd.AddCommand(NewGenCommand())
	cmd.AddCommand(NewSetupCommand())

	return cmd
}

// Execute runs the CLI.
func Execute() {
	cmd := NewRootCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
