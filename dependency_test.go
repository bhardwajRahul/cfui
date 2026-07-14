package main

import (
	"os"
	"strings"
	"testing"
)

func TestCloudflaredModuleUsesMatchingQUICFork(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	contents := string(data)
	for _, required := range []string{
		"github.com/cloudflare/cloudflared v0.0.0-20260713102814-2601f87b5728",
		"replace github.com/quic-go/quic-go => github.com/chungthuang/quic-go v0.45.1-0.20250428085412-43229ad201fd",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("go.mod is missing cloudflared dependency alignment %q", required)
		}
	}
}
