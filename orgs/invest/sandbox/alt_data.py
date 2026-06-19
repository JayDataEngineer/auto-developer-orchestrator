"""
Alt-data cache wrapper — wraps web MCP responses in a local cache.

The world is not ephemeral: SEC filings, on-chain snapshots, news articles don't
change rapidly. Re-fetching the same URL 5 times in one session wastes compute.

This script provides a thin cache layer that:
  - Hashes (url|query) → filename
  - Stores JSON responses with TTL
  - Returns cached on hit, fetches on miss

Usage:
    # Cache-wrapped URL fetch
    python3 alt_data.py fetch --url "https://www.sec.gov/..." --ttl 3600

    # Cache-wrapped query (symbolic — actual web MCP call happens outside)
    python3 alt_data.py cache-lookup --key "sec_10k_aapl_2025"

    # List cache entries
    python3 alt_data.py list

    # Clear expired entries
    python3 alt_data.py gc
"""

import argparse
import hashlib
import json
import os
import sys
import time
from datetime import datetime
from pathlib import Path


CACHE_DIR = Path(os.environ.get(
    "ALT_DATA_CACHE_DIR",
    "/sandbox/workspace/cache/alt_data"
))


def cache_key(s: str) -> str:
    """SHA256 of input → 16-char hex key."""
    return hashlib.sha256(s.encode("utf-8")).hexdigest()[:16]


def cache_path(key: str) -> Path:
    return CACHE_DIR / f"{key}.json"


def cache_lookup(key: str, ttl: int = 3600):
    """Return cached value if exists and not expired, else None."""
    p = cache_path(key)
    if not p.exists():
        return None
    try:
        entry = json.loads(p.read_text())
        age = time.time() - entry.get("cached_at", 0)
        if age > ttl:
            return None
        return entry
    except Exception:
        return None


def cache_store(key: str, source: str, data):
    """Store data in cache with metadata."""
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    entry = {
        "key": key,
        "source": source,
        "cached_at": time.time(),
        "cached_at_iso": datetime.now().isoformat(),
        "data": data,
    }
    cache_path(key).write_text(json.dumps(entry, indent=2, default=str))
    return entry


def list_entries():
    """List all cache entries."""
    if not CACHE_DIR.exists():
        return []
    entries = []
    for p in CACHE_DIR.glob("*.json"):
        try:
            entry = json.loads(p.read_text())
            entries.append({
                "key": entry.get("key"),
                "source": entry.get("source"),
                "cached_at_iso": entry.get("cached_at_iso"),
                "age_seconds": round(time.time() - entry.get("cached_at", 0), 0),
                "size_bytes": p.stat().st_size,
            })
        except Exception:
            continue
    return entries


def gc(ttl: int = 86400):
    """Garbage-collect entries older than ttl seconds."""
    if not CACHE_DIR.exists():
        return {"deleted": 0}
    deleted = 0
    now = time.time()
    for p in CACHE_DIR.glob("*.json"):
        try:
            entry = json.loads(p.read_text())
            if now - entry.get("cached_at", 0) > ttl:
                p.unlink()
                deleted += 1
        except Exception:
            p.unlink()
            deleted += 1
    return {"deleted": deleted, "ttl_seconds": ttl}


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="cmd")

    p_fetch = sub.add_parser("fetch", help="Fetch URL (caller must implement actual HTTP)")
    p_fetch.add_argument("--url", required=True)
    p_fetch.add_argument("--ttl", type=int, default=3600)

    p_lookup = sub.add_parser("cache-lookup", help="Check if key exists in cache")
    p_lookup.add_argument("--key", required=True)
    p_lookup.add_argument("--ttl", type=int, default=3600)

    p_store = sub.add_parser("cache-store", help="Store JSON data (read from stdin) under a key")
    p_store.add_argument("--key", required=True)
    p_store.add_argument("--source", default="manual")

    sub.add_parser("list", help="List all cache entries")
    p_gc = sub.add_parser("gc", help="Garbage-collect old entries")
    p_gc.add_argument("--ttl", type=int, default=86400)

    args = parser.parse_args()

    if args.cmd == "fetch":
        # This script is a cache front-end; the actual HTTP fetch is delegated
        # to the calling agent (which has the web MCP tools). We just compute
        # the cache key and tell the agent where to look first.
        key = cache_key(args.url)
        cached = cache_lookup(key, args.ttl)
        if cached:
            print(json.dumps({
                "cache_hit": True,
                "key": key,
                "url": args.url,
                "age_seconds": round(time.time() - cached["cached_at"], 0),
                "data": cached["data"],
            }, indent=2, default=str))
        else:
            print(json.dumps({
                "cache_hit": False,
                "key": key,
                "url": args.url,
                "instruction": f"Fetch via web MCP scrape/research, then store with: alt_data.py cache-store --key {key}",
            }, indent=2))

    elif args.cmd == "cache-lookup":
        cached = cache_lookup(args.key, args.ttl)
        if cached:
            print(json.dumps({"cache_hit": True, **cached}, indent=2, default=str))
        else:
            print(json.dumps({"cache_hit": False, "key": args.key}, indent=2))

    elif args.cmd == "cache-store":
        data = json.load(sys.stdin)
        entry = cache_store(args.key, args.source, data)
        print(json.dumps({"stored": True, "key": args.key, "path": str(cache_path(args.key))}, indent=2))

    elif args.cmd == "list":
        entries = list_entries()
        print(json.dumps({"count": len(entries), "entries": entries}, indent=2, default=str))

    elif args.cmd == "gc":
        result = gc(args.ttl)
        print(json.dumps(result, indent=2))

    else:
        parser.print_help()


if __name__ == "__main__":
    main()
