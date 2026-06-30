# Contributing to Process Scheduler Simulator

Contributions are welcome! This document outlines the process and standards.

## Getting Started

1. **Fork** the repository on GitHub
2. **Clone** your fork locally
3. **Create a branch** for your work: `git checkout -b feat/my-feature`
4. **Make changes** following the code style below
5. **Run checks**: `make ci`
6. **Commit** with a conventional commit message
7. **Push** and open a Pull Request

## Prerequisites

- Go 1.23+
- [golangci-lint](https://golangci-lint.run/) (installed automatically by `make lint`)
- Docker (optional, for container testing)

## Code Style

### Go

- Run `make fmt` before committing (gofmt + goimports)
- All code must pass `golangci-lint` with the project's [`.golangci.yml`](.golangci.yml)
- Follow [Effective Go](https://go.dev/doc/effective_go) and the [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- No panics in request paths — use error returns
- Check all errors (`errcheck` linter enforces this)

### Frontend

- Vanilla JS (no framework)
- Escape all user-controlled values before `innerHTML` injection (use `escapeHtml()`)
- Validate all user input before sending to the server
- Test with: empty values, unicode, emoji, special characters, injection-like payloads

### Tests

- New features must include tests
- Tests must pass under `-race`
- Use the existing test patterns in `internal/simulator/scheduler_test.go` as a reference
- Benchmark new hot paths if applicable

## Commit Convention

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add new scheduler
fix: resolve race condition in simulator
docs: update README
test: add regression test for CFS
chore: bump dependencies
refactor: simplify scheduler interface
```

## Pull Request Process

1. Reference any relevant issues in your PR description (`Closes #123`)
2. Ensure CI passes (GitHub Actions runs automatically)
3. Keep PRs focused — one feature/fix per PR
4. Update documentation if behavior changes

## Project Structure

See the [README](README.md#architecture) for the architecture overview. Key conventions:

- **`internal/`** — private packages, not importable externally
- **`cmd/server/`** — application entrypoint only
- **`web/`** — WebSocket server and static frontend
- **`api/`** — OpenAPI specification
- Schedulers are stateless w.r.t. the ready queue (the engine owns it)
- All process mutation is serialized by the simulator's mutex

## Reporting Issues

Use [GitHub Issues](https://github.com/sanskarpan/process-scheduler-simulator/issues) to report bugs or request features. Include:

- Go version (`go version`)
- Steps to reproduce
- Expected vs actual behavior
- Logs (if applicable)

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
