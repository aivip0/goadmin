package logger

import (
	"log/slog"
	"os"
)

func New(level string) *slog.Logger {
	var parsed slog.Level
	_ = parsed.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parsed}))
}
