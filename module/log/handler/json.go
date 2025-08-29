package handler

import (
	"context"
	"io"
	"log/slog"

	"github.com/kusold/mightydns"
)

func init() {
	mightydns.RegisterModule(&JSONHandler{})
}

type JSONHandler struct {
	HandlerConfig
	slogHandler slog.Handler
	writer      io.Writer
}

func (JSONHandler) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "logger.json",
		New: func() mightydns.Module { return new(JSONHandler) },
	}
}

func (h *JSONHandler) Provision(ctx mightydns.Context) error {
	writer, err := h.GetWriter()
	if err != nil {
		return err
	}
	h.writer = writer

	options := h.GetHandlerOptions()
	h.slogHandler = slog.NewJSONHandler(writer, options)

	return nil
}

func (h *JSONHandler) Cleanup() error {
	if closer, ok := h.writer.(io.Closer); ok && h.writer != nil {
		return closer.Close()
	}
	return nil
}

func (h *JSONHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.slogHandler.Enabled(ctx, level)
}

func (h *JSONHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.slogHandler.Handle(ctx, r)
}

func (h *JSONHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &JSONHandler{
		HandlerConfig: h.HandlerConfig,
		slogHandler:   h.slogHandler.WithAttrs(attrs),
		writer:        h.writer,
	}
}

func (h *JSONHandler) WithGroup(name string) slog.Handler {
	return &JSONHandler{
		HandlerConfig: h.HandlerConfig,
		slogHandler:   h.slogHandler.WithGroup(name),
		writer:        h.writer,
	}
}
