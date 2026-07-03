#!/usr/bin/env bash
# egress-dns-refresh-loop.sh — sleep 5min + refresh-egress-dns.sh, forever.
#
# Supervisor-managed alternative to cron. Runs as its own program so we
# don't need to install cronie in the sandbox image. The loop:
#   - sleep 300s (5 min) — DNS TTLs on most CDNs are 60-300s, so 5 min
#     catches new IPs within ~1 TTL
#   - call refresh-egress-dns.sh — re-resolves + re-applies iptables
#   - swallow errors so the loop doesn't die on a single bad refresh
#
# Errors are loud in the logs but non-fatal to the loop. The supervisor
# restarts this if the process itself dies (autorestart=true).
set -euo pipefail

INTERVAL="${PUX_EGRESS_REFRESH_INTERVAL:-300}"

while true; do
    sleep "$INTERVAL"
    if ! /usr/local/bin/refresh-egress-dns.sh >/var/log/supervisor/egress-refresh.log 2>&1; then
        echo "egress-refresh-loop: refresh failed (non-fatal, will retry next cycle)" >&2
    fi
done
