package r2dav

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/afero"
)

func TestWebDAVHandlerRequiresBasicAuth(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := fs.MkdirAll("/docs", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := afero.WriteFile(fs, "/docs/readme.txt", []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc := newTestService(t, fakeCloudflareClient{token: r2WriteToken()}, fs)
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	cfg := svc.cfgMgr.Get()
	cfg.TunnelManagement.APIToken = "token"
	cfg.TunnelManagement.AccountID = "account"
	cfg.R2WebDAV.Enabled = true
	cfg.R2WebDAV.BucketName = "bucket"
	cfg.R2WebDAV.WebDAVUsername = "dav"
	cfg.R2WebDAV.WebDAVPasswordHash = hash
	if err := svc.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	missingReq := httptest.NewRequest(http.MethodGet, EndpointPath+"docs/readme.txt", nil)
	missingRec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", missingRec.Code)
	}

	okReq := httptest.NewRequest(http.MethodGet, EndpointPath+"docs/readme.txt", nil)
	okReq.SetBasicAuth("dav", "secret")
	okRec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d: %s", okRec.Code, okRec.Body.String())
	}
	if okRec.Body.String() != "hello" {
		t.Fatalf("unexpected body %q", okRec.Body.String())
	}
}
