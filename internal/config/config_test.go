package config

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cfui/internal/persist"

	_ "github.com/lib-x/entsqlite"
)

func TestEffectiveTunnelManagementEnvironmentOverrides(t *testing.T) {
	t.Setenv("CFUI_TUNNEL_MGMT_ENABLED", "true")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "env-account")
	t.Setenv("CLOUDFLARE_TUNNEL_ID", "env-tunnel")
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-token")

	cfg := DefaultConfig()
	cfg.TunnelManagement = TunnelManagementConfig{
		Enabled:   false,
		AccountID: "saved-account",
		TunnelID:  "saved-tunnel",
		APIToken:  "saved-token",
	}

	effective := cfg.EffectiveTunnelManagement()
	if !effective.Enabled {
		t.Fatal("expected environment to enable tunnel management")
	}
	if effective.AccountID != "env-account" || effective.TunnelID != "env-tunnel" || effective.APIToken != "env-token" {
		t.Fatalf("unexpected effective config: %#v", effective)
	}
}

func TestNewManagerAutoCreatesDatabase(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if got := mgr.Get().SoftwareName; got != "cfui" {
		t.Fatalf("default config not loaded, software_name = %q", got)
	}

	if _, err := os.Stat(persist.DBPath(dir)); err != nil {
		t.Fatalf("expected database file to exist: %v", err)
	}
}

func TestNewManagerMigratesLegacyConfigJSON(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "config.json")
	legacyCfg := DefaultConfig()
	legacyCfg.Token = "legacy-token"
	legacyCfg.AutoStart = true
	legacyCfg.MCPEnabled = true
	legacyCfg.DDNS.Enabled = true
	legacyCfg.DDNS.IntervalMins = 9
	legacyCfg.DDNS.Records = []DDNSRecord{{
		Name:    "home.example.com",
		ZoneID:  "zone-1",
		Type:    "A",
		Proxied: true,
		TTL:     1,
	}}

	data, err := json.Marshal(legacyCfg)
	if err != nil {
		t.Fatalf("Marshal legacy config: %v", err)
	}
	if err := os.WriteFile(legacyPath, data, 0644); err != nil {
		t.Fatalf("Write legacy config: %v", err)
	}

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	got := mgr.Get()
	if got.Token != legacyCfg.Token || got.AutoStart != legacyCfg.AutoStart || !got.MCPEnabled {
		t.Fatalf("legacy config not migrated correctly: %#v", got)
	}
	if got.DDNS.IntervalMins != 9 || len(got.DDNS.Records) != 1 || got.DDNS.Records[0].Name != "home.example.com" {
		t.Fatalf("legacy DDNS config not migrated correctly: %#v", got.DDNS)
	}

	if _, err := os.Stat(filepath.Join(dir, "config.json.migrated")); err != nil {
		t.Fatalf("expected migrated backup to exist: %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy config.json to be renamed, stat err = %v", err)
	}

	reloaded, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager reload: %v", err)
	}
	if reloaded.Get().Token != legacyCfg.Token {
		t.Fatalf("expected config to load from database after migration, got %#v", reloaded.Get())
	}
}

func TestNewManagerMigratesLegacyAppConfigsTable(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(persist.DBPath(dir))+"?cache=shared&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE app_configs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		"key" TEXT UNIQUE NOT NULL,
		payload BLOB NOT NULL
	)`); err != nil {
		t.Fatalf("Create legacy app_configs: %v", err)
	}

	legacyCfg := DefaultConfig()
	legacyCfg.Token = "db-legacy-token"
	legacyCfg.TunnelManagement.APIToken = "api-token-from-db"
	legacyCfg.DDNS.Enabled = true
	legacyCfg.DDNS.IPSources = []IPSource{{URL: "https://example.com/ip", IPType: "ipv4"}}
	legacyCfg.DDNS.Records = []DDNSRecord{{
		Name:     "host.example.com",
		ZoneID:   "zone-db",
		ZoneName: "example.com",
		Type:     "A",
		Value:    "{IPV4}",
		Proxied:  true,
		TTL:      120,
	}}

	payload, err := json.Marshal(legacyCfg)
	if err != nil {
		t.Fatalf("Marshal legacy payload: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO app_configs("key", payload) VALUES(?, ?)`, defaultConfigKey, payload); err != nil {
		t.Fatalf("Insert legacy payload: %v", err)
	}

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	got := mgr.Get()
	if got.Token != legacyCfg.Token || got.TunnelManagement.APIToken != legacyCfg.TunnelManagement.APIToken {
		t.Fatalf("legacy app_configs data not migrated correctly: %#v", got)
	}
	if !got.DDNS.Enabled || len(got.DDNS.IPSources) != 1 || len(got.DDNS.Records) != 1 {
		t.Fatalf("legacy DDNS data not migrated correctly: %#v", got.DDNS)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='app_configs'`).Scan(&count); err != nil {
		t.Fatalf("Check legacy table removal: %v", err)
	}
	if count != 0 {
		t.Fatal("expected app_configs table to be dropped after migration")
	}
}
