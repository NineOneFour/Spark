package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
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

	// The id picks the card the file joins, so it must be the one the
	// collector would name this snapshot; otherwise anyone could put a file
	// of any project or type onto someone else's card.
	if want := camelCase(p.Name) + "__" + camelCase(p.Type); id != want {
		http.Error(w, fmt.Sprintf("file id %s does not match the snapshot, which the collector names %s", id, want), http.StatusUnprocessableEntity)
		return
	}

	fileID := v.Username + "__" + id
	var resp pushResponse
	gone := false
	// The file and its state are written under the state lock, which
	// removing an account also holds while it renames the account's files,
	// so a push that races a removal can't leave a file under the old name.
	err = m.s.updateState(func(st map[string]*fileState) (bool, error) {
		d, err := m.s.auth.load()
		if err != nil {
			return false, err
		}
		if d.find(v.Username) == nil {
			gone = true
			return false, nil
		}
		if err := writeFileAtomic(filepath.Join(m.s.cfg.Root, "Projects", fileID+".md"), []byte(req.Content)); err != nil {
			return false, err
		}
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
	if gone {
		http.Error(w, "unknown API key", http.StatusUnauthorized)
		return
	}
	if err != nil {
		log.Printf("save push %s: %v", fileID, err)
		http.Error(w, "could not save the snapshot", http.StatusInternalServerError)
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

// wordRe and camelCase match the collector's camel_case: join the letter and
// digit runs, "Spark / Web App" -> "sparkWebApp".
var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

func camelCase(text string) string {
	var b strings.Builder
	for i, w := range wordRe.FindAllString(text, -1) {
		w = strings.ToLower(w)
		if i > 0 {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			w = string(r)
		}
		b.WriteString(w)
	}
	return b.String()
}
