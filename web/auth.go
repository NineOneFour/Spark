package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

// Login is the same in both modes: accounts in Config/accounts.json with
// bcrypt passwords, and a signed session cookie. Local has one account, from
// SPARK_USERNAME/SPARK_PASSWORD. Remote starts with that account as admin
// and adds more by invite (remote_accounts.go).
const (
	accountsFile   = "accounts.json"
	sessionKeyFile = "session_key.json"
	sessionCookie  = "spark_session"
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
	root  string
	store *sessions.CookieStore

	mu sync.Mutex // serializes read-modify-write of accounts.json
	// Failed logins are serialized behind a one-second delay, which caps
	// password guessing at about one attempt per second in total.
	failMu sync.Mutex
}

// newAuth loads the session key and makes sure the env account exists with
// the env password, as admin. Changing the password in the env changes it
// here, and signs that account out.
func newAuth(root, username, password string) (*auth, error) {
	key, err := loadSessionKey(root)
	if err != nil {
		return nil, err
	}
	a := &auth{root: root, store: sessions.NewCookieStore(key)}
	a.store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   int(sessionLength.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
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
		return true, nil
	})
	return a, err
}

// loadSessionKey reads the cookie signing key, creating it on first start.
// It is kept in Config so sessions survive a restart.
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

// passwordTag ties a session to the password it was made with, so changing
// a password signs that account out everywhere.
func passwordTag(acct *account) string {
	sum := sha256.Sum256([]byte(acct.Password))
	return hex.EncodeToString(sum[:8])
}

// sessionAccount returns the logged-in account, or nil.
func (a *auth) sessionAccount(r *http.Request) *account {
	sess, err := a.store.Get(r, sessionCookie)
	if err != nil {
		return nil
	}
	username, _ := sess.Values["user"].(string)
	tag, _ := sess.Values["pw"].(string)
	d, err := a.load()
	if err != nil {
		log.Printf("load accounts: %v", err)
		return nil
	}
	acct := d.find(username)
	if acct == nil || tag != passwordTag(acct) {
		return nil
	}
	return acct
}

// setSession marks the cookie Secure only over HTTPS (directly or via a proxy
// such as Caddy); browsers drop Secure cookies on plain http to a LAN address.
func (a *auth) setSession(w http.ResponseWriter, r *http.Request, acct *account) error {
	sess, _ := a.store.New(r, sessionCookie)
	sess.Values["user"] = acct.Username
	sess.Values["pw"] = passwordTag(acct)
	opts := *a.store.Options
	opts.Secure = r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	sess.Options = &opts
	return sess.Save(r, w)
}

// dummyHash is compared against when the username doesn't exist, so a wrong
// username takes as long as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("spark-dummy"), bcrypt.DefaultCost)

func (a *auth) checkCredentials(username, password string) *account {
	d, err := a.load()
	if err != nil {
		log.Printf("load accounts: %v", err)
		return nil
	}
	acct := d.find(username)
	hash := dummyHash
	if acct != nil {
		hash = []byte(acct.Password)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil && acct != nil {
		return acct
	}
	a.failMu.Lock()
	time.Sleep(time.Second)
	a.failMu.Unlock()
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
	s.render(w, r, "login", map[string]any{"Title": "Log in to Spark"})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	username, password := r.PostFormValue("username"), r.PostFormValue("password")
	acct := s.auth.checkCredentials(username, password)
	if acct == nil {
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, r, "login", map[string]any{
			"Title":    "Log in to Spark",
			"Error":    "Wrong username or password.",
			"Username": username,
		})
		return
	}
	if err := s.auth.setSession(w, r, acct); err != nil {
		log.Printf("save session: %v", err)
		http.Error(w, "Could not log you in. Check the server log.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout clears the session cookie. It is a POST with the form token, so
// another site can't log people out.
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.checkPost(r) {
		http.Error(w, "This form has expired. Reload the page and try again.", http.StatusForbidden)
		return
	}
	sess, _ := s.auth.store.New(r, sessionCookie)
	opts := *s.auth.store.Options
	opts.MaxAge = -1
	sess.Options = &opts
	if err := sess.Save(r, w); err != nil {
		log.Printf("clear session: %v", err)
	}
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
