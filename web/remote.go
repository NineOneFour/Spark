package main

import (
	"errors"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// remoteMode is a hosted, multi-user deployment. Local deployments push
// snapshots to it (remote_api.go), tagged with the pushing key's username;
// people log in with accounts the admin invites (remote_accounts.go).
type remoteMode struct {
	s    *server
	rate *rateLimiter // valid API calls per account
}

func newRemoteMode(s *server) *remoteMode {
	m := &remoteMode{s: s, rate: newRateLimiter()}
	if s.cfg.URL == nil {
		log.Printf("SPARK_URL is not set: any host name is accepted, and invite links and the cross-site check follow the request's Host header. Set it before exposing this remote to the Internet")
	}
	switch {
	case s.cfg.APIRate == 0:
		log.Printf("SPARK_API_RATE is 0: API calls per account are not limited")
	case s.cfg.APIRate > defaultAPIRate:
		log.Printf("SPARK_API_RATE is %d: more API calls per account than the recommended %d a minute", s.cfg.APIRate, defaultAPIRate)
	}
	warnMaxProjects(m.maxProjects())
	return m
}

// warnMaxProjects logs a project cap looser than the default.
func warnMaxProjects(n int) {
	switch {
	case n == 0:
		log.Printf("%s: projects per account are not limited", remoteSettingsFile)
	case n > defaultMaxProjects:
		log.Printf("%s: %d projects per account, more than the recommended %d", remoteSettingsFile, n, defaultMaxProjects)
	}
}

// hostAllowed: a remote is meant to be reached over the network, so without
// SPARK_URL it answers to any host name.
func (m *remoteMode) hostAllowed(string) bool { return true }

func (m *remoteMode) routes(mux *http.ServeMux) {
	s := m.s
	mux.HandleFunc("POST /settings/accept-types", s.auth.require(s.updateSettings(m.changeAcceptTypes)))
	mux.HandleFunc("POST /settings/max-projects", s.auth.require(s.updateSettings(m.changeMaxProjects)))
	mux.HandleFunc("POST /settings/invites", s.auth.require(m.changeInvites))
	mux.HandleFunc("POST /settings/accounts", s.auth.require(s.updateSettings(m.changeAccounts)))
	mux.HandleFunc("GET /account", s.auth.require(m.account))
	mux.HandleFunc("POST /account/keys", s.auth.require(m.changeKeys))
	mux.HandleFunc("POST /account/password", s.auth.require(m.changePassword))
	mux.HandleFunc("POST /account/logout-all", s.auth.require(m.logoutEverywhere))
	mux.HandleFunc("GET /invite/{token}", m.invitePage)
	mux.HandleFunc("POST /invite/{token}", m.acceptInvite)
	mux.HandleFunc("GET /api/types", m.requireKey(m.apiTypes))
	mux.HandleFunc("PUT /api/files/{id}", m.requireKey(m.apiPush))
	mux.HandleFunc("GET /api/priorities", m.requireKey(m.apiPriorities))
}

// Remote files are username__projectName__projectType.
var remoteFileRe = regexp.MustCompile(`^([a-z0-9]+(?:-[a-z0-9]+)*)__([\p{L}\p{N}]+__[\p{L}\p{N}]+)$`)

func (m *remoteMode) fileKey(id string) (string, string, bool) {
	match := remoteFileRe.FindStringSubmatch(id)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

func (m *remoteMode) acceptsType(name string, listed map[string]bool) bool {
	return listed[name] || (m.acceptAll() && typeNameRe.MatchString(name))
}

// Only the admin or the file's owner changes a file's priority or archives it.
func (m *remoteMode) canEdit(v *account, p *Project) bool {
	return v != nil && (v.Admin || v.Username == p.Owner)
}

func (m *remoteMode) canEditSettings(v *account) bool { return v != nil && v.Admin }

func (m *remoteMode) stateChanged() {}

// pageData: remote has an account page per person, linked from the header.
func (m *remoteMode) pageData(data map[string]any) { data["Remote"] = true }

func (m *remoteMode) settingsData(r *http.Request, v *account, data map[string]any) {
	if !m.canEditSettings(v) {
		return
	}
	d, err := m.s.auth.load()
	if err != nil {
		log.Printf("load accounts: %v", err)
	}
	accounts := append([]*account(nil), d.Accounts...)
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Username < accounts[j].Username })
	data["RemoteAdmin"] = true
	data["AcceptAll"] = m.acceptAll()
	data["MaxProjects"] = m.maxProjects()
	data["Accounts"] = accounts
	data["Invites"] = d.Invites
	data["FirstAdmin"] = m.s.cfg.Username
}

// remoteSettingsFile holds remote-only settings. A remote accepts the types
// in project_types.json, or every type when accept_all_types is set (types
// still need a valid name). max_projects_per_account caps each account's
// files, archived ones included; unset means defaultMaxProjects, 0 no limit.
const remoteSettingsFile = "remote.json"

const defaultMaxProjects = 50

type remoteSettings struct {
	AcceptAllTypes bool `json:"accept_all_types"`
	MaxProjects    *int `json:"max_projects_per_account,omitempty"`
}

func (m *remoteMode) settings() remoteSettings {
	var rs remoteSettings
	err := readJSON(filepath.Join(configDir(m.s.cfg.Root), remoteSettingsFile), &rs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		reportInvalid(remoteSettingsFile, err)
	}
	return rs
}

func (m *remoteMode) acceptAll() bool { return m.settings().AcceptAllTypes }

func (m *remoteMode) maxProjects() int {
	if n := m.settings().MaxProjects; n != nil && *n >= 0 {
		return *n
	}
	return defaultMaxProjects
}

func (m *remoteMode) changeAcceptTypes(r *http.Request) error {
	rs := m.settings()
	rs.AcceptAllTypes = r.PostFormValue("all") == "on"
	return writeJSON(filepath.Join(configDir(m.s.cfg.Root), remoteSettingsFile), rs)
}

func (m *remoteMode) changeMaxProjects(r *http.Request) error {
	n, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("max")))
	if err != nil || n < 0 || n > 100000 {
		return userError("Use a whole number of projects, or 0 for no limit.")
	}
	rs := m.settings()
	rs.MaxProjects = &n
	if err := writeJSON(filepath.Join(configDir(m.s.cfg.Root), remoteSettingsFile), rs); err != nil {
		return err
	}
	warnMaxProjects(n)
	return nil
}
