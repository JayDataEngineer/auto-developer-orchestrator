"""Offline tests for the _shape_tweet / _shape_user / _shape_media helpers.

No network, no twscrape. Uses SimpleNamespace mocks that mimic twscrape 0.19's
dataclass models (the shapers only use getattr, so any object with the right
attributes works). Catches regressions when twscrape's field names shift.

Field reference (twscrape 0.19):
  Tweet:    id, id_str, url, date, user, lang, rawContent,
            replyCount, retweetCount, likeCount, quoteCount, bookmarkedCount,
            conversationId, hashtags, cashtags, mentionedUsers, links,
            media (single Media), viewCount, retweetedTweet, quotedTweet,
            inReplyToTweetId, inReplyToUser, inReplyToScreenName
  User:     id, username, displayname, rawDescription, created,
            followersCount, friendsCount, statusesCount, favouritesCount,
            listedCount, mediaCount, location, profileImageUrl,
            profileBannerUrl, verified, blue, protected, url
  Media:    photos: list[MediaPhoto], videos: list[MediaVideo],
            animated: list[MediaAnimated]
"""
from __future__ import annotations

import datetime as dt
from types import SimpleNamespace as NS

from src.twitter_client import (
    _shape_animated,
    _shape_media,
    _shape_tweet,
    _shape_user,
)


def _mk_user(username="alice", **overrides) -> NS:
    base = dict(
        id=123, username=username, displayname="Alice", rawDescription=None,
        location=None, url=f"https://x.com/{username}", verified=False,
        blue=False, protected=False, followersCount=10, friendsCount=20,
        statusesCount=5, favouritesCount=3, listedCount=1, mediaCount=2,
        created=None, profileImageUrl=None, profileBannerUrl=None,
    )
    base.update(overrides)
    return NS(**base)


def _mk_tweet(tid=999, **overrides) -> NS:
    base = dict(
        id=tid, id_str=str(tid), url=f"https://x.com/alice/status/{tid}",
        date=dt.datetime(2024, 6, 1, 10, 0, 0, tzinfo=dt.timezone.utc),
        user=_mk_user(), lang="en",
        rawContent="hello world",
        replyCount=2, retweetCount=3, likeCount=10, quoteCount=1,
        bookmarkedCount=0, viewCount="500",
        conversationId=tid, conversationIdStr=str(tid),
        hashtags=[], cashtags=[], mentionedUsers=[], links=[],
        media=None, retweetedTweet=None, quotedTweet=None,
        inReplyToTweetId=None, inReplyToTweetIdStr=None,
        inReplyToUser=None, inReplyToScreenName=None,
    )
    base.update(overrides)
    return NS(**base)


# ── _shape_user ────────────────────────────────────────────────────────────

def test_shape_user_minimal():
    out = _shape_user(_mk_user())
    assert out["id"] == "123"
    assert out["username"] == "alice"
    assert out["name"] == "Alice"  # twscrape field is `displayname`
    assert out["verified"] is False
    assert out["protected"] is False
    assert out["blue"] is False
    assert out["followers"] == 10
    assert out["following"] == 20
    assert out["tweets"] == 5
    assert out["likes"] == 3
    assert out["listed"] == 1
    assert out["media_count"] == 2
    assert out["created_at"] is None
    assert out["bio"] is None  # twscrape field is `rawDescription`


def test_shape_user_none_returns_none():
    assert _shape_user(None) is None


def test_shape_user_serializes_created_at_iso():
    created = dt.datetime(2024, 1, 15, 12, 30, 45, tzinfo=dt.timezone.utc)
    out = _shape_user(_mk_user(created=created))
    assert out["created_at"] == "2024-01-15T12:30:45+00:00"


# ── _shape_media ───────────────────────────────────────────────────────────

def test_shape_media_none_returns_none():
    assert _shape_media(None) is None


def test_shape_media_empty_returns_none():
    """Media object with no photos/videos/animated → None (no `media` key on tweet)."""
    empty_media = NS(photos=[], videos=[], animated=[])
    assert _shape_media(empty_media) is None


def test_shape_media_with_photos_only():
    media = NS(
        photos=[NS(url="https://pbs.twimg.com/a.jpg"),
                NS(url="https://pbs.twimg.com/b.jpg")],
        videos=[], animated=[],
    )
    out = _shape_media(media)
    assert out["count"] == 2
    assert len(out["photos"]) == 2
    assert out["photos"][0] == {"type": "photo", "url": "https://pbs.twimg.com/a.jpg"}
    assert out["videos"] == []
    assert out["animated"] == []


def test_shape_media_with_video_includes_variants():
    media = NS(
        photos=[],
        videos=[NS(
            thumbnailUrl="https://pbs.twimg.com/thumb.jpg",
            duration=8320,
            views=100,
            variants=[
                NS(contentType="video/mp4", bitrate=832000, url="https://v/lo.mp4"),
                NS(contentType="video/mp4", bitrate=2160000, url="https://v/hi.mp4"),
            ],
        )],
        animated=[],
    )
    out = _shape_media(media)
    assert out["count"] == 1
    v = out["videos"][0]
    assert v["type"] == "video"
    assert v["thumbnail_url"] == "https://pbs.twimg.com/thumb.jpg"
    assert v["duration_ms"] == 8320
    assert v["views"] == 100
    assert len(v["variants"]) == 2
    assert v["variants"][0]["bitrate"] == 832000
    assert v["variants"][0]["content_type"] == "video/mp4"
    assert v["variants"][1]["url"].endswith("hi.mp4")


def test_shape_animated_gif():
    a = NS(thumbnailUrl="https://x/thumb.jpg", videoUrl="https://x/gif.mp4")
    out = _shape_animated(a)
    assert out["type"] == "animated_gif"
    assert out["thumbnail_url"] == "https://x/thumb.jpg"
    assert out["video_url"] == "https://x/gif.mp4"


# ── _shape_tweet ───────────────────────────────────────────────────────────

def test_shape_tweet_basic():
    out = _shape_tweet(_mk_tweet())
    assert out["id"] == "999"
    assert out["text"] == "hello world"  # from rawContent
    assert out["lang"] == "en"
    assert out["url"] == "https://x.com/alice/status/999"
    assert out["stats"]["likes"] == 10
    assert out["stats"]["retweets"] == 3
    assert out["stats"]["replies"] == 2
    assert out["stats"]["quotes"] == 1
    assert out["stats"]["bookmarks"] == 0  # twscrape: bookmarkedCount
    assert out["stats"]["views"] == "500"
    assert out["author"]["username"] == "alice"
    assert out["created_at"] == "2024-06-01T10:00:00+00:00"
    assert out["conversation_id"] == "999"
    assert out["in_reply_to_tweet_id"] is None
    assert out["in_reply_to_user"] is None
    assert out["in_reply_to_screen_name"] is None
    assert "media" not in out  # no media attached
    assert "quoted" not in out
    assert "retweeted" not in out
    assert out["hashtags"] == []
    assert out["mentioned"] == []
    assert out["links"] == []


def test_shape_tweet_uses_raw_content_not_text():
    """twscrape 0.19 has no `text` field — primary is `rawContent`."""
    out = _shape_tweet(_mk_tweet(rawContent="the real text", text="legacy fallback"))
    assert out["text"] == "the real text"


def test_shape_tweet_falls_back_to_text_if_no_raw_content():
    """If a future twscrape version drops rawContent, fall back to text.
    Defensive getattr chain — we don't want the server to break on a
    twscrape rename."""
    out = _shape_tweet(_mk_tweet(rawContent=None, text="fallback text"))
    assert out["text"] == "fallback text"


def test_shape_tweet_with_media():
    media = NS(
        photos=[NS(url="https://pbs.twimg.com/x.jpg")],
        videos=[],
        animated=[],
    )
    out = _shape_tweet(_mk_tweet(media=media))
    assert out["media"]["count"] == 1
    assert out["media"]["photos"][0]["url"] == "https://pbs.twimg.com/x.jpg"


def test_shape_tweet_with_quoted_tweet():
    """twscrape field is `quotedTweet`, NOT `quotedStatus`."""
    quoted = _mk_tweet(tid=555, user=_mk_user("bob"), rawContent="quoted")
    out = _shape_tweet(_mk_tweet(quotedTweet=quoted))
    assert out["quoted"]["id"] == "555"
    assert out["quoted"]["text"] == "quoted"
    assert out["quoted"]["author"]["username"] == "bob"


def test_shape_tweet_with_retweeted_tweet():
    """twscrape field is `retweetedTweet`, NOT `retweetedStatus`."""
    rt = _mk_tweet(tid=888, user=_mk_user("carol"), rawContent="RT this")
    out = _shape_tweet(_mk_tweet(retweetedTweet=rt))
    assert out["retweeted"]["id"] == "888"
    assert out["retweeted"]["author"]["username"] == "carol"


def test_shape_tweet_reply_fields():
    """Replies use inReplyToTweetId + inReplyToScreenName + inReplyToUser (UserRef)."""
    reply_user = NS(username="dave", id=42)
    out = _shape_tweet(_mk_tweet(
        tid=1001,
        rawContent="@dave thanks",
        inReplyToTweetId=1000,
        inReplyToScreenName="dave",
        inReplyToUser=reply_user,
        conversationId=1000,
    ))
    assert out["in_reply_to_tweet_id"] == "1000"
    assert out["in_reply_to_screen_name"] == "dave"
    assert out["in_reply_to_user"] == "dave"  # username extracted from UserRef
    assert out["conversation_id"] == "1000"


def test_shape_tweet_empty_strings_become_none_for_ids():
    out = _shape_tweet(_mk_tweet(conversationId="", inReplyToTweetId=""))
    assert out["conversation_id"] is None
    assert out["in_reply_to_tweet_id"] is None


def test_shape_tweet_captures_hashtags_mentions_links():
    out = _shape_tweet(_mk_tweet(
        rawContent="hi @bob #tag $CASh https://t.co/x",
        hashtags=["tag"],
        cashtags=["CASh"],
        mentionedUsers=[NS(username="bob"), NS(username="carol")],
        links=[NS(url="https://t.co/x")],
    ))
    assert out["hashtags"] == ["tag"]
    assert out["cashtags"] == ["CASh"]
    assert out["mentioned"] == ["bob", "carol"]
    assert out["links"] == ["https://t.co/x"]
