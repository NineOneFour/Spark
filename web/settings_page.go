package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
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
	s.renderSettings(w, http.StatusOK, "")
}

func (s *server) renderSettings(w http.ResponseWriter, status int, errMsg string) {
	roots, err := readScanRoots(s.cfg.Root)
	if err != nil {
		log.Printf("load scan roots: %v", err)
	}
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

	w.WriteHeader(status)
	s.render(w, "settings", map[string]any{
		"Title":      "Settings · Spark",
		"Error":      errMsg,
		"CSRF":       s.csrf,
		"ScanRoots":  roots,
		"Types":      types,
		"Priorities": priorities,
	})
}

// updateSettings wraps a settings change: it checks the post, serializes
// writes, and either redirects back to the page or shows the error there.
func (s *server) updateSettings(change func(r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkPost(r) {
			http.Error(w, "This form has expired. Reload the settings page and try again.", http.StatusForbidden)
			return
		}
		s.settingsMu.Lock()
		err := change(r)
		s.settingsMu.Unlock()

		var userErr userError
		switch {
		case errors.As(err, &userErr):
			s.renderSettings(w, http.StatusBadRequest, userErr.Error())
		case err != nil:
			log.Printf("save settings: %v", err)
			s.renderSettings(w, http.StatusInternalServerError, "Could not save the settings. Check the server log.")
		default:
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
		}
	}
}

// userError is a problem with what was typed into a form, shown on the page.
type userError string

func (e userError) Error() string { return string(e) }

func (s *server) changeScanRoots(r *http.Request) error {
	path := strings.TrimSpace(r.PostFormValue("path"))
	roots, err := readScanRoots(s.cfg.Root)
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
	return writeJSON(filepath.Join(configDir(s.cfg.Root), scanRootsFile), roots)
}

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
