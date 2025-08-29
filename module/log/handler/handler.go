package handler

import (
	"io"
	"log/slog"
	"os"
)

type HandlerConfig struct {
	Output    string `json:"output,omitempty"`
	AddSource bool   `json:"add_source,omitempty"`
}

func (c *HandlerConfig) GetWriter() (io.Writer, error) {
	switch c.Output {
	case "stderr":
		return os.Stderr, nil
	case "stdout", "":
		return os.Stdout, nil
	default:
		return os.OpenFile(c.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	}
}

func (c *HandlerConfig) GetHandlerOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{
		AddSource: c.AddSource,
	}
}
