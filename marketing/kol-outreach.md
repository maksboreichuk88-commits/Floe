# KOL Outreach Criteria & Templates

## Selection Criteria (The "Rule of 10")

To hit our goal of 5,000 stars in 30-45 days, we need high-signal amplification. Do not blindly DM massive accounts. We are targeting the "Pragmatic Builder" demographic on X (Twitter).

1. **Follower Range:** 15,000 - 80,000 (Sweet spot: they have reach, but aren't too famous to read DMs).
2. **Current Focus:** Has tweeted specifically about **local AI, Ollama, prompt engineering, or Go architecture** in the last 30 days.
3. **Builder Mindset:** Actively maintains at least one open-source repo with >500 stars.
4. **No Shills:** Timeline contains less than 10% explicit "sponsored/paid" content.
5. **Engagement:** High ratio of replies-to-tweets (indicates a real community, not bot followers).

## Outreach Strategy: The "Review Ask"

Do not ask for a retweet. Ask for architectural review.

### DM Template (The "Go/Rust" Angle)

> "Hey [Name], I've loved your recent threads on scaling local AI infrastructure. I just open-sourced Floe — it's a zero-config local AI gateway that handles circuit-breaking across OpenAI/Anthropic/Ollama. 
> 
> We explicitly chose Go over Rust purely for the 30-day iteration speed to hit our target, and I'd genuinely love your brutally honest take on our routing architecture before we do our big HN push next week. Link: github.com/floe-dev/floe"

### DM Template (The "Security" Angle)

> "Hi [Name], your teardown of the OpenClaw vulnerability last month changed how we build. I took those lessons and integrated them into Floe, a local AI gateway we just open-sourced. 
>
> All workflow evaluations run in a strictly templated sandbox with zero os/exec calls, plus AES-256-GCM for all API keys on disk. If you have 2 minutes, I'd value your feedback on the `internal/workflow/sandbox.go` implementation."
