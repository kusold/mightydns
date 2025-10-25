# MightyDNS

A modular DNS server written in Go, inspired by Caddy's architecture.

## Features

- **Modular Architecture**: Plugin-based system for extensible functionality
- **DNS Zones**: Forward zones with local records and upstream fallback
- **Policy Engine**: Client-based routing and configuration overrides
- **Split-Horizon DNS**: Different DNS views for different client groups
- **Hot Reloading**: Zero-downtime configuration changes
- **JSON Configuration**: Human-readable configuration with validation

## Quick Start

### Basic DNS Server

```bash
# Run with default configuration (upstream resolver)
./mightydns

# Run with custom configuration
./mightydns -config config.json
```

### Example Configurations

**Simple Upstream Resolver:**
```json
{
  "apps": {
    "dns": {
      "servers": {
        "main": {
          "listen": [":53"],
          "handler": {
            "handler": "dns.resolver.upstream",
            "upstreams": ["8.8.8.8:53", "1.1.1.1:53"]
          }
        }
      }
    }
  }
}
```

**Forward Zone with Local Records:**
```json
{
  "apps": {
    "dns": {
      "servers": {
        "main": {
          "listen": [":53"],
          "handler": {
            "handler": "dns.zone.manager",
            "zones": [
              {
                "type": "forward",
                "zone": "local.example.com.",
                "records": {
                  "api.local.example.com.": {"type": "A", "value": "192.168.1.10"},
                  "web.local.example.com.": {"type": "A", "value": "192.168.1.20"}
                }
              }
            ],
            "default_upstream": {
              "upstreams": ["8.8.8.8:53"]
            }
          }
        }
      }
    }
  }
}
```

**Split-Horizon DNS with Policies:**
```json
{
  "apps": {
    "dns": {
      "servers": {
        "main": {
          "listen": [":53"],
          "handler": {
            "handler": "policy",
            "base_handler": {
              "handler": "dns.zone.manager",
              "zones": [
                {
                  "type": "forward",
                  "zone": "app.example.com.",
                  "records": {
                    "api.app.example.com.": {"type": "A", "value": "192.168.1.10"}
                  }
                }
              ]
            },
            "client_groups": {
              "internal": {"sources": ["192.168.0.0/16"]},
              "external": {"sources": ["0.0.0.0/0"]}
            },
            "policies": [
              {
                "match": {"client_group": "external"},
                "overrides": {
                  "dns.zone.manager": {
                    "zones": [
                      {
                        "type": "forward",
                        "zone": "app.example.com.",
                        "records": {
                          "api.app.example.com.": {"type": "A", "value": "203.0.113.10"}
                        }
                      }
                    ]
                  }
                }
              }
            ]
          }
        }
      }
    }
  }
}
```

## Documentation

- [DNS Zones](docs/zones.md) - Comprehensive zone configuration guide
- [DNS Zones Quick Reference](docs/zones-quickref.md) - Quick reference and examples
- [Architecture](docs/architecture.md) - System design and module development

## Available Modules

### DNS Handlers
- `dns.resolver.upstream` - Forward queries to upstream DNS servers
- `dns.zone.manager` - Manage local DNS zones with upstream fallback

### Policy & Routing
- `policy` - Client-based routing and configuration overrides

### Logging
- `logger.text` - Human-readable text logging
- `logger.json` - Structured JSON logging

## Development Setup

### Prerequisites

Choose one of the following development environments:

#### Option 1: Nix (Recommended)
```bash
# Enter the development shell (includes all dependencies)
nix develop

# Install pre-commit hooks
pre-commit install --install-hooks
```

#### Option 2: Manual Installation
- Go 1.21+
- golangci-lint
- pre-commit

```bash
# Install pre-commit hooks
pre-commit install --install-hooks
```

### Code Quality

This project enforces conventional commits and code quality standards:

- **Conventional Commits**: All commit messages must follow the [Conventional Commits](https://www.conventionalcommits.org/) specification
- **Pre-commit Hooks**: Automatically validate commit messages and code quality before commits
- **CI Validation**: GitHub Actions validate commits in pull requests

### Building and Testing

```bash
# Build the project
go build ./cmd/mightydns

# Run tests
go test ./...

# Run linting
golangci-lint run
```

### Contributing

1. Install pre-commit hooks: `pre-commit install --install-hooks`
2. Follow conventional commit format for all commits
3. Ensure all tests pass and linting is clean before submitting PRs
