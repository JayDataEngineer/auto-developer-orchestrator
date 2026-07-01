#!/usr/bin/env python3
"""
Twitter session manager — bootstrap and cookie handling.

Usage:
  python3 session.py --bootstrap                # Open browser for manual login (run via VNC)
  python3 session.py --check                    # Check if session cookies are valid
  python3 session.py --export                   # Export cookies from Chrome CDP
  python3 session.py --info                     # Show session info
  python3 session.py --cookies-from-browser NAME[:PROFILE]
                                                # Pull cookies from a host browser (yt-dlp pattern)

--cookies-from-browser mirrors yt-dlp's flag. NAME is one of:
  chrome, chromium, brave, edge, firefox, opera, vivaldi, safari, whale

Optional PROFILE selects a specific browser profile. Examples:
  --cookies-from-browser chrome
  --cookies-from-browser firefox:Personal
  --cookies-from-browser chrome:Profile 1

The host browser must already be logged into Twitter. No captcha solving
needed — the human solved it during normal browsing.
"""
import argparse
import json
import os
import sys
import time
from datetime import datetime

try:
    from paths import twitter_cookies as _cookies_path
    from paths import twitter_cookies_legacy as _cookies_legacy_path
except ImportError:
    _cookies_path = None
    _cookies_legacy_path = None


def _resolve_cookies_path():
    """Walk the candidate chain: canonical (data_dir) → legacy (in-container root).

    Returns the first existing non-empty file. Falls back to the canonical
    path for error messages when neither exists (so users see the right
    "expected at <canonical>" hint).
    """
    candidates = []
    if _cookies_path is not None:
        candidates.append(_cookies_path())
    if _cookies_legacy_path is not None:
        candidates.append(_cookies_legacy_path())
    for p in candidates:
        if p.exists() and p.stat().st_size > 0:
            return p
    return candidates[0] if candidates else None


def cookies_exist():
    p = _resolve_cookies_path()
    return p is not None and p.exists() and p.stat().st_size > 0


def load_cookies():
    if not cookies_exist():
        return None
    with open(_resolve_cookies_path()) as f:
        return json.load(f)


def save_cookies(cookies):
    # Always write to the canonical path so in-sandbox bootstraps (VNC login,
    # CDP export, --cookies-from-browser) keep working.
    if _cookies_path is None:
        raise RuntimeError("paths module not available; cannot resolve canonical cookies path")
    with open(_cookies_path(), "w") as f:
        json.dump(cookies, f, indent=2)


def check_session():
    """Check if saved cookies look valid (not expired)."""
    data = load_cookies()
    if not data:
        return {"valid": False, "reason": "No session file"}

    cookies = data if isinstance(data, list) else data.get("cookies", [])
    if not cookies:
        return {"valid": False, "reason": "Empty cookie list"}

    twitter_cookies = [c for c in cookies if "twitter" in c.get("domain", "") or "x.com" in c.get("domain", "")]
    if not twitter_cookies:
        return {"valid": False, "reason": "No twitter.com or x.com cookies found"}

    auth_token = any(c.get("name") == "auth_token" for c in twitter_cookies)
    ct0 = any(c.get("name") == "ct0" for c in twitter_cookies)

    saved_at = data.get("saved_at", "unknown") if isinstance(data, dict) else "unknown"

    return {
        "valid": auth_token and ct0,
        "twitter_cookies": len(twitter_cookies),
        "has_auth_token": auth_token,
        "has_ct0": ct0,
        "saved_at": saved_at,
        "reason": "Session looks valid" if (auth_token and ct0) else "Missing critical cookies (auth_token or ct0)",
    }


def bootstrap_interactive():
    """Launch browser for manual Twitter login. Run this via VNC.

    DISPLAY=:99 python3 session.py --bootstrap
    """
    try:
        from seleniumbase import SB
    except ImportError:
        print(json.dumps({"error": "seleniumbase not installed. Run: pip install seleniumbase"}))
        return

    print("Opening browser for Twitter login...")
    print("Log into your Twitter account, then press Enter in this terminal when done.")

    with SB(uc=True, headless=False) as sb:
        sb.open("https://x.com/login")
        sb.sleep(2)

        # Wait for user to log in manually
        input(">>> Press Enter AFTER you have logged in and see your home timeline <<<\n")

        # Verify we're logged in by checking the URL
        current_url = sb.get_current_url()
        if "login" in current_url:
            print("WARNING: Still on login page. Session may not be valid.")
        else:
            print(f"Current URL: {current_url}")

        # Extract cookies
        cookies = sb.driver.get_cookies()
        twitter_cookies = [c for c in cookies if "twitter" in c.get("domain", "") or "x.com" in c.get("domain", "")]

        session_data = {
            "cookies": cookies,
            "twitter_cookies_count": len(twitter_cookies),
            "saved_at": datetime.now().isoformat(),
            "source_url": current_url,
        }
        save_cookies(session_data)

        print(f"\nSaved {len(twitter_cookies)} Twitter cookies to {_cookies_path() if _cookies_path else '<unknown>'}")
        result = check_session()
        print(json.dumps(result, indent=2))


def export_from_cdp():
    """Export cookies from the running Chrome CDP instance in the sandbox.

    This works if Chrome is already open and logged into Twitter.
    python3 session.py --export
    """
    try:
        import requests
    except ImportError:
        print(json.dumps({"error": "requests not installed"}))
        return

    cdp_url = "http://127.0.0.1:9222"

    try:
        # Get the first tab's webSocketDebuggerUrl
        tabs = requests.get(f"{cdp_url}/json").json()
        if not tabs:
            print(json.dumps({"error": "No Chrome tabs found"}))
            return

        ws_url = tabs[0].get("webSocketDebuggerUrl", "")
        if not ws_url:
            print(json.dumps({"error": "No debugger URL found"}))
            return

        # Use CDP to get all cookies
        import subprocess
        # Use a simple node script to get cookies via CDP
        script = f"""
        const WebSocket = require('ws');
        const ws = new WebSocket('{ws_url}');
        ws.on('open', () => {{
            ws.send(JSON.stringify({{id: 1, method: 'Network.getAllCookies'}}));
        }});
        ws.on('message', (data) => {{
            const resp = JSON.parse(data);
            if (resp.id === 1) {{
                console.log(JSON.stringify(resp.result.cookies));
                ws.close();
            }}
        }});
        """

        result = subprocess.run(
            ["node", "-e", script],
            capture_output=True, text=True, timeout=10
        )

        if result.returncode != 0:
            # Fallback: try seleniumbase
            print(json.dumps({"error": f"CDP export failed: {result.stderr}"}))
            return

        all_cookies = json.loads(result.stdout)
        twitter_cookies = [c for c in all_cookies if "twitter" in c.get("domain", "") or "x.com" in c.get("domain", "")]

        session_data = {
            "cookies": all_cookies,
            "twitter_cookies_count": len(twitter_cookies),
            "saved_at": datetime.now().isoformat(),
            "source": "cdp_export",
        }
        save_cookies(session_data)

        print(f"Exported {len(twitter_cookies)} Twitter cookies")
        result = check_session()
        print(json.dumps(result, indent=2))

    except Exception as e:
        print(json.dumps({"error": str(e)}))


def parse_browser_flag(flag):
    """Parse 'chrome:Profile 1' → ('chrome', 'Profile 1') or 'firefox' → ('firefox', None).

    Mirrors yt-dlp's --cookies-from-browser NAME[:PROFILE][::CONTAINER] format.
    Container support (Firefox-only) is parsed but ignored — Twitter doesn't use containers.
    """
    if "::" in flag:
        flag = flag.split("::")[0]  # drop container
    if ":" in flag:
        name, profile = flag.split(":", 1)
        return name.strip(), profile.strip() or None
    return flag.strip(), None


def sync_from_browser(flag):
    """Pull Twitter cookies from a host browser — same pattern as yt-dlp --cookies-from-browser.

    Requires the `browser_cookie3` pip package. The host browser must already be
    logged into x.com. Cookies are read from the browser's SQLite cookie database.

    On Linux, browsers encrypt cookies with a keyring-backed key — the user's
    D-Bus session must be available (or run with the keyring unlocked). On macOS
    and Windows, the OS keychain handles this transparently.
    """
    try:
        import browser_cookie3
    except ImportError:
        print(json.dumps({
            "error": "browser_cookie3 not installed. Add to sandbox pip_packages.",
        }))
        return

    name, profile = parse_browser_flag(flag)
    name_lower = name.lower()

    loaders = {
        "chrome": browser_cookie3.chrome,
        "chromium": browser_cookie3.chromium,
        "brave": browser_cookie3.brave,
        "edge": browser_cookie3.edge,
        "firefox": browser_cookie3.firefox,
        "opera": browser_cookie3.opera,
        "vivaldi": browser_cookie3.vivaldi,
        "safari": browser_cookie3.safari,
    }

    loader = loaders.get(name_lower)
    if loader is None:
        print(json.dumps({
            "error": f"Unknown browser: {name}",
            "supported": sorted(loaders.keys()),
        }))
        return

    kwargs = {}
    if profile:
        # Firefox uses 'profile'; Chrome family uses 'profile_name'
        if name_lower == "firefox":
            kwargs["profile"] = profile
        else:
            kwargs["profile_name"] = profile

    try:
        # domain_name filters to x.com + twitter.com cookies
        cj = loader(domain_name="x.com", **kwargs)
    except Exception as e:
        print(json.dumps({
            "error": f"Failed to read cookies from {name}: {e}",
            "hint": "Browser must be installed and the user must have logged in to x.com at least once.",
        }))
        return

    cookies = []
    for c in cj:
        cookies.append({
            "name": c.name,
            "value": c.value,
            "domain": c.domain,
            "path": c.path,
            # browser_cookie3 returns secure as int (0/1); Chrome W3C wants bool
            "secure": bool(c.secure),
            "expires": getattr(c, "expires", None),
            "httponly": False,
        })

    twitter_cookies = [c for c in cookies if "twitter" in c.get("domain", "") or "x.com" in c.get("domain", "")]

    session_data = {
        "cookies": cookies,
        "twitter_cookies_count": len(twitter_cookies),
        "saved_at": datetime.now().isoformat(),
        "source": f"browser:{flag}",
    }
    save_cookies(session_data)

    result = check_session()
    result["synced_from"] = flag
    result["total_cookies"] = len(cookies)
    print(json.dumps(result, indent=2))


def main():
    parser = argparse.ArgumentParser(description="Twitter session manager")
    parser.add_argument("--bootstrap", action="store_true", help="Interactive login (run via VNC)")
    parser.add_argument("--check", action="store_true", help="Check saved session")
    parser.add_argument("--export", action="store_true", help="Export from running Chrome CDP")
    parser.add_argument("--info", action="store_true", help="Show session info")
    parser.add_argument(
        "--cookies-from-browser",
        metavar="NAME[:PROFILE]",
        help="Pull cookies from host browser (yt-dlp pattern): chrome, firefox, brave, edge, etc.",
    )
    args = parser.parse_args()

    if args.bootstrap:
        bootstrap_interactive()
    elif args.check:
        print(json.dumps(check_session(), indent=2))
    elif args.export:
        export_from_cdp()
    elif args.cookies_from_browser:
        sync_from_browser(args.cookies_from_browser)
    elif args.info:
        data = load_cookies()
        if data:
            print(f"Session file: {_resolve_cookies_path()}")
            print(f"Saved at: {data.get('saved_at', 'unknown')}")
            print(f"Source: {data.get('source', 'interactive')}")
            cookies = data.get("cookies", [])
            twitter = [c for c in cookies if "twitter" in c.get("domain", "") or "x.com" in c.get("domain", "")]
            print(f"Total cookies: {len(cookies)}")
            print(f"Twitter cookies: {len(twitter)}")
            for c in twitter[:5]:
                print(f"  {c['name']}: domain={c.get('domain', '')}")
        else:
            print("No session file found")
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
