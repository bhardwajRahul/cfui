package r2dav

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
