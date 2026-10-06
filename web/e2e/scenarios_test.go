//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	for try := 0; ; try++ {
		r := send(t, remote, key, id, demo)
		if r.status != http.StatusTooManyRequests || try == 3 {
			return r.status
		}
		wait, _ := strconv.Atoi(r.retryAfter)
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

type apiReply struct {
	status     int
	body       string
	retryAfter string
}

// send makes one push of content under id, with no retry.
func send(t *testing.T, remote *spark, key, id, content string) apiReply {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content})
	req, _ := http.NewRequest(http.MethodPut, remote.url+"/api/files/"+id, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return apiReply{resp.StatusCode, string(data), resp.Header.Get("Retry-After")}
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

// Failed logins lock an account; while locked even the right password is
// refused, until the operator runs web unlock.
func TestLoginLockoutAndUnlock(t *testing.T) {
	remote := startSpark(t, "remote", "SPARK_MODE=remote", "SPARK_USERNAME=admin", "SPARK_PASSWORD="+adminPass,
		"SPARK_PENALTY_START=0", "SPARK_LOCKOUT_AFTER=2")
	c := remote.client()
	try := func(pass string) reply {
		return c.submit("/login", "/login", url.Values{"username": {"admin"}, "password": {pass}})
	}
	expect(t, try("wrong-password-one"), http.StatusUnauthorized, "first wrong password")
	expect(t, try(adminPass), http.StatusTooManyRequests, "right password during the 2 s wait")
	time.Sleep(2100 * time.Millisecond)
	r := try("wrong-password-two")
	expect(t, r, http.StatusUnauthorized, "second wrong password")
	contains(t, r, "locked", "second wrong password")
	time.Sleep(2100 * time.Millisecond)
	expect(t, try(adminPass), http.StatusTooManyRequests, "right password while locked")
	if !strings.Contains(remote.logs(), `security: account locked account="admin"`) {
		t.Error("no security line for the lockout")
	}

	if out := remote.command("unlock", "admin"); !strings.Contains(out, "unlocked") {
		t.Fatalf("web unlock: %s", out)
	}
	expect(t, try(adminPass), http.StatusSeeOther, "right password after unlock")
}

// Wrong API keys block the address until web unlock --ip; then valid calls
// are rate limited per account, large files refused, and new projects over
// the cap refused while updates still work.
func TestAPILimits(t *testing.T) {
	remote := startSpark(t, "remote", "SPARK_MODE=remote", "SPARK_USERNAME=admin", "SPARK_PASSWORD="+adminPass,
		"SPARK_PENALTY_START=0", "SPARK_LOCKOUT_AFTER=1", "SPARK_API_RATE=4", "SPARK_MAX_FILE_KB=1")
	_, key := join(t, remote)
	admin := remote.client()
	admin.login("admin", adminPass)
	expect(t, admin.submit("/settings", "/settings/max-projects", url.Values{"max": {"1"}}), http.StatusSeeOther, "set the project cap")

	// One wrong key blocks the address, even for the right key.
	if r := send(t, remote, "spk_wrong", demoID, demo); r.status != http.StatusUnauthorized {
		t.Fatalf("wrong key: got %d, want 401", r.status)
	}
	r := send(t, remote, key, demoID, demo)
	m := regexp.MustCompile(`web unlock --ip (\S+)`).FindStringSubmatch(r.body)
	if r.status != http.StatusForbidden || m == nil {
		t.Fatalf("right key from a blocked address: %d %s", r.status, r.body)
	}
	if !strings.Contains(remote.logs(), "security: api ip blocked") {
		t.Error("no security line for the blocked address")
	}
	remote.command("unlock", "--ip", m[1])

	// Four calls a minute: three pushes and an update, then the fifth is refused.
	other := strings.Replace(demo, "E2E Demo", "E2E Other", 1)
	for _, step := range []struct {
		id, content string
		want        int
		what        string
	}{
		{demoID, demo, http.StatusOK, "push after unlock"},
		{demoID, demo + strings.Repeat("x", 2048), http.StatusRequestEntityTooLarge, "file over 1 KB"},
		{"e2eOther__sideProject", other, http.StatusConflict, "second project over a cap of 1"},
		{demoID, demo, http.StatusOK, "update over the cap"},
		{demoID, demo, http.StatusTooManyRequests, "fifth call in a minute"},
	} {
		if r := send(t, remote, key, step.id, step.content); r.status != step.want {
			t.Fatalf("%s: got %d, want %d: %s", step.what, r.status, step.want, r.body)
		}
	}
}
