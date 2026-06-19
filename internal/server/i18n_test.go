package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandleI18nMergesLegacyAndSplitLocaleFiles(t *testing.T) {
	s := &Server{
		locales: fstest.MapFS{
			"locales/en.toml": {
				Data: []byte(`
[hello]
other = "Hello"

[oauth_title]
other = "Legacy OAuth"
`),
			},
			"locales/en/oauth.toml": {
				Data: []byte(`
[oauth_title]
other = "Split OAuth"
`),
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/i18n/en", nil)
	rec := httptest.NewRecorder()
	s.handleI18n(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["hello"] != "Hello" {
		t.Fatalf("legacy key not loaded: %#v", got)
	}
	if got["oauth_title"] != "Split OAuth" {
		t.Fatalf("split key did not override legacy key: %#v", got)
	}
}
