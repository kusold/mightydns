package handler

import (
	"context"
	"io"
	"log/slog"

	"github.com/kusold/mightydns"
)

func init() {
	mightydns.RegisterModule(&TextHandler{})
}

type TextHandler struct {
	HandlerConfig
	slogHandler slog.Handler
	writer      io.Writer
}

func (TextHandler) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "logger.text",
		New: func() mightydns.Module { return new(TextHandler) },
	}
}

func (h *TextHandler) Provision(ctx mightydns.Context) error {
	writer, err := h.GetWriter()
	if err != nil {
		return err
	}
	h.writer = writer

	options := h.GetHandlerOptions()
	h.slogHandler = slog.NewTextHandler(writer, options)

	return nil
}

func (h *TextHandler) Cleanup() error {
	if closer, ok := h.writer.(io.Closer); ok && h.writer != nil {
		return closer.Close()
	}
	return nil
}

func (h *TextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.slogHandler.Enabled(ctx, level)
}

func (h *TextHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.slogHandler.Handle(ctx, r)
}

func (h *TextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TextHandler{
		HandlerConfig: h.HandlerConfig,
		slogHandler:   h.slogHandler.WithAttrs(attrs),
		writer:        h.writer,
	}
}

func (h *TextHandler) WithGroup(name string) slog.Handler {
	return &TextHandler{
		HandlerConfig: h.HandlerConfig,
		slogHandler:   h.slogHandler.WithGroup(name),
		writer:        h.writer,
	}
}
