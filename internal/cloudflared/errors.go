package cloudflared

import "strings"

// Error classification drives the auto-restart and protocol-fallback
// decisions. Matching is substring-based because the embedded library
// returns plain wrapped errors without sentinel types.

var (
	// QUIC-specific error patterns.
	quicErrorPatterns = []string{
		"quic",
		"timeout: no recent network activity",
		"failed to dial to edge with quic",
		"failed to accept quic stream",
	}

	// General connection error patterns that might be protocol-related.
	connectionErrorPatterns = []string{
		"connection refused",
		"connection reset",
		"connection timeout",
	}

	// Network errors that are retryable.
	retryableErrorPatterns = []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"no route to host",
		"broken pipe",
		"i/o timeout",
	}

	// Configuration/authentication errors that are not retryable.
	nonRetryableErrorPatterns = []string{
		"invalid token",
		"token is not valid", // cloudflared: "Provided Tunnel token is not valid."
		"invalid tunnel secret",
		"authentication failed",
		"unauthorized",
		"forbidden",
		"bad request",
		"invalid configuration",
		"missing required",
	}
)

// IsProtocolRelatedError reports whether an error looks like a transport
// problem worth counting against the current protocol in auto mode.
func IsProtocolRelatedError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	for _, pattern := range quicErrorPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}
	for _, pattern := range connectionErrorPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}
	return false
}

// IsRetryableError reports whether an error should trigger auto-restart.
// Network errors are retryable; configuration and authentication errors are
// not. Unknown errors default to retryable so transient edge problems
// recover without operator intervention.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	for _, pattern := range retryableErrorPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}
	for _, pattern := range nonRetryableErrorPatterns {
		if strings.Contains(errMsg, pattern) {
			return false
		}
	}
	return true
}
