#!/usr/bin/env bash
# refresh-egress-dns.sh — re-resolve hostnames in egress.conf + re-apply firewall.
#
# Companion to apply-egress-policy.sh. Runs periodically to refresh DNS-based
# allowlist entries when CDNs rotate IPs. Without this, a hostname that
# resolves to new IPs mid-session leaves the firewall allowing dead IPs and
# dropping live ones — the agent sees mysterious timeouts hours into a
# session because the firewall is using a stale snapshot from boot time.
#
# Format of egress.conf (produced by policy.EgressRules in Go):
#   # host: example.com       <- DNS-resolved hostname, refresh me
#   1.2.3.4 443
#   1.2.3.5 443
#   5.6.7.8 443               <- literal IP (no preceding host comment), copy verbatim
#
# This script:
#   1. Parses egress.conf into per-host blocks (via `# host:` markers)
#   2. For each DNS host, re-resolves via getent + emits new IPs
#   3. Literal-IP lines (no preceding host comment) are copied verbatim
#   4. Atomically replaces egress.conf
#   5. Calls apply-egress-policy.sh to flush + re-add iptables rules
#
# Fault tolerance: if re-resolution fails for a host, the OLD IP lines for
# that host are kept as-is. Better to keep working-but-stale IPs than to
# drop the firewall's allow entries on transient DNS failure.
#
# Idempotent: produces the same result whether called once or 100 times.
set -euo pipefail

EGRESS_CONF=/sandbox/workspace/.pux/egress.conf
LOG_PREFIX="egress-refresh:"

if [ ! -f "$EGRESS_CONF" ]; then
    echo "$LOG_PREFIX no policy staged ($EGRESS_CONF absent), nothing to refresh"
    exit 0
fi

if [ ! -s "$EGRESS_CONF" ]; then
    echo "$LOG_PREFIX policy staged but empty, nothing to refresh"
    exit 0
fi

# Associative arrays need bash 4+. The sandbox image ships bash 5+.
declare -A HOST_PORTS        # space-separated ports seen for this host
declare -A HOST_ORIG_LINES   # original "ip port" lines for fallback on DNS failure
declare -a ORDERED_HOSTS     # hosts in the order they appeared
LITERAL_ENTRIES=""           # newline-separated "ip port" lines without a preceding host

CURRENT_HOST=""
while IFS= read -r line || [ -n "$line" ]; do
    # Skip blank lines.
    [ -z "$line" ] && continue

    case "$line" in
        "# host: "*)
            # Comment marker for a DNS-resolved hostname. Strip prefix to
            # get the hostname. Marks the start of a host block — all
            # subsequent IP/PORT lines belong to this host until the next
            # marker (or end of file).
            CURRENT_HOST="${line#\# host: }"
            if [ -z "${HOST_PORTS[$CURRENT_HOST]+x}" ]; then
                ORDERED_HOSTS+=("$CURRENT_HOST")
                HOST_PORTS["$CURRENT_HOST"]=""
                HOST_ORIG_LINES["$CURRENT_HOST"]=""
            fi
            ;;
        \#*)
            # Other comments — skip silently.
            ;;
        *)
            # "<ip> <port>" line. ip = first token, port = remainder.
            ip="${line%% *}"
            port="${line#* }"
            if [ -z "$ip" ] || [ -z "$port" ]; then
                echo "$LOG_PREFIX WARN: malformed line '$line', skipping" >&2
                continue
            fi
            if [ -z "$CURRENT_HOST" ]; then
                # Literal IP entry — no preceding host comment, copy verbatim.
                LITERAL_ENTRIES="${LITERAL_ENTRIES}${ip} ${port}"$'\n'
            else
                HOST_PORTS["$CURRENT_HOST"]="${HOST_PORTS[$CURRENT_HOST]} ${port}"
                HOST_ORIG_LINES["$CURRENT_HOST"]="${HOST_ORIG_LINES[$CURRENT_HOST]}${ip} ${port}"$'\n'
            fi
            ;;
    esac
done < "$EGRESS_CONF"

# Re-emit to a temp file.
TMP="${EGRESS_CONF}.tmp.$$"
trap 'rm -f "$TMP"' EXIT

# Literals first (no host comment, just the IP lines).
if [ -n "$LITERAL_ENTRIES" ]; then
    printf '%s' "$LITERAL_ENTRIES" >> "$TMP"
fi

# Per-host with re-resolution.
for HOST in "${ORDERED_HOSTS[@]}"; do
    echo "# host: $HOST" >> "$TMP"
    # `|| true` because getent exits non-zero on resolution failure — under
    # `set -e` that would abort the script and leave egress.conf half-written.
    # Empty NEW_IPS falls through to the fallback path (keep old IPs).
    NEW_IPS=$(getent hosts "$HOST" 2>/dev/null | awk '{print $1}' | sort -u || true)
    if [ -z "$NEW_IPS" ]; then
        echo "$LOG_PREFIX WARN: re-resolve failed for $HOST, keeping old IPs" >&2
        printf '%s' "${HOST_ORIG_LINES[$HOST]}" >> "$TMP"
        continue
    fi
    # Dedupe + sort ports.
    PORTS=$(echo "${HOST_PORTS[$HOST]}" | tr ' ' '\n' | sort -u)
    for PORT in $PORTS; do
        [ -z "$PORT" ] && continue
        for IP in $NEW_IPS; do
            echo "$IP $PORT" >> "$TMP"
        done
    done
done

# Atomically replace + re-apply.
mv "$TMP" "$EGRESS_CONF"
trap - EXIT

echo "$LOG_PREFIX refreshed, re-applying firewall"

# Delegate to the boot script — it flushes OUTPUT + re-adds from the
# freshly-written conf. Same code path as boot ensures consistency.
exec bash /usr/local/bin/apply-egress-policy.sh
