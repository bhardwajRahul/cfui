package server

import (
	"cfui/internal/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeaturesTogglePreservesDDNSRecords(t *testing.T) {
	s := newServerTestServer(t)
	cfg := s.cfgMgr.Get()
	cfg.TunnelManagement.Enabled = true
	cfg.DDNS.Enabled = true
	cfg.DDNS.Records = []config.DDNSRecord{{
		Name: "home.example.com", ZoneID: "zone-1", ZoneName: "example.com",
		Type: "A", Value: "{IPV4}", Comment: "keep me", TTL: 1,
	}}
	if err := s.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(`{"ddns":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleFeatures(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("features status %d: %s", rec.Code, rec.Body.String())
	}

	got := s.cfgMgr.Get().DDNS
	if got.Enabled {
		t.Fatal("expected DDNS to be disabled")
	}
	if len(got.Records) != 1 || got.Records[0].Comment != "keep me" {
		t.Fatalf("DDNS records were not preserved: %#v", got.Records)
	}
}

func TestConfigPostMergesOmittedFeatureConfig(t *testing.T) {
	s := newServerTestServer(t)
	cfg := s.cfgMgr.Get()
	cfg.MCPEnabled = true
	cfg.DDNS.Enabled = true
	cfg.DDNS.Records = []config.DDNSRecord{{
		Name: "home.example.com", ZoneID: "zone-1", ZoneName: "example.com",
		Type: "A", Value: "{IPV4}", Comment: "preserved", TTL: 1,
	}}
	if err := s.cfgMgr.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"token":"new-token","auto_restart":true,"software_name":"cfui"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("config status %d: %s", rec.Code, rec.Body.String())
	}

	var resp config.Config
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token != "new-token" || !resp.MCPEnabled || len(resp.DDNS.Records) != 1 || resp.DDNS.Records[0].Comment != "preserved" {
		t.Fatalf("config post did not merge omitted fields: %#v", resp)
	}
}
