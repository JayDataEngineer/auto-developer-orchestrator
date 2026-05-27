#!/usr/bin/env bash
# E2E test: verify sandbox desktop has background + icons after EnableDesktopMode.
# Requires a running sandbox container (created by the orchestrator).
#
# Usage:
#   ./scripts/test-sandbox-desktop.sh                  # auto-detect running sandbox
#   ./scripts/test-sandbox-desktop.sh <container_name> # specific container

set -uo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass=0
fail=0

ok() { echo -e "${GREEN}PASS${NC} $1"; ((pass++)); }
err() { echo -e "${RED}FAIL${NC} $1"; ((fail++)); }
warn() { echo -e "${YELLOW}WARN${NC} $1"; }

# Find sandbox container
CONTAINER="${1:-}"
if [ -z "$CONTAINER" ]; then
    CONTAINER=$(docker ps --filter "ancestor=pux-sandbox:latest" --format "{{.Names}}" | head -1)
fi

if [ -z "$CONTAINER" ]; then
    echo "No running pux-sandbox container found. Start one first."
    exit 1
fi

echo "Testing sandbox desktop: $CONTAINER"
echo ""

# 1. pcmanfm is running
if docker exec "$CONTAINER" pgrep -x pcmanfm > /dev/null 2>&1; then
    ok "pcmanfm process is running"
else
    err "pcmanfm process NOT running"
fi

# 2. fluxbox rootCommand is empty (not clobbering pcmanfm)
ROOT_CMD=$(docker exec "$CONTAINER" grep "session.screen0.rootCommand" /root/.fluxbox/init 2>/dev/null | sed 's/.*rootCommand://' | xargs)
if [ -z "$ROOT_CMD" ]; then
    ok "fluxbox rootCommand is empty (pcmanfm manages desktop)"
else
    err "fluxbox rootCommand is '$ROOT_CMD' — will clobber pcmanfm on restart"
fi

# 3. Desktop icons exist
ICON_COUNT=$(docker exec "$CONTAINER" bash -c 'ls /root/Desktop/*.desktop 2>/dev/null | wc -l')
if [ "$ICON_COUNT" -ge 3 ]; then
    ok "desktop has $ICON_COUNT .desktop files"
else
    err "desktop only has $ICON_COUNT .desktop files (expected >= 3)"
fi

# 4. pcmanfm config has correct section header ([*] for pcmanfm 1.3)
SECTION=$(docker exec "$CONTAINER" head -1 /root/.config/pcmanfm/default/desktop-items-0.conf 2>/dev/null)
if [ "$SECTION" = "[*]" ]; then
    ok "pcmanfm config uses [*] section header (pcmanfm 1.3 compatible)"
else
    err "pcmanfm config section header is '$SECTION' — expected [*] (pcmanfm 1.3 ignores [desktop] and overwrites with defaults)"
fi

# 5. pcmanfm config has background or wallpaper color set
BG_COLOR=$(docker exec "$CONTAINER" grep -E "^(wallpaper|desktop_bg)=" /root/.config/pcmanfm/default/desktop-items-0.conf 2>/dev/null | head -1 | sed 's/.*=//')
if [ -n "$BG_COLOR" ]; then
    ok "pcmanfm background color set: $BG_COLOR"
else
    err "pcmanfm background color not configured"
fi

# 6. supervisord has pcmanfm-desktop with autorestart
AUTORESTART=$(docker exec "$CONTAINER" grep -A1 "program:pcmanfm-desktop" /etc/supervisor/conf.d/sandbox.conf 2>/dev/null | grep "autorestart" || true)
if echo "$AUTORESTART" | grep -q "true"; then
    ok "supervisord pcmanfm-desktop autorestart=true"
else
    warn "could not verify pcmanfm-desktop autorestart setting"
fi

# 7. fluxbox hasn't been excessively restarting (sanity check)
FLUXBOX_UPTIME=$(docker exec "$CONTAINER" supervisorctl status fluxbox 2>/dev/null | awk '{print $NF}' || echo "unknown")
if [ "$FLUXBOX_UPTIME" != "unknown" ]; then
    ok "fluxbox uptime: $FLUXBOX_UPTIME"
else
    warn "could not check fluxbox uptime"
fi

# 8. Verify desktop icons are readable (not corrupt)
CHROME_ICON=$(docker exec "$CONTAINER" test -r /root/Desktop/Google-Chrome.desktop && echo "ok" || echo "missing")
TERM_ICON=$(docker exec "$CONTAINER" test -r /root/Desktop/Terminal.desktop && echo "ok" || echo "missing")
FILE_ICON=$(docker exec "$CONTAINER" test -r /root/Desktop/File-Browser.desktop && echo "ok" || echo "missing")
if [ "$CHROME_ICON" = "ok" ] && [ "$TERM_ICON" = "ok" ] && [ "$FILE_ICON" = "ok" ]; then
    ok "all 3 desktop icons readable (Chrome, Terminal, File-Browser)"
else
    err "desktop icons missing: Chrome=$CHROME_ICON Terminal=$TERM_ICON File-Browser=$FILE_ICON"
fi

# 9. Trigger fluxbox restart and verify pcmanfm survives
echo ""
echo "Testing fluxbox restart resilience..."
docker exec "$CONTAINER" bash -c 'DISPLAY=:99 pkill -f fluxbox 2>/dev/null || true'
sleep 2
# supervisord should restart fluxbox
if docker exec "$CONTAINER" pgrep -x fluxbox > /dev/null 2>&1; then
    ok "fluxbox restarted by supervisord"
else
    err "fluxbox did NOT restart after kill"
fi
# pcmanfm should still be running
if docker exec "$CONTAINER" pgrep -x pcmanfm > /dev/null 2>&1; then
    ok "pcmanfm survived fluxbox restart"
else
    err "pcmanfm died after fluxbox restart"
fi

echo ""
echo "Results: $pass passed, $fail failed"
[ "$fail" -eq 0 ] && exit 0 || exit 1
