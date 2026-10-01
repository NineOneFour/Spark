package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// newCSRFToken returns a random token for this process. Every settings form
// carries it, so another site can't post to the page even when login is off
// and there is no session cookie to tie a token to.
func newCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("csrf token: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// checkPost rejects cross-site form posts: by the browser's Sec-Fetch-Site or
// Origin header when present, and always by the form token.
func (s *server) checkPost(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			return false
		}
	}
	return subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(s.csrf)) == 1
}

type priorityColor struct {
	Priority string
	Color    string
}

func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, http.StatusOK, "", nil)
}

// renderSettings shows the settings page. extra carries one-time values to
// show, such as a new invite link.
func (s *server) renderSettings(w http.ResponseWriter, r *http.Request, status int, errMsg string, extra map[string]any) {
	v := viewer(r)
	data := map[string]any{
		"Title": "Settings · Spark",
		"Error": errMsg,
		"CSRF":  s.csrf,
	}
	if s.mode.canEditSettings(v) {
		types, err := loadProjectTypes(s.cfg.Root)
		if err != nil {
			log.Printf("load project types: %v", err)
		}
		colors, err := loadPriorityColors(s.cfg.Root)
		if err != nil {
			log.Printf("load priority colors: %v", err)
			colors = defaultPriorityColors
		}
		var priorities []priorityColor
		for p := 1; p <= 5; p++ {
			priorities = append(priorities, priorityColor{strconv.Itoa(p), colors[strconv.Itoa(p)]})
		}
		data["Types"], data["Priorities"] = types, priorities
		data["EditSettings"] = true
	}

	// Archived files are hidden everywhere else, so this is the only way
	// back to them.
	projects, err := s.loadProjects()
	if err != nil {
		log.Printf("load projects: %v", err)
	}
	var archived []*Project
	for _, p := range projects {
		if p.Archived && s.mode.canEdit(v, p) {
			archived = append(archived, p)
		}
	}
	data["Archived"] = archived

	s.mode.settingsData(r, v, data)
	for k, val := range extra {
		data[k] = val
	}
	w.WriteHeader(status)
	s.render(w, r, "settings", data)
}

// updateSettings wraps a settings change: it checks the post and the
// viewer's right to change shared settings, serializes writes, and either
// redirects back to the page or shows the error there.
func (s *server) updateSettings(change func(r *http.Request) error) http.HandlerFunc {
	return s.updateSettingsAs(func(r *http.Request) bool { return s.mode.canEditSettings(viewer(r)) }, change)
}

// updateSettingsAs is updateSettings with its own permission check, for
// settings any viewer may change (such as their own API keys).
func (s *server) updateSettingsAs(allowed func(r *http.Request) bool, change func(r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkPost(r) {
			http.Error(w, "This form has expired. Reload the settings page and try again.", http.StatusForbidden)
			return
		}
		if !allowed(r) {
			http.Error(w, "Only an admin can change this.", http.StatusForbidden)
			return
		}
		s.settingsMu.Lock()
		err := change(r)
		s.settingsMu.Unlock()

		var userErr userError
		switch {
		case errors.As(err, &userErr):
			s.renderSettings(w, r, http.StatusBadRequest, userErr.Error(), nil)
		case err != nil:
			log.Printf("save settings: %v", err)
			s.renderSettings(w, r, http.StatusInternalServerError, "Could not save the settings. Check the server log.", nil)
		default:
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
		}
	}
}

// userError is a problem with what was typed into a form, shown on the page.
type userError string

func (e userError) Error() string { return string(e) }

func (s *server) changeProjectTypes(r *http.Request) error {
	name := strings.TrimSpace(r.PostFormValue("name"))
	color := strings.ToLower(r.PostFormValue("color"))

	// Work on the file as written, so entries the page can't show (invalid
	// ones) are kept for the user to fix by hand rather than silently lost.
	var types []projectType
	path := filepath.Join(configDir(s.cfg.Root), projectTypesFile)
	if err := readJSON(path, &types); err != nil {
		return err
	}
	i := slices.IndexFunc(types, func(t projectType) bool { return t.Name == name })

	switch action := r.PostFormValue("action"); action {
	case "add", "save":
		if action == "add" {
			if !typeNameRe.MatchString(name) {
				return userError("A type name uses lowercase letters and digits, with words joined by hyphens, like client-work.")
			}
			if i >= 0 {
				return userError(fmt.Sprintf("%s already exists.", name))
			}
		} else if i < 0 {
			return userError(fmt.Sprintf("%s no longer exists.", name))
		}
		if !colorRe.MatchString(color) {
			return userError("Pick a color.")
		}
		if action == "add" {
			types = append(types, projectType{name, color})
		} else {
			types[i].Color = color
		}
	case "remove":
		if i >= 0 {
			types = slices.Delete(types, i, i+1)
		}
	default:
		return userError("Unknown action.")
	}
	return writeJSON(path, types)
}

func (s *server) changePriorityColors(r *http.Request) error {
	colors := map[string]string{}
	for p := range defaultPriorityColors {
		c := strings.ToLower(r.PostFormValue("color-" + p))
		if !colorRe.MatchString(c) {
			return userError(fmt.Sprintf("Pick a color for priority %s.", p))
		}
		colors[p] = c
	}
	return writeJSON(filepath.Join(configDir(s.cfg.Root), priorityColorsFile), colors)
}
