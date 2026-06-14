package cli

import (
	"database/sql"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/iimmutable/cc-modelrouter/internal/auth"
	"github.com/iimmutable/cc-modelrouter/internal/usage"
	"github.com/spf13/cobra"
)

// NewKeysCommand creates the keys subcommand group.
func NewKeysCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage API keys for multi-user mode",
		Long:  `Manage API keys for multi-user authentication. Keys are stored as SHA-256 hashes in SQLite.`,
	}
	cmd.AddCommand(NewKeysCreateCommand())
	cmd.AddCommand(NewKeysListCommand())
	cmd.AddCommand(NewKeysRevokeCommand())
	cmd.AddCommand(NewGroupsCommand())
	return cmd
}

// NewGroupsCommand creates the groups subcommand group.
func NewGroupsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Manage user groups for multi-user mode",
		Long:  `Manage user groups that map API keys to routing profiles with QoS settings.`,
	}
	cmd.AddCommand(NewGroupsListCommand())
	cmd.AddCommand(NewGroupsCreateCommand())
	cmd.AddCommand(NewGroupsUpdateCommand())
	cmd.AddCommand(NewGroupsDeleteCommand())
	return cmd
}

// --- keys create ---

func NewKeysCreateCommand() *cobra.Command {
	var name, group string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeysCreate(name, group)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Key name (description)")
	cmd.Flags().StringVar(&group, "group", "", "Group name to assign key to")
	cmd.MarkFlagRequired("group")
	return cmd
}

func runKeysCreate(name, group string) error {
	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)

	g, err := ks.GetGroupByName(group)
	if err != nil {
		return fmt.Errorf("failed to look up group: %w", err)
	}
	if g == nil {
		return fmt.Errorf("group not found: %s", group)
	}

	rawKey, keyID, err := ks.CreateKey(name, g.ID)
	if err != nil {
		return fmt.Errorf("failed to create key: %w", err)
	}

	fmt.Printf("Key created successfully!\n")
	fmt.Printf("  ID:    %d\n", keyID)
	fmt.Printf("  Name:  %s\n", name)
	fmt.Printf("  Group: %s\n", group)
	fmt.Printf("\n  API Key (save now, cannot be retrieved again):\n")
	fmt.Printf("  %s\n", rawKey)
	return nil
}

// --- keys list ---

func NewKeysListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeysList()
		},
	}
}

func runKeysList() error {
	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	keys, err := ks.ListKeys()
	if err != nil {
		return fmt.Errorf("failed to list keys: %w", err)
	}

	if len(keys) == 0 {
		fmt.Println("No API keys found.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPREFIX\t\tNAME\tGROUP\t\tACTIVE\tLAST USED")
	for _, k := range keys {
		lastUsed := "never"
		if k.LastUsed != nil {
			lastUsed = k.LastUsed.Format("2006-01-02 15:04")
		}
		active := "yes"
		if !k.IsActive {
			active = "no"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			k.KeyID, k.KeyPrefix, k.Name, k.GroupName, active, lastUsed)
	}
	tw.Flush()
	return nil
}

// --- keys revoke ---

func NewKeysRevokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeysRevoke(args[0])
		},
	}
}

func runKeysRevoke(idStr string) error {
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		return fmt.Errorf("invalid key ID: %s", idStr)
	}

	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	if err := ks.RevokeKey(id); err != nil {
		return fmt.Errorf("failed to revoke key: %w", err)
	}

	fmt.Printf("Key %d revoked.\n", id)
	return nil
}

// --- groups list ---

func NewGroupsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all user groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsList()
		},
	}
}

func runGroupsList() error {
	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	groups, err := ks.ListGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	if len(groups) == 0 {
		fmt.Println("No groups found. Create one with: ccrouter groups create")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\t\tPROFILE\t\tPRIORITY\tMAX_CONC\tMEMBERS")
	for _, g := range groups {
		members, _ := ks.GetGroupMemberCount(g.ID)
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.2f\t%d\t%d\n",
			g.ID, g.Name, g.Profile, g.PriorityWeight, g.MaxConcurrency, members)
	}
	tw.Flush()
	return nil
}

// --- groups create ---

func NewGroupsCreateCommand() *cobra.Command {
	var name, profile string
	var priority float64
	var maxConc int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user group",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsCreate(name, profile, priority, maxConc)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Group name")
	cmd.Flags().StringVar(&profile, "profile", "", "Route profile to use")
	cmd.Flags().Float64Var(&priority, "priority", 1.0, "Priority weight (0-1)")
	cmd.Flags().IntVar(&maxConc, "max-concurrency", 0, "Max concurrent requests (0=unlimited)")
	cmd.MarkFlagRequired("name")
	return cmd
}

func runGroupsCreate(name, profile string, priority float64, maxConc int) error {
	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	id, err := ks.CreateGroup(name, profile, priority, maxConc)
	if err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}

	fmt.Printf("Group created: %s (ID: %d, profile: %s, priority: %.2f)\n",
		name, id, profile, priority)
	return nil
}

// --- groups update ---

func NewGroupsUpdateCommand() *cobra.Command {
	var priority float64
	var maxConc int
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a user group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUpdate(args[0], cmd, priority, maxConc)
		},
	}
	cmd.Flags().Float64Var(&priority, "priority", 0, "New priority weight (0=keep)")
	cmd.Flags().IntVar(&maxConc, "max-concurrency", -1, "New max concurrency (-1=keep)")
	return cmd
}

func runGroupsUpdate(idStr string, cmd *cobra.Command, priority float64, maxConc int) error {
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		return fmt.Errorf("invalid group ID: %s", idStr)
	}

	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)

	groups, err := ks.ListGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	var current *auth.GroupInfo
	for _, grp := range groups {
		if grp.ID == id {
			current = grp
			break
		}
	}
	if current == nil {
		return fmt.Errorf("group not found: %d", id)
	}

	newPriority := current.PriorityWeight
	newMaxConc := current.MaxConcurrency

	if priority > 0 {
		newPriority = priority
	}
	if cmd.Flags().Changed("max-concurrency") {
		newMaxConc = maxConc
	}

	if err := ks.UpdateGroup(id, current.Profile, newPriority, newMaxConc); err != nil {
		return fmt.Errorf("failed to update group: %w", err)
	}

	fmt.Printf("Group %d updated.\n", id)
	return nil
}

// --- groups delete ---

func NewGroupsDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a user group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsDelete(args[0])
		},
	}
}

func runGroupsDelete(idStr string) error {
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		return fmt.Errorf("invalid group ID: %s", idStr)
	}

	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	if err := ks.DeleteGroup(id); err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}

	fmt.Printf("Group %d deleted.\n", id)
	return nil
}

// openUsageDB opens the usage database and creates tables if needed.
func openUsageDB() (*sql.DB, error) {
	dbPath, err := usage.DBPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get db path: %w", err)
	}
	db, err := usage.InitDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	return db, nil
}
