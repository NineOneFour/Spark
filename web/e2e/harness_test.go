//go:build e2e

// Package e2e runs whole Spark deployments, a remote and the locals that push
// to it, and drives them the way people and cron do: HTTP forms, the sync API,
// and collector.py on real folders. Each Spark is either a web process or a
// container from the image. It runs only on demand, through run.sh.
package e2e

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	webBin = flag.String("web", "", "built web binary: run each Spark as a process")
	image  = flag.String("image", "", "Docker image: run each Spark as a container instead")
)

const (
	collectorSrc = "../../collector/collector.py" // tests run from web/e2e
	adminPass    = "admin-password-long"
	samPass      = "sam-password-long"
	waitLimit    = 20 * time.Second
)

// network is the Docker network the containers share, so a local reaches a
// remote by container name.
var network string

func TestMain(m *testing.M) {
	flag.Parse()
	if (*webBin == "") == (*image == "") {
		fmt.Fprintln(os.Stderr, "e2e: pass exactly one of -web or -image (run.sh does this)")
		os.Exit(2)
	}
	if *image != "" {
		network = fmt.Sprintf("spark-e2e-%d", os.Getpid())
		if out, err := exec.Command("docker", "network", "create", network).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "e2e: docker network create: %v\n%s", err, out)
			os.Exit(1)
		}
	}
	code := m.Run()
	if network != "" {
		exec.Command("docker", "network", "rm", network).Run()
	}
	os.Exit(code)
}

// spark is one running deployment with its own SparkRoot.
type spark struct {
	t    *testing.T
	name string
	root string // SparkRoot on the host
	url  string // as the test reaches it
	peer string // as other Sparks reach it
	port int
	env  []string

	cmd     *exec.Cmd     // process mode
	out     *bytes.Buffer // process mode
	created bool          // docker mode
}

var sparkCount atomic.Int32

// startSpark starts a Spark and stops it when the test ends. env is extra
// SPARK_* settings, such as the mode and login.
func startSpark(t *testing.T, name string, env ...string) *spark {
	t.Helper()
	s := &spark{t: t, name: name, root: t.TempDir(), port: freePort(t), env: env}
	s.url = fmt.Sprintf("http://127.0.0.1:%d", s.port)
	if *image != "" {
		s.name = fmt.Sprintf("%s-%s-%d", network, name, sparkCount.Add(1))
		s.peer = "http://" + s.name + ":8080"
	} else {
		s.peer = s.url
		s.env = append(s.env, "SPARK_ROOT="+s.root, fmt.Sprintf("SPARK_ADDR=127.0.0.1:%d", s.port))
		// The image's entrypoint puts collector.py in SparkRoot; do it here.
		data, err := os.ReadFile(collectorSrc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(s.root, "collector.py"), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(s.remove) // runs before t.TempDir removes the root
	s.start()
	return s
}

func startRemote(t *testing.T, name string) *spark {
	return startSpark(t, name, "SPARK_MODE=remote", "SPARK_USERNAME=admin", "SPARK_PASSWORD="+adminPass)
}

func (s *spark) start() {
	s.t.Helper()
	if *image == "" {
		s.out = &bytes.Buffer{}
		s.cmd = exec.Command(*webBin)
		s.cmd.Env = append(hostEnv(), s.env...)
		s.cmd.Stdout, s.cmd.Stderr = s.out, s.out
		if err := s.cmd.Start(); err != nil {
			s.t.Fatal(err)
		}
	} else if !s.created {
		args := []string{"run", "-d", "--name", s.name, "--network", network,
			"-p", fmt.Sprintf("127.0.0.1:%d:8080", s.port),
			"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
			"-v", s.root + ":/spark"}
		for _, e := range s.env {
			args = append(args, "-e", e)
		}
		docker(s.t, append(args, *image)...)
		s.created = true
	} else {
		docker(s.t, "start", s.name)
	}
	s.waitReady()
}

func (s *spark) stop() {
	s.t.Helper()
	if *image == "" {
		if s.cmd != nil {
			s.cmd.Process.Kill()
			s.cmd.Wait()
			s.cmd = nil
		}
		return
	}
	docker(s.t, "stop", "-t", "1", s.name)
}

// restart also makes a local pull from its remotes at once.
func (s *spark) restart() {
	s.t.Helper()
	s.stop()
	s.start()
}

func (s *spark) remove() {
	if s.t.Failed() {
		s.t.Logf("--- log of %s ---\n%s", s.name, s.logs())
	}
	if *image == "" {
		s.stop()
	} else if s.created {
		exec.Command("docker", "rm", "-f", s.name).Run()
	}
}

// command runs a web subcommand against this Spark's SparkRoot, the way an
// operator would (`docker exec <name> web ...` with the image), and returns
// its output.
func (s *spark) command(args ...string) string {
	s.t.Helper()
	var cmd *exec.Cmd
	if *image == "" {
		cmd = exec.Command(*webBin, args...)
		cmd.Env = append(hostEnv(), s.env...)
	} else {
		cmd = exec.Command("docker", append([]string{"exec", s.name, "web"}, args...)...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("web %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (s *spark) logs() string {
	if *image == "" {
		if s.out == nil {
			return ""
		}
		return s.out.String()
	}
	out, _ := exec.Command("docker", "logs", s.name).CombinedOutput()
	return string(out)
}

func (s *spark) waitReady() {
	s.t.Helper()
	deadline := time.Now().Add(waitLimit)
	for time.Now().Before(deadline) {
		resp, err := http.Get(s.url + "/colors.css")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Fatalf("%s did not come up at %s\n%s", s.name, s.url, s.logs())
}

// client is one browser: its own cookies, and redirects left unfollowed so
// a test can check them.
type client struct {
	t    *testing.T
	base string
	hc   *http.Client
}

type reply struct {
	status   int
	body     string
	location string
}

func (s *spark) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: s.t, base: s.url, hc: &http.Client{
		Jar:           jar,
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *client) do(req *http.Request) reply {
	c.t.Helper()
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, string(body), resp.Header.Get("Location")}
}

func (c *client) get(path string) reply {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	return c.do(req)
}

// post sends a form as-is, with Origin set as a browser would (empty for none).
func (c *client) post(path string, form url.Values, origin string) reply {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.base+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return c.do(req)
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// token loads page and returns its form token.
func (c *client) token(page string) string {
	c.t.Helper()
	r := c.get(page)
	m := csrfRe.FindStringSubmatch(r.body)
	if m == nil {
		c.t.Fatalf("no form token on %s (status %d)", page, r.status)
	}
	return m[1]
}

// submit posts a form the way a browser does from page: with page's token,
// from the same origin.
func (c *client) submit(page, action string, form url.Values) reply {
	c.t.Helper()
	form.Set("csrf", c.token(page))
	return c.post(action, form, c.base)
}

func (c *client) login(user, pass string) {
	c.t.Helper()
	expect(c.t, c.submit("/login", "/login", url.Values{"username": {user}, "password": {pass}}), http.StatusSeeOther, "log in as "+user)
}

func expect(t *testing.T, r reply, status int, what string) {
	t.Helper()
	if r.status != status {
		body := r.body
		if len(body) > 600 {
			body = body[:600] + "…"
		}
		t.Fatalf("%s: got %d, want %d (location %q)\n%s", what, r.status, status, r.location, body)
	}
}

func contains(t *testing.T, r reply, want, what string) {
	t.Helper()
	if !strings.Contains(r.body, want) {
		t.Fatalf("%s: page does not contain %q", what, want)
	}
}

// waitFor retries check until it passes, for things that happen in the
// background, such as a push.
func waitFor(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: still not done after %v: %v", what, waitLimit, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type fileState struct {
	Priority  string     `json:"priority"`
	Archived  bool       `json:"archived"`
	ArchiveAt *time.Time `json:"archive_at"`
}

// setArchiveAt edits one entry of state.json by hand, standing in for the
// passage of time.
func setArchiveAt(t *testing.T, root, id, at string) {
	t.Helper()
	path := filepath.Join(root, "Config", "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]map[string]any{}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st[id] == nil {
		t.Fatalf("no state for %s", id)
	}
	st[id]["archive_at"] = at
	data, _ = json.Marshal(st)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readState(root string) (map[string]fileState, error) {
	st := map[string]fileState{}
	data, err := os.ReadFile(filepath.Join(root, "Config", "state.json"))
	if err != nil {
		return nil, err
	}
	return st, json.Unmarshal(data, &st)
}

type remoteConfig struct {
	Name  string   `json:"name"`
	Types []string `json:"types"`
}

func readRemotes(root string) ([]remoteConfig, error) {
	var rcs []remoteConfig
	data, err := os.ReadFile(filepath.Join(root, "Config", "remotes.json"))
	if err != nil {
		return nil, err
	}
	return rcs, json.Unmarshal(data, &rcs)
}

func fileExists(root, rel string) error {
	_, err := os.Stat(filepath.Join(root, rel))
	return err
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// hostEnv is this process's environment without SPARK_* settings, so the
// host's own Spark config can't leak into a test.
func hostEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "SPARK_") {
			env = append(env, e)
		}
	}
	return env
}

func docker(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
