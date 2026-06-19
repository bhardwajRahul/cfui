package cfoauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	relayStatePrefix       = "cfui1."
	defaultLocalCallback   = "http://127.0.0.1:14333/oauth/callback"
	defaultCallbackPath    = "/oauth/callback"
	maxRelayStateURLLength = 2048
)

type relayStatePayload struct {
	State       string `json:"s"`
	CallbackURL string `json:"u"`
}

func encodeRelayState(state, callbackURL, callbackPath string) (string, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return "", fmt.Errorf("oauth state is required")
	}
	normalized, err := normalizeLocalCallbackURL(callbackURL, callbackPath)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(relayStatePayload{State: state, CallbackURL: normalized})
	if err != nil {
		return "", err
	}
	return relayStatePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func RelayStateCallbackURL(state string) (string, bool) {
	payload, ok := decodeRelayState(state)
	if !ok || strings.TrimSpace(payload.CallbackURL) == "" {
		return "", false
	}
	return payload.CallbackURL, true
}

func decodeRelayState(state string) (relayStatePayload, bool) {
	raw := strings.TrimSpace(state)
	if !strings.HasPrefix(raw, relayStatePrefix) {
		return relayStatePayload{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, relayStatePrefix))
	if err != nil {
		return relayStatePayload{}, false
	}
	var payload relayStatePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return relayStatePayload{}, false
	}
	payload.State = strings.TrimSpace(payload.State)
	payload.CallbackURL = strings.TrimSpace(payload.CallbackURL)
	return payload, payload.State != "" && payload.CallbackURL != ""
}

func normalizeLocalCallbackURL(raw, callbackPath string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultLocalCallback
	}
	if len(raw) > maxRelayStateURLLength {
		return "", fmt.Errorf("cfui callback URL is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid cfui callback URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("cfui callback URL must use http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("cfui callback URL must include a host")
	}
	if u.User != nil {
		return "", fmt.Errorf("cfui callback URL must not include credentials")
	}
	expectedPath := strings.TrimSpace(callbackPath)
	if expectedPath == "" {
		expectedPath = defaultCallbackPath
	}
	if !strings.HasPrefix(expectedPath, "/") {
		expectedPath = "/" + expectedPath
	}
	if u.Path != expectedPath {
		return "", fmt.Errorf("cfui callback URL path must be %s", expectedPath)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func normalizeRelayCallbackURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
