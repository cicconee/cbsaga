package logging

import (
	"log/slog"
	"os"
)

type Logger struct {
	*slog.Logger
}

func New(service string) *Logger {
	// TODO: Make handler and log level configurable.
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	base := slog.New(slog.NewJSONHandler(os.Stdout, opts)).With("service", service)
	return &Logger{Logger: base}
}

func (l *Logger) Info(msg string, kv ...any) {
	l.Logger.Info(msg, kv...)
}

func (l *Logger) Debug(msg string, kv ...any) {
	l.Logger.Debug(msg, kv...)
}

func (l *Logger) Warn(msg string, kv ...any) {
	l.Logger.Warn(msg, kv...)
}

func (l *Logger) Error(msg string, kv ...any) {
	l.Logger.Error(msg, kv...)
}
