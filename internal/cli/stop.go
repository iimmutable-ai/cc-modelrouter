package cli

import (
	"fmt"
	"os"
	"syscall"
	"text/tabwriter"

	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/spf13/cobra"
)

// NewStopCommand creates the stop command.
func NewStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop [instance-id]",
		Short: "Stop a router instance",
		Long: `Stops a running router instance. If no instance ID is provided, stops all running instances.

Arguments:
  instance-id    Optional. The specific instance ID to stop.
                  Use "ccrouter status" to list all instances.
                  If omitted, stops all running instances.

Flags:
  -f, --force    Force stop using SIGKILL instead of SIGTERM.
                  Use this when the instance doesn't respond to normal shutdown.
                  This is an immediate termination without graceful shutdown.

Examples:
  # Stop all running instances
  ccrouter stop

  # Stop a specific instance
  ccrouter stop abc123def456

  # Force stop an unresponsive instance
  ccrouter stop --force abc123def456`,
		RunE: runStop,
	}

	cmd.Flags().BoolP("force", "f", false, "Force stop (SIGKILL)")

	return cmd
}

func runStop(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	// List all instances
	instances, err := daemon.ListInstances()
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	if len(instances) == 0 {
		fmt.Println("No instances found")
		return nil
	}

	// Determine signal to use
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}

	// If instance ID provided, stop only that instance
	if len(args) > 0 {
		instanceID := args[0]
		found := false
		for _, inst := range instances {
			if inst.ID == instanceID {
				found = true
				if err := stopInstance(inst, sig); err != nil {
					return err
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("instance not found: %s", instanceID)
		}
		return nil
	}

	// Stop all running instances
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "INSTANCE ID\tPORT\tSTATUS\tRESULT")

	stopped := 0
	for _, inst := range instances {
		status := "running"
		result := "stopped"

		if !daemon.IsRunning(inst) {
			status = "dead"
			result = "cleaned"
			daemon.DeleteInstance(inst.ID)
		} else {
			if err := stopInstance(inst, sig); err != nil {
				result = fmt.Sprintf("error: %v", err)
			} else {
				stopped++
			}
		}

		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", inst.ID, inst.Port, status, result)
	}

	w.Flush()
	fmt.Printf("\nStopped %d instance(s)\n", stopped)

	return nil
}

func stopInstance(inst *daemon.InstanceMetadata, sig syscall.Signal) error {
	proc, err := os.FindProcess(inst.PID)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send signal
	if err := proc.Signal(sig); err != nil {
		// Process might already be dead
		daemon.DeleteInstance(inst.ID)
		return fmt.Errorf("failed to signal process: %w", err)
	}

	// Clean up instance file
	daemon.DeleteInstance(inst.ID)

	fmt.Printf("Stopped instance %s (PID %d)\n", inst.ID, inst.PID)
	return nil
}
