# r/selfhosted Launch Post
**Title:** I got tired of SaaS AI gateways vacuuming up my prompt data, so I built a 12MB local orchestrator in Go

Hey r/selfhosted,

A few months ago I wanted to build an internal tool that queried Claude and OpenAI, but I was struck by how frustrating it was. If I used a SaaS gateway, I was basically handing them a copy of all my proprietary prompt data. If I built it myself, I ended up writing hundreds of lines of brittle retry and circuit-breaker logic.

So I built **Floe** to solve this for myself.

It’s completely open-source (Apache 2.0) and designed strictly for the self-hosted community. 

**What it actually does:**
It's a single static binary. You drop it on your server, point your apps at `http://localhost:4400/v1/chat/completions`, and Floe handles the rest.
- **Unifies APIs:** Translates a single request format into OpenAI, Anthropic, or local Ollama.
- **Fails over instantly:** If an API goes down, the circuit breaker flips and routes to your backup provider in milliseconds.
- **Keeps your data yours:** Every request, response, and token count is logged entirely to a local SQLite database (`/data/audit.db`). Nothing leaves your machine.
- **Protects your keys:** AES-256-GCM vault so API keys aren't sitting in plaintext ENV vars.

I also threw together a lightweight web dashboard so you can actually see how much money you are spending across all your APIs in real-time.

Code is here: https://github.com/floe-dev/floe

I built this primarily to scratch my own itch, but I'd genuinely love feedback from people who run a lot of local services. (Especially on the Docker compose setup—I locked it down with `cap_drop: ALL` and `read_only: true`, let me know if I missed anything).

Enjoy!
