# TWITTER_RESEARCH

How to scrape Twitter/X using the injected cookie session.

## Session Bootstrap (one-time)

If `data/.twitter-session.json` does NOT exist, pull cookies from the host browser:

```bash
python3 plugins/twitter-automation/skills/twitter-automation/scripts/twitter_session.py --cookies-from-browser chrome
# or: --cookies-from-browser brave, firefox, firefox:Personal
```

This uses the yt-dlp pattern (`browser_cookie3` lib) to extract x.com cookies.

## Inject Cookies into sb_server

```python
import json
with open("data/.twitter-session.json") as f:
    cookies = json.load(f)

# Then via browser tools:
# 1. browse_to({url: "https://x.com"})
# 2. For each cookie: set_cookie({name: ..., value: ..., domain: ".x.com", ...})
# Or use restore_session({path: "data/.twitter-session.json"})
```

## Verify Auth Worked

After injection:
1. `browse_to({url: "https://x.com/home"})`
2. Look for the "Post" button or `@handle` in the nav
3. If you see "Log in" instead, session is invalid → re-pull cookies

## Pages Worth Scraping

| Page | URL | What you get |
|------|-----|--------------|
| Home timeline | `https://x.com/home` | Recent tweets from accounts you follow |
| Notifications | `https://x.com/notifications` | Mentions, replies |
| Trending | `https://x.com/explore/tabs/trending` | What's hot right now |
| Search | `https://x.com/search?q=<query>&f=live` | Real-time search |
| Profile | `https://x.com/<handle>` | Specific account's recent tweets |

## Reading Tweets with Images

When a tweet has an image:
1. `browser_screenshot` to capture the viewport
2. Use vision tools (describe_image / ocr) to extract text or describe content
3. Note the description in the JSON output as `image_desc`

## Avoid Bot Detection

- Don't scroll faster than ~2 seconds per page
- Add random delays (1-3 sec) between actions
- Don't navigate to >20 pages in one session
- If you hit a "Verify you're human" CAPTCHA, return error — don't try to solve

## Helper Library

`plugins/twitter-automation/skills/twitter-automation/scripts/twitter_helpers.py` provides:
- `twitter_session()` — context manager with pre-injected cookies
- `read_tweets(limit=20)` — read timeline tweets
- `is_logged_in()` — boolean check
- `scroll_for_more(times=3)` — scroll timeline

## Output Format

Always write to `research/twitter.json` (bind-mounted to host for review). See twitter-researcher role prompt for the schema.
