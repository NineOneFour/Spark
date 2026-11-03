package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	sessionCookie = "spark_session"
	sessionLength = 30 * 24 * time.Hour
)

// auth issues and checks a stateless session cookie: "<expiry>.<hmac>".
// The signing key comes from the credentials, so changing the password
// signs everyone out.
type auth struct {
	username, password string
	key                []byte
	// failMu is the login limiter. An attempt must take it before comparing
	// credentials; a failed attempt holds it for failDelay. Attempts that find
	// it held are refused without being evaluated, so password guessing is
	// capped at one evaluated failure per failDelay in total.
	failMu sync.Mutex
	// compares counts evaluated credential comparisons (used by tests).
	compares atomic.Int64
}

// failDelay is how long a failed login holds the limiter.
var failDelay = time.Second

func newAuth(username, password string) *auth {
	sum := sha256.Sum256([]byte("spark-session\x00" + username + "\x00" + password))
	return &auth{username: username, password: password, key: sum[:]}
}

func (a *auth) sign(expiry int64) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(strconv.FormatInt(expiry, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *auth) valid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	exp, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	expiry, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(a.sign(expiry)))
}

// checkCredentials reports whether the credentials are right. busy is true
// when the limiter is held by another failed attempt; the credentials were
// not evaluated and the caller should answer 429.
func (a *auth) checkCredentials(username, password string) (ok, busy bool) {
	if !a.failMu.TryLock() {
		return false, true
	}
	a.compares.Add(1)
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) == 1
	if userOK && passOK {
		a.failMu.Unlock()
		return true, false
	}
	time.Sleep(failDelay)
	a.failMu.Unlock()
	return false, false
}

// setSession marks the cookie Secure only over HTTPS (directly or via a proxy
// such as Caddy); browsers drop Secure cookies on plain http to a LAN address.
func (a *auth) setSession(w http.ResponseWriter, r *http.Request) {
	expiry := time.Now().Add(sessionLength).Unix()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    strconv.FormatInt(expiry, 10) + "." + a.sign(expiry),
		Path:     "/",
		MaxAge:   int(sessionLength.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

// require redirects to the login page unless the request has a valid session.
// With login off (a == nil) it lets every request through.
func (a *auth) require(next http.HandlerFunc) http.HandlerFunc {
	if a == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.valid(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
