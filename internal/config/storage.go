package config

import (
	"context"
	"encoding/json"
	"os"

	"cfui/internal/configmigrate"
	"cfui/internal/logger"
	"cfui/internal/persist"
	"cfui/internal/persist/ent"
	"cfui/internal/persist/ent/appsetting"
	"cfui/internal/persist/ent/ddnsipsource"
	"cfui/internal/persist/ent/ddnsrecord"
	"cfui/internal/persist/ent/ddnssetting"
	"cfui/internal/persist/ent/tunnelmanagement"
	"cfui/internal/persist/ent/tunneltoken"
)

const defaultConfigKey = "default"

func (m *Manager) loadLocked(ctx context.Context) (Config, error) {
	if cfg, ok, err := m.loadStructuredConfig(ctx); err != nil {
		return Config{}, err
	} else if ok {
		if err := configmigrate.Cleanup(ctx, m.dir, configmigrate.SourceLegacyAppTable); err != nil && logger.Sugar != nil {
			logger.Sugar.Warnf("Failed to delete migrated legacy app_configs table: %v", err)
		}
		return cfg, nil
	}

	legacy, err := configmigrate.Load(ctx, m.dir, defaultConfigKey)
	if err != nil {
		return Config{}, err
	}
	if legacy.Source != configmigrate.SourceNone {
		cfg, err := decodeConfig(legacy.Payload)
		if err != nil {
			return Config{}, err
		}
		if err := m.saveLocked(ctx, cfg); err != nil {
			return Config{}, err
		}
		logLegacyMigration(legacy.Source, m.dir)
		cleanupLegacyMigration(ctx, m.dir, legacy.Source)
		return cfg, nil
	}

	cfg := DefaultConfig()
	if err := m.saveLocked(ctx, cfg); err != nil {
		return Config{}, err
	}
	if logger.Sugar != nil {
		logger.Sugar.Infof("Initialized default configuration in %s", persist.DBPath(m.dir))
	}
	return cfg, nil
}

func (m *Manager) saveLocked(ctx context.Context, cfg Config) error {
	tx, err := m.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = saveAppSetting(ctx, tx, cfg); err != nil {
		return err
	}
	if err = saveTunnelToken(ctx, tx, cfg.Token); err != nil {
		return err
	}
	if err = saveTunnelManagement(ctx, tx, cfg.TunnelManagement); err != nil {
		return err
	}
	if err = saveDDNSSetting(ctx, tx, cfg.DDNS); err != nil {
		return err
	}
	if err = replaceDDNSIPSources(ctx, tx, cfg.DDNS.IPSources); err != nil {
		return err
	}
	if err = replaceDDNSRecords(ctx, tx, cfg.DDNS.Records); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (m *Manager) loadStructuredConfig(ctx context.Context) (Config, bool, error) {
	settingsRow, err := m.client.AppSetting.Query().Where(appsetting.Key(defaultConfigKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}

	cfg := DefaultConfig()
	cfg.DDNS.IPSources = []IPSource{}
	cfg.DDNS.Records = []DDNSRecord{}

	cfg.AutoStart = settingsRow.AutoStart
	cfg.AutoRestart = settingsRow.AutoRestart
	cfg.CustomTag = settingsRow.CustomTag
	cfg.SoftwareName = settingsRow.SoftwareName
	cfg.Protocol = settingsRow.Protocol
	cfg.GracePeriod = settingsRow.GracePeriod
	cfg.Region = settingsRow.Region
	cfg.Retries = settingsRow.Retries
	cfg.MetricsEnable = settingsRow.MetricsEnable
	cfg.MetricsPort = settingsRow.MetricsPort
	cfg.LogLevel = settingsRow.LogLevel
	cfg.LogFile = settingsRow.LogFile
	cfg.LogJSON = settingsRow.LogJSON
	cfg.EdgeIPVersion = settingsRow.EdgeIPVersion
	cfg.EdgeBindAddress = settingsRow.EdgeBindAddress
	cfg.PostQuantum = settingsRow.PostQuantum
	cfg.NoTLSVerify = settingsRow.NoTLSVerify
	cfg.ExtraArgs = settingsRow.ExtraArgs
	cfg.MCPEnabled = settingsRow.McpEnabled

	if tokenRow, err := m.client.TunnelToken.Query().Where(tunneltoken.Key(defaultConfigKey)).Only(ctx); err == nil {
		cfg.Token = tokenRow.Token
	} else if !ent.IsNotFound(err) {
		return Config{}, false, err
	}

	if managementRow, err := m.client.TunnelManagement.Query().Where(tunnelmanagement.Key(defaultConfigKey)).Only(ctx); err == nil {
		cfg.TunnelManagement = TunnelManagementConfig{
			Enabled:   managementRow.Enabled,
			AccountID: managementRow.AccountID,
			TunnelID:  managementRow.TunnelID,
			APIToken:  managementRow.APIToken,
			APIEmail:  managementRow.APIEmail,
			APIKey:    managementRow.APIKey,
		}
	} else if !ent.IsNotFound(err) {
		return Config{}, false, err
	}

	if ddnsRow, err := m.client.DDNSSetting.Query().Where(ddnssetting.Key(defaultConfigKey)).Only(ctx); err == nil {
		cfg.DDNS.Enabled = ddnsRow.Enabled
		cfg.DDNS.IntervalMins = ddnsRow.IntervalMins
		cfg.DDNS.OnlyOnChange = ddnsRow.OnlyOnChange
		cfg.DDNS.MaxRetries = ddnsRow.MaxRetries
	} else if !ent.IsNotFound(err) {
		return Config{}, false, err
	}

	sourceRows, err := m.client.DDNSIPSource.Query().
		Where(ddnsipsource.SettingsKey(defaultConfigKey)).
		Order(ddnsipsource.BySortOrder()).
		All(ctx)
	if err != nil {
		return Config{}, false, err
	}
	for _, row := range sourceRows {
		cfg.DDNS.IPSources = append(cfg.DDNS.IPSources, IPSource{
			URL:    row.URL,
			IPType: row.IPType,
		})
	}

	recordRows, err := m.client.DDNSRecord.Query().
		Where(ddnsrecord.SettingsKey(defaultConfigKey)).
		Order(ddnsrecord.BySortOrder()).
		All(ctx)
	if err != nil {
		return Config{}, false, err
	}
	for _, row := range recordRows {
		cfg.DDNS.Records = append(cfg.DDNS.Records, DDNSRecord{
			Name:     row.Name,
			ZoneID:   row.ZoneID,
			ZoneName: row.ZoneName,
			Type:     row.Type,
			Value:    row.Value,
			Comment:  NormalizeDDNSRecordComment(row.Comment),
			Proxied:  row.Proxied,
			TTL:      row.TTL,
		})
	}

	return cfg, true, nil
}

func saveAppSetting(ctx context.Context, tx *ent.Tx, cfg Config) error {
	row, err := tx.AppSetting.Query().Where(appsetting.Key(defaultConfigKey)).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = tx.AppSetting.Create().
			SetKey(defaultConfigKey).
			SetAutoStart(cfg.AutoStart).
			SetAutoRestart(cfg.AutoRestart).
			SetCustomTag(cfg.CustomTag).
			SetSoftwareName(cfg.SoftwareName).
			SetProtocol(cfg.Protocol).
			SetGracePeriod(cfg.GracePeriod).
			SetRegion(cfg.Region).
			SetRetries(cfg.Retries).
			SetMetricsEnable(cfg.MetricsEnable).
			SetMetricsPort(cfg.MetricsPort).
			SetLogLevel(cfg.LogLevel).
			SetLogFile(cfg.LogFile).
			SetLogJSON(cfg.LogJSON).
			SetEdgeIPVersion(cfg.EdgeIPVersion).
			SetEdgeBindAddress(cfg.EdgeBindAddress).
			SetPostQuantum(cfg.PostQuantum).
			SetNoTLSVerify(cfg.NoTLSVerify).
			SetExtraArgs(cfg.ExtraArgs).
			SetMcpEnabled(cfg.MCPEnabled).
			Save(ctx)
		return err
	}
	if err != nil {
		return err
	}

	_, err = tx.AppSetting.UpdateOneID(row.ID).
		SetAutoStart(cfg.AutoStart).
		SetAutoRestart(cfg.AutoRestart).
		SetCustomTag(cfg.CustomTag).
		SetSoftwareName(cfg.SoftwareName).
		SetProtocol(cfg.Protocol).
		SetGracePeriod(cfg.GracePeriod).
		SetRegion(cfg.Region).
		SetRetries(cfg.Retries).
		SetMetricsEnable(cfg.MetricsEnable).
		SetMetricsPort(cfg.MetricsPort).
		SetLogLevel(cfg.LogLevel).
		SetLogFile(cfg.LogFile).
		SetLogJSON(cfg.LogJSON).
		SetEdgeIPVersion(cfg.EdgeIPVersion).
		SetEdgeBindAddress(cfg.EdgeBindAddress).
		SetPostQuantum(cfg.PostQuantum).
		SetNoTLSVerify(cfg.NoTLSVerify).
		SetExtraArgs(cfg.ExtraArgs).
		SetMcpEnabled(cfg.MCPEnabled).
		Save(ctx)
	return err
}

func saveTunnelToken(ctx context.Context, tx *ent.Tx, token string) error {
	row, err := tx.TunnelToken.Query().Where(tunneltoken.Key(defaultConfigKey)).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = tx.TunnelToken.Create().
			SetKey(defaultConfigKey).
			SetToken(token).
			Save(ctx)
		return err
	}
	if err != nil {
		return err
	}

	_, err = tx.TunnelToken.UpdateOneID(row.ID).
		SetToken(token).
		Save(ctx)
	return err
}

func saveTunnelManagement(ctx context.Context, tx *ent.Tx, cfg TunnelManagementConfig) error {
	row, err := tx.TunnelManagement.Query().Where(tunnelmanagement.Key(defaultConfigKey)).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = tx.TunnelManagement.Create().
			SetKey(defaultConfigKey).
			SetEnabled(cfg.Enabled).
			SetAccountID(cfg.AccountID).
			SetTunnelID(cfg.TunnelID).
			SetAPIToken(cfg.APIToken).
			SetAPIEmail(cfg.APIEmail).
			SetAPIKey(cfg.APIKey).
			Save(ctx)
		return err
	}
	if err != nil {
		return err
	}

	_, err = tx.TunnelManagement.UpdateOneID(row.ID).
		SetEnabled(cfg.Enabled).
		SetAccountID(cfg.AccountID).
		SetTunnelID(cfg.TunnelID).
		SetAPIToken(cfg.APIToken).
		SetAPIEmail(cfg.APIEmail).
		SetAPIKey(cfg.APIKey).
		Save(ctx)
	return err
}

func saveDDNSSetting(ctx context.Context, tx *ent.Tx, cfg DDNSConfig) error {
	row, err := tx.DDNSSetting.Query().Where(ddnssetting.Key(defaultConfigKey)).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = tx.DDNSSetting.Create().
			SetKey(defaultConfigKey).
			SetEnabled(cfg.Enabled).
			SetIntervalMins(cfg.IntervalMins).
			SetOnlyOnChange(cfg.OnlyOnChange).
			SetMaxRetries(cfg.MaxRetries).
			Save(ctx)
		return err
	}
	if err != nil {
		return err
	}

	_, err = tx.DDNSSetting.UpdateOneID(row.ID).
		SetEnabled(cfg.Enabled).
		SetIntervalMins(cfg.IntervalMins).
		SetOnlyOnChange(cfg.OnlyOnChange).
		SetMaxRetries(cfg.MaxRetries).
		Save(ctx)
	return err
}

func replaceDDNSIPSources(ctx context.Context, tx *ent.Tx, sources []IPSource) error {
	if _, err := tx.DDNSIPSource.Delete().Where(ddnsipsource.SettingsKey(defaultConfigKey)).Exec(ctx); err != nil {
		return err
	}

	builders := make([]*ent.DDNSIPSourceCreate, 0, len(sources))
	for i, src := range sources {
		builders = append(builders, tx.DDNSIPSource.Create().
			SetSettingsKey(defaultConfigKey).
			SetSortOrder(i).
			SetURL(src.URL).
			SetIPType(src.IPType))
	}
	if len(builders) == 0 {
		return nil
	}
	return tx.DDNSIPSource.CreateBulk(builders...).Exec(ctx)
}

func replaceDDNSRecords(ctx context.Context, tx *ent.Tx, records []DDNSRecord) error {
	if _, err := tx.DDNSRecord.Delete().Where(ddnsrecord.SettingsKey(defaultConfigKey)).Exec(ctx); err != nil {
		return err
	}

	builders := make([]*ent.DDNSRecordCreate, 0, len(records))
	for i, rec := range records {
		builders = append(builders, tx.DDNSRecord.Create().
			SetSettingsKey(defaultConfigKey).
			SetSortOrder(i).
			SetName(rec.Name).
			SetZoneID(rec.ZoneID).
			SetZoneName(rec.ZoneName).
			SetType(rec.Type).
			SetValue(rec.Value).
			SetComment(NormalizeDDNSRecordComment(rec.Comment)).
			SetProxied(rec.Proxied).
			SetTTL(rec.TTL))
	}
	if len(builders) == 0 {
		return nil
	}
	return tx.DDNSRecord.CreateBulk(builders...).Exec(ctx)
}

func decodeConfig(payload []byte) (Config, error) {
	cfg := DefaultConfig()
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func cleanupLegacyMigration(ctx context.Context, dir string, source configmigrate.Source) {
	err := configmigrate.Cleanup(ctx, dir, source)
	if err == nil || (source == configmigrate.SourceLegacyJSON && os.IsNotExist(err)) {
		return
	}

	if logger.Sugar == nil {
		return
	}

	switch source {
	case configmigrate.SourceLegacyAppTable:
		logger.Sugar.Warnf("Failed to delete migrated legacy app_configs table: %v", err)
	case configmigrate.SourceLegacyJSON:
		logger.Sugar.Warnf("Failed to rename migrated legacy config.json in %s: %v", dir, err)
	}
}

func logLegacyMigration(source configmigrate.Source, dir string) {
	if logger.Sugar == nil {
		return
	}

	switch source {
	case configmigrate.SourceLegacyAppTable:
		logger.Sugar.Infof("Migrated legacy config from app_configs to structured tables in %s", persist.DBPath(dir))
	case configmigrate.SourceLegacyJSON:
		logger.Sugar.Infof("Migrated legacy config from config.json to structured tables in %s", persist.DBPath(dir))
	}
}
