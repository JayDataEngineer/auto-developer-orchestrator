#!/usr/bin/env bash
# Deploy Twitter backbone scripts + saved session to /sandbox/ so
# twitter_post.py finds paths.py and the session file in the expected
# locations. Runs as a boot job after sb_server is up.
set -euo pipefail

SHARED=/sandbox/workspace/profiles/_shared/sandbox

# Copy backbone scripts to /sandbox/ (where paths.py expects to live)
for f in paths.py twitter_post.py twitter_session.py twitter_helpers.py; do
    if [ -f "$SHARED/$f" ]; then
        cp "$SHARED/$f" "/sandbox/$f"
        chmod 0555 "/sandbox/$f"
    fi
done

# Deploy saved session as fallback when live cookie extraction fails
# (Brave not installed, user not logged in, etc.)
mkdir -p /sandbox/workspace/data
SESSION_SRC="/sandbox/workspace/profiles/specialists/twitter-agent/data/.twitter-session.json"
if [ -f "$SESSION_SRC" ]; then
    cp "$SESSION_SRC" /sandbox/workspace/data/.twitter-session.json
fi

echo "Twitter scripts deployed to /sandbox/"
