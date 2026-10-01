package main

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The sync API (see api.go). Ingest is a plain Markdown write after checking
// the snapshot against format.md; sanitizing happens at display, on every
// render. The remote never runs pushed content.

// localFileRe is a local file id as the collector names it (projectName__
// projectType, camel case of letters and digits), so a pushed id can never
// hold a path separator, a dot, or a third "__".
var localFileRe = regexp.MustCompile(`^[\p{L}\p{N}]+__[\p{L}\p{N}]+$`)

// requireKey lets a request through only with a valid API key, and makes the
// key's account the viewer.
func (m *remoteMode) requireKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || key == "" {
			http.Error(w, "missing API key", http.StatusUnauthorized)
			return
		}
		d, err := m.s.auth.load()
		if err != nil {
			log.Printf("load accounts: %v", err)
			http.Error(w, "could not check the API key", http.StatusInternalServerError)
			return
		}
		hash := tokenHash(key)
		for _, acct := range d.Accounts {
			for _, k := range acct.Keys {
				if k.Hash == hash {
					next(w, r.WithContext(withViewer(r.Context(), acct)))
					return
				}
			}
		}
		http.Error(w, "unknown API key", http.StatusUnauthorized)
	}
}

func writeAPI(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (m *remoteMode) apiTypes(w http.ResponseWriter, r *http.Request) {
	types, err := loadProjectTypes(m.s.cfg.Root)
	if err != nil {
		log.Printf("load project types: %v", err)
		http.Error(w, "could not read the project types", http.StatusInternalServerError)
		return
	}
	resp := typesResponse{All: m.acceptAll(), Types: []string{}}
	for _, t := range types {
		resp.Types = append(resp.Types, t.Name)
	}
	writeAPI(w, resp)
}

// apiPush stores a snapshot as username__id.md, overwriting the last push,
// and applies the priority and archive flag if they were sent. The remote
// stamps a priority change with its own clock on arrival.
func (m *remoteMode) apiPush(w http.ResponseWriter, r *http.Request) {
	v := viewer(r)
	id := r.PathValue("id")
	if !localFileRe.MatchString(id) {
		http.Error(w, "file id must be projectName__projectType", http.StatusBadRequest)
		return
	}
	var req pushRequest
	r.Body = http.MaxBytesReader(w, r.Body, 2*maxPush) // JSON escaping can double the size
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "body must be a JSON push request", http.StatusBadRequest)
		return
	}
	if len(req.Content) > maxPush {
		http.Error(w, "snapshot is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if req.Priority != nil {
		if _, ok := priorityNames[*req.Priority]; !ok {
			http.Error(w, "priority must be 1 to 5", http.StatusUnprocessableEntity)
			return
		}
	}

	types, err := loadProjectTypes(m.s.cfg.Root)
	if err != nil {
		log.Printf("load project types: %v", err)
		http.Error(w, "could not read the project types", http.StatusInternalServerError)
		return
	}
	listed := map[string]bool{}
	for _, t := range types {
		listed[t.Name] = true
	}
	p, err := parseSnapshot([]byte(req.Content), func(name string) bool { return m.acceptsType(name, listed) })
	if err != nil {
		http.Error(w, "snapshot rejected: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	fileID := v.Username + "__" + id
	if err := writeFileAtomic(filepath.Join(m.s.cfg.Root, "Projects", fileID+".md"), []byte(req.Content)); err != nil {
		log.Printf("save push %s: %v", fileID, err)
		http.Error(w, "could not save the snapshot", http.StatusInternalServerError)
		return
	}

	var resp pushResponse
	err = m.s.updateState(func(st map[string]*fileState) (bool, error) {
		e := st[fileID]
		if e == nil {
			e = seedState(p.startPriority)
			st[fileID] = e
		}
		if req.Priority != nil {
			e.Priority, e.PrioritySet = *req.Priority, time.Now().UTC()
		}
		if req.Archived != nil {
			e.Archived = *req.Archived
		}
		resp = pushResponse{Priority: e.Priority, PrioritySet: e.PrioritySet}
		return true, nil
	})
	if err != nil {
		log.Printf("save state for %s: %v", fileID, err)
		http.Error(w, "could not save the snapshot's state", http.StatusInternalServerError)
		return
	}
	writeAPI(w, resp)
}

// apiPriorities returns the priority of every file the key's user pushed,
// under the ids their local deployment uses.
func (m *remoteMode) apiPriorities(w http.ResponseWriter, r *http.Request) {
	prefix := viewer(r).Username + "__"
	out := map[string]priorityEntry{}
	err := m.s.updateState(func(st map[string]*fileState) (bool, error) {
		for fileID, e := range st {
			if id, ok := strings.CutPrefix(fileID, prefix); ok {
				out[id] = priorityEntry{Priority: e.Priority, PrioritySet: e.PrioritySet}
			}
		}
		return false, nil
	})
	if err != nil {
		log.Printf("load state: %v", err)
		http.Error(w, "could not read priorities", http.StatusInternalServerError)
		return
	}
	writeAPI(w, out)
}
