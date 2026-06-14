// Package auth provides API key management and validation for multi-user mode.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// KeyInfo contains information about an API key.
type KeyInfo struct {
	KeyID     int64
	KeyPrefix string
	UserName  string
	GroupName string
	IsActive  bool
	CreatedAt time.Time
	LastUsed  *time.Time
}

// GroupInfo contains information about a user group.
type GroupInfo struct {
	ID             int64
	Name           string
	Profile        string
	PriorityWeight float64
	MaxConcurrency int
	CreatedAt      time.Time
}

// UserInfo is the resolved user identity stored in request context.
// GroupName and Profile are resolved by the caller via GetUserGroup or fallback from api_keys.group_id.
type UserInfo struct {
	KeyID     int64
	KeyPrefix string
	UserName  string
	GroupID   int64
	GroupName string
	Profile   string
}

// MultiUserSettings holds the singleton multi-user settings row.
type MultiUserSettings struct {
	Enabled       bool
	GlobalMaxConc int
	WREDMinDepth  float64
	WREDMaxDepth  float64
}

// KeyPrefixLen is the number of characters shown in key prefixes.
const KeyPrefixLen = 12

// cacheTTL is how long cached lookups remain valid.
const cacheTTL = 5 * time.Second

type cacheEntry struct {
	info      *UserInfo
	expiresAt time.Time
}

// KeyStore manages API keys, user groups, and multi-user settings in SQLite.
type KeyStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]*cacheEntry // key_hash -> cached UserInfo
}

// NewKeyStore creates a new KeyStore backed by the given database.
// The database must already have the api_keys, user_groups, multi_user_settings,
// and group_members tables created.
func NewKeyStore(db *sql.DB) *KeyStore {
	return &KeyStore{
		db:    db,
		cache: make(map[string]*cacheEntry),
	}
}

// hashKey returns the SHA-256 hash of a raw API key.
func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// GenerateKey creates a new API key with the prefix "sk-ccr-".
// Returns the raw key (only returned at creation time) and its hash.
func GenerateKey() (raw string, hash string, prefix string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", "", fmt.Errorf("failed to generate key: %w", err)
	}
	raw = "sk-ccr-" + hex.EncodeToString(b[:])
	hash = hashKey(raw)
	prefix = raw[:KeyPrefixLen]
	return raw, hash, prefix, nil
}

// --- Settings CRUD ---

// GetSettings reads the singleton multi-user settings from SQLite.
func (ks *KeyStore) GetSettings() (*MultiUserSettings, error) {
	var s MultiUserSettings
	err := ks.db.QueryRow(
		"SELECT enabled, global_max_conc, wred_min_depth, wred_max_depth FROM multi_user_settings WHERE id = 1",
	).Scan(&s.Enabled, &s.GlobalMaxConc, &s.WREDMinDepth, &s.WREDMaxDepth)
	if err != nil {
		return nil, fmt.Errorf("failed to read multi-user settings: %w", err)
	}
	return &s, nil
}

// UpdateSettings writes the multi-user settings to SQLite.
func (ks *KeyStore) UpdateSettings(s *MultiUserSettings) error {
	_, err := ks.db.Exec(
		"UPDATE multi_user_settings SET enabled = ?, global_max_conc = ?, wred_min_depth = ?, wred_max_depth = ? WHERE id = 1",
		s.Enabled, s.GlobalMaxConc, s.WREDMinDepth, s.WREDMaxDepth,
	)
	if err != nil {
		return fmt.Errorf("failed to update multi-user settings: %w", err)
	}
	return nil
}

// --- Group CRUD ---

// CreateGroup creates a new user group. Returns the group ID.
func (ks *KeyStore) CreateGroup(name, profile string, priorityWeight float64, maxConcurrency int) (int64, error) {
	if name == "" {
		return 0, fmt.Errorf("group name is required")
	}
	result, err := ks.db.Exec(
		"INSERT INTO user_groups (name, profile, priority_weight, max_concurrency) VALUES (?, ?, ?, ?)",
		name, profile, priorityWeight, maxConcurrency,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create group: %w", err)
	}
	return result.LastInsertId()
}

// GetGroupByName retrieves a group by name.
func (ks *KeyStore) GetGroupByName(name string) (*GroupInfo, error) {
	var g GroupInfo
	err := ks.db.QueryRow(
		"SELECT id, name, profile, priority_weight, max_concurrency, created_at FROM user_groups WHERE name = ?",
		name,
	).Scan(&g.ID, &g.Name, &g.Profile, &g.PriorityWeight, &g.MaxConcurrency, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}
	return &g, nil
}

// GroupInfoWithCount extends GroupInfo with a pre-computed member count.
type GroupInfoWithCount struct {
	GroupInfo
	MemberCount int
}

// ListGroupsWithMemberCounts returns all groups with their member counts in a single query.
func (ks *KeyStore) ListGroupsWithMemberCounts() ([]*GroupInfoWithCount, error) {
	rows, err := ks.db.Query(`
		SELECT ug.id, ug.name, ug.profile, ug.priority_weight, ug.max_concurrency, ug.created_at,
		       COALESCE(gm.cnt, 0)
		FROM user_groups ug
		LEFT JOIN (SELECT group_id, COUNT(*) as cnt FROM group_members GROUP BY group_id) gm
		  ON ug.id = gm.group_id
		ORDER BY ug.name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list groups with counts: %w", err)
	}
	defer rows.Close()

	var groups []*GroupInfoWithCount
	for rows.Next() {
		var g GroupInfoWithCount
		if err := rows.Scan(&g.ID, &g.Name, &g.Profile, &g.PriorityWeight, &g.MaxConcurrency, &g.CreatedAt, &g.MemberCount); err != nil {
			return nil, fmt.Errorf("failed to scan group with count: %w", err)
		}
		groups = append(groups, &g)
	}
	return groups, rows.Err()
}

// ListGroups returns all groups.
func (ks *KeyStore) ListGroups() ([]*GroupInfo, error) {
	rows, err := ks.db.Query(
		"SELECT id, name, profile, priority_weight, max_concurrency, created_at FROM user_groups ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list groups: %w", err)
	}
	defer rows.Close()

	var groups []*GroupInfo
	for rows.Next() {
		var g GroupInfo
		if err := rows.Scan(&g.ID, &g.Name, &g.Profile, &g.PriorityWeight, &g.MaxConcurrency, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, &g)
	}
	return groups, rows.Err()
}

// UpdateGroup updates an existing group.
func (ks *KeyStore) UpdateGroup(id int64, profile string, priorityWeight float64, maxConcurrency int) error {
	result, err := ks.db.Exec(
		"UPDATE user_groups SET profile = ?, priority_weight = ?, max_concurrency = ? WHERE id = ?",
		profile, priorityWeight, maxConcurrency, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update group: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("group not found: %d", id)
	}
	ks.invalidateCache()
	return nil
}

// DeleteGroup deletes a group. Returns error if members still reference it.
func (ks *KeyStore) DeleteGroup(id int64) error {
	var count int
	err := ks.db.QueryRow("SELECT COUNT(*) FROM group_members WHERE group_id = ?", id).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check group references: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot delete group: %d members still assigned to it", count)
	}

	result, err := ks.db.Exec("DELETE FROM user_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("group not found: %d", id)
	}
	return nil
}

// --- Member CRUD ---

// AddGroupMember adds a user to a group.
func (ks *KeyStore) AddGroupMember(groupID int64, userName string) error {
	_, err := ks.db.Exec(
		"INSERT OR IGNORE INTO group_members (group_id, user_name) VALUES (?, ?)",
		groupID, userName,
	)
	if err != nil {
		return fmt.Errorf("failed to add group member: %w", err)
	}
	return nil
}

// RemoveGroupMember removes a user from a group.
func (ks *KeyStore) RemoveGroupMember(groupID int64, userName string) error {
	_, err := ks.db.Exec(
		"DELETE FROM group_members WHERE group_id = ? AND user_name = ?",
		groupID, userName,
	)
	if err != nil {
		return fmt.Errorf("failed to remove group member: %w", err)
	}
	return nil
}

// ListGroupMembers returns all user names in a group.
func (ks *KeyStore) ListGroupMembers(groupID int64) ([]string, error) {
	rows, err := ks.db.Query(
		"SELECT user_name FROM group_members WHERE group_id = ? ORDER BY user_name",
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list group members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		members = append(members, name)
	}
	return members, rows.Err()
}

// GetUserGroup returns the group info for a user (via group_members join).
func (ks *KeyStore) GetUserGroup(userName string) (*GroupInfo, error) {
	var g GroupInfo
	err := ks.db.QueryRow(`
		SELECT ug.id, ug.name, ug.profile, ug.priority_weight, ug.max_concurrency, ug.created_at
		FROM group_members gm
		JOIN user_groups ug ON gm.group_id = ug.id
		WHERE gm.user_name = ?`,
		userName,
	).Scan(&g.ID, &g.Name, &g.Profile, &g.PriorityWeight, &g.MaxConcurrency, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user group: %w", err)
	}
	return &g, nil
}

// GetGroupByID returns group info by its ID.
func (ks *KeyStore) GetGroupByID(groupID int64) (*GroupInfo, error) {
	var g GroupInfo
	err := ks.db.QueryRow(`
		SELECT id, name, profile, priority_weight, max_concurrency, created_at
		FROM user_groups
		WHERE id = ?`,
		groupID,
	).Scan(&g.ID, &g.Name, &g.Profile, &g.PriorityWeight, &g.MaxConcurrency, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group by ID: %w", err)
	}
	return &g, nil
}

// GetUserProfile returns the profile for a user by looking up their group.
func (ks *KeyStore) GetUserProfile(userName string) (string, error) {
	g, err := ks.GetUserGroup(userName)
	if err != nil {
		return "", err
	}
	if g == nil {
		return "", nil
	}
	return g.Profile, nil
}

// GetGroupMemberCount returns the number of members in a group.
func (ks *KeyStore) GetGroupMemberCount(groupID int64) (int, error) {
	var count int
	err := ks.db.QueryRow("SELECT COUNT(*) FROM group_members WHERE group_id = ?", groupID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// --- Key CRUD ---

// CreateKey creates a new API key for a user. Returns the full raw key (only shown once) and the key ID.
// The raw key is encrypted and stored alongside the hash for later retrieval.
func (ks *KeyStore) CreateKey(userName string, groupID int64) (rawKey string, keyID int64, err error) {
	rawKey, keyHash, prefix, err := GenerateKey()
	if err != nil {
		return "", 0, err
	}

	encrypted, err := Encrypt(rawKey)
	if err != nil {
		return "", 0, fmt.Errorf("failed to encrypt key: %w", err)
	}

	result, err := ks.db.Exec(
		"INSERT INTO api_keys (key_hash, key_prefix, name, user_name, group_id, key_encrypted) VALUES (?, ?, ?, ?, ?, ?)",
		keyHash, prefix, userName, userName, groupID, encrypted,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return "", 0, fmt.Errorf("key hash collision (extremely rare), please retry")
		}
		return "", 0, fmt.Errorf("failed to create key: %w", err)
	}

	keyID, err = result.LastInsertId()
	if err != nil {
		return "", 0, fmt.Errorf("failed to get key ID: %w", err)
	}
	return rawKey, keyID, nil
}

// GetRawKey retrieves and decrypts the raw API key by its ID.
func (ks *KeyStore) GetRawKey(id int64) (string, error) {
	var encrypted string
	err := ks.db.QueryRow("SELECT key_encrypted FROM api_keys WHERE id = ?", id).Scan(&encrypted)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("key not found: %d", id)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get key: %w", err)
	}
	if encrypted == "" {
		return "", fmt.Errorf("key %d has no encrypted data (created before encryption was enabled)", id)
	}
	return Decrypt(encrypted)
}

// GetRawKeyByUserName finds the first active key by user name and returns its decrypted raw key and ID.
func (ks *KeyStore) GetRawKeyByUserName(userName string) (string, int64, error) {
	var id int64
	var encrypted string
	err := ks.db.QueryRow(
		"SELECT id, key_encrypted FROM api_keys WHERE user_name = ? AND is_active = 1 LIMIT 1",
		userName,
	).Scan(&id, &encrypted)
	if err == sql.ErrNoRows {
		return "", 0, fmt.Errorf("no active key found for user: %s", userName)
	}
	if err != nil {
		return "", 0, fmt.Errorf("failed to look up key: %w", err)
	}
	if encrypted == "" {
		return "", 0, fmt.Errorf("key for '%s' has no encrypted data (created before encryption was enabled)", userName)
	}
	raw, err := Decrypt(encrypted)
	if err != nil {
		return "", 0, err
	}
	return raw, id, nil
}

// ValidateKey validates a raw API key and returns the associated user info.
// Uses a short-lived in-memory cache to avoid SQLite queries on every request.
// GroupName and Profile are resolved from group_members.
func (ks *KeyStore) ValidateKey(rawKey string) (*UserInfo, error) {
	keyHash := hashKey(rawKey)

	// Check cache first
	ks.mu.RLock()
	entry, ok := ks.cache[keyHash]
	ks.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.info, nil
	}

	// Cache miss or expired — query SQLite (no JOIN, just key fields)
	var info UserInfo
	err := ks.db.QueryRow(`
		SELECT id, key_prefix, user_name, group_id
		FROM api_keys
		WHERE key_hash = ? AND is_active = 1`,
		keyHash,
	).Scan(&info.KeyID, &info.KeyPrefix, &info.UserName, &info.GroupID)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to validate key: %w", err)
	}

	// Resolve group and profile from group_members
	group, err := ks.GetUserGroup(info.UserName)
	if err == nil && group != nil {
		info.GroupName = group.Name
		info.Profile = group.Profile
	} else if info.GroupID > 0 {
		// Fallback: resolve group directly from api_keys.group_id
		g, err := ks.GetGroupByID(info.GroupID)
		if err == nil && g != nil {
			info.GroupName = g.Name
			info.Profile = g.Profile
		}
	}

	// Update cache
	ks.mu.Lock()
	ks.cache[keyHash] = &cacheEntry{
		info:      &info,
		expiresAt: time.Now().Add(cacheTTL),
	}
	ks.mu.Unlock()

	// Async update last_used (non-blocking)
	go ks.updateLastUsed(info.KeyID)

	return &info, nil
}

// updateLastUsed sets the last_used timestamp for a key. Called asynchronously.
func (ks *KeyStore) updateLastUsed(keyID int64) {
	ks.db.Exec("UPDATE api_keys SET last_used = CURRENT_TIMESTAMP WHERE id = ?", keyID)
}

// ListKeys returns all API keys.
func (ks *KeyStore) ListKeys() ([]*KeyInfo, error) {
	rows, err := ks.db.Query(`
		SELECT ak.id, ak.key_prefix, ak.user_name, ug.name, ak.is_active, ak.created_at, ak.last_used
		FROM api_keys ak
		LEFT JOIN user_groups ug ON ak.group_id = ug.id
		ORDER BY ak.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	defer rows.Close()

	var keys []*KeyInfo
	for rows.Next() {
		var k KeyInfo
		if err := rows.Scan(&k.KeyID, &k.KeyPrefix, &k.UserName, &k.GroupName, &k.IsActive, &k.CreatedAt, &k.LastUsed); err != nil {
			return nil, fmt.Errorf("failed to scan key: %w", err)
		}
		keys = append(keys, &k)
	}
	return keys, rows.Err()
}

// RevokeKey deactivates an API key by ID.
func (ks *KeyStore) RevokeKey(id int64) error {
	result, err := ks.db.Exec("UPDATE api_keys SET is_active = 0 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to revoke key: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("key not found: %d", id)
	}
	ks.invalidateCache()
	return nil
}

// DeleteKey permanently removes a revoked (inactive) API key by ID.
// Returns an error if the key is still active or not found.
func (ks *KeyStore) DeleteKey(id int64) error {
	result, err := ks.db.Exec("DELETE FROM api_keys WHERE id = ? AND is_active = 0", id)
	if err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("active key cannot be deleted or key not found: %d", id)
	}
	ks.invalidateCache()
	return nil
}

// invalidateCache clears all cached key lookups.
func (ks *KeyStore) invalidateCache() {
	ks.mu.Lock()
	ks.cache = make(map[string]*cacheEntry)
	ks.mu.Unlock()
}
