package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode"
)

type Config struct {
	Server  ServerConfig
	Logger  LoggerConfig
	CORS    CORSConfig
	HDDTGDT HDDTGDTConfig
	Updater UpdaterConfig
}

type ServerConfig struct {
	Host              string
	Port              int
	StaticDir         string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	ShouldOpenBrowser bool
}

func (c ServerConfig) BuildListenAddress() string {
	return net.JoinHostPort(
		c.Host,
		fmt.Sprintf(
			"%d",
			c.Port,
		),
	)
}

func (c ServerConfig) BuildBrowserURL() string {
	host := c.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(
		host,
		fmt.Sprintf(
			"%d",
			c.Port,
		),
	)
}

type LoggerConfig struct {
	Level  string
	Format string
}

type CORSConfig struct {
	AllowedOrigins []string
}

type UpdaterConfig struct {
	IsEnabled  bool
	Repository string
	Timeout    time.Duration
}

type HDDTGDTConfig struct {
	Endpoint             string
	Timeout              time.Duration
	MaxQueryDays         int
	MaxExportDays        int
	MinRequestInterval   time.Duration
	RateLimitRetries     int
	RateLimitBaseDelay   time.Duration
	QueryCacheTTL        time.Duration
	SessionSkew          time.Duration
	SessionStorePath     string
	SessionEncryptionKey []byte
}

func (c HDDTGDTConfig) Validate() error {
	u, err := url.Parse(c.Endpoint)

	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid EIF_HDDT_GDT_ENDPOINT")
	}

	if c.MaxQueryDays <= 0 {
		return fmt.Errorf("EIF_HDDT_GDT_MAX_QUERY_DAYS must be > 0")
	}

	if c.MaxExportDays <= 0 {
		return fmt.Errorf("EIF_HDDT_GDT_MAX_EXPORT_DAYS must be > 0")
	}

	if c.MinRequestInterval < 0 {
		return fmt.Errorf("EIF_HDDT_GDT_MIN_REQUEST_INTERVAL must be >= 0")
	}

	if c.RateLimitRetries < 0 {
		return fmt.Errorf("EIF_HDDT_GDT_RATE_LIMIT_RETRIES must be >= 0")
	}

	if c.RateLimitBaseDelay < 0 {
		return fmt.Errorf("EIF_HDDT_GDT_RATE_LIMIT_BASE_DELAY must be >= 0")
	}

	if c.QueryCacheTTL < 0 {
		return fmt.Errorf("EIF_HDDT_GDT_QUERY_CACHE_TTL must be >= 0")
	}

	if c.SessionStorePath == "" {
		return fmt.Errorf("EIF_SESSION_STORE_PATH is required")
	}

	// AES-256 yêu cầu key đúng 32 bytes. Trên Windows key có thể được tự tạo
	// và bảo vệ bằng DPAPI; môi trường server vẫn có thể override bằng env.
	if len(c.SessionEncryptionKey) != 32 {
		return fmt.Errorf("session encryption key must be exactly 32 bytes")
	}

	return nil
}

func (c UpdaterConfig) Validate() error {
	if !c.IsEnabled {
		return nil
	}

	parts := strings.Split(c.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("EIF_UPDATE_REPOSITORY must look like owner/repository")
	}
	for _, part := range parts {
		for _, char := range part {
			if !unicode.IsLetter(char) && !unicode.IsDigit(char) &&
				char != '-' && char != '_' && char != '.' {
				return fmt.Errorf("EIF_UPDATE_REPOSITORY contains invalid characters")
			}
		}
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("EIF_UPDATE_TIMEOUT must be > 0")
	}

	return nil
}
