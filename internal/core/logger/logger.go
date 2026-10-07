package logger

import (
	"log/slog"
	"os"
	"strings"
)

func Initialize(levelName, format string) *slog.Logger {
	level := parseLevel(levelName)
	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.EqualFold(
		format,
		"text",
	) {
		handler = slog.NewTextHandler(
			os.Stdout,
			opts,
		)
	} else {
		handler = slog.NewJSONHandler(
			os.Stdout,
			opts,
		)
	}

	log := slog.New(handler)
	slog.SetDefault(log)

	return log
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
