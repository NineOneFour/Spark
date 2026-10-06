package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v3"
)

// priorityNames are the names format.md gives each priority.
var priorityNames = map[string]string{
	"1": "Right now",
	"2": "Up next",
	"3": "When I can",
	"4": "Eventually",
	"5": "Someday maybe",
}

// listSections hold "- Title" / "\t- description" items instead of prose.
var listSections = map[string]bool{
	"Key Decisions Outstanding": true,
	"Major Blockers":            true,
	"Remaining Work":            true,
}

type Project struct {
	ID          string // filename without .md: the state.json key
	Key         string // project__type: the card this file belongs to
	Owner       string // username on remote; empty on local
	Name        string
	Description string
	Updated     time.Time
	Priority    string // 1-5, from state.json
	Archived    bool   // from state.json
	Type        string
	Sections    []Section
}

type Section struct {
	Title string
	Body  template.HTML // prose sections
	Items []Item        // list sections
}

type Item struct {
	Title       template.HTML
	Description template.HTML
}

type frontMatter struct {
	Project     string `yaml:"project"`
	Description string `yaml:"description"`
	LastUpdated string `yaml:"last_updated"`
	ProjectType string `yaml:"project_type"`
}

// loadProjects reads every snapshot in SparkRoot/Projects, drops invalid
// ones, applies state.json, and returns the rest (archived included) sorted
// by name. The project_type must be one the mode accepts. Invalid files are
// logged, not shown.
func (s *server) loadProjects() ([]*Project, error) {
	types, err := loadProjectTypes(s.cfg.Root)
	if err != nil {
		return nil, err
	}
	listed := map[string]bool{}
	for _, t := range types {
		listed[t.Name] = true
	}
	validType := func(name string) bool { return s.mode.acceptsType(name, listed) }

	dir := filepath.Join(s.cfg.Root, "Projects")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var projects []*Project
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		owner, key, ok := s.mode.fileKey(id)
		if !ok {
			reportInvalid(name, errors.New("filename does not match this mode's naming scheme"))
			continue
		}
		// Every page reparses every file, so a huge one would slow them all.
		if info, err := e.Info(); err == nil && info.Size() > int64(s.cfg.MaxFileBytes) {
			reportInvalid(name, fmt.Errorf("larger than %d KB (SPARK_MAX_FILE_KB)", s.cfg.MaxFileBytes>>10))
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			reportInvalid(name, err)
			continue
		}
		p, err := parseSnapshot(raw, validType)
		reportInvalid(name, err)
		if err != nil {
			continue
		}
		p.ID, p.Owner, p.Key = id, owner, key
		projects = append(projects, p)
	}
	if err := s.applyState(projects); err != nil {
		return nil, err
	}

	sort.Slice(projects, func(i, j int) bool {
		a, b := strings.ToLower(projects[i].Name), strings.ToLower(projects[j].Name)
		if a != b {
			return a < b
		}
		return projects[i].ID < projects[j].ID
	})
	return projects, nil
}

// Card is one project__type on the landing page. On remote it can hold one
// file per user; on local it always holds exactly one.
type Card struct {
	Key         string
	Name        string
	Description string
	Updated     time.Time
	Priority    string
	Type        string
	Files       []*Project // not archived, sorted by owner
}

// buildCards groups the files that aren't archived into cards. A viewer with
// a file on the card sees that file's priority and text; everyone else sees
// the most urgent priority and the newest file's text.
func buildCards(projects []*Project, v *account) []*Card {
	byKey := map[string]*Card{}
	var cards []*Card
	for _, p := range projects {
		if p.Archived {
			continue
		}
		c := byKey[p.Key]
		if c == nil {
			c = &Card{Key: p.Key, Type: p.Type}
			byKey[p.Key] = c
			cards = append(cards, c)
		}
		c.Files = append(c.Files, p)
	}
	for _, c := range cards {
		sort.Slice(c.Files, func(i, j int) bool { return c.Files[i].Owner < c.Files[j].Owner })
		shown := c.Files[0]
		c.Priority = shown.Priority
		for _, f := range c.Files {
			if f.Updated.After(shown.Updated) {
				shown = f
			}
			if f.Priority < c.Priority { // "1" is the most urgent
				c.Priority = f.Priority
			}
		}
		for _, f := range c.Files {
			if v != nil && f.Owner != "" && f.Owner == v.Username {
				shown = f
				c.Priority = f.Priority
			}
		}
		c.Name, c.Description, c.Updated = shown.Name, shown.Description, shown.Updated
	}
	sort.SliceStable(cards, func(i, j int) bool {
		return strings.ToLower(cards[i].Name) < strings.ToLower(cards[j].Name)
	})
	return cards
}

var (
	invalidMu sync.Mutex
	invalid   = map[string]string{} // filename -> last logged error
)

// reportInvalid logs a bad file once, rather than on every page view, and
// logs again only if the problem changes.
func reportInvalid(name string, err error) {
	invalidMu.Lock()
	defer invalidMu.Unlock()
	if err == nil {
		delete(invalid, name)
		return
	}
	if invalid[name] != err.Error() {
		invalid[name] = err.Error()
		log.Printf("skipping %s: %v", name, err)
	}
}

// parseSnapshot checks a snapshot against format.md. The caller sets ID,
// Owner and Key from the filename.
func parseSnapshot(raw []byte, validType func(string) bool) (*Project, error) {
	fm, body, err := splitFrontMatter(raw)
	if err != nil {
		return nil, err
	}

	var meta frontMatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return nil, fmt.Errorf("front matter: %w", err)
	}
	if meta.Project == "" || meta.Description == "" {
		return nil, errors.New("project and description are required")
	}
	updated, err := time.Parse(time.RFC3339, meta.LastUpdated)
	if err != nil {
		return nil, fmt.Errorf("last_updated %q is not an ISO 8601 timestamp with offset", meta.LastUpdated)
	}
	if !validType(meta.ProjectType) {
		return nil, fmt.Errorf("unsupported project_type %q", meta.ProjectType)
	}

	return &Project{
		Name:        meta.Project,
		Description: meta.Description,
		Updated:     updated,
		Type:        meta.ProjectType,
		Sections:    parseSections(body),
	}, nil
}

func splitFrontMatter(raw []byte) (fm, body []byte, err error) {
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return nil, nil, errors.New("missing front matter")
	}
	rest := raw[len("---\n"):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return nil, nil, errors.New("unterminated front matter")
	}
	return rest[:end], rest[end+len("\n---\n"):], nil
}

// parseSections splits the body on top-level "# " headings. List sections
// become items; everything else is rendered as Markdown prose.
func parseSections(body []byte) []Section {
	var sections []Section
	var title string
	var lines []string

	flush := func() {
		text := strings.TrimSpace(strings.Join(lines, "\n"))
		if title == "" || text == "" {
			return
		}
		s := Section{Title: title}
		if listSections[title] {
			s.Items = parseItems(lines)
		} else {
			s.Body = renderMarkdown(text)
		}
		sections = append(sections, s)
	}

	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "# ") {
			flush()
			title = strings.TrimSpace(line[2:])
			lines = nil
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return sections
}

// parseItems reads "- Title" lines, each followed by an indented
// "- description" line.
func parseItems(lines []string) []Item {
	var items []Item
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		text := strings.TrimSpace(trimmed[2:])
		indented := line != strings.TrimLeft(line, " \t")
		if indented && len(items) > 0 {
			last := &items[len(items)-1]
			if last.Description == "" {
				last.Description = renderInline(text)
			} else {
				last.Description += " " + renderInline(text)
			}
			continue
		}
		items = append(items, Item{Title: renderInline(text)})
	}
	return items
}

// goldmark escapes raw HTML by default, so snapshot content cannot inject
// markup. Snapshots pushed to a remote come from other people, so its output
// also goes through bluemonday on every render, in case a goldmark bug or a
// future extension lets something through.
var (
	md        = goldmark.New()
	sanitizer = newSanitizer()
)

// newSanitizer starts from bluemonday's policy for user content and narrows
// it: links only to web and mail addresses, and never passing the referrer
// on. Remote images are already blocked by the CSP (img-src falls back to
// 'self').
func newSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoReferrerOnLinks(true)
	p.RequireNoFollowOnLinks(true)
	return p
}

func renderMarkdown(text string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(text), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(text))
	}
	return template.HTML(sanitizer.SanitizeBytes(buf.Bytes()))
}

// renderInline renders one line of Markdown without the wrapping <p>.
func renderInline(text string) template.HTML {
	html := strings.TrimSpace(string(renderMarkdown(text)))
	html = strings.TrimPrefix(html, "<p>")
	html = strings.TrimSuffix(html, "</p>")
	return template.HTML(html)
}
