package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/rajsingh/tsdnsreflector/internal/config"
)

type Logger struct {
	*slog.Logger
}

func New(cfg config.LoggingConfig) *Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(cfg.Level),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if !strings.EqualFold(cfg.Format, "json") {
				if a.Key == slog.TimeKey {
					return slog.Attr{}
				}
				if a.Key == slog.LevelKey {
					return slog.Attr{}
				}
			}
			return a
		},
	}

	var handler slog.Handler
	var output io.Writer = os.Stdout

	if cfg.LogFile != "" {
		file, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Warn("Failed to open log file, using stdout", "error", err, "file", cfg.LogFile)
		} else {
			output = file
		}
	}
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(output, opts)
	default:
		handler = &tsnetStyleHandler{
			output: output,
			opts:   opts,
		}
	}

	return &Logger{
		Logger: slog.New(handler),
	}
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		Logger: l.WithGroup("context"),
	}
}

func Default() *Logger {
	return New(config.LoggingConfig{
		Level:  "info",
		Format: "text",
	})
}

func (l *Logger) UpdateConfig(cfg config.LoggingConfig) {
	newLogger := New(cfg)

	*l = *newLogger
}

// WithZone creates a logger with zone context
func (l *Logger) WithZone(zoneName string) *Logger {
	return &Logger{
		Logger: l.With("zone", zoneName),
	}
}

// ZoneInfo logs an info message with automatic zone context
func (l *Logger) ZoneInfo(zone, msg string, args ...any) {
	l.WithZone(zone).Info(msg, args...)
}

// ZoneError logs an error message with automatic zone context
func (l *Logger) ZoneError(zone, msg string, args ...any) {
	l.WithZone(zone).Error(msg, args...)
}

// ZoneDebug logs a debug message with automatic zone context
func (l *Logger) ZoneDebug(zone, msg string, args ...any) {
	l.WithZone(zone).Debug(msg, args...)
}

// ZoneWarn logs a warn message with automatic zone context
func (l *Logger) ZoneWarn(zone, msg string, args ...any) {
	l.WithZone(zone).Warn(msg, args...)
}

type tsnetStyleHandler struct {
	output io.Writer
	opts   *slog.HandlerOptions
}

func (h *tsnetStyleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

//nolint:gocritic // slog.Handler interface requires value type
func (h *tsnetStyleHandler) Handle(ctx context.Context, record slog.Record) error {
	timestamp := record.Time.Format("2006/01/02 15:04:05")

	var line strings.Builder
	line.WriteString(timestamp)
	line.WriteString(" tsdnsreflector: ")
	line.WriteString(record.Message)

	record.Attrs(func(a slog.Attr) bool {
		line.WriteString(" ")
		line.WriteString(a.Key)
		line.WriteString("=")
		line.WriteString(a.Value.String())
		return true
	})

	line.WriteString("\n")

	_, err := h.output.Write([]byte(line.String()))
	return err
}

func (h *tsnetStyleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *tsnetStyleHandler) WithGroup(name string) slog.Handler {
	return h
}
