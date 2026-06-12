package main

import (
	"os"
	"strings"
	"testing"
)

func TestAppInitStartsStatusPollingBeforeOptionalRemoteFeatures(t *testing.T) {
	js, err := os.ReadFile("web/dist/js/app-init.js")
	if err != nil {
		t.Fatalf("read app-init.js: %v", err)
	}
	src := string(js)

	statusIdx := strings.Index(src, "await fetchStatus();")
	intervalIdx := strings.Index(src, "setInterval(fetchStatus, 2000);")
	managerIdx := strings.Index(src, "if (state.features.tunnel_manager)")
	if statusIdx < 0 || intervalIdx < 0 || managerIdx < 0 {
		t.Fatalf("app-init.js is missing expected init markers")
	}
	if statusIdx > managerIdx || intervalIdx > managerIdx {
		t.Fatalf("tunnel status polling must start before optional Tunnel Manager remote initialization")
	}
}

func TestAppTunnelDoesNotFallbackProfileTokenToLegacyConfig(t *testing.T) {
	js, err := os.ReadFile("web/dist/js/app-tunnel.js")
	if err != nil {
		t.Fatalf("read app-tunnel.js: %v", err)
	}
	src := string(js)

	if strings.Contains(src, "profile.token || cfg.token") {
		t.Fatalf("new or empty tunnel profiles must not inherit the legacy top-level token in the form")
	}
	if !strings.Contains(src, "$('token-input').value = profile ? (profile.token || '') : (cfg?.token || '');") {
		t.Fatalf("app-tunnel.js is missing the profile-safe token form binding")
	}
}
