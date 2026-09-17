package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const dnsLookupTimeout = 3 * time.Second

var lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// ValidatePublicHTTPURL verifies that raw is an HTTP(S) URL whose host does not
// point to localhost, private networks, link-local addresses, or metadata IPs.
func ValidatePublicHTTPURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("url is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New("url scheme must be http or https")
	}
	if parsed.User != nil {
		return errors.New("url userinfo is not allowed")
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return errors.New("url host is required")
	}
	if strings.Contains(host, "%") {
		return fmt.Errorf("url host %q is not allowed", host)
	}
	if strings.TrimSuffix(strings.ToLower(host), ".") == "localhost" {
		return fmt.Errorf("url host %q is not allowed", host)
	}

	_, err = resolvePublicIPs(context.Background(), host)
	return err
}

// ResolvePublicTCPAddress resolves a callback destination once, verifies every
// returned address is public, and returns an IP endpoint suitable for dialing.
// Dialing this returned endpoint prevents a second DNS lookup from rebinding a
// previously validated hostname to a private address.
func ResolvePublicTCPAddress(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid callback address: %w", err)
	}

	addrs, err := resolvePublicIPs(ctx, host)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(addrs[0].String(), port), nil
}

func resolvePublicIPs(parent context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("url host is required")
	}
	if strings.Contains(host, "%") {
		return nil, fmt.Errorf("url host %q is not allowed", host)
	}
	if strings.TrimSuffix(strings.ToLower(host), ".") == "localhost" {
		return nil, fmt.Errorf("url host %q is not allowed", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := validatePublicIP(host, ip); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}

	ctx, cancel := context.WithTimeout(parent, dnsLookupTimeout)
	defer cancel()

	addrs, err := lookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve url host %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve url host %q: no addresses", host)
	}
	result := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if err := validatePublicIP(host, addr.IP); err != nil {
			return nil, err
		}
		result = append(result, addr.IP)
	}
	return result, nil
}

func validatePublicIP(host string, ip net.IP) error {
	if isUnsafeIP(ip) {
		return fmt.Errorf("url host %q resolves to disallowed address %s", host, ip.String())
	}
	return nil
}

func isUnsafeIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		!ip.IsGlobalUnicast()
}
