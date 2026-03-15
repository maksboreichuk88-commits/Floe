# Contributing to Floe

First off, thank you for considering contributing to Floe! We're building the fastest, most secure local AI gateway, and community contributions are what make open source incredible.

## 🏗️ Prerequisites

- **Go 1.23+** (Required for the backend and CLI)
- **Node.js 20+ & pnpm** (Required for the Next.js dashboard)
- **Docker** (Optional, for running integration test environments)

## 🚀 Rapid Setup

1. **Fork & Clone:**
   ```sh
   git clone https://github.com/YOUR_USERNAME/floe.git
   cd floe
   ```

2. **Start the Development Environment:**
   ```sh
   make dev
   ```
   This command starts the backend gateway on `http://localhost:4400` with hot-reload enabled.

## 📁 Repository Structure

Floe follows standard Go layout practices, with a heavy emphasis on Domain-Driven Design (DDD) in the `internal/` directory.

- `cmd/floe/`: The Cobra CLI entrypoint.
- `internal/gateway/`: The core HTTP proxy, middleware, router, and circuit breaker.
- `internal/provider/`: Adapters for OpenAI, Anthropic, Ollama, etc. **(Best place for first PRs!)**
- `internal/workflow/`: The YAML DAG engine and restricted sandbox.
- `dashboard/`: The Next.js 15 UI.

## 🧪 Testing

We have a hard rule: **Features require tests. Bug fixes require regression tests.**

Run the Go test suite:
```sh
make test
```

If you modify the configuration loader or the workflow YAML parser, you **must** run the fuzz tests for at least 60 seconds to ensure no panics:
```sh
make fuzz
```

## 🔐 Security Guidelines

Security is paramount for Floe (reference our mitigations against vulnerabilities like CVE-2026-25253). 
- **NO `os/exec` calls.** Do not introduce any shell execution vectors.
- **NO Plaintext Secrets.** Utilize `internal/vault` for all credential handling.
- If you find a security vulnerability, please refer to [SECURITY.md](SECURITY.md) instead of creating a public issue.

## 📝 Pull Request Process

1. Create a descriptive branch name (`feat/add-mistral-provider` or `fix/circuit-breaker-race`).
2. Write the code, ensuring `make lint` passes completely clean.
3. Ensure test coverage remains above 90% (`make test` reports coverage).
4. Submit the PR using our template.
5. A maintainer will review within 48 hours.

## 🏷️ Issue Labels

- `good first issue`: Self-contained tasks, great for getting familiar with the codebase.
- `help wanted`: Core team is asking for community assistance on a complex problem.
- `provider`: Requests or bugs related to specific LLM adapters.
