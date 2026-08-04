"""Offline tests for TwitterReadClient — verifies the clamp, the _api_or_raise
guard, and that tool methods shape twscrape output correctly.

No network, no twscrape API. We construct a TwitterReadClient with a stub
pool + API that yields canned mock tweets, then assert the shaped output.
"""
from __future__ import annotations

import datetime as dt
from types import SimpleNamespace as NS
from unittest.mock import AsyncMock, MagicMock

import pytest

from src.settings import Settings
from src.twitter_client import TwitterReadClient


def _mk_user(username="alice") -> NS:
    return NS(
        id=2, username=username, displayname="Alice", rawDescription=None,
        location=None, url=f"https://x.com/{username}", verified=False,
        blue=False, protected=False, followersCount=10, friendsCount=20,
        statusesCount=5, favouritesCount=3, listedCount=1, mediaCount=2,
        created=None, profileImageUrl=None, profileBannerUrl=None,
    )


def _mk_tweet(tid=1, **overrides) -> NS:
    base = dict(
        id=tid, id_str=str(tid), url=f"https://x.com/alice/status/{tid}",
        date=None, user=_mk_user(), lang="en", rawContent="hello",
        replyCount=0, retweetCount=0, likeCount=5, quoteCount=0,
        bookmarkedCount=0, viewCount=None,
        conversationId=tid, conversationIdStr=str(tid),
        hashtags=[], cashtags=[], mentionedUsers=[], links=[],
        media=None, retweetedTweet=None, quotedTweet=None,
        inReplyToTweetId=None, inReplyToTweetIdStr=None,
        inReplyToUser=None, inReplyToScreenName=None,
    )
    base.update(overrides)
    return NS(**base)


def _make_settings(**overrides) -> Settings:
    """Bypass env-var discovery — just construct Settings + assign accounts."""
    s = Settings()
    s.accounts = overrides.pop("accounts", [])
    for k, v in overrides.items():
        setattr(s, k, v)
    return s


def _make_client(**settings_overrides) -> TwitterReadClient:
    client = TwitterReadClient(_make_settings(**settings_overrides))
    # Skip startup(); install a stub API.
    client._api = MagicMock()
    client._added = 1
    return client


# ── clamp ──────────────────────────────────────────────────────────────────

def test_clamp_respects_max_limit():
    client = _make_client(max_limit=200)
    assert client._clamp(0) == 1       # min 1
    assert client._clamp(-5) == 1      # negative → 1
    assert client._clamp(50) == 50
    assert client._clamp(200) == 200
    assert client._clamp(1000) == 200  # capped
    assert client._clamp("not a number") == 200  # bad input → max


def test_clamp_custom_max_limit():
    client = _make_client(max_limit=50)
    assert client._clamp(100) == 50


# ── startup guards ─────────────────────────────────────────────────────────

def test_api_or_raise_when_not_started():
    client = TwitterReadClient(_make_settings())
    with pytest.raises(RuntimeError, match="not initialized"):
        client._api_or_raise()


# ── tool method behaviors ─────────────────────────────────────────────────

@pytest.mark.asyncio
async def test_search_shapes_results():
    client = _make_client()

    async def _aiter(*a, **kw):
        yield _mk_tweet(tid=1, rawContent="hello", likeCount=5)

    client._api.search = _aiter
    out = await client.search("hello", limit=10)
    assert out["query"] == "hello"
    assert out["count"] == 1
    assert out["tweets"][0]["text"] == "hello"
    assert out["tweets"][0]["author"]["username"] == "alice"
    assert out["tweets"][0]["stats"]["likes"] == 5


@pytest.mark.asyncio
async def test_user_replies_uses_tweets_and_replies_endpoint():
    """user_replies must call api.user_tweets_and_replies (NOT api.user_tweets)."""
    client = _make_client()

    async def _empty_aiter(*a, **kw):
        if False:  # never yields
            yield None

    client._api.user_tweets_and_replies = _empty_aiter
    client._api.user_by_login = AsyncMock(
        return_value=NS(id=42, username="alice")
    )

    out = await client.user_replies("alice", limit=5)
    assert out["username"] == "alice"
    assert out["user_id"] == "42"
    assert out["count"] == 0
    client._api.user_by_login.assert_awaited_once_with("alice")


@pytest.mark.asyncio
async def test_user_media_filters_to_tweets_with_media_only():
    """twscrape 0.19: Tweet.media is a single Media dataclass with .photos/
    .videos/.animated lists. user_media must check those, not just truthiness
    of Tweet.media (which is always truthy — it's an object)."""
    client = _make_client()
    media_with_photo = NS(photos=[NS(url="http://x/y.jpg")], videos=[], animated=[])
    media_empty = NS(photos=[], videos=[], animated=[])
    tweet_with_media = _mk_tweet(tid=1, media=media_with_photo)
    tweet_without_media = _mk_tweet(tid=2, media=media_empty)
    tweet_no_media_attr = _mk_tweet(tid=3, media=None)

    async def _aiter(*a, **kw):
        for t in [tweet_with_media, tweet_without_media, tweet_no_media_attr, tweet_with_media]:
            yield t

    client._api.user_tweets = _aiter
    client._api.user_by_login = AsyncMock(return_value=NS(id=1, username="u"))

    out = await client.user_media("u", limit=5)
    assert out["count"] == 2  # only the two with photo media
    assert all("media" in t for t in out["tweets"])


@pytest.mark.asyncio
async def test_tweet_details_returns_found_false_when_missing():
    client = _make_client()
    client._api.tweet_details = AsyncMock(return_value=None)
    out = await client.tweet_details("999")
    assert out == {"tweet_id": "999", "found": False}


@pytest.mark.asyncio
async def test_tweet_replies_excludes_self_from_thread():
    """tweet_replies iterates the main tweet's .thread list (if twscrape
    populated one) and excludes the target tweet itself."""
    client = _make_client()
    target_id = 100
    self_tweet = _mk_tweet(tid=100, rawContent="main")
    reply1 = _mk_tweet(tid=101, rawContent="reply 1",
                       inReplyToTweetId=100, inReplyToScreenName="alice")
    reply2 = _mk_tweet(tid=102, rawContent="reply 2",
                       inReplyToTweetId=100, inReplyToScreenName="alice")
    # twscrape's tweet_details returns the main tweet with .thread populated
    self_tweet.thread = [reply1, reply2]
    client._api.tweet_details = AsyncMock(return_value=self_tweet)

    out = await client.tweet_replies(str(target_id), limit=10)
    assert out["found"] is True
    assert out["count"] == 2
    ids = [r["id"] for r in out["replies"]]
    assert ids == ["101", "102"]
    assert "100" not in ids  # self excluded


@pytest.mark.asyncio
async def test_health_reports_pool_size_and_transport():
    client = _make_client()
    client._added = 3
    # pool is None (we bypassed startup) — health should not crash, just
    # skip the live stats call.
    out = await client.health()
    assert out["ok"] is True
    assert out["pool_size"] == 3
    assert out["transport"] == "direct_twitter_graphql_via_twscrape"
    assert "max_limit" in out
    assert "db_path" in out
    # active_accounts/inactive_accounts may be absent (no pool to query)
    # or None — both are acceptable
