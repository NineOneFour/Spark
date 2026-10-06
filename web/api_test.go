package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"testing"
	"time"
)

func TestIPKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":            "203.0.113.9",
		"::ffff:203.0.113.9":     "203.0.113.9",
		"2001:db8:1:2:3:4:5:6":   "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::abc": "2001:db8:1:2::/64",
	} {
		if got := ipKey(netip.MustParseAddr(in)); got != want {
			t.Errorf("ipKey(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestIsPrivateAddr(t *testing.T) {
	for in, want := range map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "172.20.0.5": true, "192.168.1.9": true,
		"100.100.1.1": true, "::1": true, "fd7a:115c:a1e0::1": true,
		"8.8.8.8": false, "172.32.0.1": false, "100.128.0.1": false, "2001:db8::1": false,
	} {
		if got := isPrivateAddr(netip.MustParseAddr(in)); got != want {
			t.Errorf("isPrivateAddr(%s) = %v", in, got)
		}
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter()
	for i := 0; i < 3; i++ {
		if ok, _ := rl.allow("sam", 3); !ok {
			t.Fatalf("call %d refused", i+1)
		}
	}
	ok, wait := rl.allow("sam", 3)
	if ok || wait < 15*time.Second || wait > 20*time.Second {
		t.Errorf("4th call: ok %v, wait %v; want refused, about 20 s", ok, wait)
	}
	if ok, _ := rl.allow("lee", 3); !ok {
		t.Error("another account was limited")
	}
	if ok, _ := rl.allow("sam", 0); !ok {
		t.Error("rate 0 should mean no limit")
	}
}

// apiRemote is a remote with one account, sam, and sam's API key.
func apiRemote(t *testing.T, cfg config) (*remoteMode, http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	if err := ensureSettings(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/Projects", 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Root, cfg.Mode, cfg.Username, cfg.Password, cfg.SessionIdle = root, "remote", "admin", "admin-password-long", time.Hour
	s := &server{cfg: cfg, tmpl: parseTemplates(), csrf: newCSRFToken()}
	var err error
	if s.auth, err = newAuth(cfg); err != nil {
		t.Fatal(err)
	}
	key := "spk_test"
	err = s.auth.update(func(d *accountsData) (bool, error) {
		d.Accounts = append(d.Accounts, &account{Username: "sam", Keys: []apiKey{{Name: "laptop", Hash: tokenHash(key), Created: time.Now()}}})
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m := newRemoteMode(s)
	s.mode = m
	mux := http.NewServeMux()
	m.routes(mux)
	return m, mux, key
}

const apiDemo = `---
project: "Demo %d"
description: A test project.
last_updated: 2026-10-01T09:00:00-04:00
priority: 3
project_type: side-project
---

# Project Description

%s
`

func apiPushReq(t *testing.T, h http.Handler, key string, n int, filler string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(pushRequest{Content: fmt.Sprintf(apiDemo, n, filler)})
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/files/demo%d__sideProject", n), bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	r.RemoteAddr = "203.0.113.9:4000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAPIWrongKeys(t *testing.T) {
	_, h, key := apiRemote(t, config{PenaltyStart: 0, LockoutAfter: 2, APIRate: 120, MaxFileBytes: 128 << 10})
	get := func(k string) int {
		r := httptest.NewRequest("GET", "/api/types", nil)
		r.Header.Set("Authorization", "Bearer "+k)
		r.RemoteAddr = "203.0.113.9:4000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := get("wrong"); got != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", got)
	}
	if got := get(key); got != http.StatusTooManyRequests {
		t.Fatalf("right key during the wait: %d", got)
	}
}

func TestAPILimits(t *testing.T) {
	m, h, key := apiRemote(t, config{PenaltyStart: 4, LockoutAfter: 11, APIRate: 120, MaxFileBytes: 1 << 10})

	// The remote states its limits.
	r := httptest.NewRequest("GET", "/api/types", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	r.RemoteAddr = "203.0.113.9:4000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var types typesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &types); err != nil || types.Limits == nil ||
		*types.Limits != (apiLimits{RatePerMinute: 120, MaxFileBytes: 1 << 10, MaxProjects: defaultMaxProjects}) {
		t.Fatalf("limits: %s", w.Body)
	}

	// Too large.
	if w := apiPushReq(t, h, key, 1, string(bytes.Repeat([]byte("x"), 2<<10))); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("large push: %d %s", w.Code, w.Body)
	}

	// Project cap: a new project over it is refused, an update isn't.
	two := 2
	if err := writeJSON(configDir(m.s.cfg.Root)+"/"+remoteSettingsFile, remoteSettings{MaxProjects: &two}); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		if w := apiPushReq(t, h, key, n, "ok"); w.Code != http.StatusOK {
			t.Fatalf("push %d: %d %s", n, w.Code, w.Body)
		}
	}
	if w := apiPushReq(t, h, key, 3, "ok"); w.Code != http.StatusConflict {
		t.Errorf("third project: %d %s", w.Code, w.Body)
	}
	if w := apiPushReq(t, h, key, 2, "changed"); w.Code != http.StatusOK {
		t.Errorf("update over the cap: %d %s", w.Code, w.Body)
	}

	// The key's use was noted.
	d, _ := m.s.auth.load()
	if k := d.find("sam").Keys[0]; k.LastUsed == nil {
		t.Error("key use not recorded")
	}
}

func TestAPIRate(t *testing.T) {
	_, h, key := apiRemote(t, config{PenaltyStart: 4, LockoutAfter: 11, APIRate: 2, MaxFileBytes: 128 << 10})
	codes := []int{}
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/api/priorities", nil)
		r.Header.Set("Authorization", "Bearer "+key)
		r.RemoteAddr = "203.0.113.9:4000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		codes = append(codes, w.Code)
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != http.StatusTooManyRequests {
		t.Errorf("codes %v, want 200 200 429", codes)
	}
}
