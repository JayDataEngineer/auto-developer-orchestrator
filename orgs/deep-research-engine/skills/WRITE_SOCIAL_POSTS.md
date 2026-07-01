# WRITE_SOCIAL_POSTS

The social-post-writer's checklist. Loaded automatically into the social-post-writer's prompt via skills_dir.

## Platforms and limits

| Platform | Char limit | Notes |
|---|---|---|
| Twitter/X (single) | 280 | Emoji = 2 chars; URLs = 23 chars (t.co wrapping) |
| Twitter/X (thread) | 280/post | ≤12 posts in a thread (engagement drops after) |
| Mastodon | 500 (default; varies by instance) | Content warnings for sensitive topics |
| Bluesky | 300 | Supports markdown links; no auto-wrap |
| LinkedIn | 3000 | But engagement drops past 1300 — keep it tight |

## Workflow

### Step 1 — Read the brief
Open `artifacts/brief.md`. Note every claim with its citation. You will only write claims that are in the brief.

### Step 2 — Pick the angle
What is the single most interesting thing in this brief? That's your hook. If you can't say it in 1 sentence, you don't have an angle yet.

Examples of strong hooks:
- A specific number: "30 voice messages from one source."
- A contradiction: "He says he infiltrated them. They say he didn't."
- A stake: "What this chat reveals about doxing operations in Montana."

Examples of weak hooks:
- "Let's talk about X."
- "Here are some thoughts on X."
- "🧵 THREAD:"

### Step 3 — Draft
Write to `artifacts/posts/<platform>_<n>.md`:

```markdown
---
platform: twitter
post_number: 1
char_count: 264
citations: [1, 3]
---

In a recent 903 MB corpus ingest, one chat sent 30 voice messages
about four named individuals in Flathead Valley.
Addresses, gun ownership, license plates — all in the open.[1][3]
```

The front-matter block is metadata for the CTO to audit. Below `---` is the actual post text.

### Step 4 — Character count audit
Run this before reporting done:

```bash
# Strip front-matter, count chars in the body
awk '/^---$/{flag=!flag; next} flag' artifacts/posts/twitter_1.md | head -c 1000 | wc -m
```

Or in Python:

```python
import frontmatter  # pip install python-frontmatter
post = frontmatter.load("artifacts/posts/twitter_1.md")
n = len(post.content)
assert n <= 280, f"Too long: {n}"
```

### Step 3b — Persist each post to SurrealDB
After drafting, save every post file as a `source` record so future agents can find prior posts by content:

```bash
for f in artifacts/posts/*.md; do
  [ "$(basename "$f")" = "_INDEX.md" ] && continue
  python3 /sandbox/surreal_client.py save-source \
    --kind post \
    --path "$f" \
    --content-file "$f" \
    --title "$(basename "$f" .md)"
done
```

Why: lets a future CTO vector-search "have we already posted about doxing in Flathead?" before commissioning a new thread. Without this, posts only exist on disk and get rediscovered by grep.

### Step 5 — Citation audit
For each post, list which brief sources it cites in `citations:` front-matter. Every factual claim must trace to one of those sources.

### Step 6 — Voice check
Re-read each post. Does it sound like a real person? Test:
- Read aloud — does anything feel awkward?
- Check for AI-tells: "delve into", "navigate the complexities", "in today's world", "it is worth noting", "needless to say"
- Check for hedges: "could be argued", "some might say" — name the source or cut the claim

### Step 7 — Build the index
Write `artifacts/posts/_INDEX.md`:

```markdown
# Posts index

## Twitter/X thread
1. [twitter_1.md](twitter_1.md) — <hook>
2. [twitter_2.md](twitter_2.md) — <claim>
...

## Mastodon (optional alt cut)
- [mastodon_1.md](mastodon_1.md) — <single 500-char post>

## Total chars
- Twitter thread: <N> posts, <M> chars total
- Mastodon: <N> posts, <M> chars total
```

## Stop conditions

- Every load-bearing claim in the brief has a post covering it (not every minor claim — pick what matters)
- Character counts validated per platform
- Citations mapped per post

## Pitfalls

- **Emoji bloat** — Twitter counts emoji as 2 chars. Mastodon/Bluesky count as 1.
- **URL wrapping** — Twitter wraps URLs to 23 chars regardless of actual length. If your post has a 100-char URL, it counts as 23 chars.
- **Hashtag limit** — ≤3 per post. More hurts engagement and looks spammy.
- **Thread pacing** — first post needs to stand alone (some scroll past threads). Last post needs a payoff (people who finish the thread deserve a punchline).
- **Cross-posting** — a 280-char Twitter post dumped unchanged on Mastodon is wasted space. Adapt per platform.
- **Mastodon CW** — for sensitive content (this dataset qualifies), use a content warning: `mcp__web__scrape` won't help here, you need to actually format the post for Mastodon's CW field.
