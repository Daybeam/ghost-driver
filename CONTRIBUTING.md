# Contributing to Ghost-Driver MCP

Thank you for considering contributing to **Ghost-Driver MCP**! We welcome bug reports, feature proposals, documentation improvements, and pull requests.

## Development Workflow

### Prerequisites
- **Go**: 1.22 or higher
- **Chrome / Chromium** or **Firefox** for running automation tests

### Getting Started

1. **Clone the repository**:
   ```bash
   git clone https://github.com/Daybeam/ghost-driver.git
   cd ghost-driver
   ```

2. **Run tests**:
   ```bash
   go test -v ./...
   ```

3. **Verify code quality & formatting**:
   ```bash
   go fmt ./...
   go vet ./...
   ```

4. **Build binary**:
   ```bash
   make build
   # or
   go build ./cmd/ghost-driver
   ```

## Architectural Guidelines

- **Atomic Primitives First**: Atomic tools must remain fast, deterministic, and stateless. Complex loops or retry logic belong in Orchestrators or AI Agents, not inside atomic tools.
- **Protocol Neutrality**: When adding new browser capabilities, ensure both CDP (Chrome) and BiDi (Firefox) paths are considered or cleanly handled via stubs/adapters.
- **Concurrency & Resource Safety**: Ensure browser clients, WS handlers, and file operations do not leak connections, goroutines, or temporary files. Always run `make check` before submitting a PR.

## Submitting Pull Requests

1. Create a feature branch: `git checkout -b feature/my-feature`
2. Ensure all tests pass: `make check`
3. Commit your changes with clear messages.
4. Open a Pull Request against the `main` branch.

## License

By contributing to Ghost-Driver, you agree that your contributions will be licensed under the project's [MIT License](LICENSE).
