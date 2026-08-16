"""
Twitter automation helpers — shared library for agent-written scripts.

These wrap the cookie-loading + SeleniumBase session dance so that
ad-hoc scripts (like_tweet.py, read_mentions.py, etc.) can be 5-10 lines
instead of 50. Designed to be obvious for fast/cheap models like
DeepSeek V4 Flash — explicit kwargs, no cleverness.

Usage in agent scripts:

    from twitter_helpers import twitter_session, read_tweets

    with twitter_session() as sb:
        sb.open("https://x.com/notifications/mentions")
        sb.sleep(3)
        tweets = read_tweets(sb)
        for author, text in tweets:
            print(f"{author}: {text}")

Cookie source:
    /sandbox/workspace/data/.twitter-session.json — populated by session.py
    (use `session.py --cookies-from-browser brave` first).
"""
import json
import os
from contextlib import contextmanager
from typing import Generator

try:
    from paths import twitter_cookies as _cookies_path
except ImportError:
    _cookies_path = None


def load_cookies() -> list[dict]:
    """Load saved Twitter cookies. Returns [] if no session file.

    Run `python3 /sandbox/session.py --cookies-from-browser brave` first
    if this returns empty — that pulls fresh cookies from the host browser.
    """
    if _cookies_path is None:
        return []
    p = _cookies_path()
    if not p.exists():
        return []
    with open(p) as f:
        data = json.load(f)
    if isinstance(data, list):
        return data
    return data.get("cookies", [])


def has_valid_session() -> bool:
    """True if auth_token + ct0 are present in saved cookies."""
    cookies = load_cookies()
    names = {c.get("name") for c in cookies}
    return "auth_token" in names and "ct0" in names


@contextmanager
def twitter_session(headless: bool = True, wait_seconds: int = 3) -> Generator:
    """Yield a SeleniumBase UC session with cookies pre-injected.

    Opens https://x.com first (required to set the domain before adding
    cookies), then injects all saved cookies, then yields the `sb` object.

    Caller navigates wherever it wants after — typically x.com/home,
    x.com/notifications/mentions, etc.

    Args:
        headless: True for CI/server, False for VNC debugging.
        wait_seconds: Seconds to sleep after initial page load (default 3).
                     Increase to 5-7 if you're hitting intermittent login walls.

    Yields:
        SeleniumBase SB instance. Use sb.open(url), sb.find_elements(sel),
        sb.click(sel), sb.type(sel, text), sb.get_current_url(), sb.get_page_source().

    Example:
        with twitter_session() as sb:
            sb.open("https://x.com/home")
            sb.sleep(3)
            tweets = sb.find_elements('[data-testid="tweetText"]')
    """
    from seleniumbase import SB

    cookies = load_cookies()
    if not cookies:
        cookies_file = _cookies_path() if _cookies_path else "unknown"
        raise RuntimeError(
            f"No Twitter session at {cookies_file}. "
            f"Run: python3 /sandbox/session.py --cookies-from-browser brave"
        )

    with SB(uc=True, headless=headless) as sb:
        # Must open x.com first to establish domain before add_cookie
        sb.open("https://x.com")
        sb.sleep(wait_seconds)

        injected = 0
        for cookie in cookies:
            try:
                sb.driver.add_cookie({
                    "name": cookie["name"],
                    "value": cookie["value"],
                    "domain": cookie.get("domain", ".x.com"),
                    "path": cookie.get("path", "/"),
                    # Chrome W3C requires bool, not int (browser_cookie3 returns int)
                    "secure": bool(cookie.get("secure", True)),
                })
                injected += 1
            except Exception:
                continue

        if not any(c["name"] == "auth_token" for c in cookies if c.get("name") == "auth_token"):
            raise RuntimeError(
                "auth_token missing from saved cookies — session is invalid. "
                "Re-run: python3 /sandbox/session.py --cookies-from-browser brave"
            )

        yield sb


def read_tweets(sb, max_tweets: int = 50) -> list[tuple[str, str]]:
    """Extract (author, text) pairs from the current page.

    Pairs by DOM order. Authors come from [data-testid="User-Name"],
    text from [data-testid="tweetText"]. If counts mismatch, the
    shorter list wins.

    Call this AFTER navigating to a page with tweets (timeline, mentions,
    search results, user profile).

    Args:
        sb: SeleniumBase instance from twitter_session().
        max_tweets: Cap on returned pairs (default 50).
    """
    try:
        tweet_els = sb.find_elements('[data-testid="tweetText"]')
    except Exception:
        tweet_els = []
    try:
        author_els = sb.find_elements('[data-testid="User-Name"]')
    except Exception:
        author_els = []

    pairs = []
    for i in range(min(len(tweet_els), len(author_els), max_tweets)):
        tweet = tweet_els[i].text.strip()
        author = author_els[i].text.strip()
        if tweet or author:
            pairs.append((author, tweet))
    return pairs


def scroll_for_more(sb, times: int = 3, sleep_per: int = 2) -> None:
    """Scroll down N times to load lazy-loaded content. No-op safe."""
    for _ in range(times):
        try:
            sb.scroll_to_bottom()
            sb.sleep(sleep_per)
        except Exception:
            break


def is_logged_in(sb) -> bool:
    """Heuristic: True if URL is not a login flow AND tweet elements exist."""
    url = sb.get_current_url()
    if any(p in url for p in ["/login", "/i/flow/login", "/session/new"]):
        return False
    try:
        return len(sb.find_elements('[data-testid="tweetText"]')) > 0
    except Exception:
        return False
