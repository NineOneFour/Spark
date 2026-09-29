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

	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v3"
)

var validPriority = map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true, "archived": true}

var validType = map[string]bool{"key-project": true, "side-project": true, "experiment": true, "just-for-fun": true}

// listSections hold "- Title" / "\t- description" items instead of prose.
var listSections = map[string]bool{
	"Key Decisions Outstanding": true,
	"Major Blockers":            true,
	"Remaining Work":            true,
}

type Project struct {
	ID          string // filename without .md, used in the URL
	Machine     string
	Folder      string
	Name        string
	Description string
	Updated     time.Time
	Priority    string
	Type        string
	ShowMachine bool // set when another card has the same folder name
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
	Priority    string `yaml:"priority"`
	ProjectType string `yaml:"project_type"`
}

// loadProjects reads every snapshot in dir, drops invalid and archived ones,
// merges folders on the merge list, and returns the rest sorted by name.
// Invalid files are logged, not shown.
func loadProjects(dir string, merge map[string]bool) ([]*Project, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var all []*Project
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		p, err := parseFile(filepath.Join(dir, name))
		reportInvalid(name, err)
		if err != nil {
			continue
		}
		if p.Priority == "archived" {
			continue
		}
		all = append(all, p)
	}

	// Keep only the newest snapshot of each merged folder.
	newest := map[string]*Project{}
	var projects []*Project
	for _, p := range all {
		key := strings.ToLower(p.Folder)
		if !merge[key] {
			projects = append(projects, p)
			continue
		}
		if cur, ok := newest[key]; !ok || p.Updated.After(cur.Updated) {
			newest[key] = p
		}
	}
	for _, p := range newest {
		projects = append(projects, p)
	}

	count := map[string]int{}
	for _, p := range projects {
		count[strings.ToLower(p.Folder)]++
	}
	for _, p := range projects {
		p.ShowMachine = count[strings.ToLower(p.Folder)] > 1
	}

	sort.Slice(projects, func(i, j int) bool {
		a, b := strings.ToLower(projects[i].Name), strings.ToLower(projects[j].Name)
		if a != b {
			return a < b
		}
		return projects[i].Machine < projects[j].Machine
	})
	return projects, nil
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

func parseFile(path string) (*Project, error) {
	id := strings.TrimSuffix(filepath.Base(path), ".md")
	machine, folder, ok := strings.Cut(id, "__")
	if !ok || machine == "" || folder == "" {
		return nil, errors.New("filename is not <machine-id>__<folder>.md")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
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
	if !validPriority[meta.Priority] {
		return nil, fmt.Errorf("unsupported priority %q", meta.Priority)
	}
	if !validType[meta.ProjectType] {
		return nil, fmt.Errorf("unsupported project_type %q", meta.ProjectType)
	}

	sections := parseSections(body)
	return &Project{
		ID:          id,
		Machine:     machine,
		Folder:      folder,
		Name:        meta.Project,
		Description: meta.Description,
		Updated:     updated,
		Priority:    meta.Priority,
		Type:        meta.ProjectType,
		Sections:    sections,
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

// goldmark escapes raw HTML by default, so snapshot content cannot inject markup.
var md = goldmark.New()

func renderMarkdown(text string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(text), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(text))
	}
	return template.HTML(buf.String())
}

// renderInline renders one line of Markdown without the wrapping <p>.
func renderInline(text string) template.HTML {
	html := strings.TrimSpace(string(renderMarkdown(text)))
	html = strings.TrimPrefix(html, "<p>")
	html = strings.TrimSuffix(html, "</p>")
	return template.HTML(html)
}
