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

// KeyInfo contains information about a validated API key.
type KeyInfo struct {
	KeyID     int64
	KeyPrefix string
	Name      string
	GroupID   int64
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
type UserInfo struct {
	KeyID     int64
	KeyPrefix string
	Name      string
	GroupID   int64
	GroupName string
	Profile   string
}

// KeyPrefixLen is the number of characters shown in key prefixes.
const KeyPrefixLen = 12

// cacheTTL is how long cached lookups remain valid.
const cacheTTL = 5 * time.Second

type cacheEntry struct {
	info      *UserInfo
	expiresAt time.Time
}

// KeyStore manages API keys and user groups in SQLite.
type KeyStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]*cacheEntry // key_hash -> cached UserInfo
}

// NewKeyStore creates a new KeyStore backed by the given database.
// The database must already have the api_keys and user_groups tables created.
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

// GenerateKey creates a new API key with the prefix "sk-ccrouter-".
// Returns the raw key (only returned at creation time) and its hash.
func GenerateKey() (raw string, hash string, prefix string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", "", fmt.Errorf("failed to generate key: %w", err)
	}
	raw = "sk-ccrouter-" + hex.EncodeToString(b[:])
	hash = hashKey(raw)
	prefix = raw[:KeyPrefixLen]
	return raw, hash, prefix, nil
}

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

// DeleteGroup deletes a group. Returns error if keys still reference it.
func (ks *KeyStore) DeleteGroup(id int64) error {
	var count int
	err := ks.db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE group_id = ? AND is_active = 1", id).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check group references: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot delete group: %d active API keys reference it", count)
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

// CreateKey creates a new API key for a user in the given group.
// Returns the full raw key (only shown once) and the key ID.
func (ks *KeyStore) CreateKey(name string, groupID int64) (rawKey string, keyID int64, err error) {
	rawKey, keyHash, prefix, err := GenerateKey()
	if err != nil {
		return "", 0, err
	}

	result, err := ks.db.Exec(
		"INSERT INTO api_keys (key_hash, key_prefix, name, group_id) VALUES (?, ?, ?, ?)",
		keyHash, prefix, name, groupID,
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

// ValidateKey validates a raw API key and returns the associated user info.
// Uses a short-lived in-memory cache to avoid SQLite queries on every request.
func (ks *KeyStore) ValidateKey(rawKey string) (*UserInfo, error) {
	keyHash := hashKey(rawKey)

	// Check cache first
	ks.mu.RLock()
	entry, ok := ks.cache[keyHash]
	ks.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.info, nil
	}

	// Cache miss or expired — query SQLite
	var info UserInfo
	err := ks.db.QueryRow(`
		SELECT ak.id, ak.key_prefix, ak.name, ak.group_id, ug.name, ug.profile
		FROM api_keys ak
		JOIN user_groups ug ON ak.group_id = ug.id
		WHERE ak.key_hash = ? AND ak.is_active = 1`,
		keyHash,
	).Scan(&info.KeyID, &info.KeyPrefix, &info.Name, &info.GroupID, &info.GroupName, &info.Profile)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to validate key: %w", err)
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

// ListKeys returns all API keys with their group info.
func (ks *KeyStore) ListKeys() ([]*KeyInfo, error) {
	rows, err := ks.db.Query(`
		SELECT ak.id, ak.key_prefix, ak.name, ak.group_id, ug.name, ak.is_active, ak.created_at, ak.last_used
		FROM api_keys ak
		JOIN user_groups ug ON ak.group_id = ug.id
		ORDER BY ak.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	defer rows.Close()

	var keys []*KeyInfo
	for rows.Next() {
		var k KeyInfo
		if err := rows.Scan(&k.KeyID, &k.KeyPrefix, &k.Name, &k.GroupID, &k.GroupName, &k.IsActive, &k.CreatedAt, &k.LastUsed); err != nil {
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

// GetGroupMemberCount returns the number of (active) keys in a group.
func (ks *KeyStore) GetGroupMemberCount(groupID int64) (int, error) {
	var count int
	err := ks.db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE group_id = ? AND is_active = 1", groupID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// invalidateCache clears all cached key lookups.
func (ks *KeyStore) invalidateCache() {
	ks.mu.Lock()
	ks.cache = make(map[string]*cacheEntry)
	ks.mu.Unlock()
}
