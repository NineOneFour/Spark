// Command web serves the Spark dashboard: a card per project snapshot, and a
// readable page for each one. It runs in one of two modes. Local reads the
// snapshots the collector copies into SparkRoot/Projects and pushes them to
// remotes; remote receives pushes from many users and shows them together.
// Everything here is shared by both modes; mode-only code is in local*.go and
// remote*.go, behind the mode interface.
package main

import (
	"bufio"
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type config struct {
	Root     string // SparkRoot
	Addr     string
	Mode     string // "local" or "remote"
	Username string
	Password string

	URL            *url.URL       // SPARK_URL, the address people open; nil when unset
	AllowNetwork   bool           // SPARK_ALLOW_NETWORK: a local answers to any host name
	TrustedProxies []netip.Prefix // SPARK_TRUSTED_PROXIES

	PenaltyStart int // SPARK_PENALTY_START: failures that wait 2 s before the waits grow
	LockoutAfter int // the failure that locks (SPARK_LOCKOUT_AFTER); 0 = never
	MinPassword  int // SPARK_MIN_PASSWORD_LENGTH, in characters

	SessionIdle time.Duration // SPARK_SESSION_IDLE: a session unused this long ends

	APIRate          int  // SPARK_API_RATE: valid API calls per account per minute; 0 = no limit
	MaxFileBytes     int  // SPARK_MAX_FILE_KB, in bytes: the largest snapshot read, pushed or accepted
	AllowHTTPRemotes bool // SPARK_ALLOW_HTTP_REMOTES: http:// remotes on any address
}

// Defaults for the settings that can be loosened, so startup can warn when
// one is.
const (
	defaultPenaltyStart = 4
	defaultMinPassword  = 15
	defaultSessionIdle  = 24 * time.Hour
	defaultAPIRate      = 120
	defaultMaxFileKB    = 128
	// maxPasswordBytes is bcrypt's limit; it ignores anything longer.
	maxPasswordBytes = 72
)

type server struct {
	cfg  config
	mode mode
	auth *auth
	tmpl map[string]*template.Template
	csrf string

	settingsMu sync.Mutex // serializes read-modify-write of settings files
	stateMu    sync.Mutex // serializes read-modify-write of state.json
}

// mode is the boundary between the shared core and the mode-only code. The
// core never asks which mode it is in; it asks the mode.
type mode interface {
	// routes registers the mode's own pages and endpoints.
	routes(mux *http.ServeMux)
	// fileKey splits a snapshot's id (its filename without .md) into its
	// owner and its card key (project__type). ok is false for a name the
	// mode doesn't store.
	fileKey(id string) (owner, key string, ok bool)
	// acceptsType reports whether snapshots of this type are shown.
	acceptsType(name string, listed map[string]bool) bool
	// canEdit reports whether the viewer may change a file's priority or
	// archive it. v is nil when login is off.
	canEdit(v *account, p *Project) bool
	// canEditSettings reports whether the viewer may change the shared
	// settings (project types and colors).
	canEditSettings(v *account) bool
	// settingsData adds the mode's own sections to the settings page.
	settingsData(r *http.Request, v *account, data map[string]any)
	// pageData adds what the mode needs on every page, such as header links.
	pageData(data map[string]any)
	// stateChanged is called after a priority or archive change.
	stateChanged()
	// hostAllowed reports whether to answer a request for this host name
	// (lowercase, no port) when SPARK_URL is not set.
	hostAllowed(host string) bool
}

func main() {
	configPath := flag.String("config", "", "path to web env file (optional; environment variables override it)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if args := flag.Args(); len(args) > 0 {
		os.Exit(runCommand(cfg, args))
	}
	cfg.warnLoosened()

	if err := ensureSettings(cfg.Root); err != nil {
		log.Fatalf("settings: %v", err)
	}

	s := &server{cfg: cfg, tmpl: parseTemplates(), csrf: newCSRFToken()}
	if cfg.Username != "" {
		if s.auth, err = newAuth(cfg); err != nil {
			log.Fatalf("login: %v", err)
		}
	} else {
		log.Printf("no SPARK_USERNAME/SPARK_PASSWORD set: login is off, anyone who can reach %s can read it", cfg.Addr)
	}
	if cfg.Mode == "remote" {
		s.mode = newRemoteMode(s)
	} else if s.mode, err = newLocalMode(s); err != nil {
		log.Fatalf("settings: %v", err)
	}

	// Not in Go's built-in list, and the image has no /etc/mime.types.
	mime.AddExtensionType(".woff2", "font/woff2")
	static, _ := fs.Sub(staticFS, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	// Public like /static/, so the login page is styled too.
	mux.HandleFunc("GET /colors.css", s.colors)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.auth.require(s.index))
	mux.HandleFunc("GET /p/{key}", s.auth.require(s.project))
	mux.HandleFunc("POST /p/{key}/state", s.auth.require(s.changeState))
	mux.HandleFunc("GET /settings", s.auth.require(s.settings))
	mux.HandleFunc("POST /settings/types", s.auth.require(s.updateSettings(s.changeProjectTypes)))
	mux.HandleFunc("POST /settings/priorities", s.auth.require(s.updateSettings(s.changePriorityColors)))
	s.mode.routes(mux)

	log.Printf("listening on %s in %s mode, SparkRoot is %s", cfg.Addr, cfg.Mode, cfg.Root)
	// A remote faces the Internet, so a client that opens connections and
	// sends nothing must not hold them forever. Writes get longer than reads
	// because a login can wait for a free password check.
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.edge(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	log.Fatal(srv.ListenAndServe())
}

func parseTemplates() map[string]*template.Template {
	funcs := template.FuncMap{
		"date":         func(t time.Time) string { return t.Format("Jan 2, 2006") },
		"priorityName": func(p string) string { return priorityNames[p] },
	}
	tmpl := map[string]*template.Template{}
	for _, page := range []string{"index", "project", "login", "settings", "account", "invite"} {
		tmpl[page] = template.Must(template.New("").Funcs(funcs).ParseFS(templateFS, "templates/base.html", "templates/"+page+".html"))
	}
	return tmpl
}

// render adds what every page needs (the viewer, for the header) to data.
// It takes the status so headers are set before they are sent.
func (s *server) render(w http.ResponseWriter, r *http.Request, status int, page string, data map[string]any) {
	data["Viewer"] = viewer(r)
	data["CSRF"] = s.csrfToken(viewer(r)) // also used by the log out button in the header
	data["MinPassword"] = s.cfg.MinPassword
	s.mode.pageData(data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl[page].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	projects, err := s.loadProjects()
	if err != nil {
		log.Printf("load projects: %v", err)
		http.Error(w, "Could not read the project directory. Check the server log.", http.StatusInternalServerError)
		return
	}
	s.render(w, r, http.StatusOK, "index", map[string]any{"Title": "Spark", "Cards": buildCards(projects, viewer(r))})
}

// project shows one card's files: one tab per owner, the viewer's own file
// first selected, or the owner named by ?u=.
func (s *server) project(w http.ResponseWriter, r *http.Request) {
	projects, err := s.loadProjects()
	if err != nil {
		log.Printf("load projects: %v", err)
		http.Error(w, "Could not read the project directory. Check the server log.", http.StatusInternalServerError)
		return
	}
	var card *Card
	for _, c := range buildCards(projects, viewer(r)) {
		if c.Key == r.PathValue("key") {
			card = c
		}
	}
	if card == nil {
		http.NotFound(w, r)
		return
	}
	selected := card.Files[0]
	if v := viewer(r); v != nil {
		for _, f := range card.Files {
			if f.Owner == v.Username {
				selected = f
			}
		}
	}
	if u := r.URL.Query().Get("u"); u != "" {
		for _, f := range card.Files {
			if f.Owner == u {
				selected = f
			}
		}
	}
	s.render(w, r, http.StatusOK, "project", map[string]any{
		"Title":      selected.Name,
		"Card":       card,
		"Project":    selected,
		"Tabs":       selected.Owner != "",
		"CanEdit":    s.mode.canEdit(viewer(r), selected),
		"Priorities": []string{"1", "2", "3", "4", "5"},
	})
}

// colors serves the per-deployment type and priority colors as custom
// properties for the card and band classes. Names and colors were checked
// against strict patterns on load, so they are safe to write into CSS.
func (s *server) colors(w http.ResponseWriter, r *http.Request) {
	types, err := loadProjectTypes(s.cfg.Root)
	if err != nil {
		log.Printf("load project types: %v", err)
	}
	priorities, err := loadPriorityColors(s.cfg.Root)
	if err != nil {
		log.Printf("load priority colors: %v", err)
		priorities = defaultPriorityColors
	}

	var b strings.Builder
	for p := 1; p <= 5; p++ {
		fmt.Fprintf(&b, ".p-%d { --pc: %s; }\n", p, priorities[strconv.Itoa(p)])
	}
	for _, t := range types {
		fmt.Fprintf(&b, ".t-%s { --tc: %s; }\n", t.Name, t.Color)
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, b.String())
}

var configKeys = []string{
	"SPARK_ROOT", "SPARK_ADDR", "SPARK_MODE", "SPARK_USERNAME", "SPARK_PASSWORD",
	"SPARK_URL", "SPARK_ALLOW_NETWORK", "SPARK_TRUSTED_PROXIES",
	"SPARK_PENALTY_START", "SPARK_LOCKOUT_AFTER", "SPARK_LOCKOUT", "SPARK_MIN_PASSWORD_LENGTH",
	"SPARK_SESSION_IDLE", "SPARK_API_RATE", "SPARK_MAX_FILE_KB", "SPARK_ALLOW_HTTP_REMOTES",
}

// loadConfig reads the env file if one is given, then lets environment
// variables override it. Every setting is optional in local mode.
func loadConfig(path string) (config, error) {
	vals := map[string]string{}
	if path != "" {
		var err error
		if vals, err = readEnvFile(path); err != nil {
			return config{}, err
		}
	}
	for _, k := range configKeys {
		if v, ok := os.LookupEnv(k); ok {
			vals[k] = v
		}
	}

	cfg := config{
		Root:     vals["SPARK_ROOT"],
		Addr:     vals["SPARK_ADDR"],
		Mode:     vals["SPARK_MODE"],
		Username: vals["SPARK_USERNAME"],
		Password: vals["SPARK_PASSWORD"],
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:9140"
	}
	if cfg.Root == "" {
		// Default to the folder the binary sits in, the same rule the
		// collector uses.
		exe, err := os.Executable()
		if err != nil {
			return config{}, fmt.Errorf("SPARK_ROOT is not set and the binary location is unknown: %w", err)
		}
		cfg.Root = filepath.Dir(exe)
	}
	var err error
	if v := strings.TrimSpace(vals["SPARK_URL"]); v != "" {
		if cfg.URL, err = parseSparkURL(v); err != nil {
			return config{}, err
		}
	}
	if v := strings.TrimSpace(vals["SPARK_ALLOW_NETWORK"]); v != "" {
		if cfg.AllowNetwork, err = strconv.ParseBool(v); err != nil {
			return config{}, fmt.Errorf("SPARK_ALLOW_NETWORK %q: use true or false", v)
		}
	}
	if cfg.TrustedProxies, err = parseTrustedProxies(vals["SPARK_TRUSTED_PROXIES"]); err != nil {
		return config{}, err
	}
	if cfg.PenaltyStart, err = intSetting(vals, "SPARK_PENALTY_START", defaultPenaltyStart, 0, 1000); err != nil {
		return config{}, err
	}
	lockoutDefault := 0
	if cfg.PenaltyStart > 0 {
		lockoutDefault = cfg.PenaltyStart + 7
	}
	if cfg.LockoutAfter, err = intSetting(vals, "SPARK_LOCKOUT_AFTER", lockoutDefault, 1, 1000); err != nil {
		return config{}, err
	}
	switch v := strings.ToLower(strings.TrimSpace(vals["SPARK_LOCKOUT"])); v {
	case "", "on", "true":
	case "off", "false":
		cfg.LockoutAfter = 0
	default:
		return config{}, fmt.Errorf("SPARK_LOCKOUT %q: use on or off", v)
	}
	if cfg.MinPassword, err = intSetting(vals, "SPARK_MIN_PASSWORD_LENGTH", defaultMinPassword, 1, maxPasswordBytes); err != nil {
		return config{}, err
	}
	cfg.SessionIdle = defaultSessionIdle
	if v := strings.TrimSpace(vals["SPARK_SESSION_IDLE"]); v != "" {
		if cfg.SessionIdle, err = time.ParseDuration(v); err != nil || cfg.SessionIdle < time.Minute {
			return config{}, fmt.Errorf("SPARK_SESSION_IDLE %q: use a duration of at least a minute, like 24h or 30m", v)
		}
	}
	if cfg.APIRate, err = intSetting(vals, "SPARK_API_RATE", defaultAPIRate, 0, 100000); err != nil {
		return config{}, err
	}
	kb, err := intSetting(vals, "SPARK_MAX_FILE_KB", defaultMaxFileKB, 1, 16384)
	if err != nil {
		return config{}, err
	}
	cfg.MaxFileBytes = kb << 10
	if v := strings.TrimSpace(vals["SPARK_ALLOW_HTTP_REMOTES"]); v != "" {
		if cfg.AllowHTTPRemotes, err = strconv.ParseBool(v); err != nil {
			return config{}, fmt.Errorf("SPARK_ALLOW_HTTP_REMOTES %q: use true or false", v)
		}
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return config{}, errors.New("set both SPARK_USERNAME and SPARK_PASSWORD, or neither")
	}
	if cfg.Password != "" {
		if msg := cfg.passwordProblem(cfg.Password); msg != "" {
			return config{}, fmt.Errorf("SPARK_PASSWORD: %s (SPARK_MIN_PASSWORD_LENGTH sets the minimum)", msg)
		}
	}
	switch cfg.Mode {
	case "", "local":
		cfg.Mode = "local"
	case "remote":
		// The first admin's username tags their pushes, so it goes into
		// filenames and is held to the same shape as every other username.
		if cfg.Username == "" {
			return config{}, errors.New("remote mode needs SPARK_USERNAME and SPARK_PASSWORD for the first admin")
		}
		if !usernameRe.MatchString(cfg.Username) {
			return config{}, fmt.Errorf("SPARK_USERNAME %q: %s", cfg.Username, usernameRule)
		}
		if strings.HasPrefix(cfg.Username, removedPrefix) {
			return config{}, fmt.Errorf("SPARK_USERNAME %q: names starting with %s are kept for the files of removed people", cfg.Username, removedPrefix)
		}
	default:
		return config{}, fmt.Errorf("SPARK_MODE %q: use local or remote", cfg.Mode)
	}
	return cfg, nil
}

// intSetting reads a whole-number setting, def when unset.
func intSetting(vals map[string]string, key string, def, lo, hi int) (int, error) {
	v := strings.TrimSpace(vals[key])
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s %q: use a whole number from %d to %d", key, v, lo, hi)
	}
	return n, nil
}

// passwordProblem says what is wrong with a new password, or "".
func (c config) passwordProblem(p string) string {
	if utf8.RuneCountInString(p) < c.MinPassword {
		return fmt.Sprintf("use at least %d characters", c.MinPassword)
	}
	if len(p) > maxPasswordBytes {
		return fmt.Sprintf("use at most %d bytes (about %d letters)", maxPasswordBytes, maxPasswordBytes)
	}
	return ""
}

// warnLoosened logs each limit set looser than its default. Spark still
// runs; the operator decides.
func (c config) warnLoosened() {
	if c.MinPassword < defaultMinPassword {
		log.Printf("SPARK_MIN_PASSWORD_LENGTH is %d, below the recommended %d", c.MinPassword, defaultMinPassword)
	}
	if c.PenaltyStart > defaultPenaltyStart {
		log.Printf("SPARK_PENALTY_START is %d: failed logins wait only 2 s for the first %d (recommended %d)", c.PenaltyStart, c.PenaltyStart, defaultPenaltyStart)
	}
	if c.MaxFileBytes > defaultMaxFileKB<<10 {
		log.Printf("SPARK_MAX_FILE_KB is %d: snapshots may be larger than the recommended %d KB", c.MaxFileBytes>>10, defaultMaxFileKB)
	}
	if c.SessionIdle > defaultSessionIdle {
		log.Printf("SPARK_SESSION_IDLE is %v: sessions stay open longer without use than the recommended %v", c.SessionIdle, defaultSessionIdle)
	}
	switch {
	case c.LockoutAfter == 0:
		log.Printf("lockout is off: failed logins only wait, and never lock an account")
	case c.PenaltyStart > 0 && c.LockoutAfter > c.PenaltyStart+7:
		log.Printf("SPARK_LOCKOUT_AFTER is %d: an account locks later than the recommended %d", c.LockoutAfter, c.PenaltyStart+7)
	}
}

// runCommand runs a subcommand instead of the server, such as
// `web unlock sam`, and returns the exit code.
func runCommand(cfg config, args []string) int {
	if args[0] == "unlock" {
		ip := len(args) == 3 && (args[1] == "--ip" || args[1] == "-ip")
		if len(args) == 2 || ip {
			name, what := args[len(args)-1], "account"
			if ip {
				what = "ip"
			}
			ok, err := unlock(cfg.Root, ip, name)
			switch {
			case err != nil:
				fmt.Fprintf(os.Stderr, "unlock: %v\n", err)
				return 1
			case !ok:
				fmt.Printf("%s has no failures to clear\n", name)
			default:
				fmt.Printf("security: unlocked %s=%q by=\"web unlock\"\n", what, name)
			}
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, `usage: web [-config file]                       run the server
       web [-config file] unlock <username>      clear an account's failed logins and lock
       web [-config file] unlock --ip <address>  clear an address's wrong API keys and lock`)
	return 2
}

func readEnvFile(path string) (map[string]string, error) {
	vals := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s: invalid line %q", path, line)
		}
		vals[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	return vals, sc.Err()
}

// unquote removes one matching pair of surrounding quotes, so a password
// that ends in a quote keeps it.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
