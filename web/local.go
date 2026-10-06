package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// localMode is a deployment on one person's machine: the collector fills
// Projects/ from the host, and the sync loop (local_sync.go) pushes files to
// remotes by project type.
type localMode struct {
	s    *server
	kick chan struct{} // asks the sync loop to push now

	// Used by the sync loop only.
	limits   map[string]remoteLimits // by remote name
	nextCall map[string]time.Time    // by remote name: when pace lets the next call go
}

func newLocalMode(s *server) (*localMode, error) {
	// Only local has a collector, so only local gets its settings file.
	if err := ensureFiles(s.cfg.Root, map[string]any{scanRootsFile: []string{}}); err != nil {
		return nil, err
	}
	if s.cfg.AllowNetwork && s.cfg.URL == nil {
		if s.auth == nil {
			log.Printf("SPARK_ALLOW_NETWORK is on and login is off: anyone who can reach %s can read every snapshot and change the settings, including where snapshots are pushed. Turn login on", s.cfg.Addr)
		} else {
			log.Printf("SPARK_ALLOW_NETWORK is on: answering to any host name, not only localhost")
		}
	}
	if s.cfg.AllowHTTPRemotes {
		log.Printf("SPARK_ALLOW_HTTP_REMOTES is on: API keys may travel unencrypted to http:// remotes on any address")
	}
	l := &localMode{s: s, kick: make(chan struct{}, 1), limits: map[string]remoteLimits{}, nextCall: map[string]time.Time{}}
	go l.syncLoop()
	return l, nil
}

// hostAllowed: a local answers only to localhost unless SPARK_ALLOW_NETWORK
// is on, so a website can't reach it through the visitor's browser.
func (l *localMode) hostAllowed(host string) bool {
	return l.s.cfg.AllowNetwork || isLocalhost(host)
}

func (l *localMode) routes(mux *http.ServeMux) {
	s := l.s
	mux.HandleFunc("POST /settings/scan-roots", s.auth.require(s.updateSettings(l.changeScanRoots)))
	mux.HandleFunc("POST /settings/remotes", s.auth.require(s.updateSettings(l.changeRemotes)))
}

// fileKey: local files are projectName__projectType, with no owner.
func (l *localMode) fileKey(id string) (string, string, bool) { return "", id, true }

func (l *localMode) acceptsType(name string, listed map[string]bool) bool { return listed[name] }

// Local has at most one account, so whoever can see the page owns it all.
func (l *localMode) canEdit(*account, *Project) bool { return true }
func (l *localMode) canEditSettings(*account) bool   { return true }

func (l *localMode) pageData(map[string]any) {}

func (l *localMode) stateChanged() {
	select {
	case l.kick <- struct{}{}:
	default: // a push is already pending
	}
}

func (l *localMode) settingsData(r *http.Request, v *account, data map[string]any) {
	roots, err := readScanRoots(l.s.cfg.Root)
	if err != nil {
		log.Printf("load scan roots: %v", err)
	}
	remotes, err := readRemotes(l.s.cfg.Root)
	if err != nil {
		log.Printf("load remotes: %v", err)
	}
	types, err := loadProjectTypes(l.s.cfg.Root)
	if err != nil {
		log.Printf("load project types: %v", err)
	}
	data["Local"] = true
	data["ScanRoots"] = roots
	data["Remotes"] = remoteViews(remotes, types)
}

func (l *localMode) changeScanRoots(r *http.Request) error {
	path := strings.TrimSpace(r.PostFormValue("path"))
	roots, err := readScanRoots(l.s.cfg.Root)
	if err != nil {
		return err
	}
	switch r.PostFormValue("action") {
	case "add":
		// The collector reads these on the host, so the container can't check
		// that they exist; it can only check that they are absolute.
		if !strings.HasPrefix(path, "/") && path != "~" && !strings.HasPrefix(path, "~/") {
			return userError("A scan root must start with / or ~/.")
		}
		if slices.Contains(roots, path) {
			return userError(fmt.Sprintf("%s is already a scan root.", path))
		}
		roots = append(roots, path)
	case "remove":
		roots = slices.DeleteFunc(roots, func(p string) bool { return p == path })
	default:
		return userError("Unknown action.")
	}
	return writeJSON(filepath.Join(configDir(l.s.cfg.Root), scanRootsFile), roots)
}

// readScanRoots returns scan_roots.json as written. A missing file is an
// empty list, so the page still works if someone deleted it.
func readScanRoots(root string) ([]string, error) {
	var roots []string
	err := readJSON(filepath.Join(configDir(root), scanRootsFile), &roots)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return roots, err
}

// remotesFile lists the remotes this deployment pushes to. The key is kept
// as given: local needs it to push, and it never leaves this SparkRoot
// except in the Authorization header.
const remotesFile = "remotes.json"

type remoteConfig struct {
	Name  string   `json:"name"`
	URL   string   `json:"url"`
	Key   string   `json:"key"`
	Types []string `json:"types"` // local project types pushed to this remote
}

// remoteView is what the settings page shows of a remote: never the key.
type remoteView struct {
	Name  string
	URL   string
	Types []typeChoice
}

type typeChoice struct {
	Name    string
	Checked bool
}

func readRemotes(root string) ([]remoteConfig, error) {
	var remotes []remoteConfig
	err := readJSON(filepath.Join(configDir(root), remotesFile), &remotes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return remotes, err
}

// remoteViews pairs each remote with every local type, ticked if pushed.
func remoteViews(remotes []remoteConfig, types []projectType) []remoteView {
	var views []remoteView
	for _, rc := range remotes {
		v := remoteView{Name: rc.Name, URL: rc.URL}
		for _, t := range types {
			v.Types = append(v.Types, typeChoice{t.Name, slices.Contains(rc.Types, t.Name)})
		}
		views = append(views, v)
	}
	return views
}

// changeRemotes adds a remote (after checking the URL and key work), saves
// which types go to it (only types the remote accepts), or removes it.
func (l *localMode) changeRemotes(r *http.Request) error {
	remotes, err := readRemotes(l.s.cfg.Root)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	i := slices.IndexFunc(remotes, func(rc remoteConfig) bool { return rc.Name == name })

	switch r.PostFormValue("action") {
	case "add":
		if !typeNameRe.MatchString(name) {
			return userError("A remote name uses lowercase letters and digits, with words joined by hyphens, like work.")
		}
		if i >= 0 {
			return userError(fmt.Sprintf("%s already exists.", name))
		}
		rc := remoteConfig{
			Name: name,
			URL:  strings.TrimRight(strings.TrimSpace(r.PostFormValue("url")), "/"),
			Key:  strings.TrimSpace(r.PostFormValue("key")),
		}
		if u, err := url.Parse(rc.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return userError("The URL must start with https:// (or http://).")
		}
		if _, err := l.fetchRemoteTypes(rc); err != nil {
			return userError(fmt.Sprintf("Could not connect to %s: %v", rc.URL, err))
		}
		remotes = append(remotes, rc)
	case "types":
		if i < 0 {
			return userError(fmt.Sprintf("%s no longer exists.", name))
		}
		chosen := r.PostForm["type"]
		// Only newly ticked types need the remote's say, so unticking
		// works while the remote is down.
		var added []string
		for _, t := range chosen {
			if !slices.Contains(remotes[i].Types, t) {
				added = append(added, t)
			}
		}
		if len(added) > 0 {
			accepted, err := l.fetchRemoteTypes(remotes[i])
			if err != nil {
				return userError(fmt.Sprintf("Could not reach %s to check its project types: %v", name, err))
			}
			for _, t := range added {
				if !accepted.All && !slices.Contains(accepted.Types, t) {
					return userError(fmt.Sprintf("%s does not accept %s projects. It accepts: %s.", name, t, strings.Join(accepted.Types, ", ")))
				}
			}
		}
		remotes[i].Types = chosen
	case "remove":
		if i >= 0 {
			remotes = slices.Delete(remotes, i, i+1)
		}
	case "resync":
		if i < 0 {
			return userError(fmt.Sprintf("%s no longer exists.", name))
		}
		if err := l.resetPushes(name); err != nil {
			return err
		}
		l.stateChanged()
		return nil
	default:
		return userError("Unknown action.")
	}
	if action := r.PostFormValue("action"); action == "add" || action == "remove" {
		// A remote added under a removed one's name may be a different
		// server, so what was pushed to the old one says nothing about it.
		if err := l.forgetPushes(name); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(configDir(l.s.cfg.Root), remotesFile), remotes); err != nil {
		return err
	}
	l.stateChanged() // push to a new remote, or newly chosen types, now
	return nil
}

// resetPushes makes the next push mirror this deployment to a remote, for
// "Push everything again": every file's record for it is set to differ in
// content, priority and archive flag, so all three are sent and replace the
// remote's. Files never pushed there keep having no record, so one archived
// before it was ever pushed still stays here.
func (l *localMode) resetPushes(name string) error {
	return l.s.updateState(func(st map[string]*fileState) (bool, error) {
		changed := false
		for _, e := range st {
			if rec := e.Sync[name]; rec != nil {
				*rec = syncRecord{Archived: !e.Archived} // no hash, no priority
				changed = true
			}
		}
		return changed, nil
	})
}

// forgetPushes drops every file's sync record for a remote, so the next
// push treats it as new.
func (l *localMode) forgetPushes(name string) error {
	return l.s.updateState(func(st map[string]*fileState) (bool, error) {
		changed := false
		for _, e := range st {
			if _, ok := e.Sync[name]; ok {
				delete(e.Sync, name)
				changed = true
			}
		}
		return changed, nil
	})
}
