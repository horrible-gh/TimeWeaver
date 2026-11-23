package config

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// publicOrigin selects the browser's canonical origin independently of the
// bind address. Wildcard listeners are not usable browser host names. An HTTPS
// origin requires an explicitly trusted TLS-terminating proxy; the Go listener
// itself uses HTTP and never trusts forwarding headers from arbitrary peers.
func publicOrigin(raw, host string, port int, proxies []netip.Prefix) (string, error) {
	if raw == "" {
		ip := net.ParseIP(host)
		switch {
		case ip != nil && ip.IsUnspecified():
			host = "localhost"
		case strings.EqualFold(host, "localhost"), ip != nil && ip.IsLoopback():
		default:
			return "", errors.New("PUBLIC_ORIGIN is required when LISTEN_HOST is not a wildcard or loopback address")
		}
		raw = "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return "", errors.New("PUBLIC_ORIGIN must be an exact HTTP(S) origin without credentials, a path, query or fragment")
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("PUBLIC_ORIGIN contains an invalid port")
	}
	portText := u.Port()
	if portText != "" {
		v, e := strconv.Atoi(portText)
		if e != nil || v < 1 || v > 65535 {
			return "", errors.New("PUBLIC_ORIGIN contains an invalid port")
		}
		portText = strconv.Itoa(v)
		if u.Scheme == "http" && v == 80 || u.Scheme == "https" && v == 443 {
			portText = ""
		}
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && !strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("PUBLIC_ORIGIN requires HTTPS for remote browser access")
	}
	if u.Scheme == "https" && len(proxies) == 0 {
		return "", errors.New("HTTPS PUBLIC_ORIGIN requires TRUSTED_PROXIES for the TLS-terminating reverse proxy")
	}
	// Browsers omit default ports and canonicalize host names in Origin/Host.
	// Store that same origin so exact-origin request checks remain usable.
	host = strings.ToLower(u.Hostname())
	if ip != nil {
		host = ip.String()
	}
	u.Host = host
	if portText != "" {
		u.Host = net.JoinHostPort(host, portText)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	u.Path = ""
	return u.String(), nil
}
