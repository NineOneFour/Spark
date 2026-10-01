// Command web serves the Spark dashboard: a card per project snapshot, and a
// readable page for each one. It reads snapshots from SparkRoot/Projects and
// writes only the settings files in SparkRoot/Config.
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
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type config struct {
	Root     string // SparkRoot
	Addr     string
	Username string
	Password string
}

type server struct {
	cfg  config
	auth *auth
	tmpl map[string]*template.Template
	csrf string

	settingsMu sync.Mutex // serializes read-modify-write of settings files
}

func main() {
	configPath := flag.String("config", "", "path to web env file (optional; environment variables override it)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	funcs := template.FuncMap{
		"date": func(t time.Time) string { return t.Format("Jan 2, 2006") },
	}
	tmpl := map[string]*template.Template{}
	for _, page := range []string{"index", "project", "login", "settings"} {
		tmpl[page] = template.Must(template.New("").Funcs(funcs).ParseFS(templateFS, "templates/base.html", "templates/"+page+".html"))
	}

	if err := ensureSettings(cfg.Root); err != nil {
		log.Fatalf("settings: %v", err)
	}

	s := &server{cfg: cfg, tmpl: tmpl, csrf: newCSRFToken()}
	if cfg.Username != "" {
		s.auth = newAuth(cfg.Username, cfg.Password)
	} else {
		log.Printf("no SPARK_USERNAME/SPARK_PASSWORD set: login is off, anyone who can reach %s can read it", cfg.Addr)
	}

	static, _ := fs.Sub(staticFS, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	// Public like /static/, so the login page is styled too.
	mux.HandleFunc("GET /colors.css", s.colors)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("GET /{$}", s.auth.require(s.index))
	mux.HandleFunc("GET /p/{id}", s.auth.require(s.project))
	mux.HandleFunc("GET /settings", s.auth.require(s.settings))
	mux.HandleFunc("POST /settings/scan-roots", s.auth.require(s.updateSettings(s.changeScanRoots)))
	mux.HandleFunc("POST /settings/types", s.auth.require(s.updateSettings(s.changeProjectTypes)))
	mux.HandleFunc("POST /settings/priorities", s.auth.require(s.updateSettings(s.changePriorityColors)))

	log.Printf("listening on %s, SparkRoot is %s", cfg.Addr, cfg.Root)
	log.Fatal(http.ListenAndServe(cfg.Addr, securityHeaders(mux)))
}

func (s *server) render(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl[page].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	projects, err := loadProjects(s.cfg.Root)
	if err != nil {
		log.Printf("load projects: %v", err)
		http.Error(w, "Could not read the project directory. Check the server log.", http.StatusInternalServerError)
		return
	}
	s.render(w, "index", map[string]any{"Title": "Spark", "Projects": projects})
}

func (s *server) project(w http.ResponseWriter, r *http.Request) {
	projects, err := loadProjects(s.cfg.Root)
	if err != nil {
		log.Printf("load projects: %v", err)
		http.Error(w, "Could not read the project directory. Check the server log.", http.StatusInternalServerError)
		return
	}
	id := r.PathValue("id")
	for _, p := range projects {
		if p.ID == id {
			s.render(w, "project", map[string]any{"Title": p.Name, "Project": p})
			return
		}
	}
	http.NotFound(w, r)
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

func (s *server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil || s.auth.valid(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, "login", map[string]any{"Title": "Log in to Spark"})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	username, password := r.PostFormValue("username"), r.PostFormValue("password")
	if !s.auth.checkCredentials(username, password) {
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, "login", map[string]any{
			"Title":    "Log in to Spark",
			"Error":    "Wrong username or password.",
			"Username": username,
		})
		return
	}
	s.auth.setSession(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

var configKeys = []string{"SPARK_ROOT", "SPARK_ADDR", "SPARK_USERNAME", "SPARK_PASSWORD"}

// loadConfig reads the env file if one is given, then lets environment
// variables override it. Every setting is optional.
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
		Username: vals["SPARK_USERNAME"],
		Password: vals["SPARK_PASSWORD"],
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
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
	if (cfg.Username == "") != (cfg.Password == "") {
		return config{}, errors.New("set both SPARK_USERNAME and SPARK_PASSWORD, or neither")
	}
	return cfg, nil
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
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return vals, sc.Err()
}
