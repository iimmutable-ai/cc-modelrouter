package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/iimmutable/cc-modelrouter/internal/auth"
	"github.com/spf13/cobra"
)

// NewGenCommand creates the gen subcommand group.
func NewGenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate configuration files",
		Long:  `Generate configuration files for Claude Code integration.`,
	}
	cmd.AddCommand(NewGenSettingsCommand())
	return cmd
}

// NewGenSettingsCommand creates the gen settings command.
func NewGenSettingsCommand() *cobra.Command {
	var url, user, key, output string

	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Generate a Claude Code settings.json for the router",
		Long: `Generate a Claude Code settings.local.json or settings.json that configures
Claude Code to use the ccrouter proxy.

The output uses Claude Code's env format with attribution disabled.

Examples:
  ccrouter gen settings --user alice
  ccrouter gen settings --key sk-ccrouter-abc123
  ccrouter gen settings --url http://myserver:8081 -o .claude/settings.local.json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenSettings(url, user, key, output)
		},
	}

	cmd.Flags().StringVar(&url, "url", "http://localhost:8081", "Router URL")
	cmd.Flags().StringVar(&user, "user", "", "Username to look up API key from keystore")
	cmd.Flags().StringVar(&key, "key", "", "API key directly (overrides --user)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: stdout)")

	return cmd
}

func runGenSettings(url, user, key, output string) error {
	// Resolve the API key
	apiKey := ""

	if key != "" {
		apiKey = key
	} else if user != "" {
		db, err := openUsageDB()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		ks := auth.NewKeyStore(db)
		raw, _, err := ks.GetRawKeyByName(user)
		if err != nil {
			return fmt.Errorf("failed to look up key for user %q: %w", user, err)
		}
		apiKey = raw
	}

	// Build settings
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       url,
		"CLAUDE_CODE_ATTRIBUTION_HEADER":          "0",
		"API_TIMEOUT_MS":                          "3000000",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
	if apiKey != "" {
		env["ANTHROPIC_API_KEY"] = apiKey
	}

	settings := map[string]interface{}{
		"env": env,
		"attribution": map[string]string{
			"commit": "",
			"pr":     "",
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if output != "" {
		if err := os.WriteFile(output, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", output, err)
		}
		fmt.Printf("Settings written to %s\n", output)
	} else {
		fmt.Println(string(data))
	}

	return nil
}
