package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/spf13/cobra"
)

// NewStatusCommand creates the status command.
func NewStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show router instance status",
		Long: `Lists all router instances with their status.

Displays a table of all router instances including:
  - Instance ID: Unique identifier for the instance
  - PID: Process ID of the running process
  - Port: Port number the instance is listening on
  - Status: "running" if alive, "dead" if process terminated
  - Config: Configuration source (project, global, or custom)
  - Uptime: How long the instance has been running

Flags:
  -a, --all    Show all instances including dead ones.
                By default, only running instances are shown.

Examples:
  # Show running instances
  ccrouter status

  # Show all instances including dead ones
  ccrouter status --all`,
		RunE: runStatus,
	}

	cmd.Flags().BoolP("all", "a", false, "Show all instances including dead ones")

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	showAll, _ := cmd.Flags().GetBool("all")

	// List all instances
	instances, err := daemon.ListInstances()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No instances found")
			return nil
		}
		return fmt.Errorf("failed to list instances: %w", err)
	}

	if len(instances) == 0 {
		fmt.Println("No instances found")
		return nil
	}

	// Display status
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "INSTANCE ID\tPID\tPORT\tSTATUS\tCONFIG\tUPTIME")

	running := 0
	dead := 0

	for _, inst := range instances {
		isRunning := daemon.IsRunning(inst)
		status := "running"
		if !isRunning {
			status = "dead"
			dead++
		} else {
			running++
		}

		// Skip dead instances unless --all is specified
		if !isRunning && !showAll {
			continue
		}

		uptime := time.Since(inst.StartTime).Round(time.Second)
		uptimeStr := formatDuration(uptime)

		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%s\n",
			inst.ID,
			inst.PID,
			inst.Port,
			status,
			inst.ConfigType,
			uptimeStr,
		)
	}

	w.Flush()

	fmt.Printf("\n%d running, %d dead\n", running, dead)

	return nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
}
