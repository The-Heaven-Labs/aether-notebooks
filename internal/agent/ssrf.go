package agent

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func validateWebhookURL(rawURL string, allowedDomains []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	host := u.Hostname()

	// Check allowlist first
	for _, d := range allowedDomains {
		if strings.EqualFold(host, d) || strings.HasSuffix(host, "."+d) {
			return nil
		}
	}

	// Resolve hostname to IPs
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("could not resolve host: %s", host)
	}

	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		if isPrivateIP(parsed) {
			return fmt.Errorf("blocked request to private IP: %s", ip)
		}
	}

	return nil
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return true
	}

	// 169.254.x.x (link-local / metadata)
	if ip := ip.To4(); ip != nil {
		if ip[0] == 169 && ip[1] == 254 {
			return true
		}
	}

	// IPv6 unique local (fd00::/8)
	if ip := ip.To16(); ip != nil && len(ip) == 16 {
		if ip[0] == 0xfd {
			return true
		}
	}

	return false
}
