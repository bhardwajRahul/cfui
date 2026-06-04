package r2dav

import (
	"context"
	"net/http"
	"os"

	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, ok := s.WebDAVCredentials()
		if !ok {
			http.Error(w, "R2 WebDAV is not configured", http.StatusServiceUnavailable)
			return
		}
		if !basicAuthOK(r, cfg.WebDAVUsername, cfg.WebDAVPasswordHash) {
			w.Header().Set("WWW-Authenticate", `Basic realm="cfui R2 WebDAV"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		fs, err := s.Filesystem(r.Context())
		if err != nil {
			http.Error(w, "R2 WebDAV filesystem unavailable", http.StatusServiceUnavailable)
			return
		}
		handler := &webdav.Handler{
			Prefix:     EndpointPath,
			FileSystem: aferoWebDAVFS{fs: fs},
			LockSystem: webdav.NewMemLS(),
		}
		handler.ServeHTTP(w, r)
	})
}

type aferoWebDAVFS struct {
	fs afero.Fs
}

func (a aferoWebDAVFS) Mkdir(_ context.Context, name string, perm os.FileMode) error {
	cleaned, err := CleanPath(name, true)
	if err != nil {
		return err
	}
	return a.fs.MkdirAll(cleaned, perm)
}

func (a aferoWebDAVFS) OpenFile(_ context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	cleaned, err := CleanPath(name, false)
	if err != nil {
		return nil, err
	}
	return a.fs.OpenFile(cleaned, flag, perm)
}

func (a aferoWebDAVFS) RemoveAll(_ context.Context, name string) error {
	cleaned, err := CleanPath(name, true)
	if err != nil {
		return err
	}
	return a.fs.RemoveAll(cleaned)
}

func (a aferoWebDAVFS) Rename(_ context.Context, oldName, newName string) error {
	return renamePath(a.fs, oldName, newName)
}

func (a aferoWebDAVFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	cleaned, err := CleanPath(name, false)
	if err != nil {
		return nil, err
	}
	return a.fs.Stat(cleaned)
}
