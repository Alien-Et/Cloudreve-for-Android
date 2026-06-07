package request

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrUnsafeURL is returned when a user-supplied URL targets an address that
// should not be reachable by server-side fetchers (loopback, private,
// link-local, cloud metadata, multicast, etc.) or uses a non-allowed scheme.
var ErrUnsafeURL = errors.New("URL is not allowed")

var downloadAllowedSchemes = map[string]struct{}{
	"http":   {},
	"https":  {},
	"ftp":    {},
	"ftps":   {},
	"sftp":   {},
	"magnet": {},
}

// trackerAllowedSchemes are schemes valid inside magnet tr=/ws=/xs=/as= params
// that we recognize and validate. Anything else is treated as non-network
// (e.g. dht://) and skipped.
var trackerAllowedSchemes = map[string]struct{}{
	"http":  {},
	"https": {},
	"udp":   {},
	"ws":    {},
	"wss":   {},
}

var bannedHostnames = map[string]struct{}{
	"localhost":             {},
	"localhost.localdomain": {},
	"ip6-localhost":         {},
	"ip6-loopback":          {},
}

// 100.64.0.0/10 (RFC 6598 CGNAT) is not flagged by net.IP.IsPrivate.
var cgnatNet = mustParseCIDR("100.64.0.0/10")

// 169.254.169.254 (cloud instance metadata) is link-local and already covered
// by IsLinkLocalUnicast, but we keep an explicit check for clarity.
var cloudMetadataIP = net.ParseIP("169.254.169.254")

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// SSRFOptions configures ValidateExternalURL.
type SSRFOptions struct {
	// Disabled short-circuits the entire check. Use when the operator has
	// explicitly opted out (e.g. node-level URLValidation.Disabled).
	Disabled bool
	// AllowedHosts contains hostnames whose URLs bypass all subsequent checks
	// (used to whitelist the operator-configured site URL host(s) and any
	// operator-trusted internal hostnames). Compared case-insensitively to
	// the URL's Hostname(). Port is ignored.
	AllowedHosts []string
	// AllowedCIDRs contains CIDR blocks whose IPs bypass the IP-class checks.
	// Resolved IPs falling within any of these are treated as safe even if
	// they would otherwise be rejected (private, link-local, ...). Malformed
	// entries are ignored. Used to opt a LAN range back in.
	AllowedCIDRs []string
	// Resolver is used for DNS lookups; nil uses net.DefaultResolver.
	Resolver *net.Resolver
}

// ValidateExternalURL returns an error wrapping ErrUnsafeURL if the URL would
// reach an internal-only address when fetched by a server-side downloader.
// Magnet links have their tr=/ws=/xs=/as= parameters validated as URLs.
func ValidateExternalURL(ctx context.Context, raw string, opt SSRFOptions) error {
	if opt.Disabled {
		return nil
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty URL: %w", ErrUnsafeURL)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse URL: %w", ErrUnsafeURL)
	}

	scheme := strings.ToLower(u.Scheme)
	if _, ok := downloadAllowedSchemes[scheme]; !ok {
		return fmt.Errorf("scheme %q not allowed: %w", u.Scheme, ErrUnsafeURL)
	}

	if scheme == "magnet" {
		return validateMagnet(ctx, u, opt)
	}

	return validateHost(ctx, u.Hostname(), opt)
}

func validateMagnet(ctx context.Context, u *url.URL, opt SSRFOptions) error {
	q := u.Query()
	for _, key := range []string{"tr", "ws", "xs", "as"} {
		for _, v := range q[key] {
			sub, err := url.Parse(strings.TrimSpace(v))
			if err != nil {
				return fmt.Errorf("magnet %s=%q: %w", key, v, ErrUnsafeURL)
			}
			if sub.Hostname() == "" {
				continue
			}
			if _, ok := trackerAllowedSchemes[strings.ToLower(sub.Scheme)]; !ok {
				continue
			}
			if err := validateHost(ctx, sub.Hostname(), opt); err != nil {
				return fmt.Errorf("magnet %s=%q: %w", key, v, err)
			}
		}
	}
	return nil
}

func validateHost(ctx context.Context, host string, opt SSRFOptions) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("empty host: %w", ErrUnsafeURL)
	}

	for _, allowed := range opt.AllowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return nil
		}
	}

	lowered := strings.ToLower(host)
	if _, banned := bannedHostnames[lowered]; banned {
		return fmt.Errorf("hostname %q is local: %w", host, ErrUnsafeURL)
	}
	if strings.HasSuffix(lowered, ".localhost") {
		return fmt.Errorf("hostname %q is local: %w", host, ErrUnsafeURL)
	}

	allowed := parseCIDRs(opt.AllowedCIDRs)

	if ip := net.ParseIP(host); ip != nil {
		return checkIPWithAllowlist(ip, allowed)
	}

	resolver := opt.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, ErrUnsafeURL)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no addresses for %q: %w", host, ErrUnsafeURL)
	}
	for _, a := range addrs {
		if err := checkIPWithAllowlist(a.IP, allowed); err != nil {
			return err
		}
	}
	return nil
}

// parseCIDRs parses CIDR strings, silently dropping malformed entries — the
// admin gets validation feedback at config-save time, so noisy errors during a
// download are not useful.
func parseCIDRs(raw []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(raw))
	for _, r := range raw {
		_, n, err := net.ParseCIDR(strings.TrimSpace(r))
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

func checkIPWithAllowlist(ip net.IP, allowed []*net.IPNet) error {
	for _, n := range allowed {
		if n.Contains(ip) {
			return nil
		}
	}
	return checkIP(ip)
}

func checkIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("invalid IP: %w", ErrUnsafeURL)
	}
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("loopback address %s: %w", ip, ErrUnsafeURL)
	case ip.IsUnspecified():
		return fmt.Errorf("unspecified address %s: %w", ip, ErrUnsafeURL)
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return fmt.Errorf("link-local address %s: %w", ip, ErrUnsafeURL)
	case ip.IsPrivate():
		return fmt.Errorf("private address %s: %w", ip, ErrUnsafeURL)
	case ip.IsMulticast():
		return fmt.Errorf("multicast address %s: %w", ip, ErrUnsafeURL)
	case ip.Equal(cloudMetadataIP):
		return fmt.Errorf("cloud metadata address %s: %w", ip, ErrUnsafeURL)
	case cgnatNet.Contains(ip):
		return fmt.Errorf("CGNAT address %s: %w", ip, ErrUnsafeURL)
	}
	return nil
}
