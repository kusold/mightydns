# MightyDNS Agent Guidelines

## Project Overview
MightyDNS is a modular DNS server written in Go, inspired by Caddy's architecture. Currently in early development stage with minimal codebase.

## Build Commands
- **Build**: `go build` (when Go files exist)
- **Test**: `go test ./...` (when tests exist)
- **Lint**: `golangci-lint run` (standard Go linting)
- **Single test**: `go test -run TestName ./package`

## Testing Information
- Find the CI plan in the .github/workflows folder

## Development Lifecycle Instructions
- Never work in the `main` branch. You should always work in a branch.
- Commit messages should follow the [Conventional Commit](https://www.conventionalcommits.org/en/v1.0.0/) standard. The body should be fairly verbose and describe what changes are being made, and more importantly why they are being made.
- Always run the linting commands and run all the tests. You if either linting or testing fails, fix the problem before commiting.
