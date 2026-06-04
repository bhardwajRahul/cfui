package r2dav

import (
	"context"
	"strings"
	"testing"

	"cfui/internal/config"

	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/spf13/afero"
)

type fakeCloudflareClient struct {
	token       cloudflare.APIToken
	verifyErr   error
	getTokenErr error
	bucketErr   error
}

func (f fakeCloudflareClient) VerifyAPIToken(context.Context) (cloudflare.APITokenVerifyBody, error) {
	if f.verifyErr != nil {
		return cloudflare.APITokenVerifyBody{}, f.verifyErr
	}
	return cloudflare.APITokenVerifyBody{ID: "token-id", Status: "active"}, nil
}

func (f fakeCloudflareClient) GetAPIToken(context.Context, string) (cloudflare.APIToken, error) {
	if f.getTokenErr != nil {
		return cloudflare.APIToken{}, f.getTokenErr
	}
	return f.token, nil
}

func (f fakeCloudflareClient) ListR2Buckets(context.Context, *cloudflare.ResourceContainer, cloudflare.ListR2BucketsParams) ([]cloudflare.R2Bucket, error) {
	return []cloudflare.R2Bucket{{Name: "bucket"}}, nil
}

func (f fakeCloudflareClient) CreateR2Bucket(context.Context, *cloudflare.ResourceContainer, cloudflare.CreateR2BucketParameters) (cloudflare.R2Bucket, error) {
	return cloudflare.R2Bucket{Name: "bucket"}, nil
}

func (f fakeCloudflareClient) GetR2Bucket(context.Context, *cloudflare.ResourceContainer, string) (cloudflare.R2Bucket, error) {
	if f.bucketErr != nil {
		return cloudflare.R2Bucket{}, f.bucketErr
	}
	return cloudflare.R2Bucket{Name: "bucket"}, nil
}

func newTestService(t *testing.T, client CloudflareClient, fs afero.Fs) *Service {
	t.Helper()
	cfgMgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewServiceForTest(
		cfgMgr,
		func(string) (CloudflareClient, error) { return client, nil },
		func(context.Context, string, string, Credentials) (afero.Fs, error) { return fs, nil },
	)
}

func r2WriteToken() cloudflare.APIToken {
	return cloudflare.APIToken{
		Policies: []cloudflare.APITokenPolicies{{
			Effect: "allow",
			PermissionGroups: []cloudflare.APITokenPermissionGroups{{
				Name: permR2StorageWrite,
			}},
		}},
	}
}

func TestSaveSettingsHashesPasswordAndKeepsExistingHash(t *testing.T) {
	svc := newTestService(t, fakeCloudflareClient{token: r2WriteToken()}, afero.NewMemMapFs())

	resp, err := svc.SaveSettings(context.Background(), SettingsRequest{
		Enabled:        false,
		AccountID:      "account",
		BucketName:     "bucket",
		Jurisdiction:   "eu",
		WebDAVUsername: "dav",
		WebDAVPassword: "secret",
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if !resp.PasswordSet {
		t.Fatal("expected password_set response")
	}
	hash := svc.cfgMgr.Get().R2WebDAV.WebDAVPasswordHash
	if hash == "" || strings.Contains(hash, "secret") {
		t.Fatalf("password hash was not stored safely: %q", hash)
	}

	if _, err := svc.SaveSettings(context.Background(), SettingsRequest{
		Enabled:        false,
		AccountID:      "account",
		BucketName:     "bucket",
		Jurisdiction:   "eu",
		WebDAVUsername: "dav2",
	}); err != nil {
		t.Fatalf("SaveSettings keep password: %v", err)
	}
	if got := svc.cfgMgr.Get().R2WebDAV.WebDAVPasswordHash; got != hash {
		t.Fatalf("expected existing password hash to be preserved")
	}
}

func TestAvailabilityRequiresAPIToken(t *testing.T) {
	svc := newTestService(t, fakeCloudflareClient{}, afero.NewMemMapFs())
	cfg := svc.cfgMgr.Get()
	cfg.R2WebDAV.AccountID = "account"
	cfg.R2WebDAV.BucketName = "bucket"
	cfg.R2WebDAV.WebDAVUsername = "dav"
	cfg.R2WebDAV.WebDAVPasswordHash = "$2a$10$hash"
	if err := svc.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	availability := svc.Settings(context.Background()).Availability
	if availability.Status != StatusAPITokenRequired || availability.CanEnable {
		t.Fatalf("unexpected availability: %#v", availability)
	}
}

func TestFeatureAvailabilityAllowsTokenBeforeBucketAndCredentials(t *testing.T) {
	svc := newTestService(t, fakeCloudflareClient{token: r2WriteToken()}, afero.NewMemMapFs())
	cfg := svc.cfgMgr.Get()
	cfg.TunnelManagement.APIToken = "token"
	cfg.TunnelManagement.AccountID = "account"
	if err := svc.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	featureAvailability := svc.FeatureAvailability(context.Background(), cfg.R2WebDAV)
	if !featureAvailability.CanEnable || featureAvailability.Status != StatusReady {
		t.Fatalf("unexpected feature availability: %#v", featureAvailability)
	}

	settingsAvailability := svc.Settings(context.Background()).Availability
	if settingsAvailability.CanEnable || settingsAvailability.Status != StatusBucketRequired {
		t.Fatalf("unexpected settings availability: %#v", settingsAvailability)
	}
}

func TestSaveSettingsAllowsEnabledPartialConfig(t *testing.T) {
	svc := newTestService(t, fakeCloudflareClient{token: r2WriteToken()}, afero.NewMemMapFs())
	cfg := svc.cfgMgr.Get()
	cfg.TunnelManagement.APIToken = "token"
	cfg.TunnelManagement.AccountID = "account"
	if err := svc.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	resp, err := svc.SaveSettings(context.Background(), SettingsRequest{
		Enabled:   true,
		AccountID: "account",
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if !resp.Enabled || resp.Availability.Status != StatusBucketRequired {
		t.Fatalf("unexpected settings response: %#v", resp)
	}
}

func TestAvailabilityRejectsMissingR2WritePermission(t *testing.T) {
	svc := newTestService(t, fakeCloudflareClient{}, afero.NewMemMapFs())
	cfg := svc.cfgMgr.Get()
	cfg.TunnelManagement.APIToken = "token"
	cfg.TunnelManagement.AccountID = "account"
	cfg.R2WebDAV.BucketName = "bucket"
	cfg.R2WebDAV.WebDAVUsername = "dav"
	cfg.R2WebDAV.WebDAVPasswordHash = "$2a$10$hash"
	if err := svc.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	availability := svc.Settings(context.Background()).Availability
	if availability.Status != StatusR2PermissionDenied || availability.CanEnable {
		t.Fatalf("unexpected availability: %#v", availability)
	}
}

func TestListFilesUsesAferoFilesystem(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := fs.MkdirAll("/docs", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := afero.WriteFile(fs, "/docs/readme.txt", []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc := newTestService(t, fakeCloudflareClient{token: r2WriteToken()}, fs)
	cfg := svc.cfgMgr.Get()
	cfg.TunnelManagement.APIToken = "token"
	cfg.TunnelManagement.AccountID = "account"
	cfg.R2WebDAV.Enabled = true
	cfg.R2WebDAV.BucketName = "bucket"
	cfg.R2WebDAV.WebDAVUsername = "dav"
	cfg.R2WebDAV.WebDAVPasswordHash = "$2a$10$hash"
	if err := svc.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	resp, err := svc.ListFiles(context.Background(), "/docs")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].Name != "readme.txt" {
		t.Fatalf("unexpected entries: %#v", resp.Entries)
	}
}
