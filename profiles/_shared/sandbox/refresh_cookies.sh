#!/usr/bin/env bash
# refresh_cookies.sh — Extract YouTube/Google cookies from a remote Brave browser
#
# Saves to data/brave-yt-cookies.txt (Netscape format for yt-dlp).
# Also copies into the sandbox container for the browser agent.
#
# Environment:
#   BRAVE_SSH_HOST  SSH destination of the Brave host (REQUIRED — no default).
#                   Example: export BRAVE_SSH_HOST=user@brave-host
#   SB_CONTAINER    Sandbox container name (default: orchestrator-sandbox-mcp-default).
#
# Run this when:
#   - Cookies expire (downloads start failing with 403)
#   - You've logged into a new site in Brave
#   - You want fresh cookies before a download session

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OUT_FILE="$REPO_ROOT/data/brave-yt-cookies.txt"
CONTAINER="${SB_CONTAINER:-orchestrator-sandbox-mcp-default}"
SSH_HOST="${BRAVE_SSH_HOST:?Set BRAVE_SSH_HOST to your Brave host, e.g. user@brave-host}"

mkdir -p "$(dirname "$OUT_FILE")"

echo "Extracting YouTube/Google cookies from Brave on ${SSH_HOST}..."

ssh -o ConnectTimeout=5 "$SSH_HOST" 'python3 << "PYEOF"
import browser_cookie3 as bc3
import os

cookie_file = os.path.expanduser("~/.var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser/Default/Cookies")
key_file = os.path.expanduser("~/.var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser/Local State")

cj = bc3.brave(cookie_file=cookie_file, key_file=key_file)
lines = ["# Netscape HTTP Cookie File"]
count = 0
for c in cj:
    if "youtube" in c.domain or "google" in c.domain:
        domain = c.domain
        inc = "TRUE" if domain.startswith(".") else "FALSE"
        sec = "TRUE" if c.secure else "FALSE"
        exp = str(c.expires or 0)
        lines.append("\t".join([domain, inc, c.path, sec, exp, c.name, c.value]))
        count += 1
print(f"Extracted {count} cookies", flush=True)
print("---START---")
print("\n".join(lines))
PYEOF
' 2>/dev/null | sed -n '/^---START---$/,$ p' | tail -n +2 > "$OUT_FILE"

COUNT=$(grep -v '^#' "$OUT_FILE" | wc -l)
echo "Saved $COUNT cookies to $OUT_FILE"

# Copy to container
if docker ps --format '{{.Names}}' | grep -q "$CONTAINER"; then
    docker cp "$OUT_FILE" "$CONTAINER:/tmp/brave-yt-cookies.txt"
    docker exec "$CONTAINER" chmod 644 /tmp/brave-yt-cookies.txt
    echo "Copied to container $CONTAINER"
else
    echo "Container $CONTAINER not running — skipped container copy"
fi

echo "Done. yt_download.sh will now use these cookies automatically."
