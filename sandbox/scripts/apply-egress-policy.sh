#!/usr/bin/env bash
# apply-egress-policy.sh — deny-by-default egress firewall for sandbox containers.
#
# Activation contract: when the harness creates a sandbox with a
# policy.yaml that declares an `egress.allow` block, it stages a flat-file
# allowlist at <project>/.pux/egress.conf. The project dir is bind-mounted
# at /sandbox/workspace, so the file shows up here. Each non-empty line in
# the conf is "<ip> <port>" — one host:port pair. "<ip>" is normally a
# literal IP (the harness pre-resolves DNS hostnames at sandbox-create time
# since DNS may not work inside the container after the firewall drops OUTPUT),
# but may be a container-resolved name like "host.docker.internal" — a
# Docker-internal name that can't be resolved on the host but IS in the
# container's /etc/hosts via the host-gateway ExtraHosts entry. We resolve
# those HERE at boot (see resolve_in_container below). Empty or absent
# conf = no enforcement.
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
# Comments (#) and blank lines are skipped. "<ip>" is normally a literal IP;
# a container-resolved name (host.docker.internal) is resolved to an IP HERE
# via /etc/hosts (local getent lookup — no DNS, so it works under
# deny-by-default) so bridge containers can reach host-side services.

# is_literal_ip returns 0 (true) if $1 looks like a literal IPv4 or IPv6
# address. IPv4: ≥3 dots (4 octets). IPv6: contains a colon.
is_literal_ip() {
    case "$1" in
        *:*)      return 0 ;; # IPv6
        *.*.*.*)  return 0 ;; # IPv4
        *)        return 1 ;;
    esac
}

# resolve_in_container maps a Docker-internal name to its /etc/hosts IP.
# getent is the canonical lookup; the awk fallback covers minimal images
# without libc-bin. Prints the first IP, empty on failure.
resolve_in_container() {
    local name="$1" ip
    ip="$(getent hosts "$name" 2>/dev/null | awk '{print $1}' | head -n1)"
    if [ -z "$ip" ]; then
        ip="$(awk -v h="$name" '$2==h||$3==h {print $1; exit}' /etc/hosts 2>/dev/null)"
    fi
    printf '%s' "$ip"
}

RULE_COUNT=0
while IFS=' ' read -r ip port || [ -n "$ip" ]; do
    # Skip blank lines and comments.
    [ -z "$ip" ] && continue
    case "$ip" in \#*) continue ;; esac

    if [ -z "$port" ]; then
        echo "$LOG_PREFIX WARN: rule '$ip' has no port, skipping" >&2
        continue
    fi

    # Container-resolved names arrive verbatim — resolve to an IP now via
    # /etc/hosts. Literal IPs skip this (already concrete).
    if ! is_literal_ip "$ip"; then
        resolved="$(resolve_in_container "$ip")"
        if [ -z "$resolved" ]; then
            echo "$LOG_PREFIX WARN: cannot resolve '$ip' via /etc/hosts, skipping" >&2
            continue
        fi
        echo "$LOG_PREFIX resolved $ip -> $resolved"
        ip="$resolved"
    fi

    iptables -A OUTPUT -d "$ip" -p tcp --dport "$port" -j ACCEPT
    echo "$LOG_PREFIX allow $ip:$port/tcp"
    RULE_COUNT=$((RULE_COUNT + 1))
done < "$EGRESS_CONF"

echo "$LOG_PREFIX applied $RULE_COUNT allow rules; OUTPUT policy is DROP"
