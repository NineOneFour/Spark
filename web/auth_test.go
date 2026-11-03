package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func loginTemplates(t *testing.T) map[string]*template.Template {
	t.Helper()
	funcs := template.FuncMap{"date": func(t time.Time) string { return t.Format("Jan 2, 2006") }}
	return map[string]*template.Template{
		"login": template.Must(template.New("").Funcs(funcs).ParseFS(templateFS, "templates/base.html", "templates/login.html")),
	}
}

func testServer(t *testing.T) *server {
	t.Helper()
	return &server{auth: newAuth("owner", "right-password"), tmpl: loginTemplates(t)}
}

func postLogin(s *server, user, pass string) *httptest.ResponseRecorder {
	form := url.Values{"username": {user}, "password": {pass}}
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.login(w, r)
	return w
}

func TestLoginLimiterAllowsOneEvaluationPerSecond(t *testing.T) {
	s := testServer(t)
	start := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	cookies := 0
	fire := func(pass string) {
		defer wg.Done()
		w := postLogin(s, "owner", pass)
		mu.Lock()
		codes[w.Code]++
		if w.Code == http.StatusTooManyRequests && w.Header().Get("Set-Cookie") != "" {
			cookies++
		}
		mu.Unlock()
	}

	// 19 wrong guesses at once; one takes the limiter and the rest are refused.
	for i := 0; i < 19; i++ {
		wg.Add(1)
		go fire("wrong")
	}
	// Once a comparison has run, the correct password arrives while the
	// limiter is held and must be refused unevaluated.
	deadline := time.Now().Add(time.Second)
	for s.auth.compares.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	wg.Add(1)
	go fire("right-password")
	wg.Wait()

	if n := s.auth.compares.Load(); n != 1 {
		t.Errorf("compares = %d, want 1", n)
	}
	if codes[http.StatusTooManyRequests] != 19 || codes[http.StatusUnauthorized] != 1 {
		t.Errorf("status counts = %v, want 19x429 and 1x401", codes)
	}
	if cookies != 0 {
		t.Errorf("%d 429 responses set a cookie", cookies)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("took %v, want under 1.5s", d)
	}
}

func TestLoginSucceedsWhenLimiterFree(t *testing.T) {
	s := testServer(t)
	w := postLogin(s, "owner", "right-password")
	if w.Code != http.StatusSeeOther || w.Header().Get("Set-Cookie") == "" {
		t.Errorf("code = %d, cookie = %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	// A success releases the limiter at once.
	if w := postLogin(s, "owner", "right-password"); w.Code != http.StatusSeeOther {
		t.Errorf("second login code = %d", w.Code)
	}
}

func TestLoginRejectsOversizedBody(t *testing.T) {
	s := testServer(t)
	body := "username=owner&password=" + strings.Repeat("a", 1<<20)
	r := httptest.NewRequest("POST", "/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.login(w, r)
	if w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 413 or 400", w.Code)
	}
	if s.auth.compares.Load() != 0 {
		t.Error("oversized body reached the credential check")
	}
}

func TestCheckCredentials(t *testing.T) {
	a := newAuth("owner", "pw")
	old := failDelay
	failDelay = time.Millisecond
	defer func() { failDelay = old }()

	tests := []struct {
		user, pass string
		want       bool
	}{
		{"owner", "pw", true},
		{"owner", "wrong", false},
		{"other", "pw", false},
		{"", "", false},
	}
	for _, tc := range tests {
		ok, busy := a.checkCredentials(tc.user, tc.pass)
		if ok != tc.want || busy {
			t.Errorf("checkCredentials(%q, %q) = %v, %v; want %v, false", tc.user, tc.pass, ok, busy, tc.want)
		}
	}
}

func TestSessionCookieValidity(t *testing.T) {
	a := newAuth("owner", "pw")
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()
	cookie := func(v string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: v})
		return r
	}
	good := func(exp int64) string { return strconv.FormatInt(exp, 10) + "." + a.sign(exp) }

	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"valid", cookie(good(future)), true},
		{"expired", cookie(good(past)), false},
		{"tampered signature", cookie(strconv.FormatInt(future, 10) + ".AAAA"), false},
		{"extended expiry", cookie(strconv.FormatInt(future+9999, 10) + "." + a.sign(future)), false},
		{"no dot", cookie("garbage"), false},
		{"non-numeric expiry", cookie("abc." + a.sign(future)), false},
		{"no cookie", httptest.NewRequest("GET", "/", nil), false},
		{"signed with other credentials", cookie(strconv.FormatInt(future, 10) + "." + newAuth("owner", "other").sign(future)), false},
	}
	for _, tc := range tests {
		if got := a.valid(tc.req); got != tc.want {
			t.Errorf("%s: valid = %v, want %v", tc.name, got, tc.want)
		}
	}
}
