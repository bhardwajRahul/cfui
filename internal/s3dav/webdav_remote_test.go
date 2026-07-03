package s3dav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

func TestRemoteWebDAVFSUsesAuthRootPrefixAndAferoOperations(t *testing.T) {
	ctx := context.Background()
	backend := webdav.NewMemFS()
	if err := backend.Mkdir(ctx, "/root", 0755); err != nil {
		t.Fatalf("Mkdir root: %v", err)
	}
	handler := &webdav.Handler{FileSystem: backend, LockSystem: webdav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "dav-user" || pass != "dav-pass" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	fs, err := newWebDAVRemoteFS(ctx, WebDAVFSConfig{
		EndpointURL: server.URL,
		RootPrefix:  "root",
	}, Credentials{
		AccessKeyID:     "dav-user",
		SecretAccessKey: "dav-pass",
	})
	if err != nil {
		t.Fatalf("newWebDAVRemoteFS: %v", err)
	}
	if err := writeFile(fs, "/docs/readme.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	files, err := listFiles(fs, "/docs")
	if err != nil {
		t.Fatalf("listFiles: %v", err)
	}
	if len(files.Entries) != 1 || files.Entries[0].Name != "readme.txt" {
		t.Fatalf("unexpected entries: %#v", files.Entries)
	}
	got, err := afero.ReadFile(fs, "/docs/readme.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected content %q", string(got))
	}
}
