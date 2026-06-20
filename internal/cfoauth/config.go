package cfoauth

import (
	"os"
	"strings"
)

const (
	defaultAuthorizationURL = "https://dash.cloudflare.com/oauth2/auth"
	defaultLogoutURL        = "https://dash.cloudflare.com/logout"
	defaultTokenURL         = "https://dash.cloudflare.com/oauth2/token"
	defaultRevokeURL        = "https://dash.cloudflare.com/oauth2/revoke"
	defaultUserInfoURL      = "https://dash.cloudflare.com/oauth2/userinfo"
	defaultRelayCallbackURL = "https://cfoauth.pushcat.eu.org/oauth/callback"
)

type Config struct {
	ClientID          string `json:"client_id"`
	ClientIDSource    string `json:"client_id_source,omitempty"`
	RelayCallbackURL  string `json:"relay_callback_url"`
	LocalCallbackPath string `json:"local_callback_path"`
	AuthorizationURL  string `json:"authorization_url"`
	LogoutURL         string `json:"logout_url"`
	TokenURL          string `json:"token_url"`
	RevokeURL         string `json:"revoke_url"`
	UserInfoURL       string `json:"userinfo_url"`
	Scopes            string `json:"scopes"`
	Configured        bool   `json:"configured"`
}

func ConfigFromEnv() Config {
	clientID := strings.TrimSpace(os.Getenv("CFUI_OAUTH_CLIENT_ID"))
	cfg := Config{
		ClientID:          clientID,
		ClientIDSource:    clientIDSource(clientID),
		RelayCallbackURL:  normalizeRelayCallbackURL(firstEnv("CFUI_OAUTH_RELAY_URL", "CFUI_OAUTH_REDIRECT_URI", defaultRelayCallbackURL)),
		LocalCallbackPath: "/oauth/callback",
		AuthorizationURL:  firstEnv("CFUI_OAUTH_AUTH_URL", defaultAuthorizationURL),
		LogoutURL:         firstEnv("CFUI_OAUTH_LOGOUT_URL", defaultLogoutURL),
		TokenURL:          firstEnv("CFUI_OAUTH_TOKEN_URL", defaultTokenURL),
		RevokeURL:         firstEnv("CFUI_OAUTH_REVOKE_URL", defaultRevokeURL),
		UserInfoURL:       firstEnv("CFUI_OAUTH_USERINFO_URL", defaultUserInfoURL),
		Scopes:            firstEnv("CFUI_OAUTH_SCOPES", DefaultScopes()),
	}
	cfg.Configured = cfg.ClientID != "" && cfg.RelayCallbackURL != ""
	return cfg
}

func clientIDSource(clientID string) string {
	if strings.TrimSpace(clientID) == "" {
		return "unset"
	}
	return "env"
}

func NormalizeClientID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > 256 {
		return "", errInvalidClientID()
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return "", errInvalidClientID()
		}
	}
	return value, nil
}

func errInvalidClientID() error {
	return &ConfigError{Message: "oauth client id must be a non-space value up to 256 bytes"}
}

type ConfigError struct {
	Message string
}

func (e *ConfigError) Error() string {
	return e.Message
}

func firstEnv(keysAndDefault ...string) string {
	if len(keysAndDefault) == 0 {
		return ""
	}
	defaultValue := keysAndDefault[len(keysAndDefault)-1]
	for _, key := range keysAndDefault[:len(keysAndDefault)-1] {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return defaultValue
}
