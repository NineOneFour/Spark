package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPenaltySchedule(t *testing.T) {
	cfg, err := loadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PenaltyStart != 4 || cfg.LockoutAfter != 11 {
		t.Fatalf("defaults: start %d, lockout after %d", cfg.PenaltyStart, cfg.LockoutAfter)
	}
	want := []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second,
		30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}
	for i, w := range want {
		if got, locked := cfg.penalty(i + 1); got != w || locked {
			t.Errorf("failure %d: wait %v locked %v, want %v", i+1, got, locked, w)
		}
	}
	if _, locked := cfg.penalty(11); !locked {
		t.Error("failure 11 should lock")
	}

	cfg.LockoutAfter = 0
	if got, locked := cfg.penalty(40); got != 32*time.Minute || locked {
		t.Errorf("lockout off, failure 40: %v %v", got, locked)
	}

	cfg.PenaltyStart, cfg.LockoutAfter = 0, 5
	for n := 1; n < 5; n++ {
		if got, _ := cfg.penalty(n); got != 2*time.Second {
			t.Errorf("start 0, failure %d: %v", n, got)
		}
	}
	if _, locked := cfg.penalty(5); !locked {
		t.Error("start 0, lockout after 5: failure 5 should lock")
	}
}

func TestLockoutSettings(t *testing.T) {
	for _, tt := range []struct {
		env       map[string]string
		start, at int
	}{
		{map[string]string{"SPARK_PENALTY_START": "0"}, 0, 0},
		{map[string]string{"SPARK_PENALTY_START": "2"}, 2, 9},
		{map[string]string{"SPARK_LOCKOUT": "off"}, 4, 0},
		{map[string]string{"SPARK_LOCKOUT_AFTER": "5"}, 4, 5},
	} {
		for k, v := range tt.env {
			t.Setenv(k, v)
		}
		cfg, err := loadConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PenaltyStart != tt.start || cfg.LockoutAfter != tt.at {
			t.Errorf("%v: start %d, lockout after %d; want %d, %d", tt.env, cfg.PenaltyStart, cfg.LockoutAfter, tt.start, tt.at)
		}
		for k := range tt.env {
			os.Unsetenv(k)
		}
	}
}

func TestPasswordRules(t *testing.T) {
	t.Setenv("SPARK_USERNAME", "admin")
	t.Setenv("SPARK_PASSWORD", "short-password")
	if _, err := loadConfig(""); err == nil {
		t.Error("a 14-character SPARK_PASSWORD was accepted")
	}
	t.Setenv("SPARK_MIN_PASSWORD_LENGTH", "8")
	if _, err := loadConfig(""); err != nil {
		t.Errorf("lowered minimum: %v", err)
	}
	cfg := config{MinPassword: 15}
	if cfg.passwordProblem("fifteen-chars!!") != "" {
		t.Error("15 characters refused")
	}
	if cfg.passwordProblem(string(make([]byte, 73))) == "" {
		t.Error("73 bytes accepted")
	}
}

func TestLockouts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(configDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config{PenaltyStart: 0, LockoutAfter: 3}
	l := newLockouts(root)

	fail := func(user string, known bool) *lockEntry {
		t.Helper()
		v, err := l.begin(user, known)
		if err != nil || !v.ok {
			t.Fatalf("begin %s: %+v %v", user, v, err)
		}
		e, err := l.end(cfg, user, known, false)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	// Waits are over at once, for the test.
	expire := func(user string, known bool) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.change(user, known, func(e *lockEntry) *lockEntry { e.Until = time.Time{}; return e })
	}

	for _, known := range []bool{true, false} {
		user := map[bool]string{true: "sam", false: "nobody"}[known]
		fail(user, known)
		if v, _ := l.begin(user, known); v.ok || v.wait <= 0 {
			t.Errorf("%s: attempt during the wait was allowed: %+v", user, v)
		}
		expire(user, known)
		fail(user, known)
		expire(user, known)
		if e := fail(user, known); !e.Locked {
			t.Errorf("%s: third failure didn't lock", user)
		}
		expire(user, known)
		if v, _ := l.begin(user, known); v.ok || !v.locked {
			t.Errorf("%s: locked account was allowed: %+v", user, v)
		}
	}

	// One attempt at a time per account.
	if v, _ := l.begin("lee", true); !v.ok {
		t.Fatal("first attempt refused")
	}
	if v, _ := l.begin("lee", true); v.ok {
		t.Error("a second attempt in flight was allowed")
	}
	if _, err := l.end(cfg, "lee", true, true); err != nil {
		t.Fatal(err)
	}

	// Only known accounts reach the file, and unlock clears them.
	data, err := os.ReadFile(filepath.Join(configDir(root), lockoutsFile))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := readLockouts(filepath.Join(configDir(root), lockoutsFile))
	if d.Accounts["sam"] == nil || d.Accounts["nobody"] != nil || d.Accounts["lee"] != nil {
		t.Errorf("lockouts.json: %s", data)
	}
	if ok, err := unlockAccount(root, "sam"); !ok || err != nil {
		t.Fatalf("unlock: %v %v", ok, err)
	}
	if v, _ := l.begin("sam", true); !v.ok {
		t.Errorf("unlocked account refused: %+v", v)
	}
}

func TestWaitText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Second:                      "1 second",
		1500 * time.Millisecond:          "2 seconds",
		30 * time.Second:                 "30 seconds",
		4*time.Minute - time.Millisecond: "4 minutes",
		32 * time.Minute:                 "32 minutes",
	} {
		if got := waitText(d); got != want {
			t.Errorf("waitText(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestLoginFlow(t *testing.T) {
	root := t.TempDir()
	if err := ensureSettings(root); err != nil {
		t.Fatal(err)
	}
	cfg := config{Root: root, Mode: "local", Username: "admin", Password: "correct-horse-battery",
		PenaltyStart: 1, LockoutAfter: 4, MinPassword: 15, SessionIdle: time.Hour}
	s := &server{cfg: cfg, tmpl: parseTemplates(), csrf: newCSRFToken()}
	var err error
	if s.auth, err = newAuth(cfg); err != nil {
		t.Fatal(err)
	}
	s.mode = &localMode{s: s}

	// The login page sets the form's cookie and token.
	w := httptest.NewRecorder()
	s.loginPage(w, httptest.NewRequest("GET", "/login", nil))
	cookie := w.Result().Cookies()[0]
	token := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(w.Body.String())[1]

	post := func(pass, tok string) int {
		form := url.Values{"username": {"admin"}, "password": {pass}, "csrf": {tok}}
		r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.login(w, r)
		return w.Code
	}
	if got := post("correct-horse-battery", "forged"); got != http.StatusForbidden {
		t.Errorf("wrong form token: %d", got)
	}
	if got := post("wrong", token); got != http.StatusUnauthorized {
		t.Errorf("wrong password: %d", got)
	}
	if got := post("correct-horse-battery", token); got != http.StatusTooManyRequests {
		t.Errorf("during the wait: %d", got)
	}
	s.auth.lock.mu.Lock()
	s.auth.lock.change("admin", true, func(e *lockEntry) *lockEntry { e.Until = time.Time{}; return e })
	s.auth.lock.mu.Unlock()
	if got := post("correct-horse-battery", token); got != http.StatusSeeOther {
		t.Errorf("right password after the wait: %d", got)
	}
}
