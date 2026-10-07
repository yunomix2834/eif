//go:build !windows

package config

import "fmt"

func getDefaultServerHost() string {
	return "127.0.0.1"
}

func shouldOpenBrowserByDefault() bool {
	return false
}

func getDefaultSessionStorePath() string {
	return "./runtime/hddtgdt-sessions.enc"
}

func loadOrCreatePlatformSessionKey(_ string) (
	[]byte,
	error,
) {
	return nil, fmt.Errorf(
		"EIF_SESSION_ENCRYPTION_KEY is required on non-Windows systems; use a secret manager or container secret",
	)
}
