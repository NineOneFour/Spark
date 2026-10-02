package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Login is the same in both modes: accounts in Config/accounts.json with
// bcrypt passwords, and server-side sessions (sessions.go). Local has one account, from
// SPARK_USERNAME/SPARK_PASSWORD. Remote starts with that account as admin
// and adds more by invite (remote_accounts.go).
const (
	accountsFile   = "accounts.json"
	sessionKeyFile = "session_key.json"
	sessionCookie  = "spark_session"
	loginCookie    = "spark_login"
	sessionLength  = 30 * 24 * time.Hour
)

// Usernames tag pushed files on remote (username__project__type.md), so they
// are held to the same shape as type names: no "__", nothing to escape.
var usernameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const usernameRule = "a username uses lowercase letters and digits, with words joined by hyphens"

type account struct {
	Username string   `json:"username"`
	Password string   `json:"password"` // bcrypt hash
	Admin    bool     `json:"admin"`
	Keys     []apiKey `json:"keys,omitempty"`
}

// apiKey is one local deployment's key. Only its SHA-256 is kept: the key is
// 32 random bytes, so a fast hash is enough, and it is shown once.
type apiKey struct {
	Name    string    `json:"name"`
	Hash    string    `json:"hash"`
	Created time.Time `json:"created"`
}

// invite is a one-time signup link for a username, stored like an API key.
type invite struct {
	Username string    `json:"username"`
	Hash     string    `json:"hash"`
	Expires  time.Time `json:"expires"`
}

type accountsData struct {
	Accounts []*account `json:"accounts"`
	Invites  []*invite  `json:"invites,omitempty"`
}

func (d *accountsData) find(username string) *account {
	for _, a := range d.Accounts {
		if a.Username == username {
			return a
		}
	}
	return nil
}

type auth struct {
	root     string
	key      []byte // derives form tokens
	sessions *sessionStore

	mu   sync.Mutex // serializes read-modify-write of accounts.json
	lock *lockouts  // failed logins, per account
	// slots caps how many passwords are checked at once. bcrypt is slow on
	// purpose, so guesses spread over many usernames can't take every CPU.
	slots chan struct{}
}

// newAuth loads the key and the sessions, and makes sure the env account
// exists with the env password, as admin. Changing the password in the env
// changes it here, and logs that account out everywhere.
func newAuth(cfg config) (*auth, error) {
	root, username, password := cfg.Root, cfg.Username, cfg.Password
	key, err := loadSessionKey(root)
	if err != nil {
		return nil, err
	}
	st, err := loadSessions(root, cfg.SessionIdle)
	if err != nil {
		return nil, err
	}
	a := &auth{root: root, key: key, sessions: st, lock: newLockouts(root), slots: make(chan struct{}, runtime.NumCPU())}
	changed := false
	err = a.update(func(d *accountsData) (bool, error) {
		acct := d.find(username)
		if acct == nil {
			acct = &account{Username: username}
			d.Accounts = append(d.Accounts, acct)
		} else if bcrypt.CompareHashAndPassword([]byte(acct.Password), []byte(password)) == nil && acct.Admin {
			return false, nil
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return false, err
		}
		acct.Password, acct.Admin = string(hash), true
		changed = true
		return true, nil
	})
	if err == nil && changed {
		err = st.endAll(username, "")
	}
	return a, err
}

// loadSessionKey reads the key form tokens are derived from, creating it on
// first start. It is kept in Config so open forms survive a restart.
func loadSessionKey(root string) ([]byte, error) {
	path := filepath.Join(configDir(root), sessionKeyFile)
	var stored struct {
		Key string `json:"key"`
	}
	err := readJSON(path, &stored)
	if errors.Is(err, fs.ErrNotExist) {
		stored.Key = randomToken(64)
		if err := writeJSON(path, stored); err != nil {
			return nil, err
		}
		log.Printf("created %s", path)
	} else if err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(stored.Key)
}

func (a *auth) load() (*accountsData, error) {
	d := &accountsData{}
	err := readJSON(filepath.Join(configDir(a.root), accountsFile), d)
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	return d, err
}

// update runs change on accounts.json under the lock and writes it back if
// change reports that it changed something.
func (a *auth) update(change func(d *accountsData) (bool, error)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, err := a.load()
	if err != nil {
		return err
	}
	changed, err := change(d)
	if err != nil || !changed {
		return err
	}
	return writeJSON(filepath.Join(configDir(a.root), accountsFile), d)
}

// passwordTag ties form tokens to the password, so a password change
// replaces them.
func passwordTag(acct *account) string {
	sum := sha256.Sum256([]byte(acct.Password))
	return hex.EncodeToString(sum[:8])
}

// sessionID is the id in the request's session cookie, or "".
func sessionID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// sessionAccount returns the logged-in account, or nil.
func (a *auth) sessionAccount(r *http.Request) *account {
	id := sessionID(r)
	if id == "" {
		return nil
	}
	username, ok := a.sessions.lookup(id)
	if !ok {
		return nil
	}
	d, err := a.load()
	if err != nil {
		log.Printf("load accounts: %v", err)
		return nil
	}
	return d.find(username)
}

// setSession starts a session and sets its cookie. The cookie is Secure
// only over HTTPS (see isHTTPS); browsers drop Secure cookies on plain http
// to a LAN address.
func (a *auth) setSession(w http.ResponseWriter, acct *account, secure bool) error {
	id, err := a.sessions.create(acct.Username)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: id, Path: "/", MaxAge: int(sessionLength.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// clearSessionCookie tells the browser to drop the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// dummyHash is compared against when the username doesn't exist, so a wrong
// username takes as long as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("spark-dummy"), bcrypt.DefaultCost)

func (a *auth) checkCredentials(d *accountsData, username, password string) *account {
	acct := d.find(username)
	hash := dummyHash
	if acct != nil {
		hash = []byte(acct.Password)
	}
	a.slots <- struct{}{}
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	<-a.slots
	if err == nil && acct != nil {
		return acct
	}
	return nil
}

type viewerKey struct{}

// viewer returns the logged-in account for a request that passed require,
// or nil when login is off.
func viewer(r *http.Request) *account {
	v, _ := r.Context().Value(viewerKey{}).(*account)
	return v
}

// require redirects to the login page unless the request has a valid session.
// With login off (a == nil) it lets every request through.
func (a *auth) require(next http.HandlerFunc) http.HandlerFunc {
	if a == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		acct := a.sessionAccount(r)
		if acct == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(withViewer(r.Context(), acct)))
	}
}

func withViewer(ctx context.Context, acct *account) context.Context {
	return context.WithValue(ctx, viewerKey{}, acct)
}

func (s *server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil || s.auth.sessionAccount(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, r, http.StatusOK, "", "")
}

func (s *server) renderLogin(w http.ResponseWriter, r *http.Request, status int, username, errMsg string) {
	s.render(w, r, status, "login", map[string]any{
		"Title":      "Log in to Spark",
		"Error":      errMsg,
		"Username":   username,
		"LoginToken": s.loginToken(w, r),
	})
}

// loginToken ties the login form to a random value in a cookie of its own,
// so another site can't log a visitor in to an account of its choosing
// (login CSRF). There is no session yet to tie it to.
func (s *server) loginToken(w http.ResponseWriter, r *http.Request) string {
	nonce := ""
	if c, err := r.Cookie(loginCookie); err == nil && len(c.Value) == 43 {
		nonce = c.Value
	} else {
		nonce = randomToken(32)
		http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: nonce, Path: "/login", HttpOnly: true, Secure: s.isHTTPS(r), SameSite: http.SameSiteStrictMode})
	}
	mac := hmac.New(sha256.New, s.auth.key)
	mac.Write([]byte("login\x00" + nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *server) checkLoginToken(r *http.Request) bool {
	c, err := r.Cookie(loginCookie)
	if err != nil || s.crossSite(r) {
		return false
	}
	mac := hmac.New(sha256.New, s.auth.key)
	mac.Write([]byte("login\x00" + c.Value))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(want)) == 1
}

// login checks a password unless the account is waiting out a penalty or is
// locked (lockout.go); refused attempts aren't checked or counted.
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	username, password := r.PostFormValue("username"), r.PostFormValue("password")
	if !s.checkLoginToken(r) {
		s.renderLogin(w, r, http.StatusForbidden, username, "This form has expired. Try again.")
		return
	}
	d, err := s.auth.load()
	if err != nil {
		log.Printf("load accounts: %v", err)
		http.Error(w, "Could not log you in. Check the server log.", http.StatusInternalServerError)
		return
	}
	known := d.find(username) != nil
	v, err := s.auth.lock.begin(username, known)
	if err != nil {
		log.Printf("load lockouts: %v", err)
		http.Error(w, "Could not log you in. Check the server log.", http.StatusInternalServerError)
		return
	}
	if !v.ok {
		msg := fmt.Sprintf("Too many failed logins for this account. Try again in %s.", waitText(v.wait))
		if v.locked {
			msg = lockedText
		}
		s.renderLogin(w, r, http.StatusTooManyRequests, username, msg)
		return
	}
	acct := s.auth.checkCredentials(d, username, password)
	e, err := s.auth.lock.end(s.cfg, username, known, acct != nil)
	if err != nil {
		log.Printf("save lockouts: %v", err)
	}
	if acct == nil {
		msg := "Wrong username or password."
		if e != nil {
			s.securityEvent(r, "login failed", username, "failures", e.Failures)
			if e.Locked {
				s.securityEvent(r, "account locked", username, "failures", e.Failures)
				msg += " " + lockedText
			} else if wait := time.Until(e.Until); wait > 2*time.Second {
				msg += fmt.Sprintf(" Try again in %s.", waitText(wait))
			}
		}
		s.renderLogin(w, r, http.StatusUnauthorized, username, msg)
		return
	}
	s.securityEvent(r, "login", username)
	if err := s.auth.setSession(w, acct, s.isHTTPS(r)); err != nil {
		log.Printf("save session: %v", err)
		http.Error(w, "Could not log you in. Check the server log.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout ends the session on the server and clears the cookie. It is a POST
// with the form token, so another site can't log people out.
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	// Log out isn't behind require, so find the viewer its form token is for.
	if acct := s.auth.sessionAccount(r); acct != nil {
		r = r.WithContext(withViewer(r.Context(), acct))
	}
	if !s.checkPost(r) {
		http.Error(w, "This form has expired. Reload the page and try again.", http.StatusForbidden)
		return
	}
	if err := s.auth.sessions.end(sessionID(r)); err != nil {
		log.Printf("end session: %v", err)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// randomToken returns n random bytes, base64url encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("random: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// tokenHash is how API keys and invite tokens are stored.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
