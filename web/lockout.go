package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

// Failed logins count per account, on a penalty schedule (SPARK_PENALTY_START,
// SPARK_LOCKOUT_AFTER, SPARK_LOCKOUT). After a failure the account is refused
// for a while; a refused attempt isn't checked and doesn't count. Enough
// failures lock it until `web unlock`. The state is in Config/lockouts.json,
// so that command, a separate process, can clear it while the server runs.

const (
	lockoutsFile = "lockouts.json"
	// maxGhosts caps the usernames tracked that have no account.
	maxGhosts = 10000
	// ghostKeyLength cuts long typed usernames down before they are kept.
	ghostKeyLength = 64
)

const lockedText = "This account is locked after too many failed logins. Whoever runs this Spark can unlock it with: web unlock <username>"

// penalty is how long the nth failure in a row makes the caller wait, and
// whether it locks instead: a 2 s wait up to the start, then 30 s, then 1,
// 2, 4, 8, 16 and 32 minutes, and 32 from there on. A start of 0 means 2 s
// forever.
func (c config) penalty(n int) (wait time.Duration, locked bool) {
	if c.LockoutAfter > 0 && n >= c.LockoutAfter {
		return 0, true
	}
	switch {
	case c.PenaltyStart == 0 || n <= c.PenaltyStart:
		return 2 * time.Second, false
	case n == c.PenaltyStart+1:
		return 30 * time.Second, false
	}
	return time.Minute << min(n-c.PenaltyStart-2, 5), false
}

type lockEntry struct {
	Failures int       `json:"failures"`
	Until    time.Time `json:"until"` // refused before this
	Locked   bool      `json:"locked,omitempty"`
}

type lockoutsData struct {
	Accounts map[string]*lockEntry `json:"accounts,omitempty"`
}

type lockouts struct {
	path string

	mu sync.Mutex // serializes read-modify-write of lockouts.json and the maps below
	// inFlight holds the usernames being checked right now. One attempt at
	// a time per account, so parallel guesses can't all slip into one wait.
	inFlight map[string]bool
	// ghosts are usernames with no account. They follow the same schedule,
	// so a refusal doesn't reveal whether an account exists, but they live
	// only in memory, capped, so guessing names can't grow the file.
	ghosts map[string]*lockEntry
}

func newLockouts(root string) *lockouts {
	return &lockouts{
		path:     filepath.Join(configDir(root), lockoutsFile),
		inFlight: map[string]bool{},
		ghosts:   map[string]*lockEntry{},
	}
}

func readLockouts(path string) (*lockoutsData, error) {
	d := &lockoutsData{}
	err := readJSON(path, d)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	if d.Accounts == nil {
		d.Accounts = map[string]*lockEntry{}
	}
	return d, err
}

func ghostKey(username string) string {
	if len(username) > ghostKeyLength {
		return username[:ghostKeyLength]
	}
	return username
}

// change runs fn on username's entry (nil if there is none) under the lock.
// fn returns the entry to keep, or nil to drop it. Known accounts are read
// from and written to the file; ghosts stay in memory.
func (l *lockouts) change(username string, known bool, fn func(e *lockEntry) *lockEntry) error {
	if !known {
		key := ghostKey(username)
		if e := fn(l.ghosts[key]); e != nil {
			l.addGhost(key, e)
		} else {
			delete(l.ghosts, key)
		}
		return nil
	}
	d, err := readLockouts(l.path)
	if err != nil {
		return err
	}
	old := d.Accounts[username]
	e := fn(old)
	if e == nil && old == nil {
		return nil
	}
	if e != nil {
		d.Accounts[username] = e
	} else {
		delete(d.Accounts, username)
	}
	return writeJSON(l.path, d)
}

// addGhost keeps a ghost entry, making room when the map is full: first by
// dropping entries whose wait is over, then any entry.
func (l *lockouts) addGhost(key string, e *lockEntry) {
	if _, ok := l.ghosts[key]; !ok && len(l.ghosts) >= maxGhosts {
		now := time.Now()
		for k, g := range l.ghosts {
			if !g.Locked && now.After(g.Until) {
				delete(l.ghosts, k)
			}
		}
		for k := range l.ghosts {
			if len(l.ghosts) < maxGhosts {
				break
			}
			delete(l.ghosts, k)
		}
	}
	l.ghosts[key] = e
}

// verdict is what begin decided about a login attempt.
type verdict struct {
	ok     bool          // check the password
	wait   time.Duration // when refused and not locked: how long is left
	locked bool
}

// begin decides whether a login attempt for username may be checked. When
// it may, the caller must call end with the result.
func (l *lockouts) begin(username string, known bool) (verdict, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := username
	if !known {
		key = ghostKey(username)
	}
	if l.inFlight[key] {
		return verdict{wait: 2 * time.Second}, nil
	}
	var e *lockEntry
	if known {
		d, err := readLockouts(l.path)
		if err != nil {
			return verdict{}, err
		}
		e = d.Accounts[username]
	} else {
		e = l.ghosts[key]
	}
	if e != nil && e.Locked {
		return verdict{locked: true}, nil
	}
	if e != nil {
		if left := time.Until(e.Until); left > 0 {
			return verdict{wait: left}, nil
		}
	}
	l.inFlight[key] = true
	return verdict{ok: true}, nil
}

// end records the result of an attempt begin allowed. A correct password
// clears the count; a wrong one moves it along the schedule. It returns the
// entry after a failure.
func (l *lockouts) end(cfg config, username string, known, correct bool) (*lockEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := username
	if !known {
		key = ghostKey(username)
	}
	delete(l.inFlight, key)
	var after *lockEntry
	err := l.change(username, known, func(e *lockEntry) *lockEntry {
		if correct {
			return nil
		}
		n := 1
		if e != nil {
			n = e.Failures + 1
		}
		wait, locked := cfg.penalty(n)
		after = &lockEntry{Failures: n, Until: time.Now().UTC().Add(wait), Locked: locked}
		return after
	})
	return after, err
}

// forget drops an account's entry, for an account that was removed.
func (l *lockouts) forget(username string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.change(username, true, func(*lockEntry) *lockEntry { return nil })
}

// unlockAccount clears an account's failures and lock. It runs as its own
// process (`web unlock`), so it works on the file alone. It reports whether
// there was anything to clear.
func unlockAccount(root, username string) (bool, error) {
	path := filepath.Join(configDir(root), lockoutsFile)
	d, err := readLockouts(path)
	if err != nil {
		return false, err
	}
	if d.Accounts[username] == nil {
		return false, nil
	}
	delete(d.Accounts, username)
	return true, writeJSON(path, d)
}

// waitText is a wait as people say it, rounded up.
func waitText(d time.Duration) string {
	if d <= time.Minute {
		s := int((d + time.Second - 1) / time.Second)
		if s == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", s)
	}
	m := int((d + time.Minute - 1) / time.Minute)
	return fmt.Sprintf("%d minutes", m)
}
