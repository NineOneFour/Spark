package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
)

// The edge is everything Spark decides from a request before any page runs:
// which host names it answers to, who sent the request, and whether it came
// over HTTPS. Request headers are set by the client, so each answer comes
// from SPARK_URL or a trusted proxy when there is one, never from the
// headers alone.

// edge wraps every route: security headers, then the host check.
func (s *server) edge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Fonts and styles are bundled, so nothing loads from another site.
		h.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if s.cfg.URL != nil && s.cfg.URL.Scheme == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		s.noteUntrustedProxy(r)
		if !s.hostAllowed(r) {
			http.Error(w, "Spark does not answer to this host name. See SPARK_URL and SPARK_ALLOW_NETWORK in INSTALL.md.", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed refuses host names Spark wasn't set up for. With SPARK_URL only
// its host name is accepted; otherwise the mode decides. This is what stops
// DNS rebinding: a website that points its own name at 127.0.0.1 still sends
// that name, so the browser can't be used to reach a local.
func (s *server) hostAllowed(r *http.Request) bool {
	host := requestHostname(r.Host)
	if s.cfg.URL != nil {
		return host == strings.ToLower(s.cfg.URL.Hostname())
	}
	return s.mode.hostAllowed(host)
}

// requestHostname is the Host header without its port or IPv6 brackets.
func requestHostname(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
}

// isLocalhost reports whether a host name only reaches this machine.
func isLocalhost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// peerAddr is the address of the connection itself.
func peerAddr(r *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap()
}

func (s *server) trusted(ip netip.Addr) bool {
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP is the address a request came from. Behind a trusted proxy it is
// the last address in X-Forwarded-For that isn't a trusted proxy, since
// everything to the left of that was written by the client. Otherwise it is
// the connection's own address.
func (s *server) clientIP(r *http.Request) netip.Addr {
	ip := peerAddr(r)
	if !s.trusted(ip) {
		return ip
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return ip
		}
		ip = hop.Unmap()
		if !s.trusted(ip) {
			return ip
		}
	}
	return ip
}

// isHTTPS reports whether the browser reached Spark over HTTPS, for the
// Secure cookie flag and invite links.
func (s *server) isHTTPS(r *http.Request) bool {
	if s.cfg.URL != nil {
		return s.cfg.URL.Scheme == "https"
	}
	return r.TLS != nil || (s.trusted(peerAddr(r)) && r.Header.Get("X-Forwarded-Proto") == "https")
}

// externalURL is Spark's address as the browser sees it, for invite links.
func (s *server) externalURL(r *http.Request) string {
	if s.cfg.URL != nil {
		return s.cfg.URL.String()
	}
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// sameOrigin checks a form post's Origin header. With SPARK_URL it must be
// that exact origin; without it only the host is compared, because the
// scheme a proxy used can't be known.
func (s *server) sameOrigin(r *http.Request, origin string) bool {
	if s.cfg.URL != nil {
		return strings.EqualFold(origin, s.cfg.URL.String())
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// securityEvent writes one security event to the normal log, prefixed
// "security:" so it can be filtered, naming the account and the client's
// address. extra is key, value pairs. Values are quoted, since some (a typed
// username) come from the request.
func (s *server) securityEvent(r *http.Request, event, account string, extra ...any) {
	line := fmt.Sprintf("security: %s account=%q ip=%s", event, ghostKey(account), s.clientIP(r))
	for i := 0; i+1 < len(extra); i += 2 {
		line += fmt.Sprintf(" %s=%q", extra[i], fmt.Sprint(extra[i+1]))
	}
	log.Print(line)
}

// untrustedProxies remembers addresses already logged for sending proxy
// headers, so each is logged once. It is capped, since anyone can send them.
var untrustedProxies = struct {
	sync.Mutex
	seen map[netip.Addr]bool
}{seen: map[netip.Addr]bool{}}

// noteUntrustedProxy logs proxy headers from an address that isn't trusted.
// They are ignored, so if it is the operator's proxy, the log line gives the
// address to put in SPARK_TRUSTED_PROXIES.
func (s *server) noteUntrustedProxy(r *http.Request) {
	if r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("X-Forwarded-Proto") == "" {
		return
	}
	ip := peerAddr(r)
	if s.trusted(ip) {
		return
	}
	untrustedProxies.Lock()
	defer untrustedProxies.Unlock()
	if untrustedProxies.seen[ip] || len(untrustedProxies.seen) >= 256 {
		return
	}
	untrustedProxies.seen[ip] = true
	log.Printf("security: ignoring proxy headers from %s; if that is your proxy, add it to SPARK_TRUSTED_PROXIES", ip)
}

// parseSparkURL checks SPARK_URL: the address people open, with no path.
func parseSparkURL(v string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(v, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("SPARK_URL %q: use the address people open, like https://spark.example.com", v)
	}
	// Browsers leave a default port out of the Origin header.
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	u.Host = strings.ToLower(u.Host)
	return u, nil
}

// parseTrustedProxies reads SPARK_TRUSTED_PROXIES: addresses or ranges,
// separated by commas or spaces.
func parseTrustedProxies(v string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.FieldsFunc(v, func(c rune) bool { return c == ',' || c == ' ' }) {
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
		} else if ip, err := netip.ParseAddr(f); err == nil {
			ip = ip.Unmap()
			out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
		} else {
			return nil, fmt.Errorf("SPARK_TRUSTED_PROXIES: %q is not an address or a range like 172.16.0.0/12", f)
		}
	}
	return out, nil
}
