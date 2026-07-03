package s3dav

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var windowsDrivePath = regexp.MustCompile(`^[A-Za-z]:`)

func CleanPath(raw string, requireObject bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "/"
	}
	if strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("path contains an invalid character")
	}
	if strings.Contains(raw, "\\") || windowsDrivePath.MatchString(raw) {
		return "", fmt.Errorf("path must use slash-separated object keys")
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("path traversal is not allowed")
		}
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(raw, "/"))
	if cleaned == "." {
		cleaned = "/"
	}
	if requireObject && cleaned == "/" {
		return "", fmt.Errorf("object path is required")
	}
	return cleaned, nil
}

func ObjectKey(cleaned string) string {
	return strings.TrimPrefix(cleaned, "/")
}

func ParentPath(cleaned string) string {
	if cleaned == "/" {
		return ""
	}
	parent := path.Dir(cleaned)
	if parent == "." {
		return "/"
	}
	return parent
}

func JoinPath(base, name string) string {
	if base == "/" {
		return "/" + strings.TrimPrefix(name, "/")
	}
	return path.Join(base, name)
}

func NormalizeRootPrefix(raw string) (string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" || raw == "." {
		return "", nil
	}
	if strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("root prefix contains an invalid character")
	}
	if strings.Contains(raw, "\\") || windowsDrivePath.MatchString(raw) {
		return "", fmt.Errorf("root prefix must use slash-separated object keys")
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("root prefix cannot contain ..")
		}
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == "/" {
		return "", nil
	}
	return strings.TrimPrefix(cleaned, "/"), nil
}

func NormalizeMountPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultMountPath
	}
	if strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("WebDAV path contains an invalid character")
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("WebDAV path must use /")
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("WebDAV path cannot contain ..")
		}
	}
	cleaned := path.Clean(raw)
	if cleaned == "." {
		cleaned = "/"
	}
	if cleaned == "/" || cleaned == "/webdav" {
		return "", fmt.Errorf("WebDAV path must be under /webdav/, for example /webdav/s3/")
	}
	if !strings.HasPrefix(cleaned, "/webdav/") {
		return "", fmt.Errorf("WebDAV path must start with /webdav/")
	}
	return strings.TrimRight(cleaned, "/") + "/", nil
}

func normalizeProvider(v string) string {
	switch strings.TrimSpace(v) {
	case ProviderCloudflareR2:
		return ProviderCloudflareR2
	default:
		return ProviderGenericS3
	}
}

func normalizeMountType(v string) string {
	switch strings.TrimSpace(v) {
	case MountTypeWebDAVRemote:
		return MountTypeWebDAVRemote
	default:
		return MountTypeS3
	}
}

func normalizeRegion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "auto"
	}
	return v
}
