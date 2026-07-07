package proxy

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/iimmutable-ai/cc-modelrouter/internal/auth"
)

func initTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "auth_test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS multi_user_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER NOT NULL DEFAULT 0,
			global_max_conc INTEGER NOT NULL DEFAULT 10,
			wred_min_depth REAL NOT NULL DEFAULT 0.0,
			wred_max_depth REAL NOT NULL DEFAULT 10.0
		)`,
		`INSERT OR IGNORE INTO multi_user_settings (id) VALUES (1)`,
		`CREATE TABLE IF NOT EXISTS user_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			profile TEXT NOT NULL DEFAULT '',
			priority_weight REAL NOT NULL DEFAULT 1.0,
			max_concurrency INTEGER NOT NULL DEFAULT 5,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS group_members (
			group_id INTEGER NOT NULL REFERENCES user_groups(id),
			user_name TEXT NOT NULL,
			PRIMARY KEY (group_id, user_name)
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			user_name TEXT NOT NULL,
			group_id INTEGER REFERENCES user_groups(id),
			is_active INTEGER NOT NULL DEFAULT 1,
			key_encrypted TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used TIMESTAMP NULL
		)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("exec ddl: %v", err)
		}
	}
	return db
}

func TestNewAuthInterceptor(t *testing.T) {
	ks := auth.NewKeyStore(nil)
	ai := NewAuthInterceptor(ks)
	if ai == nil {
		t.Fatal("NewAuthInterceptor returned nil")
	}
	if ai.KeyStore != ks {
		t.Error("KeyStore not set")
	}
}

func TestAuthenticate_EmptyToken(t *testing.T) {
	ai := NewAuthInterceptor(auth.NewKeyStore(nil))
	info, err := ai.Authenticate("")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if info != nil {
		t.Error("expected nil info for empty token")
	}
}

func TestAuthenticate_ValidToken(t *testing.T) {
	db := initTestDB(t)
	defer db.Close()

	ks := auth.NewKeyStore(db)
	rawKey, _, err := ks.CreateKey("testuser", 0)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	ai := NewAuthInterceptor(ks)
	info, err := ai.Authenticate(rawKey)
	if err != nil {
		t.Fatalf("authenticate valid key: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.UserName != "testuser" {
		t.Errorf("expected user 'testuser', got %q", info.UserName)
	}
}

func TestAuthenticate_InvalidToken(t *testing.T) {
	db := initTestDB(t)
	defer db.Close()

	ai := NewAuthInterceptor(auth.NewKeyStore(db))
	info, err := ai.Authenticate("sk-ccr-invalidkey123456789")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
	if info != nil {
		t.Error("expected nil info for invalid key")
	}
}

func TestAuthenticate_RevokedKey(t *testing.T) {
	db := initTestDB(t)
	defer db.Close()

	ks := auth.NewKeyStore(db)
	rawKey, keyID, err := ks.CreateKey("testuser", 0)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if err := ks.RevokeKey(keyID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}

	ai := NewAuthInterceptor(ks)
	info, err := ai.Authenticate(rawKey)
	if err == nil {
		t.Fatal("expected error for revoked key")
	}
	if info != nil {
		t.Error("expected nil info for revoked key")
	}
}

func TestAuthenticate_WithGroup(t *testing.T) {
	db := initTestDB(t)
	defer db.Close()

	ks := auth.NewKeyStore(db)
	groupID, err := ks.CreateGroup("admins", "admin-profile", 2.0, 10)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := ks.AddGroupMember(groupID, "testuser"); err != nil {
		t.Fatalf("add group member: %v", err)
	}

	rawKey, _, err := ks.CreateKey("testuser", groupID)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	ai := NewAuthInterceptor(ks)
	info, err := ai.Authenticate(rawKey)
	if err != nil {
		t.Fatalf("authenticate with group: %v", err)
	}
	if info.GroupName != "admins" {
		t.Errorf("expected group 'admins', got %q", info.GroupName)
	}
	if info.Profile != "admin-profile" {
		t.Errorf("expected profile 'admin-profile', got %q", info.Profile)
	}
}

func TestWriteAuthError_ResponseFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteAuthError(rec, http.StatusUnauthorized, "invalid API key")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if body["type"] != "error" {
		t.Errorf("expected type 'error', got %q", body["type"])
	}
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'error' object in response")
	}
	if errObj["type"] != "authentication_error" {
		t.Errorf("expected error.type 'authentication_error', got %q", errObj["type"])
	}
	if errObj["message"] != "invalid API key" {
		t.Errorf("expected error.message 'invalid API key', got %q", errObj["message"])
	}
}

func TestWriteAuthError_DifferentStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
	}{
		{"forbidden", http.StatusForbidden, "access denied"},
		{"unauthorized", http.StatusUnauthorized, "missing token"},
		{"bad request", http.StatusBadRequest, "malformed token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteAuthError(rec, tt.statusCode, tt.message)
			if rec.Code != tt.statusCode {
				t.Errorf("expected %d, got %d", tt.statusCode, rec.Code)
			}
		})
	}
}
