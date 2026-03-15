# Product Hunt Launch Strategy

**Tagline:** One binary. Every model. Total control over your AI.
**Description:** Floe is a 12MB Go binary that unifies OpenAI, Anthropic, and local Ollama under one API. It features instant failover circuit breakers, local AES-256 encrypted API keys, and a local SQLite audit trail so your data isn't leaked to a third-party gateway SaaS.

## Maker Comment Strategy (Post within 1 minute of launch)

"Hi Product Hunt! 👋 I'm the builder behind Floe.

Like many developers, I spent the last year constantly updating API keys across 5 different projects, implementing brittle retry logic every time Anthropic or OpenAI threw a 500, and agonizing over token costs because I lacked real-time visibility.

I built Floe to scratch that exact itch. It's an open-source (Apache 2.0), local AI gateway.

**Why developers might love this:**
1. **It's a single static binary.** No massive Docker dependencies required. Drop it in and it works.
2. **True data privacy.** We learned from the recent OpenClaw vulnerabilities. Your request logs stay in your local SQLite DB. Your API keys are AES-256-GCM encrypted.
3. **Zero-code workflows.** You can define complex LLM chains in simple YAML.
4. **Interactive Dashboard.** We built a Next.js UI to monitor your costs in real-time.

You can try it literally right now without needing any API keys. Open your terminal:
`curl -sSL https://get.floe.dev | sh && floe demo`

The repo is at https://github.com/floe-dev/floe

I’ll be here all day answering questions. I'd especially love feedback from anyone managing production AI workloads on our routing strategies!"

## Launch Assets

1. **Thumbnail:** Bold, clean icon of the Floe logo on a stark dark background. (No text).
2. **Video:** Fast-paced, 30-second screen recording of `floe demo` running and opening the dashboard.
3. **Screenshot 1:** The unified CLI output showing instant circuit-breaker failover.
4. **Screenshot 2:** The Next.js dashboard showing token metering and cost limits.
5. **Screenshot 3:** A YAML workflow file visualizing the DAG architecture.
