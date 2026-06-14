package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) (*KeyStore, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	ks := NewKeyStore(db)
	return ks, func() { db.Close() }
}

func openTestDB(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS user_groups (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		name            TEXT NOT NULL UNIQUE,
		profile         TEXT NOT NULL DEFAULT '',
		priority_weight REAL NOT NULL DEFAULT 1.0,
		max_concurrency INTEGER NOT NULL DEFAULT 0,
		created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS api_keys (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		key_hash    TEXT NOT NULL UNIQUE,
		key_prefix  TEXT NOT NULL,
		name        TEXT NOT NULL DEFAULT '',
		group_id    INTEGER NOT NULL,
		is_active   INTEGER NOT NULL DEFAULT 1,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used   DATETIME,
		FOREIGN KEY (group_id) REFERENCES user_groups(id)
	);
	CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
	CREATE INDEX IF NOT EXISTS idx_api_keys_group ON api_keys(group_id);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func TestGenerateKey(t *testing.T) {
	raw, hash, prefix, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if !strings.HasPrefix(raw, "sk-ccrouter-") {
		t.Errorf("expected key to start with 'sk-ccrouter-', got %s", raw[:15])
	}

	if len(prefix) != KeyPrefixLen {
		t.Errorf("expected prefix length %d, got %d", KeyPrefixLen, len(prefix))
	}

	h := sha256.Sum256([]byte(raw))
	expectedHash := hex.EncodeToString(h[:])
	if hash != expectedHash {
		t.Errorf("hash mismatch: expected %s, got %s", expectedHash, hash)
	}

	if len(hash) != 64 {
		t.Errorf("expected hash length 64, got %d", len(hash))
	}

	raw2, hash2, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (2nd) failed: %v", err)
	}
	if hash == hash2 {
		t.Error("two generated keys should not have same hash")
	}
	if raw == raw2 {
		t.Error("two generated keys should not be identical")
	}
}

func TestHashKey(t *testing.T) {
	key := "sk-ccrouter-test123"
	hash := hashKey(key)

	hash2 := hashKey(key)
	if hash != hash2 {
		t.Error("hash should be deterministic")
	}

	key2 := "sk-ccrouter-test456"
	hash3 := hashKey(key2)
	if hash == hash3 {
		t.Error("different keys should produce different hashes")
	}
}

func TestCreateGroup(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	id, err := ks.CreateGroup("developers", "default", 0.5, 10)
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive group ID, got %d", id)
	}

	g, err := ks.GetGroupByName("developers")
	if err != nil {
		t.Fatalf("GetGroupByName failed: %v", err)
	}
	if g == nil {
		t.Fatal("group not found")
	}
	if g.Name != "developers" {
		t.Errorf("expected name 'developers', got %s", g.Name)
	}
	if g.Profile != "default" {
		t.Errorf("expected profile 'default', got %s", g.Profile)
	}
	if g.PriorityWeight != 0.5 {
		t.Errorf("expected priority 0.5, got %f", g.PriorityWeight)
	}
	if g.MaxConcurrency != 10 {
		t.Errorf("expected max concurrency 10, got %d", g.MaxConcurrency)
	}
}

func TestCreateGroup_EmptyName(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := ks.CreateGroup("", "default", 1.0, 0)
	if err == nil {
		t.Error("expected error for empty group name")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' in error, got: %v", err)
	}
}

func TestCreateGroup_Duplicate(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := ks.CreateGroup("test", "default", 1.0, 0)
	if err != nil {
		t.Fatalf("first CreateGroup failed: %v", err)
	}

	_, err = ks.CreateGroup("test", "other", 0.5, 5)
	if err == nil {
		t.Error("expected error for duplicate group name")
	}
}

func TestGetGroupByName_NotFound(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	g, err := ks.GetGroupByName("nonexistent")
	if err != nil {
		t.Fatalf("GetGroupByName failed: %v", err)
	}
	if g != nil {
		t.Error("expected nil for nonexistent group")
	}
}

func TestListGroups(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	_, _ = ks.CreateGroup("alpha", "default", 1.0, 0)
	_, _ = ks.CreateGroup("beta", "think", 0.5, 5)

	groups, err := ks.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}

	if groups[0].Name > groups[1].Name {
		t.Error("groups should be sorted by name")
	}
}

func TestUpdateGroup(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	id, _ := ks.CreateGroup("test", "default", 1.0, 0)

	err := ks.UpdateGroup(id, "think", 0.7, 10)
	if err != nil {
		t.Fatalf("UpdateGroup failed: %v", err)
	}

	g, _ := ks.GetGroupByName("test")
	if g.Profile != "think" {
		t.Errorf("expected profile 'think', got %s", g.Profile)
	}
	if g.PriorityWeight != 0.7 {
		t.Errorf("expected priority 0.7, got %f", g.PriorityWeight)
	}
	if g.MaxConcurrency != 10 {
		t.Errorf("expected max concurrency 10, got %d", g.MaxConcurrency)
	}
}

func TestUpdateGroup_NotFound(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	err := ks.UpdateGroup(999, "default", 1.0, 0)
	if err == nil {
		t.Error("expected error for nonexistent group")
	}
}

func TestDeleteGroup(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	id, _ := ks.CreateGroup("temp", "default", 1.0, 0)

	err := ks.DeleteGroup(id)
	if err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	g, _ := ks.GetGroupByName("temp")
	if g != nil {
		t.Error("group should be deleted")
	}
}

func TestDeleteGroup_WithActiveKeys(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("test", "default", 1.0, 0)
	_, _, _ = ks.CreateKey("alice", groupID)

	err := ks.DeleteGroup(groupID)
	if err == nil {
		t.Error("expected error when deleting group with active keys")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Errorf("expected 'active' in error, got: %v", err)
	}
}

func TestCreateKey(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("developers", "default", 1.0, 0)

	rawKey, keyID, err := ks.CreateKey("alice", groupID)
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}
	if keyID <= 0 {
		t.Errorf("expected positive key ID, got %d", keyID)
	}
	if !strings.HasPrefix(rawKey, "sk-ccrouter-") {
		t.Errorf("expected key prefix 'sk-ccrouter-', got %s", rawKey[:15])
	}

	info, err := ks.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if info == nil {
		t.Fatal("key validation returned nil")
	}
	if info.Name != "alice" {
		t.Errorf("expected name 'alice', got %s", info.Name)
	}
	if info.GroupName != "developers" {
		t.Errorf("expected group 'developers', got %s", info.GroupName)
	}
	if info.Profile != "default" {
		t.Errorf("expected profile 'default', got %s", info.Profile)
	}
}

func TestValidateKey_Invalid(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("test", "default", 1.0, 0)
	_, _, _ = ks.CreateKey("alice", groupID)

	info, err := ks.ValidateKey("sk-ccrouter-wrongkey123")
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if info != nil {
		t.Error("expected nil for invalid key")
	}
}

func TestValidateKey_Cache(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("test", "default", 1.0, 0)
	rawKey, _, _ := ks.CreateKey("alice", groupID)

	info1, err := ks.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateKey (1st) failed: %v", err)
	}

	info2, err := ks.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateKey (2nd) failed: %v", err)
	}

	if info1.KeyID != info2.KeyID {
		t.Error("cached result should match")
	}

	ks.mu.RLock()
	hash := hashKey(rawKey)
	entry, ok := ks.cache[hash]
	ks.mu.RUnlock()
	if !ok {
		t.Error("expected key to be cached")
	}
	if time.Now().After(entry.expiresAt) {
		t.Error("cache entry should not be expired yet")
	}
}

func TestListKeys(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)
	groupID2, _ := ks.CreateGroup("ops", "think", 0.5, 5)

	_, _, _ = ks.CreateKey("alice", groupID)
	_, _, _ = ks.CreateKey("bob", groupID)
	_, _, _ = ks.CreateKey("charlie", groupID2)

	keys, err := ks.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}

	for _, k := range keys {
		if !k.IsActive {
			t.Error("new keys should be active")
		}
	}
}

func TestRevokeKey(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("test", "default", 1.0, 0)
	rawKey, keyID, _ := ks.CreateKey("alice", groupID)

	err := ks.RevokeKey(keyID)
	if err != nil {
		t.Fatalf("RevokeKey failed: %v", err)
	}

	info, err := ks.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if info != nil {
		t.Error("revoked key should not validate")
	}

	ks.mu.RLock()
	hash := hashKey(rawKey)
	_, ok := ks.cache[hash]
	ks.mu.RUnlock()
	if ok {
		t.Error("cache should be cleared after revoke")
	}
}

func TestRevokeKey_NotFound(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	err := ks.RevokeKey(999)
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestGetGroupMemberCount(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)
	groupID2, _ := ks.CreateGroup("ops", "think", 0.5, 5)

	_, _, _ = ks.CreateKey("alice", groupID)
	_, _, _ = ks.CreateKey("bob", groupID)
	_, _, _ = ks.CreateKey("charlie", groupID2)

	count, err := ks.GetGroupMemberCount(groupID)
	if err != nil {
		t.Fatalf("GetGroupMemberCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 members in dev, got %d", count)
	}

	count2, _ := ks.GetGroupMemberCount(groupID2)
	if count2 != 1 {
		t.Errorf("expected 1 member in ops, got %d", count2)
	}

	groupID3, _ := ks.CreateGroup("empty", "default", 1.0, 0)
	count3, _ := ks.GetGroupMemberCount(groupID3)
	if count3 != 0 {
		t.Errorf("expected 0 members in empty group, got %d", count3)
	}
}
