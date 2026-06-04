package s3dav

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
	svc := newTestService(t, fakeCloudflareClient{}, fs)
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	cfg := svc.cfgMgr.Get()
	cfg.S3WebDAV.Enabled = true
	cfg.S3WebDAV.Mounts[0].EndpointURL = "https://s3.example.com"
	cfg.S3WebDAV.Mounts[0].BucketName = "bucket"
	cfg.S3WebDAV.Mounts[0].MountPath = "/webdav/my_r2/"
	cfg.S3WebDAV.Mounts[0].AccessKeyID = "ak"
	cfg.S3WebDAV.Mounts[0].SecretAccessKey = "sk"
	cfg.S3WebDAV.Mounts[0].WebDAVUsername = "dav"
	cfg.S3WebDAV.Mounts[0].WebDAVPasswordHash = hash
	if err := svc.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	wrongPathReq := httptest.NewRequest(http.MethodGet, "/webdav/s3/docs/readme.txt", nil)
	wrongPathRec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(wrongPathRec, wrongPathReq)
	if wrongPathRec.Code != http.StatusNotFound {
		t.Fatalf("expected not found, got %d", wrongPathRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/webdav/my_r2/docs/readme.txt", nil)
	missingRec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", missingRec.Code)
	}

	okReq := httptest.NewRequest(http.MethodGet, "/webdav/my_r2/docs/readme.txt", nil)
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
