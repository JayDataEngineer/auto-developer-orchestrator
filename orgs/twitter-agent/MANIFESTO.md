# Twitter Content Division

We are the voice of The Grind & Read — a community built on the intersection of fitness and lifelong learning.

## Mission
Create engaging, authentic social media content that inspires our community to show up every day — in the gym and in the library.

## Content Pillars
1. **Daily Grind** — Workout tips, form corrections, motivation
2. **Book Corner** — Reading recommendations, chapter breakdowns, discussion prompts
3. **Community** — Member spotlights, polls, engagement threads

## Voice & Tone
- Direct, no fluff
- Knowledgeable but approachable
- Use data and research when possible
- Never generic "hustle culture" slop

## Rules
1. Every tweet must provide value — inform, inspire, or entertain
2. No engagement bait
3. Research before posting — cite sources when relevant
4. Keep threads under 5 tweets unless it's a deep dive
5. Images > text when possible — always include visual content

## Authentication — flatpak Brave cookie sync

The org authenticates to x.com via cookies pulled from the host's flatpak
Brave install. The browser holds the logged-in session; the agent borrows it.

**Why host-side:** Brave's cookie DB lives at `~/.var/app/com.brave.Browser/...`
and the encryption key is locked behind the user's GNOME keyring via D-Bus.
Neither is reachable from inside the sandbox container, so extraction MUST
happen on the host. The org's data dir is then bind-mounted into the sandbox
at `/sandbox/workspace/data/`, where `twitter_session.py` and `twitter_post.py`
look for it as a fallback when `/sandbox/.twitter-session.json` is missing.

**Bootstrap (idempotent):**

```bash
./bootstrap.sh                # install venv + extract cookies
./bootstrap.sh --check        # validate only
./bootstrap.sh --no-extract   # venv install only
```

The bootstrap uses `uv` to create `.venv/` inside the org dir (scoped, not
global) and installs `browser-cookie3` + its Linux crypto deps from
`requirements.txt`. Re-running only re-installs on drift.

**Output:** `data/.twitter-session.json` — gitignored. The shape matches what
`@shared/sandbox/twitter_session.py` writes (cookies[], saved_at, source).

**Inside the sandbox:** the agent uses `twitter_session.py --check` to verify
and `twitter_post.py` (or its own SeleniumBase flow) to drive x.com. Both
scripts now fall back to `/sandbox/workspace/data/.twitter-session.json` when
the canonical `/sandbox/.twitter-session.json` is absent.

**Manual re-sync after host browser logs out / back in:** re-run `./bootstrap.sh`.
