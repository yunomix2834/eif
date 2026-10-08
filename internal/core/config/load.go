package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func Load() (
	*Config,
	error,
) {
	port, err := readIntEnv(
		"EIF_PORT",
		6445,
	)
	if err != nil {
		return nil, err
	}

	maxQueryDays, err := readIntEnv(
		"EIF_HDDT_GDT_MAX_QUERY_DAYS",
		31,
	)
	if err != nil {
		return nil, err
	}

	maxExportDays, err := readIntEnv(
		"EIF_HDDT_GDT_MAX_EXPORT_DAYS",
		31,
	)
	if err != nil {
		return nil, err
	}

	rateLimitRetries, err := readIntEnv(
		"EIF_HDDT_GDT_RATE_LIMIT_RETRIES",
		4,
	)
	if err != nil {
		return nil, err
	}

	shouldOpenBrowser, err := readBoolEnv(
		"EIF_OPEN_BROWSER",
		shouldOpenBrowserByDefault(),
	)
	if err != nil {
		return nil, err
	}

	isUpdateEnabled, err := readBoolEnv(
		"EIF_UPDATE_ENABLED",
		true,
	)
	if err != nil {
		return nil, err
	}

	sessionStorePath := readStringEnv(
		"EIF_SESSION_STORE_PATH",
		getDefaultSessionStorePath(),
	)
	sessionEncryptionKey, err := loadSessionEncryptionKey(sessionStorePath)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Server: ServerConfig{
			Host: readStringEnv(
				"EIF_HOST",
				getDefaultServerHost(),
			),
			Port: port,
			StaticDir: readStringEnv(
				"EIF_STATIC_DIR",
				"./web/static",
			),
			ReadHeaderTimeout: readDurationEnv(
				"EIF_READ_HEADER_TIMEOUT",
				10*time.Second,
			),
			ShutdownTimeout: readDurationEnv(
				"EIF_SHUTDOWN_TIMEOUT",
				10*time.Second,
			),
			ShouldOpenBrowser: shouldOpenBrowser,
		},

		Logger: LoggerConfig{
			Level: readStringEnv(
				"EIF_LOG_LEVEL",
				"info",
			),
			Format: readStringEnv(
				"EIF_LOG_FORMAT",
				"json",
			),
		},

		CORS: CORSConfig{
			AllowedOrigins: readCSVEnv(
				"EIF_CORS_ALLOWED_ORIGINS",
				[]string{"http://localhost:3000", "http://127.0.0.1:3000"},
			),
		},

		HDDTGDT: HDDTGDTConfig{
			Endpoint: strings.TrimRight(
				readStringEnv(
					"EIF_HDDT_GDT_ENDPOINT",
					"https://hoadondientu.gdt.gov.vn",
				),
				"/",
			),
			Timeout: readDurationEnv(
				"EIF_HDDT_GDT_TIMEOUT",
				60*time.Second,
			),
			MaxQueryDays:  maxQueryDays,
			MaxExportDays: maxExportDays,
			MinRequestInterval: readDurationEnv(
				"EIF_HDDT_GDT_MIN_REQUEST_INTERVAL",
				500*time.Millisecond,
			),
			RateLimitRetries: rateLimitRetries,
			RateLimitBaseDelay: readDurationEnv(
				"EIF_HDDT_GDT_RATE_LIMIT_BASE_DELAY",
				time.Second,
			),
			QueryCacheTTL: readDurationEnv(
				"EIF_HDDT_GDT_QUERY_CACHE_TTL",
				5*time.Minute,
			),
			SessionSkew: readDurationEnv(
				"EIF_SESSION_EXPIRY_SKEW",
				30*time.Second,
			),
			SessionStorePath:     sessionStorePath,
			SessionEncryptionKey: sessionEncryptionKey,
		},

		Updater: UpdaterConfig{
			IsEnabled: isUpdateEnabled,
			Repository: readStringEnv(
				"EIF_UPDATE_REPOSITORY",
				"yunomix2834/eif-ci",
			),
			Timeout: readDurationEnv(
				"EIF_UPDATE_TIMEOUT",
				30*time.Second,
			),
		},
	}

	if err := cfg.HDDTGDT.Validate(); err != nil {
		return nil, err
	}
	if err := cfg.Updater.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadSessionEncryptionKey(sessionStorePath string) (
	[]byte,
	error,
) {
	value := strings.TrimSpace(os.Getenv("EIF_SESSION_ENCRYPTION_KEY"))
	if value != "" {
		return decodeBase64Bytes(
			"EIF_SESSION_ENCRYPTION_KEY",
			value,
			32,
		)
	}

	return loadOrCreatePlatformSessionKey(sessionStorePath)
}

func readStringEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

func readIntEnv(
	key string,
	fallback int,
) (
	int,
	error,
) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be an integer: %w",
			key,
			err,
		)
	}

	return n, nil
}

func readBoolEnv(
	key string,
	fallback bool,
) (
	bool,
	error,
) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	result, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf(
			"%s must be a boolean: %w",
			key,
			err,
		)
	}
	return result, nil
}

func readDurationEnv(
	key string,
	fallback time.Duration,
) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return d
}

func readCSVEnv(
	key string,
	fallback []string,
) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parts := strings.Split(
		value,
		",",
	)
	out := make(
		[]string,
		0,
		len(parts),
	)
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			out = append(
				out,
				item,
			)
		}
	}

	return out
}

func decodeBase64Bytes(
	key, value string,
	expectedBytes int,
) (
	[]byte,
	error,
) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf(
				"%s must be base64 encoded: %w",
				key,
				err,
			)
		}
	}

	if len(data) != expectedBytes {
		return nil, fmt.Errorf(
			"%s must decode to exactly %d bytes",
			key,
			expectedBytes,
		)
	}

	return data, nil
}
