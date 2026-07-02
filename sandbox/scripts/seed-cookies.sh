#!/usr/bin/env bash
# Seed the in-sandbox browser with cookies decoded from a base64 env var
# before the agent runs. The operator exports a cookie JSON (as emitted by
# extract_browser_cookies.py --b64) under a var named in policy.yaml's
# browser.cookies_env field. The Go policy hook injects that var into the
# container env AND sets SEED_COOKIES_ENV=<name> so this script knows where
# to look.
#
# Cookies never touch the filesystem inside the container — decoded into a
# shell variable, POSTed to sb_server.py, GC'd when the script exits.
#
# Exits 0 silently when no cookies env is set (preserves default behavior
# for orgs that don't opt into browser pre-seed).
set -euo pipefail

SB=http://127.0.0.1:9876

# No pointer var = nothing to do. Don't log — supervisor treats stderr as
# an error signal and we want silent no-op for orgs that don't use this.
if [ -z "${SEED_COOKIES_ENV:-}" ]; then
    exit 0
fi

# Resolve the actual env var name (e.g. SEED_COOKIES_ENV=TWITTER_COOKIES_B64).
COOKIES_B64="${!SEED_COOKIES_ENV:-}"
if [ -z "$COOKIES_B64" ]; then
    echo "seed-cookies: $SEED_COOKIES_ENV is empty — nothing to seed" >&2
    exit 0
fi

# Wait for sb_server.py to come up. Polls /status for up to 30s.
for _ in $(seq 1 60); do
    if curl -sf "$SB/status" >/dev/null 2>&1; then
        break
    fi
    sleep 0.5
done
if ! curl -sf "$SB/status" >/dev/null 2>&1; then
    echo "seed-cookies: sb_server.py not reachable at $SB after 30s" >&2
    exit 1
fi

# Decode the base64 into a shell variable. We want the JSON to flow straight
# into the curl payload without ever landing on disk. jq reformats + validates
# that what we decoded really is JSON (catches a corrupt env value before we
# send garbage to the browser).
COOKIES_JSON="$(printf '%s' "$COOKIES_B64" | base64 -d)"
if ! printf '%s' "$COOKIES_JSON" | jq -e 'if type == "array" then . else .cookies // empty end' >/dev/null 2>&1; then
    echo "seed-cookies: decoded value is not valid JSON (check $SEED_COOKIES_ENV)" >&2
    exit 1
fi

# Normalize: accept either a bare cookie array or {cookies: [...]} wrapper.
COOKIES_ARR="$(printf '%s' "$COOKIES_JSON" | jq -c 'if type == "array" then . else .cookies end')"
if [ "$COOKIES_ARR" = "null" ] || [ "$COOKIES_ARR" = "[]" ]; then
    echo "seed-cookies: cookie list is empty" >&2
    exit 0
fi

# Build the POST body once, then hand it to curl. An earlier revision nested
# `$(jq ...)` directly inside curl's `--data-binary "$(...)"`; with multi-KB
# payloads that form hung and sb_server.py reported "Extra data" past the
# JSON end. Pre-computing the payload var avoids the nested-substitution
# path entirely.
PAYLOAD="$(jq -nc --argjson cookies "$COOKIES_ARR" '{action:"set",cookies:$cookies}')"

# POST the bulk list. sb_server.py's /cookies action=set accepts
# {"action":"set","cookies":[...]}.
RESP="$(curl -sS -X POST "$SB/cookies" \
    -H 'content-type: application/json' \
    --data-binary "$PAYLOAD")"
if ! printf '%s' "$RESP" | jq -e '.ok == true' >/dev/null 2>&1; then
    echo "seed-cookies: POST failed: $RESP" >&2
    exit 1
fi

COUNT="$(printf '%s' "$RESP" | jq -r '.count // .name // "?"')"
echo "seed-cookies: injected via $SEED_COOKIES_ENV ($COUNT cookies)"
