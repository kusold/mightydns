# MightyDNS Agent Guidelines

## Project Overview
MightyDNS is a modular DNS server written in Go, inspired by Caddy's architecture. Currently in early development stage with minimal codebase.

## Build Commands
- **Build**: `go build ./cmd/mightydns` or `go build` for main package
- **Test**: `go test ./...` (all packages) or `go test -run TestName ./package` (single test)
- **Lint**: `golangci-lint run` (uses .golangci.yaml config with gofmt, goimports, gocritic, gosec)
- **Format**: `gofmt -s -w .` and `goimports -w .` (auto-run by golangci-lint)

## Code Style Guidelines
- **Imports**: Use `goimports` with local prefix `github.com/kusold/mightydns`
- **Formatting**: Use `gofmt -s` (simplify enabled)
- **Naming**: Go conventions - CamelCase exports, camelCase private, ALL_CAPS constants
- **Error handling**: Wrap errors with `fmt.Errorf("context: %w", err)` pattern
- **Interfaces**: Small, focused interfaces (e.g., `App`, `Module`, `DNSHandler`)
- **Structs**: JSON tags for config structs, private fields for internal state
- **Logging**: Use `log/slog` with structured logging (`logger.Info("msg", "key", value)`)
- **Comments**: Package-level comments required, exported items documented
- **Context**: Pass `context.Context` as first parameter for I/O operations

## Development Lifecycle
- Never work in the `main` branch. Always create feature branches.
- Run `golangci-lint run && go test ./...` before committing
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
- Fix all linting errors and test failures before committing
