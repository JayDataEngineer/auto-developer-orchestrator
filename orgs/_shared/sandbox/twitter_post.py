#!/usr/bin/env python3
"""
Twitter posting script for The Grind & Read club.
Uses saved session cookies (from session.py --bootstrap) for authentication.

Modes:
  --tweet "text"    Post a tweet immediately
  --draft "text"    Save as draft without posting
  --post            Post next unposted tweet from calendar
  --status          Show recent posts and draft count
  --session         Check session status
"""
import argparse
import json
import os
import sys
import time
from datetime import datetime

try:
    from paths import (
        twitter_calendar as _calendar_path,
        twitter_drafts as _drafts_path,
        twitter_cookies as _cookies_path,
    )
except ImportError:
    _calendar_path = None
    _drafts_path = None
    _cookies_path = None


def load_json(path):
    if os.path.exists(path):
        with open(path) as f:
            return json.load(f)
    return []


def save_json(path, data):
    with open(path, "w") as f:
        json.dump(data, f, indent=2)


def save_draft(tweet_text, pillar="unknown"):
    drafts = load_json(str(_drafts_path()))
    draft = {
        "text": tweet_text,
        "pillar": pillar,
        "created_at": datetime.now().isoformat(),
        "char_count": len(tweet_text),
    }
    drafts.append(draft)
    save_json(str(_drafts_path()), drafts)
    return draft


def mark_calendar_posted(tweet_text):
    calendar = load_json(str(_calendar_path()))
    for entry in calendar:
        if entry.get("tweet") == tweet_text and not entry.get("posted"):
            entry["posted"] = True
            entry["posted_at"] = datetime.now().isoformat()
    save_json(str(_calendar_path()), calendar)


def has_session():
    """Check if session cookies exist and look valid."""
    p = _cookies_path()
    if p is None or not os.path.exists(p):
        return False
    try:
        with open(p) as f:
            data = json.load(f)
        cookies = data if isinstance(data, list) else data.get("cookies", [])
        return any(c.get("name") == "auth_token" for c in cookies)
    except Exception:
        return False


def post_tweet(tweet_text):
    """Post a tweet using SeleniumBase with saved session cookies."""
    if not has_session():
        draft = save_draft(tweet_text)
        return {
            "posted": False,
            "reason": "No Twitter session. Run: python3 /sandbox/session.py --bootstrap (via VNC)",
            "draft_saved": True,
            "text": tweet_text,
            "char_count": len(tweet_text),
        }

    if len(tweet_text) > 280:
        tweet_text = tweet_text[:277] + "..."

    try:
        from seleniumbase import SB

        # Load cookies
        with open(_cookies_path()) as f:
            session_data = json.load(f)
        cookies = session_data.get("cookies", session_data) if isinstance(session_data, dict) else session_data

        with SB(uc=True, headless=True) as sb:
            # Navigate to x.com first (required before adding cookies)
            sb.open("https://x.com")
            sb.sleep(2)

            # Inject saved cookies
            for cookie in cookies:
                try:
                    # Selenium needs specific cookie format
                    c = {
                        "name": cookie["name"],
                        "value": cookie["value"],
                        "domain": cookie.get("domain", ".x.com"),
                        "path": cookie.get("path", "/"),
                    }
                    if cookie.get("secure"):
                        c["secure"] = True
                    if cookie.get("httpOnly"):
                        c["httpOnly"] = True
                    sb.driver.add_cookie(c)
                except Exception:
                    continue

            # Refresh to apply cookies
            sb.open("https://x.com/home")
            sb.sleep(3)

            # Verify we're logged in
            current_url = sb.get_current_url()
            if "login" in current_url:
                # Session expired
                draft = save_draft(tweet_text)
                return {
                    "posted": False,
                    "reason": "Session expired — re-login needed via VNC",
                    "draft_saved": True,
                    "text": tweet_text,
                    "char_count": len(tweet_text),
                }

            # Click compose tweet button
            try:
                sb.click('[data-testid="tweetButtonInline"]', timeout=5)
            except Exception:
                # Try the compose area directly
                try:
                    sb.click('[data-testid="tweetTextarea_0"]', timeout=5)
                except Exception:
                    draft = save_draft(tweet_text)
                    return {
                        "posted": False,
                        "reason": "Could not find compose tweet area",
                        "draft_saved": True,
                        "text": tweet_text,
                        "char_count": len(tweet_text),
                    }

            sb.sleep(1)

            # Type the tweet
            sb.type('[data-testid="tweetTextarea_0"]', tweet_text)
            sb.sleep(1)

            # Click the tweet button
            sb.click('[data-testid="tweetButton"]')
            sb.sleep(3)

            return {
                "posted": True,
                "text": tweet_text,
                "char_count": len(tweet_text),
                "posted_at": datetime.now().isoformat(),
            }

    except ImportError:
        draft = save_draft(tweet_text)
        return {
            "posted": False,
            "reason": "seleniumbase not installed in sandbox",
            "draft_saved": True,
            "text": tweet_text,
            "char_count": len(tweet_text),
        }
    except Exception as e:
        draft = save_draft(tweet_text)
        return {
            "posted": False,
            "reason": f"Posting error: {str(e)}",
            "draft_saved": True,
            "text": tweet_text,
            "char_count": len(tweet_text),
        }


def show_status():
    calendar = load_json(str(_calendar_path()))
    drafts = load_json(str(_drafts_path()))
    posted = sum(1 for e in calendar if e.get("posted"))
    unposted = sum(1 for e in calendar if not e.get("posted"))
    return {
        "calendar_total": len(calendar),
        "posted": posted,
        "pending": unposted,
        "drafts": len(drafts),
        "session_active": has_session(),
    }


def main():
    parser = argparse.ArgumentParser(description="Twitter posting for The Grind & Read")
    parser.add_argument("--tweet", type=str, help="Post a tweet")
    parser.add_argument("--draft", type=str, help="Save as draft")
    parser.add_argument("--post", action="store_true", help="Post next unposted from calendar")
    parser.add_argument("--status", action="store_true", help="Show status")
    parser.add_argument("--session", action="store_true", help="Check session status")
    args = parser.parse_args()

    if args.status:
        print(json.dumps(show_status(), indent=2))
        return

    if args.session:
        print(json.dumps({"session_active": has_session()}, indent=2))
        return

    if args.tweet:
        result = post_tweet(args.tweet)
        print(json.dumps(result, indent=2))
        return

    if args.draft:
        draft = save_draft(args.draft)
        print(json.dumps({"draft_saved": True, **draft}, indent=2))
        return

    if args.post:
        calendar = load_json(str(_calendar_path()))
        for entry in calendar:
            if not entry.get("posted"):
                result = post_tweet(entry["tweet"])
                if result.get("posted"):
                    mark_calendar_posted(entry["tweet"])
                print(json.dumps(result, indent=2))
                return
        print(json.dumps({"error": "No unposted tweets in calendar"}))
        return

    parser.print_help()


if __name__ == "__main__":
    main()
