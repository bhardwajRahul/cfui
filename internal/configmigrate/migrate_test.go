package configmigrate

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"cfui/internal/persist"

	_ "github.com/lib-x/entsqlite"
)

func TestLoadPrefersLegacyAppTableOverJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPayload := []byte(`{"token":"json-token"}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), jsonPayload, 0644); err != nil {
		t.Fatalf("Write legacy config.json: %v", err)
	}

	db := openTestDB(t, dir)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE app_configs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		"key" TEXT UNIQUE NOT NULL,
		payload BLOB NOT NULL
	)`); err != nil {
		t.Fatalf("Create app_configs: %v", err)
	}

	tablePayload := []byte(`{"token":"table-token"}`)
	if _, err := db.Exec(`INSERT INTO app_configs("key", payload) VALUES(?, ?)`, "default", tablePayload); err != nil {
		t.Fatalf("Insert app_configs payload: %v", err)
	}

	result, err := Load(context.Background(), dir, "default")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if result.Source != SourceLegacyAppTable {
		t.Fatalf("expected source %q, got %q", SourceLegacyAppTable, result.Source)
	}
	if !bytes.Equal(result.Payload, tablePayload) {
		t.Fatalf("expected table payload %q, got %q", tablePayload, result.Payload)
	}
}

func TestCleanupRenamesLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(legacyPath, []byte(`{"token":"legacy"}`), 0644); err != nil {
		t.Fatalf("Write legacy config.json: %v", err)
	}

	if err := Cleanup(context.Background(), dir, SourceLegacyJSON); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "config.json.migrated")); err != nil {
		t.Fatalf("expected migrated backup to exist: %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("expected config.json to be renamed, stat err = %v", err)
	}
}

func TestCleanupDropsLegacyAppConfigTable(t *testing.T) {
	dir := t.TempDir()
	db := openTestDB(t, dir)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE app_configs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		"key" TEXT UNIQUE NOT NULL,
		payload BLOB NOT NULL
	)`); err != nil {
		t.Fatalf("Create app_configs: %v", err)
	}

	if err := Cleanup(context.Background(), dir, SourceLegacyAppTable); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='app_configs'`).Scan(&count); err != nil {
		t.Fatalf("Check app_configs removal: %v", err)
	}
	if count != 0 {
		t.Fatal("expected app_configs table to be dropped")
	}
}

func openTestDB(t *testing.T, dir string) *sql.DB {
	t.Helper()

	db, err := persist.OpenRawDB(dir)
	if err != nil {
		t.Fatalf("OpenRawDB: %v", err)
	}
	return db
}
