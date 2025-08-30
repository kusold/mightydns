package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
)

func init() {
	mightydns.RegisterModule(&DNSApp{})
}

type DNSApp struct {
	Servers map[string]*DNSServer `json:"servers,omitempty"`

	ctx    mightydns.Context
	logger *slog.Logger
	mu     sync.RWMutex
}

func (app *DNSApp) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "dns",
		New: func() mightydns.Module { return new(DNSApp) },
	}
}

func (app *DNSApp) Provision(ctx mightydns.Context) error {
	app.ctx = ctx
	app.logger = ctx.Logger()

	if app.Servers == nil {
		app.Servers = make(map[string]*DNSServer)
	}

	for name, server := range app.Servers {
		if err := server.provision(ctx, app.logger.With("server", name)); err != nil {
			return fmt.Errorf("failed to provision server %s: %w", name, err)
		}
	}

	return nil
}

func (app *DNSApp) Start() error {
	app.mu.Lock()
	defer app.mu.Unlock()

	for name, server := range app.Servers {
		if err := server.start(); err != nil {
			app.logger.Error("failed to start DNS server", "server", name, "error", err)
			return fmt.Errorf("failed to start server %s: %w", name, err)
		}
		app.logger.Info("DNS server started", "server", name, "listeners", server.Listen, "protocols", server.Protocol)
	}

	return nil
}

func (app *DNSApp) Stop() error {
	app.mu.Lock()
	defer app.mu.Unlock()

	var errs []string
	for name, server := range app.Servers {
		if err := server.stop(); err != nil {
			app.logger.Error("failed to stop DNS server", "server", name, "error", err)
			errs = append(errs, fmt.Sprintf("server %s: %v", name, err))
		} else {
			app.logger.Info("DNS server stopped", "server", name)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to stop servers: %s", strings.Join(errs, "; "))
	}

	return nil
}

func (app *DNSApp) Cleanup() error {
	return app.Stop()
}

type DNSServer struct {
	Listen   []string        `json:"listen,omitempty"`
	Protocol []string        `json:"protocol,omitempty"`
	Handler  json.RawMessage `json:"handler,omitempty"`

	servers []*dns.Server
	handler mightydns.DNSHandler
	logger  *slog.Logger
	mu      sync.RWMutex
}

func (s *DNSServer) provision(ctx mightydns.Context, logger *slog.Logger) error {
	s.logger = logger

	// Set defaults
	if len(s.Listen) == 0 {
		s.Listen = []string{":53"}
	}
	if len(s.Protocol) == 0 {
		s.Protocol = []string{"udp", "tcp"}
	}

	// Provision handler if specified
	if len(s.Handler) > 0 {
		var handlerConfig map[string]interface{}
		if err := json.Unmarshal(s.Handler, &handlerConfig); err != nil {
			return fmt.Errorf("failed to unmarshal handler config: %w", err)
		}

		handlerType, exists := handlerConfig["handler"].(string)
		if !exists {
			return fmt.Errorf("handler config must specify a 'handler' field")
		}

		moduleInfo, exists := mightydns.GetModule(handlerType)
		if !exists {
			return fmt.Errorf("unknown handler module: %s", handlerType)
		}

		handlerModule := moduleInfo.New()
		if err := json.Unmarshal(s.Handler, handlerModule); err != nil {
			return fmt.Errorf("failed to unmarshal handler config: %w", err)
		}

		if provisioner, isProvisioner := handlerModule.(mightydns.Provisioner); isProvisioner {
			if err := provisioner.Provision(ctx); err != nil {
				return fmt.Errorf("failed to provision handler: %w", err)
			}
		}

		var isHandler bool
		s.handler, isHandler = handlerModule.(mightydns.DNSHandler)
		if !isHandler {
			return fmt.Errorf("handler module %s does not implement DNSHandler", handlerType)
		}
	}

	return nil
}

func (s *DNSServer) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.handler == nil {
		return fmt.Errorf("no handler configured")
	}

	// Create DNS servers for each listen address and protocol combination
	for _, addr := range s.Listen {
		for _, proto := range s.Protocol {
			server := &dns.Server{
				Addr:    addr,
				Net:     proto,
				Handler: s,
			}

			s.servers = append(s.servers, server)

			go func(srv *dns.Server) {
				s.logger.Info("starting DNS listener", "addr", srv.Addr, "protocol", srv.Net)
				if err := srv.ListenAndServe(); err != nil {
					s.logger.Error("DNS server error", "addr", srv.Addr, "protocol", srv.Net, "error", err)
				}
			}(server)
		}
	}

	return nil
}

func (s *DNSServer) stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []string
	for _, server := range s.servers {
		if err := server.Shutdown(); err != nil {
			errs = append(errs, fmt.Sprintf("%s/%s: %v", server.Addr, server.Net, err))
		}
	}

	s.servers = nil

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

// ServeDNS implements dns.Handler to route requests to the configured handler
func (s *DNSServer) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	s.mu.RLock()
	handler := s.handler
	s.mu.RUnlock()

	if handler == nil {
		s.logger.Error("no handler available for DNS request")
		m := new(dns.Msg)
		m.SetReply(r)
		m.SetRcode(r, dns.RcodeServerFailure)
		if err := w.WriteMsg(m); err != nil {
			s.logger.Error("failed to write DNS response", "error", err)
		}
		return
	}

	ctx := context.Background()
	if err := handler.ServeDNS(ctx, w, r); err != nil {
		s.logger.Error("handler error", "error", err, "question", r.Question)
		m := new(dns.Msg)
		m.SetReply(r)
		m.SetRcode(r, dns.RcodeServerFailure)
		if err := w.WriteMsg(m); err != nil {
			s.logger.Error("failed to write DNS response", "error", err)
		}
	}
}
