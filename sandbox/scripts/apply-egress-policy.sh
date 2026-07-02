#!/usr/bin/env bash
# apply-egress-policy.sh — deny-by-default egress firewall for sandbox containers.
#
# Activation contract: when the Go MCP server creates a sandbox with a
# policy.yaml that declares an `egress.allow` block, it stages a flat-file
# allowlist at <project>/.pux/egress.conf. The project dir is bind-mounted
# at /sandbox/workspace, so the file shows up here. Each non-empty line in
# the conf is "<ip> <port>" — one host:port pair, hostname pre-resolved by
# the Go side (DNS may not work inside the container after the firewall
# drops OUTPUT). Empty or absent conf = no enforcement.
#
# This script runs once at boot via supervisord. autorestart=false so the
# iptables rules persist after exit. Errors are loud: a partial firewall
# (e.g. couldn't set OUTPUT policy to DROP) aborts the script with a
# non-zero exit and supervisor logs the failure.
#
# We always allow: loopback, established/related connections, DNS (port 53
# udp+tcp). DNS is critical because the sandbox may still need to resolve
# names for outbound connections even though the policy listed IPs.
set -euo pipefail

EGRESS_CONF=/sandbox/workspace/.pux/egress.conf
LOG_PREFIX="egress-firewall:"

if [ ! -f "$EGRESS_CONF" ]; then
    echo "$LOG_PREFIX no policy staged ($EGRESS_CONF absent), exiting"
    exit 0
fi

if [ ! -s "$EGRESS_CONF" ]; then
    echo "$LOG_PREFIX policy staged but empty, exiting"
    exit 0
fi

echo "$LOG_PREFIX applying deny-by-default + allowlist from $EGRESS_CONF"

# Reset OUTPUT chain to a clean state, then deny by default.
iptables -F OUTPUT
iptables -P OUTPUT DROP

# Always allow: loopback, established connections, DNS.
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT

# Apply each rule from the staged conf. Format: one "<ip> <port>" per line.
# Comments (#) and blank lines are skipped.
RULE_COUNT=0
while IFS=' ' read -r ip port || [ -n "$ip" ]; do
    # Skip blank lines and comments.
    [ -z "$ip" ] && continue
    case "$ip" in \#*) continue ;; esac

    if [ -z "$port" ]; then
        echo "$LOG_PREFIX WARN: rule '$ip' has no port, skipping" >&2
        continue
    fi

    iptables -A OUTPUT -d "$ip" -p tcp --dport "$port" -j ACCEPT
    echo "$LOG_PREFIX allow $ip:$port/tcp"
    RULE_COUNT=$((RULE_COUNT + 1))
done < "$EGRESS_CONF"

echo "$LOG_PREFIX applied $RULE_COUNT allow rules; OUTPUT policy is DROP"
