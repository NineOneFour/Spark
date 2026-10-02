//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// demo is a valid spark.md. The comment on the project line must not reach
// the filename: the remote checks a pushed id against its own YAML parse.
const demo = `---
project: "E2E Demo" # a comment the collector must drop
description: A project the end-to-end tests push around.
last_updated: 2026-10-01T09:00:00-04:00
priority: 3
project_type: side-project
---

# Project Description

Exists only in tests.

# Current State

Pushed by the e2e suite.
`

const (
	demoID   = "e2eDemo__sideProject" // what the collector names demo
	demoPage = "/p/" + demoID
	samDemo  = "sam__" + demoID // demo as pushed to a remote by sam
)

var (
	inviteRe = regexp.MustCompile(`/invite/[A-Za-z0-9_-]+`)
	keyRe    = regexp.MustCompile(`spk_[A-Za-z0-9_-]+`)
)

// join makes the account sam on remote by invite, as the admin and then sam
// would, and returns sam's logged-in browser and a new API key.
func join(t *testing.T, remote *spark) (*client, string) {
	t.Helper()
	admin := remote.client()
	admin.login("admin", adminPass)
	r := admin.submit("/settings", "/settings/invites", url.Values{"username": {"sam"}, "action": {"create"}})
	expect(t, r, http.StatusOK, "create invite")
	link := inviteRe.FindString(r.body)
	if link == "" {
		t.Fatal("no invite link on the settings page")
	}

	sam := remote.client()
	expect(t, sam.post(link, url.Values{"password": {samPass}, "confirm": {samPass}}, remote.url), http.StatusSeeOther, "accept invite")
	r = sam.submit("/account", "/account/keys", url.Values{"name": {"laptop"}, "action": {"create"}})
	expect(t, r, http.StatusOK, "create API key")
	key := keyRe.FindString(r.body)
	if key == "" {
		t.Fatal("no API key on the account page")
	}
	return sam, key
}

// collect puts snapshot in a project folder under a new scan root, adds the
// scan root on local's settings page, and runs the collector once, as cron
// would.
func collect(t *testing.T, local *spark, me *client, snapshot string) {
	t.Helper()
	scanRoot := t.TempDir()
	project := filepath.Join(scanRoot, "demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "spark.md"), []byte(snapshot), 0o644); err != nil {
		t.Fatal(err)
	}
	expect(t, me.submit("/settings", "/settings/scan-roots", url.Values{"path": {scanRoot}, "action": {"add"}}), http.StatusSeeOther, "add scan root")
	if out, err := exec.Command("python3", filepath.Join(local.root, "collector.py")).CombinedOutput(); err != nil {
		t.Fatalf("collector: %v\n%s", err, out)
	}
}

// addRemote adds a remote on local's settings page and ticks side-project.
func addRemote(t *testing.T, me *client, name string, remote *spark, key string) {
	t.Helper()
	r := me.submit("/settings", "/settings/remotes", url.Values{"name": {name}, "url": {remote.peer}, "key": {key}, "action": {"add"}})
	expect(t, r, http.StatusSeeOther, "add remote")
	r = me.submit("/settings", "/settings/remotes", url.Values{"name": {name}, "type": {"side-project"}, "action": {"types"}})
	expect(t, r, http.StatusSeeOther, "tick side-project")
}

// team is a remote, a local (login off) pushing demo to it as sam, and both
// people's browsers.
type team struct {
	remote, local *spark
	sam           *client // on the remote
	me            *client // on the local
}

func newTeam(t *testing.T) *team {
	t.Helper()
	remote := startRemote(t, "remote")
	sam, key := join(t, remote)
	local := startSpark(t, "local")
	me := local.client()
	collect(t, local, me, demo)
	addRemote(t, me, "work", remote, key)
	waitFor(t, "push to the remote", func() error { return fileExists(remote.root, "Projects/"+samDemo+".md") })
	return &team{remote, local, sam, me}
}

// push sends demo to remote's sync API under id, as a local would, and
// returns the status. A local whose key was removed shares this address and
// makes it wait briefly, so a 429 is retried after the time it names.
func push(t *testing.T, remote *spark, key, id string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": demo})
	for try := 0; ; try++ {
		req, _ := http.NewRequest(http.MethodPut, remote.url+"/api/files/"+id, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusTooManyRequests || try == 3 {
			return resp.StatusCode
		}
		wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func TestCollectorMakesCard(t *testing.T) {
	local := startSpark(t, "local")
	me := local.client()
	collect(t, local, me, demo)

	if err := fileExists(local.root, "Projects/"+demoID+".md"); err != nil {
		t.Fatal(err)
	}
	r := me.get("/")
	expect(t, r, http.StatusOK, "landing page")
	contains(t, r, "E2E Demo", "landing page")
	r = me.get(demoPage)
	expect(t, r, http.StatusOK, "project page")
	contains(t, r, "Pushed by the e2e suite.", "project page")
}

func TestImageFillsSparkRoot(t *testing.T) {
	if *image == "" {
		t.Skip("only the image has an entrypoint")
	}
	s := startSpark(t, "local")
	for _, f := range []string{"collector.py", "setup.sh", "INSTALL.md", "Skill/SKILL.md", "Skill/format.md", "Skill/template.md", "HandoffSkill/SKILL.md", "HandoffSkill/format.md", "HandoffSkill/template.md", "Config/project_types.json"} {
		if err := fileExists(s.root, f); err != nil {
			t.Error(err)
		}
	}
}

func TestPushAndPull(t *testing.T) {
	tm := newTeam(t)
	admin := tm.remote.client()
	admin.login("admin", adminPass)
	r := admin.get(demoPage)
	expect(t, r, http.StatusOK, "remote project page")
	contains(t, r, "?u=sam", "remote project page tabs")

	// A priority set on the remote comes back on local's next pull; a
	// restart pulls at once.
	r = tm.sam.submit(demoPage, demoPage+"/state", url.Values{"file": {samDemo}, "action": {"priority"}, "priority": {"1"}})
	expect(t, r, http.StatusSeeOther, "set priority on the remote")
	tm.local.restart()
	waitFor(t, "pull the priority", func() error {
		st, err := readState(tm.local.root)
		if err != nil {
			return err
		}
		if p := st[demoID].Priority; p != "1" {
			return fmt.Errorf("local priority is %q", p)
		}
		return nil
	})

	// Archiving locally archives on the remote too.
	r = tm.me.submit(demoPage, demoPage+"/state", url.Values{"file": {demoID}, "action": {"archive"}})
	expect(t, r, http.StatusSeeOther, "archive locally")
	waitFor(t, "push the archive", func() error {
		st, err := readState(tm.remote.root)
		if err != nil {
			return err
		}
		if !st[samDemo].Archived {
			return fmt.Errorf("not archived on the remote")
		}
		return nil
	})
}

// A remote removed and added again under the same name may be a new server,
// so everything is pushed to it again, even files that haven't changed.
func TestReaddedRemoteGetsEverything(t *testing.T) {
	tm := newTeam(t)
	expect(t, tm.me.submit("/settings", "/settings/remotes", url.Values{"name": {"work"}, "action": {"remove"}}), http.StatusSeeOther, "remove remote")

	fresh := startRemote(t, "remote2")
	_, key := join(t, fresh)
	addRemote(t, tm.me, "work", fresh, key)
	waitFor(t, "push to the new remote", func() error { return fileExists(fresh.root, "Projects/"+samDemo+".md") })
}

// Push everything again mirrors local to a remote that lost its files:
// content, priority and archive flag, though nothing changed locally.
func TestPushEverythingAgain(t *testing.T) {
	tm := newTeam(t)
	r := tm.me.submit(demoPage, demoPage+"/state", url.Values{"file": {demoID}, "action": {"priority"}, "priority": {"2"}})
	expect(t, r, http.StatusSeeOther, "set priority locally")
	r = tm.me.submit(demoPage, demoPage+"/state", url.Values{"file": {demoID}, "action": {"archive"}})
	expect(t, r, http.StatusSeeOther, "archive locally")
	waitFor(t, "push the archive", func() error {
		if st, err := readState(tm.remote.root); err != nil || !st[samDemo].Archived {
			return fmt.Errorf("not archived on the remote yet (%v)", err)
		}
		return nil
	})

	tm.remote.stop()
	for _, f := range []string{"Projects/" + samDemo + ".md", "Config/state.json"} {
		if err := os.Remove(filepath.Join(tm.remote.root, f)); err != nil {
			t.Fatal(err)
		}
	}
	tm.remote.start()

	r = tm.me.submit("/settings", "/settings/remotes", url.Values{"name": {"work"}, "action": {"resync"}})
	expect(t, r, http.StatusSeeOther, "push everything again")
	waitFor(t, "mirror to the wiped remote", func() error {
		if err := fileExists(tm.remote.root, "Projects/"+samDemo+".md"); err != nil {
			return err
		}
		st, err := readState(tm.remote.root)
		if err != nil {
			return err
		}
		if e := st[samDemo]; e.Priority != "2" || !e.Archived {
			return fmt.Errorf("remote state is %+v, want priority 2, archived", e)
		}
		return nil
	})
}

func TestUntickWhileRemoteDown(t *testing.T) {
	tm := newTeam(t)
	tm.remote.stop()

	r := tm.me.submit("/settings", "/settings/remotes", url.Values{"name": {"work"}, "action": {"types"}})
	expect(t, r, http.StatusSeeOther, "untick with the remote down")
	rcs, err := readRemotes(tm.local.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rcs) != 1 || len(rcs[0].Types) != 0 {
		t.Fatalf("remotes after unticking: %+v", rcs)
	}

	// Ticking needs the remote to say it accepts the type.
	r = tm.me.submit("/settings", "/settings/remotes", url.Values{"name": {"work"}, "type": {"side-project"}, "action": {"types"}})
	expect(t, r, http.StatusBadRequest, "tick with the remote down")
}

// A pushed id picks the card the file joins, so it must match the snapshot.
func TestPushIDMustMatchSnapshot(t *testing.T) {
	remote := startRemote(t, "remote")
	_, key := join(t, remote)
	if got := push(t, remote, key, "someoneElse__sideProject"); got != http.StatusUnprocessableEntity {
		t.Errorf("push under another project's id: got %d, want 422", got)
	}
	if got := push(t, remote, key, demoID); got != http.StatusOK {
		t.Errorf("push under its own id: got %d, want 200", got)
	}
}

// Removing someone moves their files to deleted-<name>, archived 30 days
// later, so nothing is lost and the username can be invited again cleanly.
func TestRemovedUserFilesRetire(t *testing.T) {
	tm := newTeam(t)
	admin := tm.remote.client()
	admin.login("admin", adminPass)
	remove := func() {
		t.Helper()
		r := admin.submit("/settings", "/settings/accounts", url.Values{"username": {"sam"}, "action": {"remove"}})
		expect(t, r, http.StatusSeeOther, "remove sam")
	}
	remove()

	retired := "deleted-sam__" + demoID
	if err := fileExists(tm.remote.root, "Projects/"+retired+".md"); err != nil {
		t.Fatal(err)
	}
	if fileExists(tm.remote.root, "Projects/"+samDemo+".md") == nil {
		t.Fatal("sam's file is still under sam")
	}
	st, err := readState(tm.remote.root)
	if err != nil {
		t.Fatal(err)
	}
	if at := st[retired].ArchiveAt; at == nil || time.Until(*at) < 29*24*time.Hour {
		t.Fatalf("retired file's archive date is %v, want about 30 days out", at)
	}
	r := admin.get(demoPage)
	expect(t, r, http.StatusOK, "card after removal")
	contains(t, r, "?u=deleted-sam", "card after removal")

	// Thirty days on, the next page load archives it.
	setArchiveAt(t, tm.remote.root, retired, "2000-01-01T00:00:00Z")
	expect(t, admin.get(demoPage), http.StatusNotFound, "card after 30 days")
	if st, _ := readState(tm.remote.root); !st[retired].Archived {
		t.Fatal("retired file was not archived after 30 days")
	}

	// sam can be invited again and starts clean; removing sam again doesn't
	// collide with the first removal.
	_, key := join(t, tm.remote)
	expect(t, admin.get(demoPage), http.StatusNotFound, "card for the new sam")
	if got := push(t, tm.remote, key, demoID); got != http.StatusOK {
		t.Fatalf("new sam's push: got %d", got)
	}
	remove()
	if err := fileExists(tm.remote.root, "Projects/deleted-sam-2__"+demoID+".md"); err != nil {
		t.Fatal(err)
	}

	r = admin.submit("/settings", "/settings/invites", url.Values{"username": {"deleted-x"}, "action": {"create"}})
	expect(t, r, http.StatusBadRequest, "invite a reserved name")
}

// Every person on a remote sees their own form token, so one can't forge
// another's posts, and cross-site posts fail even with the right token.
func TestFormTokenIsPerUser(t *testing.T) {
	remote := startRemote(t, "remote")
	sam, _ := join(t, remote)
	admin := remote.client()
	admin.login("admin", adminPass)

	samToken, adminToken := sam.token("/account"), admin.token("/settings")
	if samToken == adminToken {
		t.Fatal("sam and admin have the same form token")
	}
	invite := func(token, origin string) reply {
		return admin.post("/settings/invites", url.Values{"username": {"lee"}, "action": {"create"}, "csrf": {token}}, origin)
	}
	expect(t, invite(samToken, remote.url), http.StatusForbidden, "admin posting sam's token")
	expect(t, invite(adminToken, "http://evil.example"), http.StatusForbidden, "cross-site post")
	expect(t, invite(adminToken, remote.url), http.StatusOK, "admin's own token")
}
