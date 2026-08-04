"""Nitter MCP — read-only Twitter via direct GraphQL.

The service is named "nitter-mcp" because it provides the Nitter-style tool
surface (search, user_tweets, replies, following). Internally it talks to
Twitter's web GraphQL endpoints directly using auth cookies via the
``twscrape`` library — no Nitter binary required, no Redis dep, faster than
HTML scraping.

Tools are READ-ONLY. Writes (post, like, RT, follow) belong to the browser
MCP at the twitter-agent org — this server is the read path, browser is the
write path.

Tool surface (10 tools):

    search(query, limit)             — full-text Twitter search
    user_tweets(username, limit)     — original tweets only
    user_replies(username, limit)    — tweets + replies (the 'last N replies' tool)
    user_media(username, limit)      — media tweets only
    user_profile(username)           — bio, counts, verified status
    user_following(username, limit)  — accounts followed (for engagement curation)
    user_followers(username, limit)  — accounts following
    tweet_details(tweet_id)          — single tweet + thread
    tweet_replies(tweet_id, limit)   — replies to a tweet
    health()                         — pool status + config introspection
"""
from __future__ import annotations

from contextlib import asynccontextmanager

from fastmcp import FastMCP
from loguru import logger
from starlette.requests import Request
from starlette.responses import JSONResponse

from .settings import get_settings
from .twitter_client import TwitterReadClient

_client: TwitterReadClient | None = None


@asynccontextmanager
async def lifespan(server: FastMCP):
    """Startup: build the twscrape pool from env-loaded accounts. Shutdown:
    close the pool cleanly so the SQLite DB isn't left locked."""
    global _client
    settings = get_settings()
    _client = TwitterReadClient(settings)
    await _client.startup()
    yield
    await _client.shutdown()
    _client = None


mcp = FastMCP(
    "nitter",
    instructions=(
        "Read-only Twitter access (Nitter-style tools) backed by direct "
        "Twitter GraphQL via twscrape. Use for bulk reads: search, listing "
        "tweets/replies, browsing following lists. Writes (post, like, RT, "
        "follow) belong to the browser MCP — call the browser tools for "
        "anything that mutates Twitter state."
    ),
    lifespan=lifespan,
)


# ── MCP tools ──────────────────────────────────────────────────────────────

@mcp.tool
async def search(query: str, limit: int = 20) -> dict:
    """Full-text search across Twitter.

    Supports all Twitter search operators:
      from:user, to:user, @user, since:YYYY-MM-DD, until:YYYY-MM-DD,
      filter:replies, filter:images, filter:videos,
      min_faves:N, min_retweets:N, min_replies:N, lang:xx, url, ...

    Args:
        query: search query (operators allowed).
        limit: max results. Default 20, hard ceiling 200 (per server config).

    Returns: {query, count, tweets[]} — each tweet has id, text, author,
    stats{likes,retweets,replies,quotes}, created_at, media[], quoted?.
    """
    return await _client.search(query, limit=limit)


@mcp.tool
async def user_tweets(username: str, limit: int = 20) -> dict:
    """Last N original tweets from a user (excludes replies and RTs).

    For 'last 50 replies' use user_replies; for retweets/likes the user
    PERFORMED, those are account-private and not exposed here.

    Args:
        username: Twitter handle, with or without leading @.
        limit: max tweets. Default 20.

    Returns: {username, user_id, count, tweets[]}.
    """
    return await _client.user_tweets(username, limit=limit)


@mcp.tool
async def user_replies(username: str, limit: int = 50) -> dict:
    """Last N tweets + replies from a user (includes their replies to others).

    THIS is the 'get last 50 replies' tool — user_tweets above excludes
    replies. Tweets and replies are interleaved in reverse-chronological
    order; each tweet has in_reply_to_status_id set when it's a reply.

    Args:
        username: Twitter handle.
        limit: max items. Default 50.

    Returns: {username, user_id, count, tweets[]}.
    """
    return await _client.user_replies(username, limit=limit)


@mcp.tool
async def user_media(username: str, limit: int = 20) -> dict:
    """Last N media tweets (images/videos) from a user.

    Implementation: twscrape has no dedicated media endpoint, so this filters
    user_tweets to those with attached media. May return fewer than `limit`
    if the user has few media tweets in the scanned window.

    Args:
        username: Twitter handle.
        limit: max items. Default 20.

    Returns: {username, user_id, count, tweets[]} — each tweet has media[].
    """
    return await _client.user_media(username, limit=limit)


@mcp.tool
async def user_profile(username: str) -> dict:
    """Profile metadata: bio, location, URL, counts (followers/following/tweets/
    likes), verified + protected flags, created_at, avatar + banner URLs.

    Args:
        username: Twitter handle.

    Returns: full profile dict, or {username, found: false} if not found.
    """
    return await _client.user_profile(username)


@mcp.tool
async def user_following(username: str, limit: int = 200) -> dict:
    """Accounts this user follows. The engagement-curation primitive:

        1. user_following('me')                       → list of accounts I follow
        2. user_tweets(each, limit=5) for each        → recent tweets per followee
        3. (browser MCP) RT/like/reply the best ones  → action

    The agent does steps 1-2 via this tool, then hands candidates to the
    browser MCP for the action.

    Args:
        username: Twitter handle whose following list to fetch.
        limit: max following to return. Default 200, hard ceiling 200.

    Returns: {username, user_id, count, following[]} — each item is a user dict.
    """
    return await _client.user_following(username, limit=limit)


@mcp.tool
async def user_followers(username: str, limit: int = 200) -> dict:
    """Accounts following this user.

    Args:
        username: Twitter handle.
        limit: max followers. Default 200.

    Returns: {username, user_id, count, followers[]}.
    """
    return await _client.user_followers(username, limit=limit)


@mcp.tool
async def tweet_details(tweet_id: str) -> dict:
    """Single tweet + its thread context (parent + conversation chain).

    Args:
        tweet_id: numeric tweet ID (the trailing digits of a x.com/user/status/<id> URL).

    Returns: full tweet dict (with media/quoted/retweeted expanded) plus
    thread[] = the surrounding tweets twscrape could fetch. found:false if
    the tweet is deleted, protected, or unreachable.
    """
    return await _client.tweet_details(tweet_id)


@mcp.tool
async def tweet_replies(tweet_id: str, limit: int = 50) -> dict:
    """Replies to a specific tweet (direct replies only, not nested).

    Args:
        tweet_id: numeric tweet ID.
        limit: max replies. Default 50.

    Returns: {tweet_id, found, count, replies[]} — replies in
    reverse-chronological order.
    """
    return await _client.tweet_replies(tweet_id, limit=limit)


@mcp.tool
async def health() -> dict:
    """Service health + account pool status + config introspection.

    Call this FIRST when debugging: confirms whether the twscrape pool came
    up, how many accounts are configured, and which transport is in use.

    Returns: {ok, pool_size, configured_accounts, db_path, max_limit, transport}.
    """
    if _client is None:
        return {"ok": False, "error": "lifespan did not start (client is None)"}
    return await _client.health()


# ── Plain HTTP health route (for docker HEALTHCHECK + ops probes) ──────────
# FastMCP exposes MCP over POST /mcp; we add a sibling GET /health for cheap
# liveness checks that don't need a full MCP round-trip.

@mcp.custom_route("/health", methods=["GET"])
async def health_route(request: Request) -> JSONResponse:
    ready = _client is not None and _client._api is not None
    return JSONResponse({"ok": ready}, status_code=200 if ready else 503)


def main() -> None:
    settings = get_settings()
    logger.info(
        f"starting nitter-mcp on {settings.host}:{settings.port}/mcp "
        f"(db={settings.db_path}, accounts={len(settings.accounts)})"
    )
    mcp.run(
        transport="http",
        host=settings.host,
        port=settings.port,
        path="/mcp",
        stateless_http=True,
    )


if __name__ == "__main__":
    main()
