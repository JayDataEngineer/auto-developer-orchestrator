package policy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// containerResolvedHosts lists hostnames that cannot be resolved on the host
// (where Load + EgressRules run — the operator's machine) but ARE resolvable
// inside the sandbox container via /etc/hosts. Docker writes these entries at
// container create from hostConfig.ExtraHosts; the manager always adds
// "host.docker.internal:host-gateway".
//
// These names are passed through to egress.conf VERBATIM — not pre-resolved
// to IPs — because net.LookupHost on the host would fail (the name is a
// Docker-internal /etc/hosts entry, not a DNS record). apply-egress-policy.sh
// resolves them at boot via `getent hosts`, which is a local /etc/hosts
// lookup (no network, no port 53) so it still works under deny-by-default.
//
// This is how bridge-networked orgs reach host-side services (e.g. a shared
// SurrealDB on the operator's machine) through the firewall.
var containerResolvedHosts = map[string]bool{
	"host.docker.internal": true,
}

func isContainerResolved(host string) bool {
	return containerResolvedHosts[strings.ToLower(host)]
}

// EgressRules returns the iptables-allow lines for each entry in
// p.Egress.Allow. Each output line is "<ip> <port>" — one host:port pair
// per line, hostname pre-resolved to IP(s). One hostname may expand to
// multiple lines (multi-IP DNS records). The consumer (apply-egress-policy.sh)
// reads this format and emits one iptables -A per line.
//
// For DNS-resolved hosts, a "# host: <name>" comment is emitted before the
// IP lines for that host. This lets refresh-egress-dns.sh re-resolve the
// hostname periodically and update the firewall when CDNs rotate IPs.
// Literal IP entries get no comment (nothing to re-resolve). Lines starting
// with '#' are skipped by apply-egress-policy.sh, so the comments are
// backwards-compatible with the existing boot script.
//
// DNS resolution happens NOW (sandbox-create time), not in-container at
// boot — by the time the firewall runs, DNS may be blocked (deny-by-default).
// Resolving up front means: (1) the policy is concrete + auditable, (2)
// the supervisor script needs zero DNS, (3) a hostname typo fails the
// sandbox create loudly instead of producing a silent no-egress state.
//
// Returns "" (no rules) when p.Egress.Allow is empty — caller treats that
// as "don't stage a conf file" → supervisor exits early with no firewall.
func EgressRules(p *Policy) (string, error) {
	if p == nil || len(p.Egress.Allow) == 0 {
		return "", nil
	}
	var lines []string
	for _, rule := range p.Egress.Allow {
		var ips []string
		if isContainerResolved(rule.Host) {
			// Docker-internal name (host.docker.internal) — resolvable only
			// inside the container via /etc/hosts. Pass through verbatim; the
			// firewall script resolves it at boot via getent (no DNS).
			ips = []string{rule.Host}
		} else {
			var err error
			ips, err = resolveHost(rule.Host)
			if err != nil {
				return "", fmt.Errorf("egress: resolve %s: %w", rule.Host, err)
			}
		}
		ports := rule.Ports
		if rule.Port != 0 {
			ports = append([]int{rule.Port}, ports...)
		}
		if len(ports) == 0 {
			return "", fmt.Errorf("egress: rule for %s has no port(s)", rule.Host)
		}
		// Emit hostname comment for DNS-resolved hosts only (literal IPs skip
		// — nothing to re-resolve; container-resolved names resolve fresh every
		// boot inside the container, so the periodic refresh script must not
		// try to re-resolve them host-side). The refresh script reads this to
		// know which hostname to re-lookup periodically.
		if net.ParseIP(rule.Host) == nil && !isContainerResolved(rule.Host) {
			lines = append(lines, "# host: "+rule.Host)
		}
		for _, ip := range ips {
			for _, port := range ports {
				if port < 1 || port > 65535 {
					return "", fmt.Errorf("egress: port %d for %s out of range", port, rule.Host)
				}
				lines = append(lines, ip+" "+strconv.Itoa(port))
			}
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}


// resolveHost returns one or more IPs for a hostname OR validates a
// literal IP. Literal IPv4/IPv6 short-circuits — no DNS lookup needed.
// DNS names resolve via net.LookupHost; all returned IPs are included
// in the allowlist so a hostname backed by multiple A records gets the
// right fan-out.
func resolveHost(host string) ([]string, error) {
	if host == "" {
		return nil, fmt.Errorf("empty host")
	}
	// Literal IP short-circuit.
	if ip := net.ParseIP(host); ip != nil {
		return []string{host}, nil
	}
	// DNS resolution — returns list of "host:ip" strings on success.
	// net.LookupHost returns IPs as bare strings.
	ips, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs for %s", host)
	}
	// Dedup in case DNS returns IPv4 + IPv6 of the same record and the
	// operator only wants tcp/443 (the script will emit both, but we
	// shouldn't emit duplicate lines).
	seen := make(map[string]bool, len(ips))
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out, nil
}
