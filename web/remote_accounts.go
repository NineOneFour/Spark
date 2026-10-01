package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	inviteLength      = 7 * 24 * time.Hour
	minPasswordLength = 8
	maxKeyNameLength  = 40
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
		s.renderSettings(w, r, http.StatusOK, "", map[string]any{
			"InviteFor":  username,
			"InviteLink": externalURL(r) + "/invite/" + token,
		})
	default:
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
	}
}

// changeAccounts removes an account: its login, API keys and sessions go;
// the files it pushed stay, shown as before, since nothing deletes
// snapshots. The first admin comes from the env and would be recreated on
// the next start, so it can't be removed here.
func (m *remoteMode) changeAccounts(r *http.Request) error {
	username := r.PostFormValue("username")
	if r.PostFormValue("action") != "remove" {
		return userError("Unknown action.")
	}
	if username == m.s.cfg.Username {
		return userError(fmt.Sprintf("%s is the admin set by SPARK_USERNAME, so it can't be removed here.", username))
	}
	return m.s.auth.update(func(d *accountsData) (bool, error) {
		n := len(d.Accounts)
		d.Accounts = slices.DeleteFunc(d.Accounts, func(a *account) bool { return a.Username == username })
		return len(d.Accounts) != n, nil
	})
}

func (m *remoteMode) account(w http.ResponseWriter, r *http.Request) {
	m.renderAccount(w, r, http.StatusOK, "", nil)
}

func (m *remoteMode) renderAccount(w http.ResponseWriter, r *http.Request, status int, errMsg string, extra map[string]any) {
	v := viewer(r)
	data := map[string]any{
		"Title":   "Account · Spark",
		"Error":   errMsg,
		"CSRF":    m.s.csrf,
		"Account": v,
	}
	for k, val := range extra {
		data[k] = val
	}
	w.WriteHeader(status)
	m.s.render(w, r, "account", data)
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
		m.renderAccount(w, r, http.StatusOK, "", map[string]any{"NewKey": key, "NewKeyName": name})
	default:
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
	if inv == nil {
		w.WriteHeader(http.StatusNotFound)
	}
	m.s.render(w, r, "invite", map[string]any{"Title": "Join Spark", "Invite": inv})
}

// acceptInvite sets the new account's password, uses up the invite, and logs
// the new user in. The token in the URL is the proof, so there is no CSRF
// token: nobody else can build this form.
func (m *remoteMode) acceptInvite(w http.ResponseWriter, r *http.Request) {
	inv := m.findInvite(r.PathValue("token"))
	if inv == nil {
		w.WriteHeader(http.StatusNotFound)
		m.s.render(w, r, "invite", map[string]any{"Title": "Join Spark"})
		return
	}
	password := r.PostFormValue("password")
	fail := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		m.s.render(w, r, "invite", map[string]any{"Title": "Join Spark", "Invite": inv, "Error": msg})
	}
	if utf8.RuneCountInString(password) < minPasswordLength {
		fail(fmt.Sprintf("Use at least %d characters.", minPasswordLength))
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
		err = m.s.auth.setSession(w, r, acct)
	}
	if err != nil {
		log.Printf("accept invite: %v", err)
		fail("Could not create the account. Ask your admin to check the server log.")
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}
