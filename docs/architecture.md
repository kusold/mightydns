# MightyDNS Architecture

MightyDNS is a modern DNS server heavily inspired by Caddy's elegant modular architecture. Like Caddy, MightyDNS follows a philosophy of "fewer moving parts" while providing extensive extensibility through a plugin-based module system.

## Design Philosophy

- **Single binary with zero dependencies**: Self-contained Go binary for simple deployment
- **Module-driven architecture**: All functionality implemented as composable modules
- **Configuration as code**: JSON-based configuration with optional human-friendly config formats
- **Hot reloading**: Zero-downtime configuration changes
- **Minimal global state**: Each configuration is immutable and atomic

## Overview

MightyDNS consists of three main components:

1. **Command interface** - CLI for process management and bootstrapping
2. **Core library** - Configuration management and module lifecycle
3. **Modules** - All DNS functionality (resolvers, authoritative zones, middleware, etc.)

```
┌─────────────────┐
│   CLI Command   │ ← User interaction
├─────────────────┤
│   Core Library  │ ← Config management, module lifecycle
├─────────────────┤
│    Modules      │ ← DNS functionality, plugins
└─────────────────┘
```

## Core Architecture

### Configuration Structure

MightyDNS uses a JSON configuration document with top-level fields:

```json
{
  "admin": {},
  "logging": {},
  "apps": { }
}
```

The core knows how to handle:
- `admin` - Management API setup
- `logging` - Structured logging configuration

Everything else is handled by modules.

### Module System

#### Module Types

**Host Modules** load and manage other modules:
- DNS apps (authoritative, recursive, forwarding)
- Server instances
- Zone managers

**Guest Modules** provide specific functionality:
- DNS handlers (A, AAAA, MX, etc.)
- Middleware (rate limiting, caching, filtering)
- Backends (file, database, API)
- Resolvers (upstream, recursive, cache)

#### Module Lifecycle

1. **Load Phase**: Deserialize JSON into typed values
2. **Provision Phase**: Setup, validate, and initialize dependencies
3. **Use Phase**: Active DNS query processing
4. **Cleanup Phase**: Resource cleanup during config changes

#### Module Namespaces

TBD

## Configuration Management

### Atomic Updates

Like Caddy, MightyDNS treats configurations as immutable units:
- New config is provisioned alongside the old
- If successful, traffic switches atomically
- Old config is cleaned up
- Zero downtime for config changes

### Admin API

RESTful API for:
- Loading new configurations
- Querying current state
- Granular config updates
- Health checking
- Metrics collection

## Module Development

### Basic Module Template

```go
package mymodule

import "github.com/kusold/mightydns"

func init() {
    mightydns.RegisterModule(DNSHandler{})
}

type DNSHandler struct {
    Zone string `json:"zone,omitempty"`
}

func (DNSHandler) MightyModule() mightydns.ModuleInfo {
    return mightydns.ModuleInfo{
        ID:  "dns.handlers.my_handler",
        New: func() mightydns.Module { return new(DNSHandler) },
    }
}

func (h *DNSHandler) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
    // Handle DNS query
    return nil
}
```

### Module Interfaces

Different module types implement specific interfaces:

```go
// DNS query handlers
type DNSHandler interface {
    ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error
}

// Middleware
type DNSMiddleware interface {
    ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, next DNSHandler) error
}

// Zone data providers
type ZoneProvider interface {
    LookupRecord(ctx context.Context, qname string, qtype uint16) ([]dns.RR, error)
}
```

## Extension Points

### Custom Protocols
- Implement new transport protocols (QUIC, HTTP/3)
- Add authentication mechanisms
- Support encrypted DNS variants

### Data Sources
- Connect to any data store
- Implement dynamic zone generation
- Add real-time zone updates

### Processing Logic
- Custom record types
- Geographic routing
- Load balancing algorithms
- Security filtering

## Benefits of This Architecture

1. **Modularity**: Each piece of functionality is isolated and replaceable
2. **Testability**: Individual modules can be unit tested in isolation
3. **Performance**: Hot paths avoid unnecessary abstraction
4. **Extensibility**: Third-party modules without core modifications
5. **Maintainability**: Clear separation of concerns
6. **Reliability**: Atomic config updates prevent inconsistent states

This architecture enables MightyDNS to be both simple for basic use cases and infinitely extensible for complex DNS infrastructure needs, following Caddy's proven design principles.
