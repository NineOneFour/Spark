package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
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
// key's account the viewer. Wrong keys count against the client's address
// (lockout.go); valid calls count against the account's rate.
func (m *remoteMode) requireKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := m.s
		key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		hash := tokenHash(key)
		var acct *account
		ip := s.clientIP(r)
		v, after, err := s.auth.lock.apiAttempt(s.cfg, ip, func() (bool, error) {
			if key == "" {
				return false, nil
			}
			d, err := s.auth.load()
			if err != nil {
				return false, err
			}
			for _, a := range d.Accounts {
				if slices.ContainsFunc(a.Keys, func(k apiKey) bool { return k.Hash == hash }) {
					acct = a
				}
			}
			return acct != nil, nil
		})
		switch {
		case err != nil:
			log.Printf("check API key: %v", err)
			http.Error(w, "could not check the API key", http.StatusInternalServerError)
			return
		case v.locked:
			http.Error(w, fmt.Sprintf("this address is blocked after too many wrong API keys; whoever runs the remote can unblock it with: web unlock --ip %s", ip), http.StatusForbidden)
			return
		case after != nil:
			s.securityEvent(r, "api key rejected", "", "failures", after.Failures)
			if after.Locked {
				s.securityEvent(r, "api ip blocked", "", "failures", after.Failures)
			}
			if key == "" {
				http.Error(w, "missing API key", http.StatusUnauthorized)
			} else {
				http.Error(w, "unknown API key", http.StatusUnauthorized)
			}
			return
		case !v.ok:
			w.Header().Set("Retry-After", strconv.Itoa(int(v.wait.Seconds())+1))
			http.Error(w, "too many wrong API keys from this address; try again in "+waitText(v.wait), http.StatusTooManyRequests)
			return
		}
		if ok, wait := m.rate.allow(acct.Username, s.cfg.APIRate); !ok {
			if m.rate.shouldLog(acct.Username) {
				s.securityEvent(r, "api throttled", acct.Username, "rate_per_minute", s.cfg.APIRate)
			}
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, fmt.Sprintf("too many requests: the limit is %d a minute per account", s.cfg.APIRate), http.StatusTooManyRequests)
			return
		}
		m.noteKeyUse(acct.Username, hash)
		next(w, r.WithContext(withViewer(r.Context(), acct)))
	}
}

// noteKeyUse records when a key was last used, for the account page. It is
// written at most hourly, so pushes don't rewrite accounts.json each time.
func (m *remoteMode) noteKeyUse(username, hash string) {
	now := time.Now().UTC()
	err := m.s.auth.update(func(d *accountsData) (bool, error) {
		a := d.find(username)
		if a == nil {
			return false, nil
		}
		i := slices.IndexFunc(a.Keys, func(k apiKey) bool { return k.Hash == hash })
		if i < 0 || (a.Keys[i].LastUsed != nil && now.Sub(*a.Keys[i].LastUsed) < keyUseEvery) {
			return false, nil
		}
		a.Keys[i].LastUsed = &now
		return true, nil
	})
	if err != nil {
		log.Printf("save key use: %v", err)
	}
}

// rateLimiter is a token bucket per account: a full minute's calls at once,
// refilled evenly over the minute.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	logged  map[string]time.Time // last "api throttled" line per account
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]*bucket{}, logged: map[string]time.Time{}}
}

// allow takes one call from the account's bucket. When it is empty it says
// how long until the next call is allowed. perMinute 0 means no limit.
func (rl *rateLimiter) allow(username string, perMinute int) (bool, time.Duration) {
	if perMinute <= 0 {
		return true, 0
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	perSecond := float64(perMinute) / 60
	b := rl.buckets[username]
	if b == nil {
		b = &bucket{tokens: float64(perMinute), last: now}
		rl.buckets[username] = b
	}
	b.tokens = min(float64(perMinute), b.tokens+now.Sub(b.last).Seconds()*perSecond)
	b.last = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / perSecond * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// shouldLog limits "api throttled" lines to one a minute per account.
func (rl *rateLimiter) shouldLog(username string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if time.Since(rl.logged[username]) < time.Minute {
		return false
	}
	rl.logged[username] = time.Now()
	return true
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
	resp := typesResponse{All: m.acceptAll(), Types: []string{}, Limits: &apiLimits{
		RatePerMinute: m.s.cfg.APIRate,
		MaxFileBytes:  m.s.cfg.MaxFileBytes,
		MaxProjects:   m.maxProjects(),
	}}
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
	max := m.s.cfg.MaxFileBytes
	r.Body = http.MaxBytesReader(w, r.Body, int64(2*max+4096)) // JSON escaping can double the size
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, fmt.Sprintf("snapshot is larger than this remote's limit of %d KB", max>>10), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "body must be a JSON push request", http.StatusBadRequest)
		return
	}
	if len(req.Content) > max {
		http.Error(w, fmt.Sprintf("snapshot is larger than this remote's limit of %d KB", max>>10), http.StatusRequestEntityTooLarge)
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
	limit := m.maxProjects()
	full := 0 // the account's project count, when a new project is refused
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
		if limit > 0 {
			n, isNew, err := m.countProjects(v.Username, fileID)
			if err != nil {
				return false, err
			}
			if isNew && n >= limit {
				full = n
				return false, nil
			}
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
	if full > 0 {
		http.Error(w, fmt.Sprintf("%s already has %d projects on this remote, the most allowed (archived ones count); ask the admin to raise the limit or remove an old project", v.Username, full), http.StatusConflict)
		return
	}
	if err != nil {
		log.Printf("save push %s: %v", fileID, err)
		http.Error(w, "could not save the snapshot", http.StatusInternalServerError)
		return
	}
	writeAPI(w, resp)
}

// countProjects counts the account's files on this remote (archived ones
// too; a removed account's files have another name) and reports whether
// fileID would be a new one.
func (m *remoteMode) countProjects(username, fileID string) (int, bool, error) {
	entries, err := os.ReadDir(filepath.Join(m.s.cfg.Root, "Projects"))
	if err != nil {
		return 0, false, err
	}
	n, isNew := 0, true
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, username+"__") && strings.HasSuffix(name, ".md") {
			n++
			if name == fileID+".md" {
				isNew = false
			}
		}
	}
	return n, isNew, nil
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
