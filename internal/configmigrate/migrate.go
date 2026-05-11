package configmigrate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"cfui/internal/persist"
)

const (
	legacyAppConfigTable = "app_configs"
	legacyConfigFile     = "config.json"
)

type Source string

const (
	SourceNone           Source = ""
	SourceLegacyJSON     Source = "legacy_json"
	SourceLegacyAppTable Source = "legacy_app_configs"
)

type Result struct {
	Source  Source
	Payload []byte
}

// Load returns the first available legacy config source in migration priority:
// deprecated app_configs table first, then legacy config.json.
func Load(ctx context.Context, dir, key string) (Result, error) {
	if payload, ok, err := loadLegacyAppConfigPayload(ctx, dir, key); err != nil {
		return Result{}, err
	} else if ok {
		return Result{
			Source:  SourceLegacyAppTable,
			Payload: payload,
		}, nil
	}

	if payload, ok, err := loadLegacyJSON(filepath.Join(dir, legacyConfigFile)); err != nil {
		return Result{}, err
	} else if ok {
		return Result{
			Source:  SourceLegacyJSON,
			Payload: payload,
		}, nil
	}

	return Result{}, nil
}

// Cleanup finalizes a successful migration by removing or renaming the legacy
// source that was imported.
func Cleanup(ctx context.Context, dir string, source Source) error {
	switch source {
	case SourceNone:
		return nil
	case SourceLegacyJSON:
		return persist.MarkLegacyMigrated(filepath.Join(dir, legacyConfigFile))
	case SourceLegacyAppTable:
		return dropLegacyAppConfigTable(ctx, dir)
	default:
		return nil
	}
}

func loadLegacyJSON(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func loadLegacyAppConfigPayload(ctx context.Context, dir, key string) ([]byte, bool, error) {
	db, err := persist.OpenRawDB(dir)
	if err != nil {
		return nil, false, err
	}
	defer db.Close()

	exists, err := tableExists(ctx, db, legacyAppConfigTable)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}

	var payload []byte
	err = db.QueryRowContext(ctx, `SELECT payload FROM app_configs WHERE "key" = ? LIMIT 1`, key).Scan(&payload)
	switch {
	case err == nil:
		return payload, true, nil
	case err == sql.ErrNoRows:
		return nil, false, nil
	default:
		return nil, false, err
	}
}

func dropLegacyAppConfigTable(ctx context.Context, dir string) error {
	db, err := persist.OpenRawDB(dir)
	if err != nil {
		return err
	}
	defer db.Close()

	exists, err := tableExists(ctx, db, legacyAppConfigTable)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	_, err = db.ExecContext(ctx, `DROP TABLE IF EXISTS app_configs`)
	return err
}

func tableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ? LIMIT 1`, tableName).Scan(&name)
	switch {
	case err == nil:
		return true, nil
	case err == sql.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}
