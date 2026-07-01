#!/usr/bin/env python3
"""Host-side cookie extractor for the browser capability.

Why this exists
---------------
The browser capability runs Chrome inside the sandbox container. Cookie
databases for the user's real browser (Chrome, Brave, Edge, Firefox, etc.)
live on the HOST filesystem, and Chromium-based browsers encrypt cookie
values with a key stored in the user's GNOME keyring (D-Bus) or macOS
keychain. Neither the cookie file nor the keyring is reachable from inside
the sandbox container, so extraction MUST happen on the host.

This script is the host-side half of the cookie bridge. The kernel's
``restore_session`` browser tool is the in-sandbox half. Together they
let any browser-using org authenticate as the user without exposing
credentials inside the container.

Availability
------------
Lives at ``orgs/_shared/sandbox/extract_browser_cookies.py`` so every
browser-using org can wire it up via ``[[sandbox.bootstrap.host_setup]]``
in their ``org.toml``. Not specific to any one org — first shipped for
twitter-agent but the contract is generic. See the "Host-browser cookie
extraction" recipe in ``config/capabilities/browser/SKILL.md``.

Supported browsers
------------------
Chromium-based: chrome, brave, edge, chromium, opera, opera_gx, vivaldi
Firefox-based:  firefox
(Auto-detects flatpak installs of any of the above on Linux.)

Output shape (browser-session-compatible)
-----------------------------------------
  {
    "cookies": [{name, value, domain, path, secure, expires, httponly}, ...],
    "localStorage": {},
    "url": "https://<domain>",
    "saved_at": ISO-8601,
    "source": "<browser>",
    "domain": "<requested domain>"
  }

The cookies list is in the exact shape ``restore_session`` expects, so
the output file can be passed directly to that tool. Canonical home is
``data/.browser-session-<domain>.json`` (see ``paths.browser_session()``)
but the ``--out`` flag lets callers write anywhere.

Usage
-----
  python3 extract_browser_cookies.py --browser brave --domain example.com
  python3 extract_browser_cookies.py --browser chrome  --domain example.com --out /path/session.json
  python3 extract_browser_cookies.py --browser brave --domain example.com --check
  python3 extract_browser_cookies.py --list-browsers
  python3 extract_browser_cookies.py --browser brave --list-domains

Exits 0 on success, 1 on usage errors, 2 on cookie-file/browser missing.
"""
import argparse
import json
import os
import sys
from datetime import datetime
from pathlib import Path

# Flatpak cookie DB + Local State locations for each Chromium-based browser.
# Firefox uses a different layout (cookies.sqlite, no encryption key needed
# in the same way) — browser_cookie3.firefox handles it natively.
FLATPAK_PATHS = {
    "brave": (
        "~/.var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser/Default/Cookies",
        "~/.var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser/Local State",
    ),
    "chrome": (
        "~/.var/app/com.google.Chrome/config/google-chrome/Default/Cookies",
        "~/.var/app/com.google.Chrome/config/google-chrome/Local State",
    ),
    "chromium": (
        "~/.var/app/org.chromium.Chromium/config/chromium/Default/Cookies",
        "~/.var/app/org.chromium.Chromium/config/chromium/Local State",
    ),
    "edge": (
        "~/.var/app/com.microsoft.Edge/config/microsoft-edge/Default/Cookies",
        "~/.var/app/com.microsoft.Edge/config/microsoft-edge/Local State",
    ),
    "opera": (
        "~/.var/app/com.opera.Opera/config/opera/Default/Cookies",
        "~/.var/app/com.opera.Opera/config/opera/Local State",
    ),
    "vivaldi": (
        "~/.var/app/com.vivaldi.Vivaldi/config/vivaldi/Default/Cookies",
        "~/.var/app/com.vivaldi.Vivaldi/config/vivaldi/Local State",
    ),
}

DEFAULT_OUT = Path.cwd() / ".browser-session.json"


def _load_cookie_jar(browser, domain=None):
    """Return a CookieJar for the given browser, auto-detecting flatpak vs native."""
    try:
        import browser_cookie3 as bc3
    except ImportError as e:
        raise SystemExit(
            f"browser_cookie3 not installed: {e}. "
            "Run: uv pip install browser-cookie3 pycryptodomex"
        )

    fn = getattr(bc3, browser, None)
    if fn is None:
        raise SystemExit(
            f"Unknown browser {browser!r}. Supported: "
            f"{', '.join(sorted(['chrome','chromium','brave','edge','opera','opera_gx','vivaldi','firefox']))}"
        )

    # Auto-detect flatpak install (Linux only)
    flatpak = FLATPAK_PATHS.get(browser)
    kwargs = {}
    if flatpak:
        cookie_file, key_file = (os.path.expanduser(p) for p in flatpak)
        if os.path.exists(cookie_file) and os.path.exists(key_file):
            kwargs["cookie_file"] = cookie_file
            kwargs["key_file"] = key_file

    if domain:
        kwargs["domain_name"] = domain

    return fn(**kwargs)


def extract(browser, domain):
    """Read cookies for the given browser+domain. Returns list[dict] in restore_session shape."""
    cj = _load_cookie_jar(browser, domain)
    out = []
    for c in cj:
        rest = getattr(c, "_rest", {}) or {}
        out.append({
            "name": c.name,
            "value": c.value,
            "domain": c.domain,
            "path": c.path,
            "secure": bool(getattr(c, "secure", False)),
            "expires": getattr(c, "expires", None),
            "httponly": bool(rest.get("HttpOnly", False)),
        })
    return out


def list_domains(browser):
    """Print every distinct cookie domain the browser has stored."""
    cj = _load_cookie_jar(browser, domain=None)
    domains = sorted({c.domain.lstrip(".") for c in cj if c.domain})
    print(json.dumps({"browser": browser, "domains": domains, "count": len(domains)}, indent=2))


def write_session(cookies, browser, domain, out_path):
    """Write cookies + metadata to out_path in restore_session shape."""
    payload = {
        "cookies": cookies,
        "localStorage": {},
        "url": f"https://{domain}",
        "saved_at": datetime.now().isoformat(),
        "source": browser,
        "domain": domain,
    }
    out_path = Path(out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(payload, indent=2))
    return out_path


def main():
    parser = argparse.ArgumentParser(
        description="Extract cookies from any installed browser for a given domain.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    parser.add_argument("--browser",
                        help="Browser name: chrome, brave, edge, chromium, opera, opera_gx, vivaldi, firefox")
    parser.add_argument("--domain",
                        help="Cookie domain filter (e.g. example.com, linkedin.com). "
                             "Use --list-domains to see what's stored.")
    parser.add_argument("--out", default=str(DEFAULT_OUT),
                        help=f"Output JSON path (default: {DEFAULT_OUT})")
    parser.add_argument("--check", action="store_true",
                        help="Only print a summary, don't write the file")
    parser.add_argument("--list-domains", action="store_true",
                        help="List every domain the specified browser has cookies for, then exit")
    parser.add_argument("--list-browsers", action="store_true",
                        help="List supported browser names, then exit")
    args = parser.parse_args()

    if args.list_browsers:
        print(json.dumps({"browsers": sorted(FLATPAK_PATHS.keys()) + ["opera_gx", "firefox"]}, indent=2))
        return

    if not args.browser:
        parser.error("--browser is required (or use --list-browsers)")

    if args.list_domains:
        list_domains(args.browser)
        return

    if not args.domain:
        parser.error("--domain is required (or use --list-domains to discover available domains)")

    cookies = extract(args.browser, args.domain)

    summary = {
        "browser": args.browser,
        "domain": args.domain,
        "total_cookies": len(cookies),
        "cookie_names": sorted({c["name"] for c in cookies}),
    }

    if args.check:
        print(json.dumps(summary, indent=2))
        sys.exit(0 if cookies else 1)

    if not cookies:
        print(json.dumps({
            "error": f"No cookies found for {args.domain!r} in {args.browser}.",
            "hint": "Has the user logged in to this site via the specified browser?",
            **summary,
        }, indent=2), file=sys.stderr)
        sys.exit(2)

    out_path = write_session(cookies, args.browser, args.domain, args.out)
    summary["out_path"] = str(out_path)
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
