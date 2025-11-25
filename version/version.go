package version

import "fmt"

var (
	// Version is the application version, injected at build time via ldflags
	Version = "dev"

	// BuildTime is the build timestamp, injected at build time via ldflags
	BuildTime = "unknown"

	// GitCommit is the git commit hash, injected at build time via ldflags
	GitCommit = "unknown"
)

// GetVersion returns the full version string
func GetVersion() string {
	if Version == "dev" {
		return "dev"
	}
	return Version
}

// GetFullVersion returns the version with build info
func GetFullVersion() string {
	if Version == "dev" {
		return fmt.Sprintf("cfui/%s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
	}
	return fmt.Sprintf("cfui/%s", Version)
}

// GetShortVersion returns just the version number for cloudflared display
func GetShortVersion() string {
	return Version
}
