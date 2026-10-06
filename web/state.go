package main

import (
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

// stateFile holds each file's priority and archive flag. They live here, not
// in the snapshot, because the collector rewrites every snapshot on each run
// and would overwrite a priority set in the web app or on a remote. The
// snapshot's own priority only seeds the entry the first time a file is seen.
const stateFile = "state.json"

// fileState is one entry in state.json, keyed by file id. On remote,
// PrioritySet is the remote's own clock when the change arrived, and the
// latest stamp wins. Sync is local only: what was last pushed to each remote.
type fileState struct {
	Priority    string                 `json:"priority"`
	PrioritySet time.Time              `json:"priority_set"`
	Archived    bool                   `json:"archived"`
	ArchiveAt   *time.Time             `json:"archive_at,omitempty"` // archived automatically from then on
	Sync        map[string]*syncRecord `json:"sync,omitempty"`
}

// updateState runs change on state.json under the state lock and writes the
// file back if change reports that it changed something.
func (s *server) updateState(change func(st map[string]*fileState) (bool, error)) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	path := filepath.Join(configDir(s.cfg.Root), stateFile)
	st := map[string]*fileState{}
	if err := readJSON(path, &st); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	changed, err := change(st)
	if err != nil || !changed {
		return err
	}
	return writeJSON(path, st)
}

// seedState is a new file's state, from the snapshot's own priority. An
// archived seed starts at priority 5, so unarchiving it later gives it one.
func seedState(start string) *fileState {
	e := &fileState{Priority: start, PrioritySet: time.Now().UTC()}
	if start == "archived" {
		e.Priority, e.Archived = "5", true
	}
	return e
}

// applyState copies each file's state onto it, seeding state.json for files
// it hasn't seen and archiving files whose archive date has passed.
func (s *server) applyState(projects []*Project) error {
	return s.updateState(func(st map[string]*fileState) (bool, error) {
		changed := false
		now := time.Now()
		for _, p := range projects {
			e := st[p.ID]
			if e == nil {
				e = seedState(p.startPriority)
				st[p.ID] = e
				changed = true
			}
			if e.ArchiveAt != nil && now.After(*e.ArchiveAt) {
				e.Archived, e.ArchiveAt = true, nil
				changed = true
			}
			p.Priority, p.Archived = e.Priority, e.Archived
		}
		return changed, nil
	})
}

// changeState sets a file's priority, or archives or unarchives it, from the
// form on the project page and the Archived list in settings.
func (s *server) changeState(w http.ResponseWriter, r *http.Request) {
	if !s.checkPost(r) {
		http.Error(w, "This form has expired. Reload the page and try again.", http.StatusForbidden)
		return
	}
	projects, err := s.loadProjects()
	if err != nil {
		log.Printf("load projects: %v", err)
		http.Error(w, "Could not read the project directory. Check the server log.", http.StatusInternalServerError)
		return
	}
	var p *Project
	for _, f := range projects {
		if f.ID == r.PostFormValue("file") && f.Key == r.PathValue("key") {
			p = f
		}
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}
	if !s.mode.canEdit(viewer(r), p) {
		http.Error(w, "Only the file's owner or an admin can change this.", http.StatusForbidden)
		return
	}

	back := "/p/" + url.PathEscape(p.Key)
	if p.Owner != "" {
		back += "?u=" + url.QueryEscape(p.Owner)
	}
	action, priority := r.PostFormValue("action"), r.PostFormValue("priority")
	switch action {
	case "priority":
		if _, ok := priorityNames[priority]; !ok {
			http.Error(w, "Unknown priority.", http.StatusBadRequest)
			return
		}
	case "archive":
		back = "/"
	case "unarchive":
		back = "/settings"
	default:
		http.Error(w, "Unknown action.", http.StatusBadRequest)
		return
	}

	err = s.updateState(func(st map[string]*fileState) (bool, error) {
		e := st[p.ID] // loadProjects seeded it, unless state.json was just removed
		if e == nil {
			e = &fileState{Priority: p.Priority, Archived: p.Archived}
			st[p.ID] = e
		}
		switch action {
		case "priority":
			e.Priority, e.PrioritySet = priority, time.Now().UTC()
		case "archive":
			e.Archived, e.ArchiveAt = true, nil // a choice made by hand wins
		case "unarchive":
			e.Archived, e.ArchiveAt = false, nil
		}
		return true, nil
	})
	if err != nil {
		log.Printf("save state: %v", err)
		http.Error(w, "Could not save the change. Check the server log.", http.StatusInternalServerError)
		return
	}
	s.mode.stateChanged()
	http.Redirect(w, r, back, http.StatusSeeOther)
}
