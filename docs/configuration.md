# Floe Configuration Guide

Floe is primarily configured via `floe.yaml` (or TOML/JSON). Floe relies on strict typing and integrity checksums to prevent misconfigurations from opening security holes.

## The `.floe.lock` Mechanism

When Floe starts, it computes the SHA-256 hash of `floe.yaml` and checks it against `.floe.lock` (if present). If the configuration has been modified without approval, Floe **will refuse to start** to prevent unauthorized changes (e.g., an attacker modifying the routing table to point to a malicious endpoint).

To update the lockfile after a valid manual change:
```bash
floe config seal
```

---

## Server Block

Sets up the primary HTTP gateway and Next.js dashboard.

```yaml
server:
  host: "127.0.0.1"    # Default. Warning: setting to 0.0.0.0 exposes Floe externally.
  port: 4400
  dashboard_port: 4401
  max_request_size: 4194304 # 4MB (CVE mitigation against memory exhaustion DoS)
```

## Security Block

Manages authentication and limits.

```yaml
security:
  # If specified, all requests must include: Authorization: Bearer <token>
  auth_token: "sk-floe-your-local-dev-token"
  
  circuit_breaker:
    failure_threshold: 5     # Flip to OPEN after 5 consecutive errors
    recovery_timeout: 30s    # Wait 30s before probing again (HALF_OPEN)
    success_threshold: 3     # Require 3 successes to CLOSE the circuit
```

## Vault Block

Manages AES-256-GCM encrypted API keys. **Never put your actual API keys in `floe.yaml`.**

```yaml
vault:
  enabled: true
  path: "/data/vault/secrets.enc"
```

To use keys from the vault in your provider config, use the `$vault:` prefix:

```yaml
providers:
  - id: "openai-prod"
    type: "openai"
    api_key: "$vault:openai_key"  # Handled securely
```

## Providers Block

The core LLM adapters. You specify an `id` (which you can use for manual routing) and a `priority` (for the `priority` routing strategy).

```yaml
providers:
  - id: "anthropic-primary"
    type: "anthropic"
    api_key: "$vault:anthropic_key"
    priority: 1
    timeout: 30s

  - id: "openai-fallback"
    type: "openai"
    api_key: "$vault:openai_key"
    priority: 2
    timeout: 60s

  - id: "local-ollama"
    type: "ollama"
    base_url: "http://127.0.0.1:11434"
    priority: 3
    timeout: 120s
```

## Routing Engine

Determines how Floe selects a provider for a request.

```yaml
routing:
  # Options:
  # - priority: Try priority 1, failover to 2, then 3.
  # - round_robin: Rotate evenly across all healthy providers.
  # - latency: Send to the provider with the lowest recent response latency (EWMA).
  strategy: "priority"
```

## Budget Control

Defines hard limits to prevent runaway costs.

```yaml
budget:
  project_limits:
    "my-side-project":
      max_cost_usd: 10.00
      max_tokens: 500000
      window: "30d"  # Reset every 30 days
```
