package cli

import (
	"fmt"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/monitor"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	"github.com/spf13/cobra"
	tea "github.com/charmbracelet/bubbletea"
)

// NewMonitorCommand creates the monitor command
func NewMonitorCommand() *cobra.Command {
	var refreshInterval time.Duration

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Live usage monitor with terminal UI",
		Long: `Start a live terminal UI for monitoring usage statistics.

This command displays a real-time dashboard with:
  - Usage statistics (requests, tokens, fallbacks) by route and model
  - Date range selection (TODAY, WEEK, MONTH, YTD, TTM)
  - Instance filtering with running/stopped indicators
  - Optional console log viewer (press 'd' when single instance selected)

Keyboard shortcuts:
  q         - Quit
  d         - Toggle console log (single instance only)
  ←/→       - Navigate date range tabs
  ↑/↓       - Navigate instance list
  space     - Pause/resume log tail
  1-7       - Toggle log level filters
  r         - Force refresh

Examples:
  # Start monitor with default settings
  ccrouter monitor

  # Monitor with 2-second refresh
  ccrouter monitor --refresh 2s`,
		RunE: runMonitor(&refreshInterval),
	}

	cmd.Flags().DurationVar(&refreshInterval, "refresh", 500*time.Millisecond, "Stats refresh interval (e.g., 1s, 500ms)")

	return cmd
}

func runMonitor(refreshInterval *time.Duration) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Get database path
		dbPath, err := usage.DBPath()
		if err != nil {
			return fmt.Errorf("failed to get db path: %w", err)
		}

		// Create monitor model
		model := monitor.NewMonitorModel(*refreshInterval, dbPath)

		// Run the Bubble Tea program
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("monitor failed: %w", err)
		}

		return nil
	}
}