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
	cmd.AddCommand(NewSettingsCommand())
	return cmd
}

// --- keys create ---

func NewKeysCreateCommand() *cobra.Command {
	var name string
	var group string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeysCreate(name, group)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "User name (description)")
	cmd.Flags().StringVar(&group, "group", "", "Group name (defaults to first group)")
	cmd.MarkFlagRequired("name")
	return cmd
}

func runKeysCreate(name, groupName string) error {
	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)

	var groupID int64
	if groupName != "" {
		g, err := ks.GetGroupByName(groupName)
		if err != nil {
			return fmt.Errorf("failed to look up group: %w", err)
		}
		if g == nil {
			return fmt.Errorf("group not found: %s", groupName)
		}
		groupID = g.ID
	} else {
		groups, err := ks.ListGroups()
		if err != nil {
			return fmt.Errorf("failed to list groups: %w", err)
		}
		if len(groups) == 0 {
			return fmt.Errorf("no groups exist; create one first with: ccrouter groups create")
		}
		groupID = groups[0].ID
	}

	rawKey, keyID, err := ks.CreateKey(name, groupID)
	if err != nil {
		return fmt.Errorf("failed to create key: %w", err)
	}

	fmt.Printf("Key created successfully!\n")
	fmt.Printf("  ID:      %d\n", keyID)
	fmt.Printf("  User:    %s\n", name)
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
	fmt.Fprintln(tw, "ID\tPREFIX\t\tUSER\t\t\tACTIVE\tLAST USED")
	for _, k := range keys {
		lastUsed := "never"
		if k.LastUsed != nil {
			lastUsed = k.LastUsed.Format("2006-01-02 15:04")
		}
		active := "yes"
		if !k.IsActive {
			active = "no"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t\t%s\t%s\n",
			k.KeyID, k.KeyPrefix, k.UserName, active, lastUsed)
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

// --- groups ---

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
	cmd.AddCommand(NewMembersCommand())
	return cmd
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
	groups, err := ks.ListGroupsWithMemberCounts()
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
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.2f\t%d\t%d\n",
			g.ID, g.Name, g.Profile, g.PriorityWeight, g.MaxConcurrency, g.MemberCount)
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

// --- members ---

func NewMembersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage group members",
		Long:  `Add, remove, or list members (users) in a group.`,
	}
	cmd.AddCommand(NewMembersListCommand())
	cmd.AddCommand(NewMembersAddCommand())
	cmd.AddCommand(NewMembersRemoveCommand())
	return cmd
}

func NewMembersListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list <group-id>",
		Short: "List members of a group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMembersList(args[0])
		},
	}
}

func runMembersList(groupIDStr string) error {
	var groupID int64
	if _, err := fmt.Sscanf(groupIDStr, "%d", &groupID); err != nil {
		return fmt.Errorf("invalid group ID: %s", groupIDStr)
	}

	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	members, err := ks.ListGroupMembers(groupID)
	if err != nil {
		return fmt.Errorf("failed to list members: %w", err)
	}

	if len(members) == 0 {
		fmt.Println("No members in group.")
		return nil
	}
	for _, m := range members {
		fmt.Println(m)
	}
	return nil
}

func NewMembersAddCommand() *cobra.Command {
	var user string
	cmd := &cobra.Command{
		Use:   "add <group-id>",
		Short: "Add a user to a group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMembersAdd(args[0], user)
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "User name to add")
	cmd.MarkFlagRequired("user")
	return cmd
}

func runMembersAdd(groupIDStr, userName string) error {
	var groupID int64
	if _, err := fmt.Sscanf(groupIDStr, "%d", &groupID); err != nil {
		return fmt.Errorf("invalid group ID: %s", groupIDStr)
	}

	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	if err := ks.AddGroupMember(groupID, userName); err != nil {
		return fmt.Errorf("failed to add member: %w", err)
	}

	fmt.Printf("Added %s to group %d.\n", userName, groupID)
	return nil
}

func NewMembersRemoveCommand() *cobra.Command {
	var user string
	cmd := &cobra.Command{
		Use:   "remove <group-id>",
		Short: "Remove a user from a group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMembersRemove(args[0], user)
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "User name to remove")
	cmd.MarkFlagRequired("user")
	return cmd
}

func runMembersRemove(groupIDStr, userName string) error {
	var groupID int64
	if _, err := fmt.Sscanf(groupIDStr, "%d", &groupID); err != nil {
		return fmt.Errorf("invalid group ID: %s", groupIDStr)
	}

	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	if err := ks.RemoveGroupMember(groupID, userName); err != nil {
		return fmt.Errorf("failed to remove member: %w", err)
	}

	fmt.Printf("Removed %s from group %d.\n", userName, groupID)
	return nil
}

// --- settings ---

func NewSettingsCommand() *cobra.Command {
	var enabled bool
	var globalMax int
	var wredMin, wredMax float64
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "View or update multi-user settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettings(cmd, enabled, globalMax, wredMin, wredMax)
		},
	}
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Enable or disable multi-user mode")
	cmd.Flags().IntVar(&globalMax, "global-max", -1, "Global max concurrency (0=auto/100, -1=keep)")
	cmd.Flags().Float64Var(&wredMin, "wred-min", -1, "WRED min depth (-1=keep)")
	cmd.Flags().Float64Var(&wredMax, "wred-max", -1, "WRED max depth (-1=keep)")
	return cmd
}

func runSettings(cmd *cobra.Command, enabled bool, globalMax int, wredMin, wredMax float64) error {
	db, err := openUsageDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	s, err := ks.GetSettings()
	if err != nil {
		return fmt.Errorf("failed to read settings: %w", err)
	}

	if cmd.Flags().Changed("enabled") {
		s.Enabled = enabled
	}
	if cmd.Flags().Changed("global-max") {
		s.GlobalMaxConc = globalMax
	}
	if cmd.Flags().Changed("wred-min") {
		s.WREDMinDepth = wredMin
	}
	if cmd.Flags().Changed("wred-max") {
		s.WREDMaxDepth = wredMax
	}

	changed := cmd.Flags().Changed("enabled") || cmd.Flags().Changed("global-max") ||
		cmd.Flags().Changed("wred-min") || cmd.Flags().Changed("wred-max")
	if changed {
		if err := ks.UpdateSettings(s); err != nil {
			return fmt.Errorf("failed to update settings: %w", err)
		}
	}

	fmt.Printf("Multi-user settings:\n")
	fmt.Printf("  Enabled:        %v\n", s.Enabled)
	fmt.Printf("  Global Max:     %d\n", s.GlobalMaxConc)
	fmt.Printf("  WRED Min Depth: %.2f\n", s.WREDMinDepth)
	fmt.Printf("  WRED Max Depth: %.2f\n", s.WREDMaxDepth)
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
