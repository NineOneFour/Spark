package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// pushEvery is how often Projects/ is checked for changed content. The
	// collector rewrites every snapshot each run, so a push is decided by
	// content hash, never by file time.
	pushEvery = time.Minute
	// pullEvery is how often each remote is asked for the priorities of
	// this deployment's files.
	pullEvery = 15 * time.Minute
)

// syncRecord is what was last pushed to (or agreed with) one remote, kept
// in the file's state.json entry. A file is pushed again when its content,
// priority or archive flag differs from the record.
type syncRecord struct {
	Hash      string    `json:"hash"`
	Priority  string    `json:"priority"`
	Archived  bool      `json:"archived"`
	RemoteSet time.Time `json:"remote_set"` // the remote's priority_set we last saw
}

var syncClient = &http.Client{Timeout: 15 * time.Second}

func (l *localMode) syncLoop() {
	push := time.NewTicker(pushEvery)
	pull := time.NewTicker(pullEvery)
	l.pull()
	l.push()
	for {
		select {
		case <-push.C:
		case <-l.kick:
		case <-pull.C:
			l.pull()
		}
		l.push()
	}
}

// records returns a copy of every file's sync records.
func (l *localMode) records() map[string]map[string]syncRecord {
	out := map[string]map[string]syncRecord{}
	err := l.s.updateState(func(st map[string]*fileState) (bool, error) {
		for id, e := range st {
			out[id] = map[string]syncRecord{}
			for name, rec := range e.Sync {
				out[id][name] = *rec
			}
		}
		return false, nil
	})
	if err != nil {
		log.Printf("sync: load state: %v", err)
	}
	return out
}

// push sends each file to every remote its type is assigned to, if it
// changed since the last push there. A file archived here before it was ever
// pushed stays here.
func (l *localMode) push() {
	remotes, err := readRemotes(l.s.cfg.Root)
	reportInvalid(remotesFile, err)
	if len(remotes) == 0 {
		return
	}
	projects, err := l.s.loadProjects()
	if err != nil {
		log.Printf("sync: load projects: %v", err)
		return
	}
	records := l.records()

	for _, p := range projects {
		raw, err := os.ReadFile(filepath.Join(l.s.cfg.Root, "Projects", p.ID+".md"))
		if err != nil {
			continue // removed since loadProjects
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])

		for _, rc := range remotes {
			if !slices.Contains(rc.Types, p.Type) {
				continue
			}
			label := fmt.Sprintf("push of %s to %s", p.ID, rc.Name)
			rec, pushed := records[p.ID][rc.Name]
			if !pushed && p.Archived {
				continue
			}
			if pushed && rec.Hash == hash && rec.Priority == p.Priority && rec.Archived == p.Archived {
				continue
			}
			if len(raw) > maxPush {
				reportInvalid(label, fmt.Errorf("larger than %d bytes", maxPush))
				continue
			}

			req := pushRequest{Content: string(raw)}
			if !pushed || rec.Priority != p.Priority {
				req.Priority = &p.Priority
			}
			if !pushed || rec.Archived != p.Archived {
				req.Archived = &p.Archived
			}
			var resp pushResponse
			err := callRemote(rc, http.MethodPut, "/api/files/"+url.PathEscape(p.ID), req, &resp)
			reportInvalid(label, err)
			if err != nil {
				continue
			}

			sent := syncRecord{Hash: hash, Priority: p.Priority, Archived: p.Archived, RemoteSet: resp.PrioritySet}
			if req.Priority == nil {
				// The remote kept its own priority, which may be newer than
				// ours; leave the stamp alone so the next pull compares it.
				sent.RemoteSet = rec.RemoteSet
			}
			err = l.s.updateState(func(st map[string]*fileState) (bool, error) {
				e := st[p.ID]
				if e == nil {
					return false, nil
				}
				if e.Sync == nil {
					e.Sync = map[string]*syncRecord{}
				}
				e.Sync[rc.Name] = &sent
				return true, nil
			})
			if err != nil {
				log.Printf("sync: save state: %v", err)
			}
		}
	}
}

// pull asks each remote for the priorities of this deployment's files and
// takes any set there since the last one seen. A priority changed here and
// not yet pushed is kept: it reaches the remote later, so it is the last
// write there too.
func (l *localMode) pull() {
	remotes, err := readRemotes(l.s.cfg.Root)
	reportInvalid(remotesFile, err)
	for _, rc := range remotes {
		var got map[string]priorityEntry
		err := callRemote(rc, http.MethodGet, "/api/priorities", nil, &got)
		reportInvalid("priorities from "+rc.Name, err)
		if err != nil {
			continue
		}
		err = l.s.updateState(func(st map[string]*fileState) (bool, error) {
			changed := false
			for id, pe := range got {
				e := st[id]
				if e == nil || e.Sync[rc.Name] == nil {
					continue
				}
				rec := e.Sync[rc.Name]
				if _, ok := priorityNames[pe.Priority]; !ok || !pe.PrioritySet.After(rec.RemoteSet) {
					continue
				}
				if e.Priority != rec.Priority {
					continue // changed here since the last push
				}
				e.Priority, e.PrioritySet = pe.Priority, pe.PrioritySet
				rec.Priority, rec.RemoteSet = pe.Priority, pe.PrioritySet
				changed = true
			}
			return changed, nil
		})
		if err != nil {
			log.Printf("sync: save state: %v", err)
		}
	}
}

// fetchRemoteTypes asks a remote which project types it accepts. It also
// proves the URL and key work.
func fetchRemoteTypes(rc remoteConfig) (typesResponse, error) {
	var t typesResponse
	err := callRemote(rc, http.MethodGet, "/api/types", nil, &t)
	return t, err
}

// callRemote sends one API call. body and out are JSON; either may be nil.
func callRemote(rc remoteConfig, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, rc.URL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+rc.Key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := syncClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("the remote rejected the API key")
	case resp.StatusCode/100 != 2:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPush)).Decode(out); err != nil {
		return fmt.Errorf("unexpected reply: %w", err)
	}
	return nil
}
