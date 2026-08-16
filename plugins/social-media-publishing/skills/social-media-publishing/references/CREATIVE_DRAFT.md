# CREATIVE_DRAFT

How to write tweet/thread options from research findings.

## Read Research First

```bash
cat research/summary.json | jq .
```

Note:
- `topics[]` — themes that came up
- `top_posts[]` — high-engagement content from twitter + telegram
- `gaps[]` — angles nobody covered (use these!)

## Angle Menu (pick 4+ different)

| Angle | Hook pattern | Example |
|-------|--------------|---------|
| Contrarian | "Everyone says X. They're wrong." | "Everyone says agents will replace developers. They won't — they'll replace the boring 80%." |
| Synthesis | "X + Y = Z" | "Combine Claude's tool use + A2A protocol = agents that actually collaborate" |
| Hot take | Bold prediction | "By end of 2026, every SaaS will ship an agent. Most will suck." |
| Behind-the-scenes | "Here's how X actually works" | "Here's how we ship 50+ agent runs/day without going bankrupt on API costs" |
| Listicle | "N things I learned about X" | "5 things I learned shipping agents to production this month" |
| Story | "I tried X. Here's what happened." | "I let an agent run my Twitter for a week. Here's what it got right." |
| Question | "What if X?" | "What if every PR review was done by an agent + a human co-signer?" |
| Data point | "X% of Y. That's Z." | "73% of agent deployments fail in week 2. The pattern is always the same." |

## Tweet Length

Hard limit: 280 characters (count carefully).

```bash
echo -n "your tweet text here" | wc -c
```

If >280, either:
- Split into a thread
- Tighten the prose (cut adjectives, use contractions)
- Move detail to a reply

## Thread Structure

For threads (2-7 tweets):
1. **Hook tweet** — provocative claim or question
2. **Context tweet** — why this matters
3-6. **Body tweets** — one idea each, concrete examples
7. **CTA tweet** — what to do next, or summary

Each tweet ≤280 chars. Each tweet should make sense standalone (people quote-retweet individual tweets).

## Image Prompt

Each option can include an `image_prompt` field — concrete description for the image-gen-worker. Examples:
- `"isometric 3D render of two robot hands shaking, neon cyberpunk palette, dark background"`
- `"minimalist flat illustration of a feedback loop with three nodes, single accent color"`
- `"split screen photo: human desk on left, robot desk on right, matching composition"`

## Output Schema

Write to `drafts/options.json`:

```json
{
  "generated_at": "2026-...",
  "options": [
    {
      "id": "A",
      "angle": "contrarian take",
      "text": "single tweet under 280 chars",
      "image_prompt": "concrete visual description",
      "rationale": "why this angle, what evidence supports it"
    },
    {
      "id": "B",
      "angle": "listicle",
      "thread": ["tweet 1 (hook)", "tweet 2", "tweet 3"],
      "image_prompt": "...",
      "rationale": "..."
    }
  ]
}
```

## Pitfalls

- **Don't copy research tweets** — use them as inspiration, not source material
- **Don't be generic** — "AI is changing everything" is not a tweet, it's a nothing
- **Don't be edgy without reason** — controversy for clicks ages badly
- **Don't promise without evidence** — if you cite a number, where did it come from?
