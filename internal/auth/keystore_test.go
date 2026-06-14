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
		user_name   TEXT NOT NULL DEFAULT '',
		group_id    INTEGER NOT NULL,
		is_active   INTEGER NOT NULL DEFAULT 1,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used   DATETIME,
		key_encrypted TEXT NOT NULL DEFAULT '',
		FOREIGN KEY (group_id) REFERENCES user_groups(id)
	);
	CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
	CREATE INDEX IF NOT EXISTS idx_api_keys_group ON api_keys(group_id);

	CREATE TABLE IF NOT EXISTS multi_user_settings (
		id               INTEGER PRIMARY KEY CHECK (id = 1),
		enabled          INTEGER NOT NULL DEFAULT 0,
		global_max_conc  INTEGER NOT NULL DEFAULT 0,
		wred_min_depth   REAL NOT NULL DEFAULT 0.5,
		wred_max_depth   REAL NOT NULL DEFAULT 0.9
	);
	INSERT OR IGNORE INTO multi_user_settings (id) VALUES (1);

	CREATE TABLE IF NOT EXISTS group_members (
		group_id  INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
		user_name TEXT    NOT NULL,
		PRIMARY KEY (group_id, user_name)
	);
	CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_name);
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

// --- Settings CRUD ---

func TestGetSettings(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	s, err := ks.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil settings")
	}
	if s.Enabled {
		t.Error("default should be disabled")
	}
	if s.GlobalMaxConc != 0 {
		t.Errorf("expected GlobalMaxConc=0, got %d", s.GlobalMaxConc)
	}
	if s.WREDMinDepth != 0.5 {
		t.Errorf("expected WREDMinDepth=0.5, got %f", s.WREDMinDepth)
	}
	if s.WREDMaxDepth != 0.9 {
		t.Errorf("expected WREDMaxDepth=0.9, got %f", s.WREDMaxDepth)
	}
}

func TestUpdateSettings(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	s := &MultiUserSettings{
		Enabled:       true,
		GlobalMaxConc: 100,
		WREDMinDepth:  0.3,
		WREDMaxDepth:  0.8,
	}
	if err := ks.UpdateSettings(s); err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}

	got, err := ks.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if !got.Enabled {
		t.Error("expected enabled=true")
	}
	if got.GlobalMaxConc != 100 {
		t.Errorf("expected GlobalMaxConc=100, got %d", got.GlobalMaxConc)
	}
	if got.WREDMinDepth != 0.3 {
		t.Errorf("expected WREDMinDepth=0.3, got %f", got.WREDMinDepth)
	}
	if got.WREDMaxDepth != 0.8 {
		t.Errorf("expected WREDMaxDepth=0.8, got %f", got.WREDMaxDepth)
	}
}

// --- Group CRUD ---

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

func TestDeleteGroup_WithMembers(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("test", "default", 1.0, 0)
	ks.AddGroupMember(groupID, "alice")

	err := ks.DeleteGroup(groupID)
	if err == nil {
		t.Error("expected error when deleting group with members")
	}
	if !strings.Contains(err.Error(), "members") {
		t.Errorf("expected 'members' in error, got: %v", err)
	}
}

// --- Member CRUD ---

func TestAddGroupMember(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)

	err := ks.AddGroupMember(groupID, "alice")
	if err != nil {
		t.Fatalf("AddGroupMember failed: %v", err)
	}

	members, err := ks.ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers failed: %v", err)
	}
	if len(members) != 1 || members[0] != "alice" {
		t.Errorf("expected [alice], got %v", members)
	}
}

func TestAddGroupMember_Duplicate(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)
	ks.AddGroupMember(groupID, "alice")

	// Second add should be a no-op (INSERT OR IGNORE)
	err := ks.AddGroupMember(groupID, "alice")
	if err != nil {
		t.Fatalf("AddGroupMember duplicate failed: %v", err)
	}

	count, _ := ks.GetGroupMemberCount(groupID)
	if count != 1 {
		t.Errorf("expected 1 member, got %d", count)
	}
}

func TestRemoveGroupMember(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)
	ks.AddGroupMember(groupID, "alice")
	ks.AddGroupMember(groupID, "bob")

	err := ks.RemoveGroupMember(groupID, "alice")
	if err != nil {
		t.Fatalf("RemoveGroupMember failed: %v", err)
	}

	members, _ := ks.ListGroupMembers(groupID)
	if len(members) != 1 || members[0] != "bob" {
		t.Errorf("expected [bob], got %v", members)
	}
}

func TestListGroupMembers(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)
	ks.AddGroupMember(groupID, "charlie")
	ks.AddGroupMember(groupID, "alice")
	ks.AddGroupMember(groupID, "bob")

	members, err := ks.ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers failed: %v", err)
	}
	if len(members) != 3 {
		t.Errorf("expected 3 members, got %d", len(members))
	}
	// Should be sorted by user_name
	if members[0] != "alice" || members[1] != "bob" || members[2] != "charlie" {
		t.Errorf("expected [alice, bob, charlie], got %v", members)
	}
}

func TestListGroupMembers_Empty(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("empty", "default", 1.0, 0)

	members, err := ks.ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers failed: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("expected 0 members, got %d", len(members))
	}
}

func TestGetUserGroup(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 0.8, 5)
	ks.AddGroupMember(groupID, "alice")

	g, err := ks.GetUserGroup("alice")
	if err != nil {
		t.Fatalf("GetUserGroup failed: %v", err)
	}
	if g == nil {
		t.Fatal("expected group info for alice")
	}
	if g.Name != "dev" {
		t.Errorf("expected group 'dev', got %s", g.Name)
	}
	if g.Profile != "default" {
		t.Errorf("expected profile 'default', got %s", g.Profile)
	}
	if g.PriorityWeight != 0.8 {
		t.Errorf("expected priority 0.8, got %f", g.PriorityWeight)
	}
}

func TestGetUserGroup_NotFound(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	g, err := ks.GetUserGroup("nobody")
	if err != nil {
		t.Fatalf("GetUserGroup failed: %v", err)
	}
	if g != nil {
		t.Error("expected nil for user with no group")
	}
}

func TestGetUserProfile(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "think", 0.5, 10)
	ks.AddGroupMember(groupID, "alice")

	profile, err := ks.GetUserProfile("alice")
	if err != nil {
		t.Fatalf("GetUserProfile failed: %v", err)
	}
	if profile != "think" {
		t.Errorf("expected profile 'think', got %s", profile)
	}
}

func TestGetUserProfile_NoGroup(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	profile, err := ks.GetUserProfile("nobody")
	if err != nil {
		t.Fatalf("GetUserProfile failed: %v", err)
	}
	if profile != "" {
		t.Errorf("expected empty profile, got %s", profile)
	}
}

func TestGetGroupMemberCount(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)
	groupID2, _ := ks.CreateGroup("ops", "think", 0.5, 5)

	ks.AddGroupMember(groupID, "alice")
	ks.AddGroupMember(groupID, "bob")
	ks.AddGroupMember(groupID2, "charlie")

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

// --- Key CRUD ---

func TestCreateKey(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	rawKey, keyID, err := ks.CreateKey("alice")
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
	if info.UserName != "alice" {
		t.Errorf("expected UserName 'alice', got %s", info.UserName)
	}
	// No group membership yet, so GroupName and Profile should be empty
	if info.GroupName != "" {
		t.Errorf("expected empty GroupName, got %s", info.GroupName)
	}
	if info.Profile != "" {
		t.Errorf("expected empty Profile, got %s", info.Profile)
	}
}

func TestValidateKey_WithGroup(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("developers", "default", 1.0, 0)
	ks.AddGroupMember(groupID, "alice")
	rawKey, _, _ := ks.CreateKey("alice")

	info, err := ks.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if info == nil {
		t.Fatal("key validation returned nil")
	}
	if info.UserName != "alice" {
		t.Errorf("expected UserName 'alice', got %s", info.UserName)
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

	_, _, _ = ks.CreateKey("alice")

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

	rawKey, _, _ := ks.CreateKey("alice")

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

	_, _, _ = ks.CreateKey("alice")
	_, _, _ = ks.CreateKey("bob")
	_, _, _ = ks.CreateKey("charlie")

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
		if k.UserName == "" {
			t.Error("UserName should not be empty")
		}
	}
}

func TestRevokeKey(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	rawKey, keyID, _ := ks.CreateKey("alice")

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

func TestGetRawKey(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	rawKey, keyID, _ := ks.CreateKey("alice")

	retrieved, err := ks.GetRawKey(keyID)
	if err != nil {
		t.Fatalf("GetRawKey failed: %v", err)
	}
	if retrieved != rawKey {
		t.Errorf("retrieved key does not match original")
	}
}

func TestGetRawKey_NotFound(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := ks.GetRawKey(999)
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestGetRawKeyByUserName(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	rawKey, keyID, _ := ks.CreateKey("alice")

	retrieved, id, err := ks.GetRawKeyByUserName("alice")
	if err != nil {
		t.Fatalf("GetRawKeyByUserName failed: %v", err)
	}
	if retrieved != rawKey {
		t.Errorf("retrieved key does not match original")
	}
	if id != keyID {
		t.Errorf("expected key ID %d, got %d", keyID, id)
	}
}

func TestGetRawKeyByUserName_NotFound(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	_, _, err := ks.GetRawKeyByUserName("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent name")
	}
	if !strings.Contains(err.Error(), "no active key found") {
		t.Errorf("expected 'no active key found' in error, got: %v", err)
	}
}

func TestGetRawKeyByUserName_RevokedKey(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	_, keyID, _ := ks.CreateKey("alice")
	ks.RevokeKey(keyID)

	_, _, err := ks.GetRawKeyByUserName("alice")
	if err == nil {
		t.Error("expected error for revoked key")
	}
	if !strings.Contains(err.Error(), "no active key found") {
		t.Errorf("expected 'no active key found' in error, got: %v", err)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"short", "hello"},
		{"api key", "sk-ccrouter-a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"},
		{"empty", ""},
		{"unicode", "hello 世界"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := Encrypt(tt.data)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}
			if encrypted == "" {
				t.Error("encrypted string should not be empty")
			}
			decrypted, err := Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}
			if decrypted != tt.data {
				t.Errorf("decrypted value does not match: got %q, want %q", decrypted, tt.data)
			}
		})
	}
}

func TestDecrypt_InvalidInput(t *testing.T) {
	_, err := Decrypt("not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64 input")
	}

	_, err = Decrypt("dGVzdA==") // valid base64 but too short for nonce
	if err == nil {
		t.Error("expected error for too-short ciphertext")
	}
}

func TestListGroupsWithMemberCounts(t *testing.T) {
	ks, cleanup := setupTestDB(t)
	defer cleanup()

	groupID, _ := ks.CreateGroup("dev", "default", 1.0, 0)
	groupID2, _ := ks.CreateGroup("ops", "think", 0.5, 5)
	_, _ = ks.CreateGroup("empty", "default", 1.0, 0)

	ks.AddGroupMember(groupID, "alice")
	ks.AddGroupMember(groupID, "bob")
	ks.AddGroupMember(groupID2, "charlie")

	groups, err := ks.ListGroupsWithMemberCounts()
	if err != nil {
		t.Fatalf("ListGroupsWithMemberCounts failed: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// Should be sorted by name: dev, empty, ops
	counts := make(map[string]int)
	for _, g := range groups {
		counts[g.Name] = g.MemberCount
	}
	if counts["dev"] != 2 {
		t.Errorf("expected 2 members in dev, got %d", counts["dev"])
	}
	if counts["ops"] != 1 {
		t.Errorf("expected 1 member in ops, got %d", counts["ops"])
	}
	if counts["empty"] != 0 {
		t.Errorf("expected 0 members in empty, got %d", counts["empty"])
	}
}
