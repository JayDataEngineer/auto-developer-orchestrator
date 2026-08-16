# nitter-mcp — Read-only Twitter MCP via direct GraphQL

**Read-only** Twitter access for the `twitter-agent` org. Named "nitter-mcp"
because it provides the [Nitter](https://github.com/zedeus/nitter)-style tool
surface (search, user_tweets, replies, following lists). Internally it talks
to **Twitter's web GraphQL endpoints directly** via the
[twscrape](https://github.com/vladkens/twscrape) library — no Nitter binary,
no Redis dep, faster than HTML scraping.

## Division of labor

| Path | Tool | Used for |
|---|---|---|
| **Read (this server)** | `mcp__nitter__*` | bulk reads: search, list tweets, replies, following lists |
| **Write (browser MCP)** | `mcp__opensandbox__*` | post tweet, like, RT, follow, reply (sandbox with a browser environment) |

The CTO at the `twitter-agent` org has both armed. Typical flow: pull
engagement candidates with `nitter` tools, act on them with `opensandbox`.

## Tool surface (10 tools)

| Tool | What it does |
|---|---|
| `search(query, limit)` | Full-text Twitter search (supports all operators: `from:`, `since:`, `filter:`, `min_faves:`, …) |
| `user_tweets(username, limit)` | Last N **original** tweets (no replies, no RTs) |
| `user_replies(username, limit)` | Last N tweets + replies interleaved — **this is the "last 50 replies" tool** |
| `user_media(username, limit)` | Last N media tweets (images/videos) |
| `user_profile(username)` | Bio, counts, verified status, avatar URL |
| `user_following(username, limit)` | Accounts followed — engagement-curation primitive |
| `user_followers(username, limit)` | Accounts following |
| `tweet_details(tweet_id)` | Single tweet + thread context |
| `tweet_replies(tweet_id, limit)` | Replies to a specific tweet |
| `health()` | Pool status + config introspection |

All tools are **read-only**. None mutate Twitter state.

## How auth works

The server reads NITTER_ACCOUNT_* env vars (typically loaded from the
gitignored `.env` via docker compose `env_file:`). Each account needs 6
vars: `USERNAME`, `EMAIL`, `PASSWORD`, `EMAIL_PASSWORD`, `AUTH_TOKEN`,
`CT0`. The cookies (`AUTH_TOKEN` + `CT0`) are the primary auth;
`PASSWORD`/`EMAIL`/`EMAIL_PASSWORD` are twscrape's fallback for the full
login flow if the cookies die.

twscrape round-robins across all configured accounts, spreading Twitter's
per-account rate limits. The SQLite DB at `/data/accounts.db` (a docker
volume) caches the login state across container restarts.

## Operation

### Build + run standalone

```bash
cd infra/nitter
cp .env.example .env
# edit .env with your account credentials
docker compose up -d --build
curl http://localhost:41730/health   # → {"ok": true}
```

### Via the orchestrator stack

The service is wired into `docker-compose.infra.yml`:

```bash
make infra                         # brings up surrealdb + media-mcp + nitter-mcp
# or just nitter-mcp:
docker compose -f docker-compose.infra.yml up -d nitter-mcp
```

### Expose to remote clients (Tailscale)

```bash
tailscale serve --set-path /nitter http://127.0.0.1:41730
# Client URL: https://<node>.ts.net:10000/nitter/mcp
```

Set `MCP_NITTER_URL=https://<node>.ts.net:10000/nitter/mcp` in the
orchestrator's `.env` so the `twitter-agent` org can reach it from a remote
sandbox.

## Credentials safety

- `.env` and `.env.raw-credentials` are gitignored via the root `.gitignore`
  `.env*` rule (line 21). Verify with `git check-ignore -v infra/nitter/.env`.
- The `infra/nitter/.env.raw-credentials` file is the master record — paste
  your credential dumps there, then run the extractor (see the commit that
  introduced this file) to regenerate `.env`.
- Both files are mode 0600 (owner read/write only).
- `NEVER` commit either file. `NEVER` echo credentials in issues/PRs/logs.

## Why not the actual Nitter binary?

We considered (a) running `zedeus/nitter` + Redis + a wrapper MCP, vs (b)
this direct-GraphQL approach, vs (c) using public Nitter instances. We chose
(b) because:

1. **The user gave us authenticated sessions** — only (a) and (b) deliver
   authenticated reads. (c) is anonymous-only.
2. **(b) is faster** — direct JSON from Twitter's GraphQL, no HTML parse.
3. **(b) is more resilient** — twscrape maintains the GraphQL operation
   hashes that twitter.com's own web client uses; we don't depend on
   Nitter's HTML output which has shifted repeatedly since mid-2024.
4. **(b) is simpler ops** — one container, no Redis, no wrapper layer.

If twscrape ever fails to keep up with Twitter auth churn, adding a real
Nitter binary as a fallback layer is a one-service compose add — the tool
surface above stays the same.

## Tests

Offline unit tests for settings parsing and tweet/user shaping (no live
Twitter calls):

```bash
cd infra/nitter
docker compose run --rm nitter-mcp python -m pytest tests/ -v
# or locally if you have the deps:
uv sync --extra dev
uv run pytest tests/ -v
```
