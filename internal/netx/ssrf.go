package netx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var blockedHosts = map[string]bool{
	"localhost":                 true,
	"metadata.google.internal":  true,
	"metadata.google":           true,
	"instance-data":             true,
}

// CheckURL rejects non-http(s) URLs and destinations that resolve to
// loopback, link-local, or RFC1918 space (SSRF).
func CheckURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("url is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("blocked scheme %q (http/https only)", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return nil, fmt.Errorf("url has no host")
	}
	if blockedHosts[host] || strings.HasSuffix(host, ".localhost") {
		return nil, fmt.Errorf("blocked host %s", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("blocked IP %s", host)
		}
		return u, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("dns lookup %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("blocked resolved IP %s for %s", ip, host)
		}
	}
	return u, nil
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// Cloud metadata / APIPA.
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}

// SafeClient returns an HTTP client that re-checks SSRF on redirects.
func SafeClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if _, err := CheckURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}
