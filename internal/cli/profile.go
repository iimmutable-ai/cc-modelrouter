// Package cli implements the command-line interface for ccrouter.
package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/spf13/cobra"
)

// NewProfileCommand creates the profile command group.
func NewProfileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage route profiles",
		Long:  `Manage route profiles for switching between different route configurations during a session.`,
	}
	cmd.AddCommand(NewProfileListCommand())
	cmd.AddCommand(NewProfileSwitchCommand())
	cmd.AddCommand(NewProfileStatusCommand())
	return cmd
}

// NewProfileListCommand lists available profiles.
func NewProfileListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all profiles",
		Long:  `List all configured route profiles. Can list from config file or running instance.`,
		RunE:  runProfileList,
	}
	cmd.Flags().String("instance", "", "Instance ID to query (uses most recent if not specified)")
	cmd.Flags().Bool("from-config", false, "List profiles from config file instead of running instance")
	return cmd
}

// NewProfileSwitchCommand switches active profile.
func NewProfileSwitchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <profile-name>",
		Short: "Switch to a profile",
		Long:  `Switch the active profile for a running router instance. Requires a running instance.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileSwitch,
	}
	cmd.Flags().String("instance", "", "Instance ID to switch (uses most recent if not specified)")
	return cmd
}

// NewProfileStatusCommand shows current profile status.
func NewProfileStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show active profile",
		Long:  `Show the currently active profile for a running router instance.`,
		RunE:  runProfileStatus,
	}
	cmd.Flags().String("instance", "", "Instance ID to query (uses most recent if not specified)")
	return cmd
}

func runProfileList(cmd *cobra.Command, args []string) error {
	fromConfig, _ := cmd.Flags().GetBool("from-config")
	instanceID, _ := cmd.Flags().GetString("instance")

	if fromConfig {
		return listProfilesFromConfig()
	}

	return listProfilesFromInstance(instanceID)
}

func listProfilesFromConfig() error {
	cfgPath := config.GlobalConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	return printProfileList(cfg)
}

func listProfilesFromInstance(instanceID string) error {
	instance, err := findRunningInstance(instanceID)
	if err != nil {
		return err
	}

	if !daemon.IsRunning(instance) {
		return fmt.Errorf("instance %s is not running", instance.ID)
	}

	// Call admin API to get profiles
	url := fmt.Sprintf("http://localhost:%d/_admin/profiles?token=%s", instance.Port, instance.AdminToken)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API returned status %d", resp.StatusCode)
	}

	var response struct {
		Profiles      []struct {
			Key         string            `json:"key"`
			Name        string            `json:"name"`
			Description string            `json:"description,omitempty"`
			IsActive    bool              `json:"isActive"`
		} `json:"profiles"`
		ActiveProfile string            `json:"activeProfile"`
		HasProfiles   bool              `json:"hasProfiles"`
		LegacyRoutes  map[string]string `json:"legacyRoutes,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Print profiles
	if !response.HasProfiles {
		fmt.Println("No profiles configured. Using legacy routes:")
		for name, route := range response.LegacyRoutes {
			fmt.Printf("  %s: %s\n", name, route)
		}
		return nil
	}

	fmt.Printf("Profiles (active: %s):\n", response.ActiveProfile)
	for _, profile := range response.Profiles {
		prefix := "  "
		if profile.IsActive {
			prefix = "  * "
		}
		desc := ""
		if profile.Description != "" {
			desc = fmt.Sprintf(" - %s", profile.Description)
		}
		fmt.Printf("%s%s [%s]%s\n", prefix, profile.Name, profile.Key, desc)
	}

	return nil
}

func runProfileSwitch(cmd *cobra.Command, args []string) error {
	profileName := args[0]
	instanceID, _ := cmd.Flags().GetString("instance")

	instance, err := findRunningInstance(instanceID)
	if err != nil {
		return err
	}

	if !daemon.IsRunning(instance) {
		return fmt.Errorf("instance %s is not running", instance.ID)
	}

	// Call admin API to switch profile
	url := fmt.Sprintf("http://localhost:%d/_admin/profiles/switch", instance.Port)
	body := fmt.Sprintf(`{"profile":"%s"}`, profileName)

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", instance.AdminToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer resp.Body.Close()

	var response struct {
		Success       bool              `json:"success"`
		ActiveProfile string            `json:"activeProfile"`
		ProfileName   string            `json:"profileName,omitempty"`
		Routes        map[string]string `json:"routes,omitempty"`
		Error         string            `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("failed to switch profile: %s", response.Error)
	}

	// Update instance metadata
	if err := daemon.UpdateActiveProfile(instance.ID, response.ActiveProfile); err != nil {
		fmt.Printf("Warning: failed to update instance metadata: %v\n", err)
	}

	fmt.Printf("Switched to profile: %s (%s)\n", response.ProfileName, response.ActiveProfile)
	fmt.Println("Active routes:")
	for name, route := range response.Routes {
		fmt.Printf("  %s: %s\n", name, route)
	}

	return nil
}

func runProfileStatus(cmd *cobra.Command, args []string) error {
	instanceID, _ := cmd.Flags().GetString("instance")

	instance, err := findRunningInstance(instanceID)
	if err != nil {
		return err
	}

	if !daemon.IsRunning(instance) {
		return fmt.Errorf("instance %s is not running", instance.ID)
	}

	// Call admin API to get active profile
	url := fmt.Sprintf("http://localhost:%d/_admin/profiles/active?token=%s", instance.Port, instance.AdminToken)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API returned status %d", resp.StatusCode)
	}

	var response struct {
		ActiveProfile string `json:"activeProfile"`
		HasProfiles   bool   `json:"hasProfiles"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !response.HasProfiles {
		fmt.Println("No profiles configured. Using legacy routes from config.")
		return nil
	}

	fmt.Printf("Active profile: %s\n", response.ActiveProfile)
	return nil
}

// findRunningInstance finds a running instance by ID, or the most recent if ID is empty.
func findRunningInstance(instanceID string) (*daemon.InstanceMetadata, error) {
	instances, err := daemon.ListInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	if len(instances) == 0 {
		return nil, fmt.Errorf("no running instances found")
	}

	if instanceID != "" {
		for _, inst := range instances {
			if inst.ID == instanceID {
				return inst, nil
			}
		}
		return nil, fmt.Errorf("instance %s not found", instanceID)
	}

	// Find the most recent running instance
	var running []*daemon.InstanceMetadata
	for _, inst := range instances {
		if daemon.IsRunning(inst) {
			running = append(running, inst)
		}
	}

	if len(running) == 0 {
		return nil, fmt.Errorf("no running instances found")
	}

	// Return the most recent (by start time)
	mostRecent := running[0]
	for _, inst := range running {
		if inst.StartTime.After(mostRecent.StartTime) {
			mostRecent = inst
		}
	}

	return mostRecent, nil
}

// printProfileList prints the profile list from a config.
func printProfileList(cfg *config.Config) error {
	if !cfg.HasProfiles() {
		fmt.Println("No profiles configured. Using legacy routes:")
		for name, route := range cfg.Router.Routes {
			fmt.Printf("  %s: %s\n", name, route)
		}
		return nil
	}

	fmt.Printf("Profiles (default at startup: %s):\n", cfg.GetDefaultProfile())
	for _, key := range cfg.GetProfileNames() {
		profile := cfg.Router.Profiles[key]
		prefix := "  "
		if key == cfg.GetDefaultProfile() {
			prefix = "  * "
		}
		desc := ""
		if profile.Description != "" {
			desc = fmt.Sprintf(" - %s", profile.Description)
		}
		fmt.Printf("%s%s [%s]%s\n", prefix, profile.Name, key, desc)
	}

	return nil
}
