#!/usr/bin/env python3
"""Read-only: open @Athletic_NA profile and dump recent tweets + profile meta.
Does NOT post, like, or follow anything.
"""
import json
import sys

sys.path.insert(0, "/sandbox/workspace/orgs/_shared/sandbox")

from twitter_helpers import twitter_session, is_logged_in  # noqa: E402

HANDLE = "Athletic_NA"
URL = f"https://x.com/{HANDLE}"

with twitter_session(headless=True, wait_seconds=4) as sb:
    sb.open(URL)
    sb.sleep(5)

    # If we got bounced to a login wall, report and stop.
    cur = sb.get_current_url()
    if any(p in cur for p in ["/login", "/i/flow/login", "/session/new"]):
        print(json.dumps({"error": "login_wall", "url": cur}))
        sys.exit(2)

    # --- Profile header metadata ---
    profile = {}
    try:
        name_el = sb.find_elements('[data-testid="UserName"]')
        if name_el:
            profile["user_card"] = name_el[0].text
    except Exception:
        pass
    for label, sel in [
        ("bio", '[data-testid="UserDescription"]'),
        ("following_count", f'a[href="/{HANDLE}/following"]'),
        ("followers_count", f'a[href="/{HANDLE}/verified_followers"]'),
    ]:
        try:
            els = sb.find_elements(sel)
            if els:
                profile[label] = els[0].text
        except Exception:
            pass

    # --- Recent tweets ---
    from twitter_helpers import read_tweets  # noqa: E402

    pairs = read_tweets(sb, max_tweets=8)

    out = {
        "logged_in": is_logged_in(sb),
        "url": cur,
        "profile": profile,
        "tweets": [{"author": a, "text": t} for a, t in pairs],
    }
    print(json.dumps(out, indent=2, ensure_ascii=False))
