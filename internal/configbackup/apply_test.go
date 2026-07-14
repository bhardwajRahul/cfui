package configbackup

import (
	"cfui/internal/config"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildExportsOnlySelectedSectionsAndCredentials(t *testing.T) {
	cfg := backupFixtureConfig()
	payload, err := Build(cfg, ExportOptions{
		Sections:         []Section{SectionS3WebDAV, SectionTunnels, SectionRemoteManagement},
		IncludeSensitive: true,
	}, "v1.2.3", time.Date(2026, 7, 14, 13, 0, 0, 0, time.FixedZone("UTC+2", 2*60*60)))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	wantSections := []Section{SectionTunnels, SectionRemoteManagement, SectionS3WebDAV, SectionSensitive}
	if !slices.Equal(payload.Sections, wantSections) {
		t.Fatalf("unexpected section order: %#v", payload.Sections)
	}
	if payload.CreatedAt.Location() != time.UTC || payload.CreatedAt.Hour() != 11 {
		t.Fatalf("creation time was not normalized to UTC: %v", payload.CreatedAt)
	}
	if payload.DDNS != nil || payload.Application != nil {
		t.Fatalf("unselected sections were exported: %#v", payload)
	}
	if payload.Tunnels == nil || payload.Tunnels.Profiles[0].Key != "one" {
		t.Fatalf("tunnel section missing: %#v", payload.Tunnels)
	}
	if payload.RemoteManagement == nil || payload.RemoteManagement.APIEmail != "owner@example.com" {
		t.Fatalf("remote section missing: %#v", payload.RemoteManagement)
	}
	if payload.Sensitive == nil || payload.Sensitive.TunnelTokens["one"] != "tunnel-secret-one" || payload.Sensitive.APIToken != "api-token" || payload.Sensitive.APIKey != "global-key" {
		t.Fatalf("selected credentials missing: %#v", payload.Sensitive)
	}
	if got := payload.Sensitive.S3["primary"]; got.AccessKeyID != "access-one" || got.SecretAccessKey != "secret-one" || got.WebDAVPasswordHash != "hash-one" {
		t.Fatalf("S3 credentials missing: %#v", got)
	}
}

func TestBuildOmitsSensitiveCredentialsByDefault(t *testing.T) {
	payload, err := Build(backupFixtureConfig(), ExportOptions{Sections: []Section{SectionTunnels, SectionRemoteManagement, SectionS3WebDAV}}, "v1", time.Now())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if payload.Sensitive != nil || slices.Contains(payload.Sections, SectionSensitive) {
		t.Fatalf("credentials were exported by default: %#v", payload.Sensitive)
	}
}

func TestApplyTunnelSectionPreservesUnselectedCredentialsAndRemoteFields(t *testing.T) {
	current := backupFixtureConfig()
	payload := validPayload(
		[]Section{SectionTunnels},
		&TunnelSection{
			ActiveKey: "one",
			Profiles: []TunnelProfile{
				{Key: "one", Name: "Imported One", LocalEnabled: true, Protocol: "quic", SoftwareName: "cfui", GracePeriod: "45s", Retries: 7, MetricsPort: 60124, LogLevel: "warn", EdgeIPVersion: "4"},
				{Key: "three", Name: "Imported Three", LocalEnabled: true, Protocol: "auto", SoftwareName: "cfui", GracePeriod: "30s", Retries: 5, MetricsPort: 60125, LogLevel: "info", EdgeIPVersion: "auto"},
			},
		},
		nil, nil, nil, nil, nil,
	)

	result, err := Apply(current, payload, []Section{SectionTunnels})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !slices.Equal(result.RemovedTunnelKeys, []string{"two"}) || !slices.Equal(result.ChangedTunnelKeys, []string{"one"}) {
		t.Fatalf("unexpected tunnel diff: removed=%v changed=%v", result.RemovedTunnelKeys, result.ChangedTunnelKeys)
	}
	one := tunnelByKey(t, result.Config, "one")
	if one.Token != "tunnel-secret-one" || one.AccountID != "account-one" || one.TunnelID != "tunnel-one" || !one.RemoteManagementEnabled {
		t.Fatalf("matching tunnel credentials or remote fields were not preserved: %#v", one)
	}
	three := tunnelByKey(t, result.Config, "three")
	if three.Token != "" || three.AccountID != "" || three.TunnelID != "" || three.RemoteManagementEnabled {
		t.Fatalf("new tunnel inherited credentials or remote fields: %#v", three)
	}
}

func TestApplyCanonicalizesKeysBeforePreservingCredentials(t *testing.T) {
	current := backupFixtureConfig()
	current.ActiveTunnelKey = "foo"
	current.Tunnels = []config.TunnelProfileConfig{{
		Key: "foo", Name: "Existing", Token: "preserved-token", LocalEnabled: true,
		SoftwareName: "cfui", Protocol: "auto", GracePeriod: "30s", Retries: 5,
		MetricsPort: 60123, LogLevel: "info", EdgeIPVersion: "auto",
	}}
	payload := validPayload(
		[]Section{SectionTunnels},
		&TunnelSection{ActiveKey: " Foo ", Profiles: []TunnelProfile{{Key: " Foo ", Name: "Imported"}}},
		nil, nil, nil, nil, nil,
	)

	result, err := Apply(current, payload, []Section{SectionTunnels})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Config.Tunnels) != 1 || result.Config.Tunnels[0].Key != "foo" || result.Config.Tunnels[0].Token != "preserved-token" {
		t.Fatalf("canonical credential preservation failed: %#v", result.Config.Tunnels)
	}
}

func TestApplyRejectsCanonicalKeyCollisions(t *testing.T) {
	payload := validPayload(
		[]Section{SectionTunnels},
		&TunnelSection{
			ActiveKey: "Prod_A",
			Profiles:  []TunnelProfile{{Key: "Prod_A"}, {Key: "prod-a"}},
		},
		nil, nil, nil, nil, nil,
	)

	if _, err := Apply(backupFixtureConfig(), payload, []Section{SectionTunnels}); !errors.Is(err, ErrInvalidBackup) {
		t.Fatalf("expected canonical collision rejection, got %v", err)
	}
}

func TestApplyS3SectionPreservesMatchingCredentials(t *testing.T) {
	current := backupFixtureConfig()
	payload := validPayload(
		[]Section{SectionS3WebDAV}, nil, nil, nil,
		&S3WebDAVSection{
			Enabled:          true,
			ActiveKey:        "primary",
			WebDAVAccessMode: config.S3WebDAVAccessModeDedicated,
			DedicatedPort:    18080,
			Mounts: []S3Mount{
				{Key: "primary", Name: "Imported", Enabled: true, WebDAVEnabled: true, WebDAVAuthEnabled: true, MountType: config.MountTypeS3, Provider: "generic_s3", Region: "auto", PathStyle: true, BucketName: "imported", MountPath: "/webdav/imported/", Jurisdiction: "default", WebDAVUsername: "imported-user"},
				{Key: "new", Name: "New", Enabled: true, WebDAVEnabled: true, MountType: config.MountTypeS3, Provider: "generic_s3", Region: "auto", PathStyle: true, BucketName: "new", MountPath: "/webdav/new/", Jurisdiction: "default"},
			},
		}, nil, nil,
	)

	result, err := Apply(current, payload, []Section{SectionS3WebDAV})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	primary := s3ByKey(t, result.Config, "primary")
	if primary.AccessKeyID != "access-one" || primary.SecretAccessKey != "secret-one" || primary.WebDAVPasswordHash != "hash-one" {
		t.Fatalf("matching S3 credentials were not preserved: %#v", primary)
	}
	newMount := s3ByKey(t, result.Config, "new")
	if newMount.AccessKeyID != "" || newMount.SecretAccessKey != "" || newMount.WebDAVPasswordHash != "" {
		t.Fatalf("new mount inherited credentials: %#v", newMount)
	}
}

func TestApplySensitiveOnlyUpdatesMatchingObjectsAndWarnsForUnknownKeys(t *testing.T) {
	current := backupFixtureConfig()
	payload := validPayload(
		[]Section{SectionTunnels, SectionRemoteManagement, SectionS3WebDAV, SectionSensitive},
		&TunnelSection{ActiveKey: "one", Profiles: []TunnelProfile{{Key: "one"}}},
		&RemoteManagementSection{Profiles: []RemoteProfile{{Key: "one"}}, APIEmail: "owner@example.com"}, nil,
		&S3WebDAVSection{ActiveKey: "primary", Mounts: []S3Mount{{Key: "primary"}}},
		nil,
		&SensitiveSection{
			TunnelTokens: map[string]string{"one": "imported-token", "missing": "ignored-token"},
			APIToken:     "imported-api-token",
			APIKey:       "imported-api-key",
			S3: map[string]S3Credentials{
				"primary": {AccessKeyID: "imported-access", SecretAccessKey: "imported-secret", WebDAVPasswordHash: "imported-hash"},
				"missing": {AccessKeyID: "ignored-access"},
			},
		},
	)

	result, err := Apply(current, payload, []Section{SectionSensitive})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if tunnelByKey(t, result.Config, "one").Token != "imported-token" || result.Config.TunnelManagement.APIToken != "imported-api-token" || result.Config.TunnelManagement.APIKey != "imported-api-key" {
		t.Fatalf("shared or tunnel credentials were not updated: %#v", result.Config.TunnelManagement)
	}
	primary := s3ByKey(t, result.Config, "primary")
	if primary.AccessKeyID != "imported-access" || primary.SecretAccessKey != "imported-secret" || primary.WebDAVPasswordHash != "imported-hash" {
		t.Fatalf("S3 credentials were not updated: %#v", primary)
	}
	if len(result.Warnings) != 2 || !strings.Contains(strings.Join(result.Warnings, " "), "missing") {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}
}

func TestApplyReplacesDDNSAndApplicationOnly(t *testing.T) {
	current := backupFixtureConfig()
	payload := validPayload(
		[]Section{SectionDDNS, SectionApplication}, nil, nil,
		&DDNSSection{
			Enabled: true, IntervalMins: 10, OnlyOnChange: false, MaxRetries: 9,
			IPSources: []IPSource{{URL: "https://new-ip.example", IPType: "ipv6"}},
			Records:   []DDNSRecord{{Name: "new.example", ZoneID: "zone-new", ZoneName: "example", Type: "AAAA", Value: "{IPV6}", Comment: "cfui", Proxied: true, TTL: 1}},
		}, nil,
		&ApplicationSection{MCPEnabled: false, OAuthClientID: "new-client", OAuthRelayCallbackURL: "https://new.example/callback"}, nil,
	)

	result, err := Apply(current, payload, []Section{SectionApplication, SectionDDNS})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Config.DDNS.IPSources) != 1 || result.Config.DDNS.IPSources[0].URL != "https://new-ip.example" || len(result.Config.DDNS.Records) != 1 || result.Config.DDNS.Records[0].Name != "new.example" {
		t.Fatalf("DDNS was not replaced: %#v", result.Config.DDNS)
	}
	if result.Config.OAuthClientID != "new-client" || result.Config.OAuthRelayCallbackURL != "https://new.example/callback" || result.Config.MCPEnabled {
		t.Fatalf("application settings were not replaced: %#v", result.Config)
	}
	if !reflect.DeepEqual(result.Config.Tunnels, current.Tunnels) || !reflect.DeepEqual(result.Config.S3WebDAV, current.S3WebDAV) {
		t.Fatal("unselected sections changed")
	}
}

func backupFixtureConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.ActiveTunnelKey = "one"
	cfg.Tunnels = []config.TunnelProfileConfig{
		{Key: "one", Name: "One", Token: "tunnel-secret-one", LocalEnabled: true, RemoteManagementEnabled: true, AccountID: "account-one", TunnelID: "tunnel-one", AutoStart: true, AutoRestart: true, SoftwareName: "cfui", Protocol: "http2", GracePeriod: "30s", Retries: 5, MetricsPort: 60123, LogLevel: "info", EdgeIPVersion: "auto"},
		{Key: "two", Name: "Two", Token: "tunnel-secret-two", LocalEnabled: true, RemoteManagementEnabled: false, AccountID: "account-two", TunnelID: "tunnel-two", AutoRestart: true, SoftwareName: "cfui", Protocol: "auto", GracePeriod: "30s", Retries: 5, MetricsPort: 60124, LogLevel: "info", EdgeIPVersion: "auto"},
	}
	cfg.TunnelManagement = config.TunnelManagementConfig{Enabled: true, AccountID: "account-one", TunnelID: "tunnel-one", APIToken: "api-token", APIEmail: "owner@example.com", APIKey: "global-key"}
	cfg.DDNS = config.DDNSConfig{Enabled: true, IntervalMins: 5, OnlyOnChange: true, MaxRetries: 3, IPSources: []config.IPSource{{URL: "https://ip.example", IPType: "ipv4"}}, Records: []config.DDNSRecord{{Name: "old.example", ZoneID: "zone-old", ZoneName: "example", Type: "A", Value: "{IPV4}", Comment: "cfui", TTL: 1}}}
	cfg.S3WebDAV = config.S3WebDAVConfig{
		Enabled: true, ActiveKey: "primary", WebDAVAccessMode: config.S3WebDAVAccessModeMain, DedicatedPort: 14334, DedicatedDomainMode: config.S3WebDAVDomainModeNone,
		Mounts: []config.S3WebDAVMountConfig{
			{Key: "primary", Name: "Primary", Enabled: true, WebDAVEnabled: true, WebDAVAuthEnabled: true, MountType: config.MountTypeS3, Provider: "generic_s3", Region: "auto", PathStyle: true, BucketName: "old", MountPath: "/webdav/old/", Jurisdiction: "default", AccessKeyID: "access-one", SecretAccessKey: "secret-one", WebDAVUsername: "user-one", WebDAVPasswordHash: "hash-one"},
			{Key: "secondary", Name: "Secondary", Enabled: true, WebDAVEnabled: true, MountType: config.MountTypeS3, Provider: "generic_s3", Region: "auto", PathStyle: true, BucketName: "secondary", MountPath: "/webdav/secondary/", Jurisdiction: "default", AccessKeyID: "access-two", SecretAccessKey: "secret-two"},
		},
	}
	cfg.MCPEnabled = true
	cfg.OAuthClientID = "old-client"
	cfg.OAuthRelayCallbackURL = "https://old.example/callback"
	return cfg
}

func validPayload(sections []Section, tunnels *TunnelSection, remote *RemoteManagementSection, ddns *DDNSSection, s3 *S3WebDAVSection, application *ApplicationSection, sensitive *SensitiveSection) Payload {
	return Payload{
		SchemaVersion:    PayloadVersion,
		CreatedAt:        time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		AppVersion:       "v1",
		Sections:         sections,
		Tunnels:          tunnels,
		RemoteManagement: remote,
		DDNS:             ddns,
		S3WebDAV:         s3,
		Application:      application,
		Sensitive:        sensitive,
	}
}

func tunnelByKey(t *testing.T, cfg config.Config, key string) config.TunnelProfileConfig {
	t.Helper()
	for _, tunnel := range cfg.Tunnels {
		if tunnel.Key == key {
			return tunnel
		}
	}
	t.Fatalf("tunnel %q not found", key)
	return config.TunnelProfileConfig{}
}

func s3ByKey(t *testing.T, cfg config.Config, key string) config.S3WebDAVMountConfig {
	t.Helper()
	for _, mount := range cfg.S3WebDAV.Mounts {
		if mount.Key == key {
			return mount
		}
	}
	t.Fatalf("S3 mount %q not found", key)
	return config.S3WebDAVMountConfig{}
}
