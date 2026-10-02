package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// The admin invites people by a one-time link they copy and send themselves;
// the remote sends no email. The new user sets a password, then makes an API
// key on their account page for each local deployment.

const (
	inviteLength     = 7 * 24 * time.Hour
	maxKeyNameLength = 40
)

// changeInvites creates an invite (and shows its link once) or revokes one.
func (m *remoteMode) changeInvites(w http.ResponseWriter, r *http.Request) {
	s := m.s
	if !s.checkPost(r) {
		http.Error(w, "This form has expired. Reload the settings page and try again.", http.StatusForbidden)
		return
	}
	if !m.canEditSettings(viewer(r)) {
		http.Error(w, "Only an admin can change this.", http.StatusForbidden)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	action := r.PostFormValue("action")
	var token string
	err := s.auth.update(func(d *accountsData) (bool, error) {
		// One invite per username: a new one replaces the old link.
		d.Invites = slices.DeleteFunc(d.Invites, func(inv *invite) bool {
			return inv.Username == username || time.Now().After(inv.Expires)
		})
		switch action {
		case "create":
			if !usernameRe.MatchString(username) {
				return false, userError("A username uses lowercase letters and digits, with words joined by hyphens, like sam or sam-lee.")
			}
			if strings.HasPrefix(username, removedPrefix) {
				return false, userError(fmt.Sprintf("Usernames starting with %s are kept for the files of removed people.", removedPrefix))
			}
			if d.find(username) != nil {
				return false, userError(fmt.Sprintf("%s already has an account.", username))
			}
			token = randomToken(32)
			d.Invites = append(d.Invites, &invite{Username: username, Hash: tokenHash(token), Expires: time.Now().UTC().Add(inviteLength)})
		case "revoke":
		default:
			return false, userError("Unknown action.")
		}
		return true, nil
	})
	var userErr userError
	switch {
	case errors.As(err, &userErr):
		s.renderSettings(w, r, http.StatusBadRequest, userErr.Error(), nil)
	case err != nil:
		log.Printf("save invites: %v", err)
		s.renderSettings(w, r, http.StatusInternalServerError, "Could not save the invite. Check the server log.", nil)
	case action == "create":
		s.securityEvent(r, "invite created", username, "by", viewer(r).Username)
		s.renderSettings(w, r, http.StatusOK, "", map[string]any{
			"InviteFor":  username,
			"InviteLink": m.s.externalURL(r) + "/invite/" + token,
		})
	default:
		s.securityEvent(r, "invite revoked", username, "by", viewer(r).Username)
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
	}
}

// changeAccounts removes an account: its login, API keys and sessions go,
// and its files move to a retired name (see retire). The first admin comes
// from the env and would be recreated on the next start, so it can't be
// removed here.
func (m *remoteMode) changeAccounts(r *http.Request) error {
	username := r.PostFormValue("username")
	if r.PostFormValue("action") != "remove" {
		return userError("Unknown action.")
	}
	if username == m.s.cfg.Username {
		return userError(fmt.Sprintf("%s is the admin set by SPARK_USERNAME, so it can't be removed here.", username))
	}
	removed := false
	err := m.s.auth.update(func(d *accountsData) (bool, error) {
		n := len(d.Accounts)
		d.Accounts = slices.DeleteFunc(d.Accounts, func(a *account) bool { return a.Username == username })
		removed = len(d.Accounts) != n
		return removed, nil
	})
	if err != nil || !removed {
		return err
	}
	m.s.securityEvent(r, "account removed", username, "by", viewer(r).Username)
	if err := m.s.auth.lock.forget(username); err != nil {
		log.Printf("clear lockout of %s: %v", username, err)
	}
	return m.retire(username)
}

const (
	// removedPrefix marks the files of removed accounts. No account can
	// have it, so a retired name never belongs to anyone.
	removedPrefix = "deleted-"
	// retiredArchiveAfter is how long a removed account's files stay on
	// the cards before they are archived.
	retiredArchiveAfter = 30 * 24 * time.Hour
)

// retire renames a removed account's files from username__… to
// deleted-username__… (deleted-username-2__… if that was used by an earlier
// removal), so they aren't lost and a new account with the same username
// starts clean. Their state moves with them, set to archive in 30 days.
func (m *remoteMode) retire(username string) error {
	dir := filepath.Join(m.s.cfg.Root, "Projects")
	from := username + "__"
	var firstErr error
	err := m.s.updateState(func(st map[string]*fileState) (bool, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		taken := func(owner string) bool {
			prefix := owner + "__"
			for id := range st {
				if strings.HasPrefix(id, prefix) {
					return true
				}
			}
			return slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.HasPrefix(e.Name(), prefix) })
		}
		to := removedPrefix + username
		for n := 2; taken(to); n++ {
			to = fmt.Sprintf("%s%s-%d", removedPrefix, username, n)
		}

		archiveAt := time.Now().UTC().Add(retiredArchiveAfter)
		move := func(rest string) {
			e := st[from+rest]
			if e == nil {
				return
			}
			delete(st, from+rest)
			if !e.Archived {
				e.ArchiveAt = &archiveAt
			}
			st[to+"__"+rest] = e
		}
		changed := false
		for _, e := range entries {
			rest, ok := strings.CutPrefix(e.Name(), from)
			if !ok || !strings.HasSuffix(rest, ".md") {
				continue
			}
			if err := os.Rename(filepath.Join(dir, e.Name()), filepath.Join(dir, to+"__"+rest)); err != nil {
				log.Printf("retire %s: %v", e.Name(), err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			move(strings.TrimSuffix(rest, ".md"))
			changed = true
		}
		return changed, nil
	})
	if err != nil {
		return err
	}
	return firstErr
}

func (m *remoteMode) account(w http.ResponseWriter, r *http.Request) {
	m.renderAccount(w, r, http.StatusOK, "", nil)
}

func (m *remoteMode) renderAccount(w http.ResponseWriter, r *http.Request, status int, errMsg string, extra map[string]any) {
	v := viewer(r)
	data := map[string]any{
		"Title":   "Account · Spark",
		"Error":   errMsg,
		"Account": v,
	}
	for k, val := range extra {
		data[k] = val
	}
	m.s.render(w, r, status, "account", data)
}

// changeKeys creates an API key (shown once) or revokes one. Every key on an
// account pushes as that account's username.
func (m *remoteMode) changeKeys(w http.ResponseWriter, r *http.Request) {
	if !m.s.checkPost(r) {
		http.Error(w, "This form has expired. Reload the account page and try again.", http.StatusForbidden)
		return
	}
	v := viewer(r)
	name := strings.TrimSpace(r.PostFormValue("name"))
	action := r.PostFormValue("action")
	var key string
	err := m.s.auth.update(func(d *accountsData) (bool, error) {
		acct := d.find(v.Username)
		if acct == nil {
			return false, userError("Your account no longer exists.")
		}
		i := slices.IndexFunc(acct.Keys, func(k apiKey) bool { return k.Name == name })
		switch action {
		case "create":
			if name == "" || utf8.RuneCountInString(name) > maxKeyNameLength {
				return false, userError(fmt.Sprintf("Name the key after the machine it's for, in up to %d characters.", maxKeyNameLength))
			}
			if i >= 0 {
				return false, userError(fmt.Sprintf("You already have a key named %s.", name))
			}
			key = "spk_" + randomToken(32)
			acct.Keys = append(acct.Keys, apiKey{Name: name, Hash: tokenHash(key), Created: time.Now().UTC()})
		case "revoke":
			if i < 0 {
				return false, nil
			}
			acct.Keys = slices.Delete(acct.Keys, i, i+1)
		default:
			return false, userError("Unknown action.")
		}
		return true, nil
	})
	// The viewer was loaded before the change; show the account as saved.
	if d, lerr := m.s.auth.load(); lerr == nil {
		if acct := d.find(v.Username); acct != nil {
			r = r.WithContext(withViewer(r.Context(), acct))
		}
	}
	var userErr userError
	switch {
	case errors.As(err, &userErr):
		m.renderAccount(w, r, http.StatusBadRequest, userErr.Error(), nil)
	case err != nil:
		log.Printf("save keys: %v", err)
		m.renderAccount(w, r, http.StatusInternalServerError, "Could not save the key. Check the server log.", nil)
	case action == "create":
		m.s.securityEvent(r, "key created", v.Username, "key", name)
		m.renderAccount(w, r, http.StatusOK, "", map[string]any{"NewKey": key, "NewKeyName": name})
	default:
		m.s.securityEvent(r, "key revoked", v.Username, "key", name)
		http.Redirect(w, r, "/account", http.StatusSeeOther)
	}
}

// findInvite returns the unexpired invite for a link's token, or nil.
func (m *remoteMode) findInvite(token string) *invite {
	d, err := m.s.auth.load()
	if err != nil {
		log.Printf("load accounts: %v", err)
		return nil
	}
	hash := tokenHash(token)
	for _, inv := range d.Invites {
		if subtle.ConstantTimeCompare([]byte(inv.Hash), []byte(hash)) == 1 && time.Now().Before(inv.Expires) {
			return inv
		}
	}
	return nil
}

func (m *remoteMode) invitePage(w http.ResponseWriter, r *http.Request) {
	inv := m.findInvite(r.PathValue("token"))
	status := http.StatusOK
	if inv == nil {
		status = http.StatusNotFound
	}
	m.s.render(w, r, status, "invite", map[string]any{"Title": "Join Spark", "Invite": inv})
}

// acceptInvite sets the new account's password, uses up the invite, and logs
// the new user in. The token in the URL is the proof, so there is no CSRF
// token: nobody else can build this form.
func (m *remoteMode) acceptInvite(w http.ResponseWriter, r *http.Request) {
	inv := m.findInvite(r.PathValue("token"))
	if inv == nil {
		m.s.render(w, r, http.StatusNotFound, "invite", map[string]any{"Title": "Join Spark"})
		return
	}
	password := r.PostFormValue("password")
	fail := func(msg string) {
		m.s.render(w, r, http.StatusBadRequest, "invite", map[string]any{"Title": "Join Spark", "Invite": inv, "Error": msg})
	}
	if msg := m.s.cfg.passwordProblem(password); msg != "" {
		fail(strings.ToUpper(msg[:1]) + msg[1:] + ".")
		return
	}
	if password != r.PostFormValue("confirm") {
		fail("The two passwords don't match.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash password: %v", err)
		fail("Could not create the account. Ask your admin to check the server log.")
		return
	}

	var acct *account
	err = m.s.auth.update(func(d *accountsData) (bool, error) {
		i := slices.IndexFunc(d.Invites, func(x *invite) bool { return x.Hash == inv.Hash })
		if i < 0 {
			return false, userError("This invite was just used or revoked.")
		}
		if d.find(inv.Username) != nil {
			return false, userError(fmt.Sprintf("%s already has an account.", inv.Username))
		}
		d.Invites = slices.Delete(d.Invites, i, i+1)
		acct = &account{Username: inv.Username, Password: string(hash)}
		d.Accounts = append(d.Accounts, acct)
		return true, nil
	})
	var userErr userError
	if errors.As(err, &userErr) {
		fail(userErr.Error())
		return
	}
	if err == nil {
		err = m.s.auth.setSession(w, r, acct, m.s.isHTTPS(r))
	}
	if err != nil {
		log.Printf("accept invite: %v", err)
		fail("Could not create the account. Ask your admin to check the server log.")
		return
	}
	m.s.securityEvent(r, "invite used", acct.Username)
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}
