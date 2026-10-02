package main

import (
	"errors"
	"io/fs"
	"log"
	"path/filepath"
	"sync"
	"time"
)

// Sessions live on the server, in Config/sessions.json, so logging out ends
// one for real and an account can be logged out everywhere. The cookie holds
// a random id; the file keeps only its hash, so reading the file doesn't let
// anyone log in. A session ends after SPARK_SESSION_IDLE without use, and
// 30 days after login whatever happens.

const (
	sessionsFile = "sessions.json"
	// lastSeenEvery is how stale a session's last use may get before it is
	// written again, so browsing doesn't rewrite the file on every page.
	lastSeenEvery = 5 * time.Minute
)

type session struct {
	Username string    `json:"username"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

type sessionsData struct {
	Sessions map[string]*session `json:"sessions"` // by tokenHash of the id
}

type sessionStore struct {
	path string
	idle time.Duration

	mu sync.Mutex // guards byHash and writes of sessions.json
	// byHash is the file in memory. The server is its only writer.
	byHash map[string]*session
}

func loadSessions(root string, idle time.Duration) (*sessionStore, error) {
	st := &sessionStore{path: filepath.Join(configDir(root), sessionsFile), idle: idle}
	var d sessionsData
	if err := readJSON(st.path, &d); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	st.byHash = d.Sessions
	if st.byHash == nil {
		st.byHash = map[string]*session{}
	}
	return st, nil
}

func (st *sessionStore) expired(s *session, now time.Time) bool {
	return now.Sub(s.LastSeen) > st.idle || now.Sub(s.Created) > sessionLength
}

// save drops expired sessions and writes the file. Callers hold mu.
func (st *sessionStore) save() error {
	now := time.Now()
	for h, s := range st.byHash {
		if st.expired(s, now) {
			delete(st.byHash, h)
		}
	}
	return writeJSON(st.path, sessionsData{Sessions: st.byHash})
}

// create starts a session and returns its id, for the cookie.
func (st *sessionStore) create(username string) (string, error) {
	id := randomToken(32)
	now := time.Now().UTC()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.byHash[tokenHash(id)] = &session{Username: username, Created: now, LastSeen: now}
	return id, st.save()
}

// lookup returns the username a session id belongs to, and notes its use.
func (st *sessionStore) lookup(id string) (string, bool) {
	h := tokenHash(id)
	now := time.Now().UTC()
	st.mu.Lock()
	defer st.mu.Unlock()
	s := st.byHash[h]
	if s == nil {
		return "", false
	}
	if st.expired(s, now) {
		delete(st.byHash, h)
		if err := st.save(); err != nil {
			log.Printf("save sessions: %v", err)
		}
		return "", false
	}
	if now.Sub(s.LastSeen) > lastSeenEvery {
		s.LastSeen = now
		if err := st.save(); err != nil {
			log.Printf("save sessions: %v", err)
		}
	}
	return s.Username, true
}

// end ends one session.
func (st *sessionStore) end(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	h := tokenHash(id)
	if st.byHash[h] == nil {
		return nil
	}
	delete(st.byHash, h)
	return st.save()
}

// endAll ends every session of an account except keepID ("" for none).
func (st *sessionStore) endAll(username, keepID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	keep := ""
	if keepID != "" {
		keep = tokenHash(keepID)
	}
	changed := false
	for h, s := range st.byHash {
		if s.Username == username && h != keep {
			delete(st.byHash, h)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return st.save()
}
