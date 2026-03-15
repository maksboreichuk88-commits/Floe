# Hacker News Launch Post
**Title:** Show HN: Floe – A 12MB Go binary that unifies all your AI providers

Hi HN,

I’m the builder of Floe (https://github.com/floe-dev/floe). Like many of you, I got exhausted juggling API keys for OpenAI, Anthropic, and local Ollama instances across different projects. My apps were full of bespoke retry logic, and I had zero visibility into prompt costs until the end of the billing cycle.

So I built Floe. It’s a single Go binary compiled completely without CGO that acts as a local AI gateway.

**Architectural choices we made:**
- **Zero dependencies:** It uses standard library HTTP handlers and `modernc.org/sqlite` instead of `go-sqlite3`. Total binary size is under 12MB.
- **Circuit Breaking:** It routes between providers based on a 3-state circuit breaker (Closed → Open → Half-Open). If Anthropic throws a 500, Floe fails over to OpenAI in <5ms.
- **Budgeting:** It meters tokens locally using a token-bucket algorithm, allowing you to set hard USD caps per project.
- **Security First:** We learned from recent supply chain attacks (like the OpenClaw vulnerability). Passwords are AES-256-GCM encrypted locally. The workflow engine evaluates YAML templates in a pure string-manipulation sandbox—absolutely no `os/exec` or shell access possible.
- **Dashboard:** There's a minimal Next.js UI packaged alongside it that reads the local SQLite DB for real-time observability.

**Why not just use LangChain / LiteLLM / Cloudflare AI Gateway?**
Because I wanted true data sovereignty. With Floe, your request/response logs never bounce through a third-party SaaS proxy. They stay in a local SQLite file. I also wanted something that requires literally zero configuration to try out.

You can test it right now without any API keys (it falls back to an internal mock provider system):
`curl -sSL https://get.floe.dev | sh && floe demo`

The repo is at https://github.com/floe-dev/floe (Apache 2.0).

I’d love to hear your thoughts on the routing algorithms or the YAML workflow DAG parser we built. Happy to answer questions!
