package usage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Record represents a usage record.
type Record struct {
	ID            int64
	InstanceID    string
	Profile       string // active route profile name
	Provider      string // provider that handled the request
	Route         string
	Model         string
	Tokens        int
	Fallbacks     int
	Timestamp     time.Time
	APIKeyPrefix  string // prefix of the API key used (multi-user)
	GroupName     string // user group name (multi-user)
}

// DBPath returns the path to the usage database.
func DBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cc-modelrouter", "usage.db"), nil
}

// InitDB initializes the usage database.
func InitDB(path string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}

	// Set SQLite pragmas for better performance
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	// SQLite works best with single connection
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Phase 1: Create table and indexes on existing columns
	tableQuery := `
	CREATE TABLE IF NOT EXISTS usage_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		instance_id TEXT NOT NULL,
		profile TEXT NOT NULL DEFAULT '',
		provider TEXT NOT NULL DEFAULT '',
		route TEXT NOT NULL,
		model TEXT NOT NULL,
		tokens INTEGER NOT NULL,
		fallbacks INTEGER NOT NULL DEFAULT 0,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_instance_route ON usage_records(instance_id, route);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON usage_records(timestamp);
	`
	if _, err := db.Exec(tableQuery); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	// Phase 2: Migrate existing databases (add profile/provider columns if missing)
	migrateColumns(db)

	// Phase 3: Create indexes on new columns (now safe, columns guaranteed to exist)
	indexQuery := `
	CREATE INDEX IF NOT EXISTS idx_profile ON usage_records(profile);
	CREATE INDEX IF NOT EXISTS idx_provider ON usage_records(provider);
	`
	if _, err := db.Exec(indexQuery); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	// Phase 4: Create multi-user tables (api_keys, user_groups)
	multiUserQuery := `
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
	if _, err := db.Exec(multiUserQuery); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create multi-user tables: %w", err)
	}

	// Phase 5: Migrate auth columns (key_encrypted for encrypted key storage)
	migrateAuthColumns(db)

	return db, nil
}

// migrateAuthColumns adds columns to auth tables if they don't exist.
func migrateAuthColumns(db *sql.DB) {
	migrations := []struct {
		table  string
		column string
		def    string
	}{
		{"api_keys", "key_encrypted", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		var count int
		row := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'", m.table, m.column))
		if err := row.Scan(&count); err != nil {
			continue
		}
		if count == 0 {
			db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", m.table, m.column, m.def))
		}
	}
}

// migrateColumns adds profile and provider columns if they don't exist.
func migrateColumns(db *sql.DB) {
	migrations := []struct {
		column string
		def    string
	}{
		{"profile", "TEXT NOT NULL DEFAULT ''"},
		{"provider", "TEXT NOT NULL DEFAULT ''"},
		{"api_key_prefix", "TEXT NOT NULL DEFAULT ''"},
		{"group_name", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		var count int
		row := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('usage_records') WHERE name='%s'", m.column))
		if err := row.Scan(&count); err != nil {
			continue
		}
		if count == 0 {
			db.Exec(fmt.Sprintf("ALTER TABLE usage_records ADD COLUMN %s %s", m.column, m.def))
		}
	}
}

// dbExecutor is an interface that both *sql.DB and *sql.Tx implement.
type dbExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// InsertRecord inserts a usage record.
func InsertRecord(db dbExecutor, r *Record) error {
	query := `
	INSERT INTO usage_records (instance_id, profile, provider, route, model, tokens, fallbacks, timestamp, api_key_prefix, group_name)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query, r.InstanceID, r.Profile, r.Provider, r.Route, r.Model, r.Tokens, r.Fallbacks, r.Timestamp, r.APIKeyPrefix, r.GroupName)
	return err
}

// PruneRecords deletes usage records older than the given time and reclaims disk space.
func PruneRecords(db *sql.DB, before time.Time) (int64, error) {
	result, err := db.Exec("DELETE FROM usage_records WHERE timestamp < ?", before)
	if err != nil {
		return 0, fmt.Errorf("failed to prune records: %w", err)
	}
	count, _ := result.RowsAffected()

	if count > 0 {
		if _, err := db.Exec("VACUUM"); err != nil {
			return count, fmt.Errorf("pruned %d records but VACUUM failed: %w", count, err)
		}
	}

	return count, nil
}

// DeleteAllRecords deletes all usage records and reclaims disk space.
func DeleteAllRecords(db *sql.DB) (int64, error) {
	result, err := db.Exec("DELETE FROM usage_records")
	if err != nil {
		return 0, fmt.Errorf("failed to delete all records: %w", err)
	}
	count, _ := result.RowsAffected()

	if _, err := db.Exec("VACUUM"); err != nil {
		return count, fmt.Errorf("deleted %d records but VACUUM failed: %w", count, err)
	}

	return count, nil
}

// GetRecordsByPeriod retrieves records within a time range, optionally filtered by instance.
func GetRecordsByPeriod(db *sql.DB, instanceID string, start, end time.Time) ([]*Record, error) {
	query := `
	SELECT id, instance_id, profile, provider, route, model, tokens, fallbacks, timestamp, api_key_prefix, group_name
	FROM usage_records
	WHERE timestamp >= ? AND timestamp <= ?
	`
	args := []any{start, end}

	if instanceID != "" {
		query += " AND instance_id = ?"
		args = append(args, instanceID)
	}

	query += " ORDER BY timestamp DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*Record
	for rows.Next() {
		var r Record
		err := rows.Scan(&r.ID, &r.InstanceID, &r.Profile, &r.Provider, &r.Route, &r.Model, &r.Tokens, &r.Fallbacks, &r.Timestamp, &r.APIKeyPrefix, &r.GroupName)
		if err != nil {
			return nil, err
		}
		records = append(records, &r)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}
