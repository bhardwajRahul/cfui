package s3dav

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mount, ok := s.WebDAVMountForPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		mountPath := mount.MountPath
		if !strings.HasPrefix(r.URL.Path, mountPath) {
			http.NotFound(w, r)
			return
		}
		if !basicAuthOK(r, mount.WebDAVUsername, mount.WebDAVPasswordHash) {
			w.Header().Set("WWW-Authenticate", `Basic realm="cfui S3 WebDAV"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		fs, err := s.Filesystem(r.Context(), mount.Key)
		if err != nil {
			http.Error(w, "S3 WebDAV filesystem unavailable", http.StatusServiceUnavailable)
			return
		}
		handler := &webdav.Handler{
			Prefix:     mountPath,
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
