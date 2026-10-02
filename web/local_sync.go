package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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

var (
	syncClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: noRedirects}
	// privateClient is for http:// remotes. It dials only private addresses,
	// checked after DNS, so an API key never crosses the Internet in the
	// clear (SPARK_ALLOW_HTTP_REMOTES lifts this).
	privateClient = &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: noRedirects,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: dialPrivateOnly}).DialContext,
		},
	}
)

// noRedirects keeps the key on the URL it was set up for; a redirect is
// reported instead of followed.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// cgnat is the shared address range Tailscale uses.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isPrivateAddr: loopback, the private ranges (10/8, 172.16/12, 192.168/16,
// fc00::/7), and Tailscale's 100.64/10.
func isPrivateAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || cgnat.Contains(ip)
}

func dialPrivateOnly(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	if !isPrivateAddr(ap.Addr()) {
		return fmt.Errorf("%s is not a private address: use https://, or set SPARK_ALLOW_HTTP_REMOTES=true to allow http:// anywhere", ap.Addr())
	}
	return nil
}

func (l *localMode) client(rc remoteConfig) *http.Client {
	if strings.HasPrefix(rc.URL, "http://") && !l.s.cfg.AllowHTTPRemotes {
		return privateClient
	}
	return syncClient
}

// limitsEvery is how often a remote is asked for its limits again.
const limitsEvery = 15 * time.Minute

type remoteLimits struct {
	apiLimits
	fetched time.Time
}

// limitsFor returns a remote's limits, asking it at most every limitsEvery.
// A remote that states none (an older one) or can't be reached gets this
// deployment's own defaults.
func (l *localMode) limitsFor(rc remoteConfig) apiLimits {
	if c, ok := l.limits[rc.Name]; ok && time.Since(c.fetched) < limitsEvery {
		return c.apiLimits
	}
	lim := apiLimits{RatePerMinute: defaultAPIRate, MaxFileBytes: l.s.cfg.MaxFileBytes}
	l.pace(rc.Name, lim.RatePerMinute)
	if t, err := l.fetchRemoteTypes(rc); err == nil && t.Limits != nil {
		lim = *t.Limits
	}
	l.limits[rc.Name] = remoteLimits{lim, time.Now()}
	return lim
}

// pace waits so calls to one remote stay under its rate, evenly spread: a
// 300-file resync at 120 a minute takes about 2½ minutes.
func (l *localMode) pace(name string, perMinute int) {
	if perMinute <= 0 {
		return
	}
	now := time.Now()
	if next := l.nextCall[name]; next.After(now) {
		time.Sleep(next.Sub(now))
		now = next
	}
	l.nextCall[name] = now.Add(time.Minute / time.Duration(perMinute))
}

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
	limits := map[string]apiLimits{}
	for _, rc := range remotes {
		limits[rc.Name] = l.limitsFor(rc)
	}
	// stopped holds remotes that said "too many requests" this round.
	stopped := map[string]bool{}

	for _, p := range projects {
		raw, err := os.ReadFile(filepath.Join(l.s.cfg.Root, "Projects", p.ID+".md"))
		if err != nil {
			continue // removed since loadProjects
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])

		for _, rc := range remotes {
			if !slices.Contains(rc.Types, p.Type) || stopped[rc.Name] {
				continue
			}
			lim := limits[rc.Name]
			label := fmt.Sprintf("push of %s to %s", p.ID, rc.Name)
			rec, pushed := records[p.ID][rc.Name]
			if !pushed && p.Archived {
				continue
			}
			if pushed && rec.Hash == hash && rec.Priority == p.Priority && rec.Archived == p.Archived {
				continue
			}
			if len(raw) > lim.MaxFileBytes {
				reportInvalid(label, fmt.Errorf("larger than %s's limit of %d KB", rc.Name, lim.MaxFileBytes>>10))
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
			l.pace(rc.Name, lim.RatePerMinute)
			err := l.callRemote(rc, http.MethodPut, "/api/files/"+url.PathEscape(p.ID), req, &resp)
			reportInvalid(label, err)
			var ae *apiError
			if errors.As(err, &ae) && ae.status == http.StatusTooManyRequests {
				stopped[rc.Name] = true // the next round tries again
			}
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
		l.pace(rc.Name, l.limitsFor(rc).RatePerMinute)
		err := l.callRemote(rc, http.MethodGet, "/api/priorities", nil, &got)
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
func (l *localMode) fetchRemoteTypes(rc remoteConfig) (typesResponse, error) {
	var t typesResponse
	err := l.callRemote(rc, http.MethodGet, "/api/types", nil, &t)
	return t, err
}

// apiError is a remote's answer other than success.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

// callRemote sends one API call. body and out are JSON; either may be nil.
func (l *localMode) callRemote(rc remoteConfig, method, path string, body, out any) error {
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
	resp, err := l.client(rc).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return &apiError{resp.StatusCode, "the remote rejected the API key"}
	case resp.StatusCode/100 == 3:
		return &apiError{resp.StatusCode, fmt.Sprintf("%s: the remote redirects to %s; use that URL", resp.Status, resp.Header.Get("Location"))}
	case resp.StatusCode/100 != 2:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &apiError{resp.StatusCode, fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReply)).Decode(out); err != nil {
		return fmt.Errorf("unexpected reply: %w", err)
	}
	return nil
}
