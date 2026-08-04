"""Twitter GraphQL client wrapping twscrape.

Holds an AccountsPool that round-robins across configured accounts. Tools
delegate to twscrape's API methods and shape responses into compact JSON
(twscrape's raw objects are pydantic models with many fields we don't need
— we keep just what's useful for the agent).
"""
from __future__ import annotations

from typing import Any

from loguru import logger
from twscrape import API, AccountsPool

from .settings import Settings


# ── Shapers (twscrape 0.19 model → compact dict) ───────────────────────────
# Field names match twscrape.models at v0.19.x. The library uses dataclasses
# (NOT pydantic BaseModel), so we read fields via getattr — that gives us
# forward-compat if twscrape adds fields, and graceful None if they remove
# one we read (the shaped dict just gets None for that key).

def _shape_user(u: Any) -> dict | None:
    if u is None:
        return None
    created = getattr(u, "created", None)
    return {
        "id": str(getattr(u, "id", "")),
        "username": getattr(u, "username", None),
        # twscrape uses `displayname`, not `name` (Twitter API field is `name`,
        # twscrape renamed it to avoid collision with __dataclass_fields__).
        "name": getattr(u, "displayname", None),
        # `rawDescription` is the bio. (No separate `description` field —
        # `descriptionLinks` is the parsed t.co URLs in the bio.)
        "bio": getattr(u, "rawDescription", None),
        "location": getattr(u, "location", None),
        "url": getattr(u, "url", None),
        "verified": bool(getattr(u, "verified", False)),
        "blue": bool(getattr(u, "blue", False)),
        "protected": bool(getattr(u, "protected", False)),
        "followers": getattr(u, "followersCount", 0),
        "following": getattr(u, "friendsCount", 0),
        "tweets": getattr(u, "statusesCount", 0),
        "likes": getattr(u, "favouritesCount", 0),
        "listed": getattr(u, "listedCount", 0),
        "media_count": getattr(u, "mediaCount", 0),
        "created_at": created.isoformat() if created else None,
        "profile_image_url": getattr(u, "profileImageUrl", None),
        "banner_url": getattr(u, "profileBannerUrl", None),
    }


def _shape_photo(p: Any) -> dict:
    return {"type": "photo", "url": getattr(p, "url", None)}


def _shape_video_variant(v: Any) -> dict:
    return {
        "bitrate": getattr(v, "bitrate", None),
        "content_type": getattr(v, "contentType", None),
        "url": getattr(v, "url", None),
    }


def _shape_video(v: Any) -> dict:
    return {
        "type": "video",
        "thumbnail_url": getattr(v, "thumbnailUrl", None),
        "duration_ms": getattr(v, "duration", None),
        "views": getattr(v, "views", None),
        "variants": [_shape_video_variant(x) for x in (getattr(v, "variants", None) or [])],
    }


def _shape_animated(a: Any) -> dict:
    return {
        "type": "animated_gif",
        "thumbnail_url": getattr(a, "thumbnailUrl", None),
        "video_url": getattr(a, "videoUrl", None),
    }


def _shape_media(m: Any) -> dict:
    """twscrape 0.19: Tweet.media is ONE Media object with photos/videos/animated
    lists — NOT a flat list of media items. We surface each category separately."""
    if m is None:
        return None
    photos = [_shape_photo(p) for p in (getattr(m, "photos", None) or [])]
    videos = [_shape_video(v) for v in (getattr(m, "videos", None) or [])]
    animated = [_shape_animated(a) for a in (getattr(m, "animated", None) or [])]
    items = photos + videos + animated
    if not items:
        return None
    return {
        "count": len(items),
        "photos": photos,
        "videos": videos,
        "animated": animated,
    }


def _shape_tweet(t: Any) -> dict:
    # twscrape's primary text field is `rawContent` (Twitter's `note_tweet`
    # text or `full_text` fallback). There is NO `text` field — older
    # twscrape versions called it that, but 0.19 standardized on rawContent.
    text = getattr(t, "rawContent", None) or getattr(t, "text", None)
    date = getattr(t, "date", None)
    # inReplyToUser is a UserRef (with username/id) — capture the username
    # for readability, fall back to None.
    in_reply_to_user = getattr(t, "inReplyToUser", None)
    out: dict = {
        "id": str(getattr(t, "id", "")),
        "text": text,
        "created_at": date.isoformat() if date else None,
        "author": _shape_user(getattr(t, "user", None)),
        "lang": getattr(t, "lang", None),
        "url": getattr(t, "url", None),
        "stats": {
            "replies": getattr(t, "replyCount", 0),
            "retweets": getattr(t, "retweetCount", 0),
            "likes": getattr(t, "likeCount", 0),
            "quotes": getattr(t, "quoteCount", 0),
            # twscrape 0.19 field is `bookmarkedCount` (not `bookmarkCount`).
            "bookmarks": getattr(t, "bookmarkedCount", 0),
            "views": getattr(t, "viewCount", None),
        },
        "conversation_id": _str_or_none(getattr(t, "conversationId", None)),
        "in_reply_to_tweet_id": _str_or_none(getattr(t, "inReplyToTweetId", None)),
        "in_reply_to_screen_name": getattr(t, "inReplyToScreenName", None),
        "in_reply_to_user": getattr(in_reply_to_user, "username", None),
        "hashtags": list(getattr(t, "hashtags", None) or []),
        "cashtags": list(getattr(t, "cashtags", None) or []),
        "mentioned": [
            getattr(u, "username", None)
            for u in (getattr(t, "mentionedUsers", None) or [])
        ],
        "links": [getattr(x, "url", None) for x in (getattr(t, "links", None) or [])],
    }
    media = _shape_media(getattr(t, "media", None))
    if media:
        out["media"] = media
    # twscrape field names: `quotedTweet` + `retweetedTweet` (not quotedStatus
    # / retweetedStatus — that was the snscrape legacy name).
    quoted = getattr(t, "quotedTweet", None)
    if quoted:
        out["quoted"] = _shape_tweet(quoted)
    rt = getattr(t, "retweetedTweet", None)
    if rt:
        out["retweeted"] = _shape_tweet(rt)
    return out


def _str_or_none(v: Any) -> str | None:
    if v is None or v == "":
        return None
    return str(v)


# ── Client ─────────────────────────────────────────────────────────────────

class TwitterReadClient:
    """Multi-account twscrape wrapper. One instance per process — held in
    server.py's lifespan context. Round-robins across the configured accounts
    to spread Twitter's per-account rate limits."""

    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self._pool: AccountsPool | None = None
        self._api: API | None = None
        self._added = 0

    async def startup(self) -> None:
        s = self.settings
        if not s.accounts:
            if not s.allow_no_accounts:
                raise RuntimeError(
                    "no NITTER_ACCOUNT_*_AUTH_TOKEN/_CT0 env vars set — refusing "
                    "to start. Set NITTER_ALLOW_NO_ACCOUNTS=true to start anyway "
                    "for diagnostics (all tool calls will return a clear error)."
                )
            logger.warning("starting with zero accounts — all tool calls will error")

        # twscrape 0.19: AccountsPool.__init__(db_file=...) — NOT db_path.
        # SQLite file is opened lazily on first query, so this doesn't fail
        # even if /data doesn't exist yet (the volume mount creates it).
        self._pool = AccountsPool(db_file=s.db_path)
        for acc in s.accounts:
            try:
                # twscrape's add_account takes all 4 credential fields PLUS
                # an optional cookies STRING (HTTP Cookie header format, NOT
                # a dict — twscrape 0.19 parse_cookies() expects "k=v; k=v").
                # If both auth_token + ct0 are present in the string, twscrape
                # auto-marks the account active — no separate set_active call
                # needed. Password/email/email_password are fallback for the
                # full re-login flow twscrape triggers if the cookies fail.
                cookie_header = f"auth_token={acc.auth_token}; ct0={acc.ct0}"
                await self._pool.add_account(
                    username=acc.username,
                    password=acc.password or "_unknown_",
                    email=acc.email or "_unknown_@example.com",
                    email_password=acc.email_password or "_unknown_",
                    cookies=cookie_header,
                )
                self._added += 1
            except Exception as e:
                logger.error(f"failed to add account {acc.username!r}: {e}")

        logger.info(f"added {self._added}/{len(s.accounts)} account(s) to twscrape pool")
        if self._added == 0 and not s.allow_no_accounts:
            raise RuntimeError("zero accounts successfully added to pool")

        # login_all() is best-effort: twscrape validates cookies per-account
        # and continues past individual failures. Wrap in try/except so a
        # transient network failure during startup doesn't kill the service.
        try:
            await self._pool.login_all()
        except Exception as e:
            logger.warning(f"login_all() raised (will use seeded cookies directly): {e}")

        self._api = API(self._pool)
        logger.info(f"nitter-mcp ready: pool_size={self._added} db={s.db_path}")

    async def shutdown(self) -> None:
        # twscrape 0.19 AccountsPool has no close() method — it holds no
        # persistent connections (just a SQLite file path + in-memory locks).
        # Drop our references and let GC clean up. SQLite locks are released
        # when the connection object (internal to twscrape) is GC'd.
        self._pool = None
        self._api = None

    def _api_or_raise(self) -> API:
        if self._api is None:
            raise RuntimeError(
                "twscrape API not initialized (startup() failed or not called)"
            )
        return self._api

    def _clamp(self, limit: int) -> int:
        try:
            n = int(limit)
        except (TypeError, ValueError):
            n = self.settings.max_limit
        return max(1, min(n, self.settings.max_limit))

    # ── Tool methods ────────────────────────────────────────────────────

    async def search(self, query: str, limit: int = 20) -> dict:
        api = self._api_or_raise()
        n = self._clamp(limit)
        out = []
        # twscrape's limit is "at-least" not "at-most" — pages of ~20 items
        # get yielded whole, so a limit=3 can return 20+. Slice ourselves
        # to honor the agent's explicit ceiling.
        async for tweet in api.search(query, limit=n):
            out.append(_shape_tweet(tweet))
            if len(out) >= n:
                break
        return {"query": query, "count": len(out), "tweets": out}

    async def _user_by_login(self, username: str) -> Any:
        api = self._api_or_raise()
        username = username.lstrip("@")
        return await api.user_by_login(username)

    async def user_profile(self, username: str) -> dict:
        u = await self._user_by_login(username)
        return _shape_user(u) or {"username": username, "found": False}

    async def user_tweets(self, username: str, limit: int = 20) -> dict:
        api = self._api_or_raise()
        n = self._clamp(limit)
        u = await self._user_by_login(username)
        out = []
        async for t in api.user_tweets(u.id, limit=n):
            out.append(_shape_tweet(t))
            if len(out) >= n:
                break
        return {"username": u.username, "user_id": str(u.id), "count": len(out), "tweets": out}

    async def user_replies(self, username: str, limit: int = 50) -> dict:
        api = self._api_or_raise()
        n = self._clamp(limit)
        u = await self._user_by_login(username)
        out = []
        async for t in api.user_tweets_and_replies(u.id, limit=n):
            out.append(_shape_tweet(t))
            if len(out) >= n:
                break
        return {"username": u.username, "user_id": str(u.id), "count": len(out), "tweets": out}

    async def user_media(self, username: str, limit: int = 20) -> dict:
        api = self._api_or_raise()
        n = self._clamp(limit)
        u = await self._user_by_login(username)
        out = []
        # twscrape has no dedicated media endpoint; filter user_tweets to
        # those with non-empty Media attached. twscrape 0.19 Tweet.media is
        # a single Media dataclass with .photos/.videos/.animated lists —
        # we count it as a media tweet if any of those has items.
        async for t in api.user_tweets(u.id, limit=max(n * 2, 20)):
            media = getattr(t, "media", None)
            if media is None:
                continue
            has_media = (
                (getattr(media, "photos", None) or [])
                or (getattr(media, "videos", None) or [])
                or (getattr(media, "animated", None) or [])
            )
            if has_media:
                out.append(_shape_tweet(t))
                if len(out) >= n:
                    break
        return {"username": u.username, "user_id": str(u.id), "count": len(out), "tweets": out}

    async def user_following(self, username: str, limit: int = 200) -> dict:
        api = self._api_or_raise()
        n = self._clamp(limit)
        u = await self._user_by_login(username)
        out = []
        async for other in api.following(u.id, limit=n):
            out.append(_shape_user(other))
            if len(out) >= n:
                break
        return {"username": u.username, "user_id": str(u.id), "count": len(out), "following": out}

    async def user_followers(self, username: str, limit: int = 200) -> dict:
        api = self._api_or_raise()
        n = self._clamp(limit)
        u = await self._user_by_login(username)
        out = []
        async for other in api.followers(u.id, limit=n):
            out.append(_shape_user(other))
            if len(out) >= n:
                break
        return {"username": u.username, "user_id": str(u.id), "count": len(out), "followers": out}

    async def tweet_details(self, tweet_id: str) -> dict:
        api = self._api_or_raise()
        tid_str = str(tweet_id).strip()
        # twscrape's tweet_details signature is (twid: int). Strip any
        # leading/trailing non-digits (URLs, leading underscores from
        # paste-format) and convert. If the input isn't a clean int, fail
        # loud with a clear error rather than silently returning None.
        try:
            tid_int = int(tid_str)
        except ValueError:
            return {
                "tweet_id": tid_str, "found": False,
                "error": f"tweet_id must be a numeric ID, got {tid_str!r}",
            }
        tweet = await api.tweet_details(tid_int)
        if tweet is None:
            return {"tweet_id": tid_str, "found": False}
        out = _shape_tweet(tweet)
        out["found"] = True
        thread = getattr(tweet, "thread", None) or []
        if thread:
            out["thread"] = [
                _shape_tweet(t)
                for t in thread
                if str(getattr(t, "id", "")) != tid_str
            ]
        return out

    async def tweet_replies(self, tweet_id: str, limit: int = 50) -> dict:
        """Replies to a specific tweet.

        Two-path strategy:
          1. Try twscrape's ``tweet_details.thread`` (cheap; sometimes empty
             in twscrape 0.19 — the field exists but isn't always populated).
          2. If (1) yielded fewer than ``limit`` replies, fall back to
             ``search("to:<author> filter:replies")`` and filter to those
             whose ``in_reply_to_tweet_id`` matches. This is broader
             (Twitter returns ALL replies to the author in the window) so
             we over-fetch 3x and filter — slower but reliable.
        """
        api = self._api_or_raise()
        tid_str = str(tweet_id).strip()
        n = self._clamp(limit)
        try:
            tid_int = int(tid_str)
        except ValueError:
            return {
                "tweet_id": tid_str, "found": False, "replies": [],
                "error": f"tweet_id must be a numeric ID, got {tid_str!r}",
            }
        tweet = await api.tweet_details(tid_int)
        if tweet is None:
            return {"tweet_id": tid_str, "found": False, "replies": []}

        # Path 1: thread
        thread = getattr(tweet, "thread", None) or []
        replies = [
            _shape_tweet(t) for t in thread
            if str(getattr(t, "id", "")) != tid_str
        ]
        if len(replies) >= n:
            return {"tweet_id": tid_str, "found": True, "count": len(replies[:n]),
                    "replies": replies[:n], "source": "thread"}

        # Path 2: search fallback
        author = getattr(getattr(tweet, "user", None), "username", None)
        if author:
            seen = {r["id"] for r in replies}
            async for t in api.search(f"to:{author} filter:replies", limit=max(n * 3, 50)):
                if str(getattr(t, "inReplyToTweetId", "")) != tid_str:
                    continue
                rid = str(getattr(t, "id", ""))
                if rid in seen:
                    continue
                replies.append(_shape_tweet(t))
                seen.add(rid)
                if len(replies) >= n:
                    break
        return {"tweet_id": tid_str, "found": True, "count": len(replies),
                "replies": replies, "source": "search_fallback"}

    async def health(self) -> dict:
        # Pull live pool stats (total/active/inactive/locked counts) from
        # twscrape. Best-effort — if the DB is locked or unreachable, fall
        # back to the in-process counters.
        pool_stats: dict = {}
        if self._pool is not None:
            try:
                pool_stats = await self._pool.stats() or {}
            except Exception as e:
                pool_stats = {"stats_error": str(e)}
        return {
            "ok": self._api is not None,
            "pool_size": self._added,
            "configured_accounts": len(self.settings.accounts),
            "active_accounts": pool_stats.get("active"),
            "inactive_accounts": pool_stats.get("inactive"),
            "db_path": self.settings.db_path,
            "max_limit": self.settings.max_limit,
            "transport": "direct_twitter_graphql_via_twscrape",
        }
