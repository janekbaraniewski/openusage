package webserve

import (
	"fmt"
	"net"
	"strings"
)

const (
	defaultListenAddr = "127.0.0.1:8080"
	envServeToken     = "OPENUSAGE_SERVE_TOKEN"
)

// IsLoopbackAddr reports whether addr binds only to a loopback interface.
// Accepts ":port" (empty host = all interfaces, NOT loopback), "host:port",
// and bare "host". Hostnames other than "localhost" are treated conservatively
// as non-loopback because we can't resolve them deterministically at startup.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if addr == "" || strings.HasPrefix(addr, ":") {
			return false
		}
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// ValidateExposure refuses to start when the server would bind to a
// non-loopback interface with no Bearer auth configured, unless the operator
// explicitly opts in with allowPublic. Returns nil when the configuration is
// safe.
func ValidateExposure(addr, authToken string, allowPublic bool) error {
	if strings.TrimSpace(authToken) != "" {
		return nil
	}
	if allowPublic {
		return nil
	}
	if IsLoopbackAddr(addr) {
		return nil
	}
	return fmt.Errorf(
		"serve: refusing to listen on %q without auth token.\n"+
			"  Choose one:\n"+
			"    1. export OPENUSAGE_SERVE_TOKEN=<secret> to enable Bearer auth, OR\n"+
			"    2. bind to loopback only:  --listen 127.0.0.1:8080, OR\n"+
			"    3. pass --allow-public if you have a network-level firewall in place",
		addr,
	)
}

func normalizeListenAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return defaultListenAddr
	}
	return addr
}
