#!/usr/bin/env bash
# yt_download.sh — YouTube downloader with Brave cookies (anti-bot bypass)
#
# Cookies exported from a real Brave browser give yt-dlp a browser identity
# so YouTube doesn't block it as a bot. The cookie file lives at:
#   data/brave-yt-cookies.txt (refreshed by refresh_cookies.sh)
#
# Usage:
#   ./yt_download.sh "URL" [output_dir]
#   ./yt_download.sh "search term" [output_dir]
#
# Examples:
#   ./yt_download.sh "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
#   ./yt_download.sh "rick astley never gonna give you up" ~/Videos
#
# To refresh cookies (when they expire or you log into a new site):
#   ./refresh_cookies.sh

set -euo pipefail
export PATH="$HOME/.deno/bin:$PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
COOKIE_FILE="$REPO_ROOT/data/brave-yt-cookies.txt"

URL="${1:?Usage: $0 'URL_or_search' [output_dir]}"
OUT_DIR="${2:-/tmp/yt-downloads}"
mkdir -p "$OUT_DIR"

# Search if not a URL
if [[ "$URL" != *"youtube.com"* && "$URL" != *"youtu.be"* && "$URL" != *"http"* ]]; then
    URL="ytsearch1:${URL}"
fi

COOKIE_ARGS=""
if [[ -f "$COOKIE_FILE" ]]; then
    COOKIE_ARGS="--cookies $COOKIE_FILE"
    echo "Using Brave cookies from: $COOKIE_FILE" >&2
else
    echo "WARNING: No cookie file at $COOKIE_FILE — YouTube may block downloads." >&2
    echo "Run $SCRIPT_DIR/refresh_cookies.sh to extract cookies from Brave." >&2
fi

echo "Downloading to: $OUT_DIR" >&2
yt-dlp \
    --remote-components ejs:github \
    $COOKIE_ARGS \
    -f "best[height<=1080]" \
    --merge-output-format mp4 \
    -o "$OUT_DIR/%(title)s.%(ext)s" \
    "$URL"

echo "" >&2
echo "=== Downloaded ===" >&2
ls -lh "$OUT_DIR"/
