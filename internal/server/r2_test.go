package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cfui/internal/r2dav"

	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/spf13/afero"
)

type serverFakeR2Client struct{}

func (serverFakeR2Client) VerifyAPIToken(context.Context) (cloudflare.APITokenVerifyBody, error) {
	return cloudflare.APITokenVerifyBody{ID: "token-id", Status: "active"}, nil
}

func (serverFakeR2Client) GetAPIToken(context.Context, string) (cloudflare.APIToken, error) {
	return cloudflare.APIToken{
		Policies: []cloudflare.APITokenPolicies{{
			Effect: "allow",
			PermissionGroups: []cloudflare.APITokenPermissionGroups{{
				Name: "Workers R2 Storage Write",
			}},
		}},
	}, nil
}

func (serverFakeR2Client) ListR2Buckets(context.Context, *cloudflare.ResourceContainer, cloudflare.ListR2BucketsParams) ([]cloudflare.R2Bucket, error) {
	return []cloudflare.R2Bucket{{Name: "bucket"}}, nil
}

func (serverFakeR2Client) CreateR2Bucket(context.Context, *cloudflare.ResourceContainer, cloudflare.CreateR2BucketParameters) (cloudflare.R2Bucket, error) {
	return cloudflare.R2Bucket{Name: "bucket"}, nil
}

func (serverFakeR2Client) GetR2Bucket(context.Context, *cloudflare.ResourceContainer, string) (cloudflare.R2Bucket, error) {
	return cloudflare.R2Bucket{Name: "bucket"}, nil
}

func TestR2FeatureEnableRequiresToken(t *testing.T) {
	s := newServerTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(`{"r2_webdav":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleFeatures(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestR2SettingsDoesNotLeakPasswordHash(t *testing.T) {
	s := newServerTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/r2/settings", strings.NewReader(`{
		"enabled": false,
		"account_id": "account",
		"bucket_name": "bucket",
		"jurisdiction": "default",
		"webdav_username": "dav",
		"webdav_password": "secret"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleR2Settings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "password_hash") {
		t.Fatalf("settings response leaked secret material: %s", rec.Body.String())
	}
	var resp r2dav.SettingsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if !resp.PasswordSet {
		t.Fatalf("expected password_set response: %#v", resp)
	}
}

func TestR2FileUploadAndListWithFakeFS(t *testing.T) {
	s := newServerTestServer(t)
	cfg := s.cfgMgr.Get()
	cfg.TunnelManagement.APIToken = "token"
	cfg.TunnelManagement.AccountID = "account"
	cfg.R2WebDAV.Enabled = true
	cfg.R2WebDAV.BucketName = "bucket"
	cfg.R2WebDAV.WebDAVUsername = "dav"
	cfg.R2WebDAV.WebDAVPasswordHash = "$2a$10$hash"
	if err := s.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	memFS := afero.NewMemMapFs()
	s.r2Svc = r2dav.NewServiceForTest(
		s.cfgMgr,
		func(string) (r2dav.CloudflareClient, error) {
			return serverFakeR2Client{}, nil
		},
		func(context.Context, string, string, r2dav.Credentials) (afero.Fs, error) {
			return memFS, nil
		},
	)

	uploadReq := httptest.NewRequest(http.MethodPut, "/api/r2/files/docs/readme.txt", bytes.NewBufferString("hello"))
	uploadRec := httptest.NewRecorder()
	s.handleR2FileObject(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload status %d: %s", uploadRec.Code, uploadRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/r2/files?path=/docs", nil)
	listRec := httptest.NewRecorder()
	s.handleR2Files(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", listRec.Code, listRec.Body.String())
	}
	if !strings.Contains(listRec.Body.String(), "readme.txt") {
		t.Fatalf("expected uploaded file in list response: %s", listRec.Body.String())
	}
}
