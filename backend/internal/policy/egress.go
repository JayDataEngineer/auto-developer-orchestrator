package policy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// EgressRules returns the iptables-allow lines for each entry in
// p.Egress.Allow. Each output line is "<ip> <port>" — one host:port pair
// per line, hostname pre-resolved to IP(s). One hostname may expand to
// multiple lines (multi-IP DNS records). The consumer (apply-egress-policy.sh)
// reads this format and emits one iptables -A per line.
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
		ips, err := resolveHost(rule.Host)
		if err != nil {
			return "", fmt.Errorf("egress: resolve %s: %w", rule.Host, err)
		}
		ports := rule.Ports
		if rule.Port != 0 {
			ports = append([]int{rule.Port}, ports...)
		}
		if len(ports) == 0 {
			return "", fmt.Errorf("egress: rule for %s has no port(s)", rule.Host)
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
