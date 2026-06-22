#!/usr/bin/env python3
"""
DuckDuckGo HTML search — tier-3 floor for the research capability.

Zero non-stdlib deps (urllib + re + json + argparse). Used by the bash-ddg
implementation of the research capability when the cloud web-research MCP is
unavailable. Returns snippet-level results only — no page fetching, no JS
rendering. See config/capabilities/research/prompts/bash-ddg.md.

Usage:
  python3 ddg.py 'latest gdp numbers'
  python3 ddg.py 'qwen 3 release date' --max 5
  python3 ddg.py 'site:arxiv.org diffusion transformers'

Output (stdout): JSON array of {title, url, snippet}. Empty array on no hits.
Errors (stderr): JSON {"error": "..."} + exit 1.

Notes:
  - DDG wraps result URLs in a redirector (//duckduckgo.com/l/?uddg=...).
    We decode uddg= to recover the real URL. Unwrapable URLs are kept as-is.
  - Snippets are HTML-escaped; we unescape basic entities.
  - DDG rate-limits aggressively. For >20 results, add --max explicitly and
    expect throttling. The script doesn't retry.
"""
import argparse
import html
import json
import re
import sys
import urllib.parse
import urllib.request

DDG_URL = "https://html.duckduckgo.com/html/"
USER_AGENT = "Mozilla/5.0 (compatible; pux-ddg/1.0)"


def _unwrap_ddg_redirect(href: str) -> str:
    """DDG wraps result URLs as //duckduckgo.com/l/?uddg=<encoded>. Decode."""
    if "uddg=" not in href:
        return href
    # Handle absolute, protocol-relative, and relative wrap URLs uniformly
    parsed = urllib.parse.urlparse(href if "://" in href else "http:" + href)
    qs = urllib.parse.parse_qs(parsed.query)
    targets = qs.get("uddg") or qs.get("rut")
    return targets[0] if targets else href


def _unescape(s: str) -> str:
    """Unescape HTML entities in DDG titles/snippets. Uses stdlib html.unescape."""
    return html.unescape(s)


def search(query: str, max_results: int = 8):
    body = urllib.parse.urlencode({"q": query}).encode()
    req = urllib.request.Request(
        DDG_URL,
        data=body,
        headers={"User-Agent": USER_AGENT, "Content-Type": "application/x-www-form-urlencoded"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        page = resp.read().decode("utf-8", errors="replace")

    # DDG result blocks: <a class="result__a" href="...">title</a>
    # followed by <a class="result__snippet" ...>snippet</a>
    results = []
    # Iterate over result blocks by splitting on result__url markers
    blocks = re.split(r'class="result__url"', page)[1:]
    for block in blocks:
        m_title = re.search(
            r'<a[^>]+class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>',
            block,
            re.DOTALL,
        )
        if not m_title:
            continue
        href = _unwrap_ddg_redirect(html.unescape(m_title.group(1)))
        title = _unescape(re.sub(r"<[^>]+>", "", m_title.group(2))).strip()
        m_snip = re.search(
            r'<a[^>]+class="result__snippet"[^>]*>(.*?)</a>',
            block,
            re.DOTALL,
        )
        snippet = ""
        if m_snip:
            snippet = _unescape(re.sub(r"<[^>]+>", "", m_snip.group(1))).strip()
        if title and href:
            results.append({"title": title, "url": href, "snippet": snippet})
        if len(results) >= max_results:
            break
    return results


def main():
    p = argparse.ArgumentParser(description="DuckDuckGo HTML search → JSON")
    p.add_argument("query", help="search query")
    p.add_argument("--max", type=int, default=8, help="max results (default 8)")
    args = p.parse_args()

    try:
        results = search(args.query, max_results=args.max)
    except Exception as e:
        print(json.dumps({"error": f"{type(e).__name__}: {e}"}), file=sys.stderr)
        sys.exit(1)

    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
