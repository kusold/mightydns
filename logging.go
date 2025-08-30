package mightydns

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

var defaultLogger *slog.Logger

type LogHandler interface {
	slog.Handler
	Module
}

func SetupLogging(config *LoggingConfig) error {
	if config == nil {
		// Default to console with INFO level
		config = &LoggingConfig{
			Level:   "INFO",
			Handler: "logger.console",
		}
	}

	level := parseLevel(config.Level)

	var handler slog.Handler
	if config.Handler == "" {
		config.Handler = "logger.console"
	}

	moduleInfo, exists := GetModule(config.Handler)
	if !exists {
		return fmt.Errorf("unknown logging handler: %s", config.Handler)
	}

	module := moduleInfo.New()
	logHandler, ok := module.(LogHandler)
	if !ok {
		return fmt.Errorf("module %s does not implement LogHandler interface", config.Handler)
	}

	// Create a basic context for provisioning
	ctx := &basicContext{}
	if provisioner, ok := logHandler.(Provisioner); ok {
		if err := provisioner.Provision(ctx); err != nil {
			return fmt.Errorf("failed to provision logging handler: %w", err)
		}
	}

	// Wrap with level filtering
	handler = &levelHandler{
		handler: logHandler,
		level:   level,
	}

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)

	return nil
}

func Logger() *slog.Logger {
	if defaultLogger == nil {
		// Fallback to default slog if not initialized
		return slog.Default()
	}
	return defaultLogger
}

func parseLevel(levelStr string) slog.Level {
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type levelHandler struct {
	handler slog.Handler
	level   slog.Level
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{
		handler: h.handler.WithAttrs(attrs),
		level:   h.level,
	}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{
		handler: h.handler.WithGroup(name),
		level:   h.level,
	}
}

type basicContext struct{}

func (c *basicContext) App(name string) (interface{}, error) {
	return nil, fmt.Errorf("no app available during logging setup")
}

func (c *basicContext) Logger() *slog.Logger {
	return slog.Default()
}
