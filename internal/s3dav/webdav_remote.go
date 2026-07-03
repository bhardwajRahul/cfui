package s3dav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib-x/aferodav"
	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

type WebDAVFSFactory func(context.Context, WebDAVFSConfig, Credentials) (afero.Fs, error)

func newWebDAVRemoteFS(ctx context.Context, cfg WebDAVFSConfig, creds Credentials) (afero.Fs, error) {
	endpoint, err := normalizeWebDAVEndpointURL(cfg.EndpointURL)
	if err != nil {
		return nil, err
	}
	rootPrefix, err := NormalizeRootPrefix(cfg.RootPrefix)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	fs := &remoteWebDAVFS{
		baseURL:    parsed,
		rootPrefix: rootPrefix,
		username:   creds.AccessKeyID,
		password:   creds.SecretAccessKey,
		client:     newRemoteWebDAVHTTPClient(),
	}
	return aferodav.New(fs, ctx), nil
}

func normalizeWebDAVEndpointURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("remote WebDAV URL is required")
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("remote WebDAV URL must be a full http:// or https:// URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("remote WebDAV URL must use http:// or https://")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func newRemoteWebDAVHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}

type remoteWebDAVFS struct {
	baseURL    *url.URL
	rootPrefix string
	username   string
	password   string
	client     *http.Client
}

func (fs *remoteWebDAVFS) Mkdir(ctx context.Context, name string, _ os.FileMode) error {
	target, err := fs.urlFor(name)
	if err != nil {
		return err
	}
	req, err := fs.newRequest(ctx, "MKCOL", target, nil)
	if err != nil {
		return err
	}
	resp, err := fs.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed {
		info, statErr := fs.Stat(ctx, name)
		if statErr == nil && info.IsDir() {
			return nil
		}
		if statErr == nil {
			return pathError("mkdir", name, os.ErrExist)
		}
		return statusPathError("mkdir", name, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return statusPathError("mkdir", name, resp.StatusCode)
}

func (fs *remoteWebDAVFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		cleaned, err := CleanPath(name, true)
		if err != nil {
			return nil, err
		}
		return newRemoteWebDAVWriteFile(ctx, fs, cleaned, perm), nil
	}
	info, err := fs.Stat(ctx, name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return &remoteWebDAVDirFile{fs: fs, ctx: ctx, name: name, info: info}, nil
	}
	target, err := fs.urlFor(name)
	if err != nil {
		return nil, err
	}
	req, err := fs.newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := fs.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, pathError("open", name, os.ErrNotExist)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusPathError("open", name, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &remoteWebDAVReadFile{
		reader: bytes.NewReader(data),
		info:   info,
		name:   name,
	}, nil
}

func (fs *remoteWebDAVFS) RemoveAll(ctx context.Context, name string) error {
	cleaned, err := CleanPath(name, true)
	if err != nil {
		return err
	}
	target, err := fs.urlFor(cleaned)
	if err != nil {
		return err
	}
	req, err := fs.newRequest(ctx, http.MethodDelete, target, nil)
	if err != nil {
		return err
	}
	resp, err := fs.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return pathError("remove", name, os.ErrNotExist)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return statusPathError("remove", name, resp.StatusCode)
}

func (fs *remoteWebDAVFS) Rename(ctx context.Context, oldName, newName string) error {
	oldCleaned, err := CleanPath(oldName, true)
	if err != nil {
		return err
	}
	newCleaned, err := CleanPath(newName, true)
	if err != nil {
		return err
	}
	source, err := fs.urlFor(oldCleaned)
	if err != nil {
		return err
	}
	destination, err := fs.urlFor(newCleaned)
	if err != nil {
		return err
	}
	req, err := fs.newRequest(ctx, "MOVE", source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Destination", destination)
	req.Header.Set("Overwrite", "T")
	resp, err := fs.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return pathError("rename", oldName, os.ErrNotExist)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return statusPathError("rename", oldName, resp.StatusCode)
}

func (fs *remoteWebDAVFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	cleaned, err := CleanPath(name, false)
	if err != nil {
		return nil, err
	}
	responses, err := fs.propfind(ctx, cleaned, "0")
	if err != nil {
		return nil, err
	}
	if len(responses) == 0 {
		return nil, pathError("stat", name, os.ErrNotExist)
	}
	info, ok := fs.fileInfoFromResponse(cleaned, responses[0])
	if !ok {
		return nil, pathError("stat", name, os.ErrNotExist)
	}
	return info, nil
}

func (fs *remoteWebDAVFS) readDir(ctx context.Context, name string) ([]os.FileInfo, error) {
	cleaned, err := CleanPath(name, false)
	if err != nil {
		return nil, err
	}
	responses, err := fs.propfind(ctx, cleaned, "1")
	if err != nil {
		return nil, err
	}
	requestPath := fs.remotePath(cleaned)
	infos := make([]os.FileInfo, 0, len(responses))
	for _, resp := range responses {
		hrefPath := hrefURLPath(resp.Href)
		if sameRemotePath(hrefPath, requestPath) {
			continue
		}
		info, ok := fs.fileInfoFromResponse(cleaned, resp)
		if !ok || info.Name() == "" {
			continue
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (fs *remoteWebDAVFS) propfind(ctx context.Context, name, depth string) ([]remoteWebDAVResponse, error) {
	target, err := fs.urlFor(name)
	if err != nil {
		return nil, err
	}
	body := strings.NewReader(`<?xml version="1.0" encoding="utf-8"?><propfind xmlns="DAV:"><allprop/></propfind>`)
	req, err := fs.newRequest(ctx, "PROPFIND", target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)
	resp, err := fs.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, pathError("propfind", name, os.ErrNotExist)
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, statusPathError("propfind", name, resp.StatusCode)
	}
	var multi remoteWebDAVMultiStatus
	if err := xml.NewDecoder(resp.Body).Decode(&multi); err != nil {
		return nil, err
	}
	return multi.Responses, nil
}

func (fs *remoteWebDAVFS) fileInfoFromResponse(requestName string, resp remoteWebDAVResponse) (os.FileInfo, bool) {
	prop, ok := resp.okProp()
	if !ok {
		return nil, false
	}
	name := strings.TrimSpace(prop.DisplayName)
	if name == "" {
		name = responseName(resp.Href)
	}
	if name == "" || sameRemotePath(hrefURLPath(resp.Href), fs.remotePath(requestName)) {
		if requestName == "/" {
			name = "/"
		} else {
			name = path.Base(strings.TrimRight(requestName, "/"))
		}
	}
	size := int64(0)
	if strings.TrimSpace(prop.ContentLength) != "" {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(prop.ContentLength), 10, 64); err == nil {
			size = parsed
		}
	}
	modTime := time.Time{}
	if strings.TrimSpace(prop.GetLastModified) != "" {
		if parsed, err := http.ParseTime(strings.TrimSpace(prop.GetLastModified)); err == nil {
			modTime = parsed
		}
	}
	return remoteWebDAVFileInfo{
		name:    strings.Trim(name, "/"),
		dir:     prop.ResourceType.Collection != nil || strings.HasSuffix(resp.Href, "/"),
		size:    size,
		modTime: modTime,
	}, true
}

func (fs *remoteWebDAVFS) newRequest(ctx context.Context, method, target string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if fs.username != "" || fs.password != "" {
		req.SetBasicAuth(fs.username, fs.password)
	}
	return req, nil
}

func (fs *remoteWebDAVFS) urlFor(name string) (string, error) {
	cleaned, err := CleanPath(name, false)
	if err != nil {
		return "", err
	}
	u := *fs.baseURL
	u.Path = fs.remotePath(cleaned)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func (fs *remoteWebDAVFS) remotePath(name string) string {
	parts := make([]string, 0, 3)
	if base := strings.Trim(fs.baseURL.Path, "/"); base != "" {
		parts = append(parts, base)
	}
	if root := strings.Trim(fs.rootPrefix, "/"); root != "" {
		parts = append(parts, root)
	}
	if rel := strings.Trim(name, "/"); rel != "" {
		parts = append(parts, rel)
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + path.Join(parts...)
}

type remoteWebDAVMultiStatus struct {
	Responses []remoteWebDAVResponse `xml:"response"`
}

type remoteWebDAVResponse struct {
	Href      string                 `xml:"href"`
	Status    string                 `xml:"status"`
	PropStats []remoteWebDAVPropStat `xml:"propstat"`
}

type remoteWebDAVPropStat struct {
	Prop   remoteWebDAVProp `xml:"prop"`
	Status string           `xml:"status"`
}

type remoteWebDAVProp struct {
	DisplayName     string                   `xml:"displayname"`
	ContentLength   string                   `xml:"getcontentlength"`
	GetLastModified string                   `xml:"getlastmodified"`
	ResourceType    remoteWebDAVResourceType `xml:"resourcetype"`
}

type remoteWebDAVResourceType struct {
	Collection *struct{} `xml:"collection"`
}

func (r remoteWebDAVResponse) okProp() (remoteWebDAVProp, bool) {
	for _, propstat := range r.PropStats {
		if webDAVStatusOK(propstat.Status) {
			return propstat.Prop, true
		}
	}
	if webDAVStatusOK(r.Status) && len(r.PropStats) > 0 {
		return r.PropStats[0].Prop, true
	}
	return remoteWebDAVProp{}, false
}

func webDAVStatusOK(status string) bool {
	fields := strings.Fields(status)
	if len(fields) < 2 {
		return false
	}
	code, err := strconv.Atoi(fields[1])
	return err == nil && code >= 200 && code < 300
}

type remoteWebDAVReadFile struct {
	reader *bytes.Reader
	info   os.FileInfo
	name   string
	closed bool
}

func (f *remoteWebDAVReadFile) Close() error {
	f.closed = true
	return nil
}

func (f *remoteWebDAVReadFile) Read(p []byte) (int, error) {
	if f.closed {
		return 0, afero.ErrFileClosed
	}
	return f.reader.Read(p)
}

func (f *remoteWebDAVReadFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, afero.ErrFileClosed
	}
	return f.reader.Seek(offset, whence)
}

func (f *remoteWebDAVReadFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, pathError("readdir", f.name, errors.New("not a directory"))
}

func (f *remoteWebDAVReadFile) Stat() (os.FileInfo, error) { return f.info, nil }

func (f *remoteWebDAVReadFile) Write([]byte) (int, error) {
	return 0, pathError("write", f.name, os.ErrPermission)
}

type remoteWebDAVDirFile struct {
	fs      *remoteWebDAVFS
	ctx     context.Context
	name    string
	info    os.FileInfo
	entries []os.FileInfo
	offset  int
	loaded  bool
	closed  bool
}

func (f *remoteWebDAVDirFile) Close() error {
	f.closed = true
	return nil
}

func (f *remoteWebDAVDirFile) Read([]byte) (int, error) {
	return 0, pathError("read", f.name, errors.New("is a directory"))
}

func (f *remoteWebDAVDirFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, afero.ErrFileClosed
	}
	switch whence {
	case io.SeekStart:
		f.offset = int(offset)
	case io.SeekCurrent:
		f.offset += int(offset)
	case io.SeekEnd:
		if err := f.load(); err != nil {
			return 0, err
		}
		f.offset = len(f.entries) + int(offset)
	}
	if f.offset < 0 {
		f.offset = 0
	}
	return int64(f.offset), nil
}

func (f *remoteWebDAVDirFile) Readdir(count int) ([]os.FileInfo, error) {
	if f.closed {
		return nil, afero.ErrFileClosed
	}
	if err := f.load(); err != nil {
		return nil, err
	}
	if f.offset >= len(f.entries) {
		return nil, io.EOF
	}
	if count <= 0 {
		out := append([]os.FileInfo(nil), f.entries[f.offset:]...)
		f.offset = len(f.entries)
		return out, nil
	}
	end := f.offset + count
	if end > len(f.entries) {
		end = len(f.entries)
	}
	out := append([]os.FileInfo(nil), f.entries[f.offset:end]...)
	f.offset = end
	return out, nil
}

func (f *remoteWebDAVDirFile) Stat() (os.FileInfo, error) { return f.info, nil }

func (f *remoteWebDAVDirFile) Write([]byte) (int, error) {
	return 0, pathError("write", f.name, os.ErrPermission)
}

func (f *remoteWebDAVDirFile) load() error {
	if f.loaded {
		return nil
	}
	entries, err := f.fs.readDir(f.ctx, f.name)
	if err != nil {
		return err
	}
	f.entries = entries
	f.loaded = true
	return nil
}

type remoteWebDAVWriteFile struct {
	mu      sync.Mutex
	ctx     context.Context
	fs      *remoteWebDAVFS
	name    string
	perm    os.FileMode
	pipe    *io.PipeWriter
	done    chan error
	started bool
	closed  bool
	size    int64
}

func newRemoteWebDAVWriteFile(ctx context.Context, fs *remoteWebDAVFS, name string, perm os.FileMode) *remoteWebDAVWriteFile {
	return &remoteWebDAVWriteFile{ctx: ctx, fs: fs, name: name, perm: perm}
}

func (f *remoteWebDAVWriteFile) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return afero.ErrFileClosed
	}
	if err := f.startLocked(); err != nil {
		f.closed = true
		f.mu.Unlock()
		return err
	}
	pipe := f.pipe
	done := f.done
	f.closed = true
	f.mu.Unlock()

	if err := pipe.Close(); err != nil {
		return err
	}
	return <-done
}

func (f *remoteWebDAVWriteFile) Read([]byte) (int, error) {
	return 0, pathError("read", f.name, os.ErrPermission)
}

func (f *remoteWebDAVWriteFile) Seek(int64, int) (int64, error) {
	return 0, pathError("seek", f.name, os.ErrPermission)
}

func (f *remoteWebDAVWriteFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, pathError("readdir", f.name, errors.New("not a directory"))
}

func (f *remoteWebDAVWriteFile) Stat() (os.FileInfo, error) {
	return remoteWebDAVFileInfo{name: path.Base(f.name), size: f.size, mode: f.perm, modTime: time.Now()}, nil
}

func (f *remoteWebDAVWriteFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return 0, afero.ErrFileClosed
	}
	if err := f.startLocked(); err != nil {
		f.mu.Unlock()
		return 0, err
	}
	pipe := f.pipe
	f.mu.Unlock()

	n, err := pipe.Write(p)
	f.mu.Lock()
	f.size += int64(n)
	f.mu.Unlock()
	return n, err
}

func (f *remoteWebDAVWriteFile) startLocked() error {
	if f.started {
		return nil
	}
	target, err := f.fs.urlFor(f.name)
	if err != nil {
		return err
	}
	reader, writer := io.Pipe()
	req, err := f.fs.newRequest(f.ctx, http.MethodPut, target, reader)
	if err != nil {
		return err
	}
	f.pipe = writer
	f.done = make(chan error, 1)
	f.started = true
	go func() {
		resp, err := f.fs.client.Do(req)
		if err != nil {
			_ = reader.CloseWithError(err)
			f.done <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			f.done <- nil
			return
		}
		f.done <- statusPathError("put", f.name, resp.StatusCode)
	}()
	return nil
}

type remoteWebDAVFileInfo struct {
	name    string
	dir     bool
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (i remoteWebDAVFileInfo) Name() string {
	if i.name == "" {
		return "/"
	}
	return i.name
}
func (i remoteWebDAVFileInfo) Size() int64 { return i.size }
func (i remoteWebDAVFileInfo) Mode() os.FileMode {
	if i.dir {
		return os.ModeDir | 0755
	}
	if i.mode != 0 {
		return i.mode
	}
	return 0644
}
func (i remoteWebDAVFileInfo) ModTime() time.Time { return i.modTime }
func (i remoteWebDAVFileInfo) IsDir() bool        { return i.dir }
func (i remoteWebDAVFileInfo) Sys() any           { return nil }

func hrefURLPath(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err == nil && u.Path != "" {
		return u.Path
	}
	return strings.TrimSpace(href)
}

func responseName(href string) string {
	p := strings.TrimRight(hrefURLPath(href), "/")
	if p == "" || p == "." {
		return ""
	}
	name, err := url.PathUnescape(path.Base(p))
	if err != nil {
		return path.Base(p)
	}
	return name
}

func sameRemotePath(a, b string) bool {
	clean := func(v string) string {
		if v == "" {
			return "/"
		}
		v = path.Clean("/" + strings.Trim(v, "/"))
		if v == "." {
			return "/"
		}
		return strings.TrimRight(v, "/")
	}
	return clean(a) == clean(b)
}

func pathError(op, name string, err error) error {
	return &os.PathError{Op: op, Path: name, Err: err}
}

func statusPathError(op, name string, code int) error {
	if code == http.StatusNotFound {
		return pathError(op, name, os.ErrNotExist)
	}
	return pathError(op, name, fmt.Errorf("remote WebDAV returned HTTP %d", code))
}
