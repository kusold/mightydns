# MightyDNS

A modular DNS server written in Go, inspired by Caddy's architecture.

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
