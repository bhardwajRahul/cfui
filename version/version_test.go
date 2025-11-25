package version

import (
	"strings"
	"testing"
)

func TestGetVersion(t *testing.T) {
	version := GetVersion()
	if version == "" {
		t.Error("Version should not be empty")
	}
}

func TestGetFullVersion(t *testing.T) {
	fullVersion := GetFullVersion()
	if !strings.Contains(fullVersion, "cfui/") {
		t.Errorf("Full version should contain 'cfui/', got: %s", fullVersion)
	}
}

func TestGetShortVersion(t *testing.T) {
	shortVersion := GetShortVersion()
	if shortVersion == "" {
		t.Error("Short version should not be empty")
	}
}
