package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func edgeServer(t *testing.T, sparkURL, proxies string, m mode) *server {
	t.Helper()
	s := &server{mode: m}
	if sparkURL != "" {
		u, err := parseSparkURL(sparkURL)
		if err != nil {
			t.Fatal(err)
		}
		s.cfg.URL = u
	}
	p, err := parseTrustedProxies(proxies)
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.TrustedProxies = p
	return s
}

func TestHostAllowed(t *testing.T) {
	local := &localMode{}
	tests := []struct {
		name, url, host string
		network         bool
		want            bool
	}{
		{"local localhost", "", "localhost:8080", false, true},
		{"local loopback v4", "", "127.0.0.1:8080", false, true},
		{"local loopback v6", "", "[::1]:8080", false, true},
		{"local rebinding name", "", "evil.example:8080", false, false},
		{"local LAN address", "", "192.168.1.5:8080", false, false},
		{"local network allowed", "", "192.168.1.5:8080", true, true},
		{"url matches", "https://Spark.Example.com", "spark.example.com", false, true},
		{"url matches with port", "https://spark.example.com", "spark.example.com:443", false, true},
		{"url other name", "https://spark.example.com", "localhost:8080", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := edgeServer(t, tt.url, "", local)
			s.cfg.AllowNetwork = tt.network
			local.s = s
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = tt.host
			if got := s.hostAllowed(r); got != tt.want {
				t.Errorf("hostAllowed(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name, proxies, peer string
		xff                 []string
		want                string
	}{
		{"no proxy", "", "203.0.113.9:5000", nil, "203.0.113.9"},
		{"untrusted peer ignores header", "", "203.0.113.9:5000", []string{"198.51.100.1"}, "203.0.113.9"},
		{"trusted proxy", "127.0.0.1", "127.0.0.1:5000", []string{"198.51.100.1"}, "198.51.100.1"},
		{"spoofed left entries", "172.16.0.0/12", "172.17.0.1:5000", []string{"1.2.3.4, 198.51.100.1"}, "198.51.100.1"},
		{"chain of proxies", "172.16.0.0/12 10.0.0.2", "172.17.0.1:5000", []string{"198.51.100.1", "10.0.0.2"}, "198.51.100.1"},
		{"trusted proxy, no header", "127.0.0.1", "127.0.0.1:5000", nil, "127.0.0.1"},
		{"garbage hop", "127.0.0.1", "127.0.0.1:5000", []string{"198.51.100.1, nonsense"}, "127.0.0.1"},
		{"mapped v4 peer", "127.0.0.1", "[::ffff:127.0.0.1]:5000", []string{"198.51.100.1"}, "198.51.100.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := edgeServer(t, "", tt.proxies, &remoteMode{})
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.peer
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := s.clientIP(r).String(); got != tt.want {
				t.Errorf("clientIP = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestIsHTTPS(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:5000"
	r.Header.Set("X-Forwarded-Proto", "https")
	if edgeServer(t, "", "", &remoteMode{}).isHTTPS(r) {
		t.Error("trusted X-Forwarded-Proto from an untrusted address")
	}
	if !edgeServer(t, "", "203.0.113.9", &remoteMode{}).isHTTPS(r) {
		t.Error("ignored X-Forwarded-Proto from a trusted proxy")
	}
	if edgeServer(t, "http://spark.lan", "203.0.113.9", &remoteMode{}).isHTTPS(r) {
		t.Error("SPARK_URL http:// should win over the header")
	}
}

func TestSameOrigin(t *testing.T) {
	s := edgeServer(t, "https://spark.example.com:443/", "", &remoteMode{})
	r := httptest.NewRequest("POST", "/", nil)
	if !s.sameOrigin(r, "https://spark.example.com") {
		t.Error("rejected its own origin")
	}
	if s.sameOrigin(r, "http://spark.example.com") {
		t.Error("accepted the http origin under an https SPARK_URL")
	}
}

func TestParseSparkURL(t *testing.T) {
	for _, bad := range []string{"spark.example.com", "ftp://x", "https://", "https://x/spark", "https://x?a=1", "https://u@x"} {
		if _, err := parseSparkURL(bad); err == nil {
			t.Errorf("parseSparkURL(%q) accepted", bad)
		}
	}
	if u, err := parseSparkURL("https://spark.example.com/"); err != nil || u.String() != "https://spark.example.com" {
		t.Errorf("parseSparkURL trailing slash = %v, %v", u, err)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	p, err := parseTrustedProxies("127.0.0.1, 172.16.0.0/12 ::1")
	if err != nil || len(p) != 3 {
		t.Fatalf("parseTrustedProxies = %v, %v", p, err)
	}
	if _, err := parseTrustedProxies("proxy.lan"); err == nil {
		t.Error("accepted a host name")
	}
}

func TestEdgeHeaders(t *testing.T) {
	s := edgeServer(t, "https://spark.example.com", "", &remoteMode{})
	h := s.edge(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "spark.example.com"
	h.ServeHTTP(w, r)
	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS under an https SPARK_URL")
	}
	if w.Code != http.StatusOK {
		t.Errorf("own host: status %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.Host = "rebound.example"
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMisdirectedRequest {
		t.Errorf("other host: status %d, want 421", w.Code)
	}
}
