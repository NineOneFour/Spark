package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCard(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	body := []byte("---\nproject: P\ndescription: A test project\nlast_updated: 2026-01-01T00:00:00Z\npriority: \"3\"\nproject_type: experiment\n---\n# Summary\nHello.\n")
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestStaleFlagIsPerMachine(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	defer func() { now = time.Now }()

	writeCard(t, dir, "fresh__a.md", base.Add(-5*time.Minute))
	writeCard(t, dir, "old__b.md", base.Add(-2*time.Hour))

	projects, err := loadProjects(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range projects {
		got[p.Machine] = p.Stale
	}
	if len(got) != 2 || got["fresh"] || !got["old"] {
		t.Errorf("stale = %v, want only old flagged", got)
	}
}

func TestStaleUsesNewestSnapshotOfMachine(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	defer func() { now = time.Now }()

	writeCard(t, dir, "m__a.md", base.Add(-3*time.Hour))
	writeCard(t, dir, "m__b.md", base.Add(-1*time.Minute))

	projects, _ := loadProjects(dir, nil)
	for _, p := range projects {
		if p.Stale {
			t.Errorf("%s flagged stale though machine synced 1 minute ago", p.ID)
		}
	}
}

func card(project, priority, typ, updated, body string) string {
	return "---\nproject: " + project + "\ndescription: desc\nlast_updated: " + updated + "\npriority: \"" + priority + "\"\nproject_type: " + typ + "\n---\n" + body
}

func writeRaw(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFileInvalid(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]string{
		"nomachine.md":  card("P", "3", "experiment", "2026-01-01T00:00:00Z", ""),
		"m__nofm.md":    "# just a heading\n",
		"m__unterm.md":  "---\nproject: P\n",
		"m__badyaml.md": "---\nproject: [\n---\n",
		"m__nodesc.md":  "---\nproject: P\nlast_updated: 2026-01-01T00:00:00Z\npriority: \"3\"\nproject_type: experiment\n---\n",
		"m__baddate.md": card("P", "3", "experiment", "yesterday", ""),
		"m__badprio.md": card("P", "9", "experiment", "2026-01-01T00:00:00Z", ""),
		"m__badtype.md": card("P", "3", "hobby", "2026-01-01T00:00:00Z", ""),
	}
	for name, content := range tests {
		writeRaw(t, dir, name, content)
		if _, err := parseFile(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestLoadProjectsDropsArchivedAndInvalid(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "m__ok.md", card("Ok", "2", "experiment", "2026-01-01T00:00:00Z", "# Summary\nhi\n"))
	writeRaw(t, dir, "m__old.md", card("Old", "archived", "experiment", "2026-01-01T00:00:00Z", ""))
	writeRaw(t, dir, "m__bad.md", "nope")
	writeRaw(t, dir, ".m__hidden.md", card("H", "1", "experiment", "2026-01-01T00:00:00Z", ""))
	projects, err := loadProjects(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "Ok" {
		t.Errorf("projects = %v", projects)
	}
}

func TestLoadProjectsMergeKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "a__site.md", card("Site old", "3", "experiment", "2026-01-01T00:00:00Z", ""))
	writeRaw(t, dir, "b__Site.md", card("Site new", "3", "experiment", "2026-02-01T00:00:00Z", ""))
	writeRaw(t, dir, "a__other.md", card("Other", "3", "experiment", "2026-01-01T00:00:00Z", ""))
	projects, _ := loadProjects(dir, map[string]bool{"site": true})
	names := map[string]bool{}
	for _, p := range projects {
		names[p.Name] = true
	}
	if len(projects) != 2 || !names["Site new"] || !names["Other"] {
		t.Errorf("names = %v", names)
	}
}

func TestLoadProjectsShowMachineOnCollision(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "a__site.md", card("S", "3", "experiment", "2026-01-01T00:00:00Z", ""))
	writeRaw(t, dir, "b__site.md", card("S", "3", "experiment", "2026-01-01T00:00:00Z", ""))
	projects, _ := loadProjects(dir, nil)
	for _, p := range projects {
		if !p.ShowMachine {
			t.Errorf("%s should show machine", p.ID)
		}
	}
}

func TestParseSections(t *testing.T) {
	body := "# Summary\nHello <script>alert(1)</script>\n# Remaining Work\n- Task one\n\t- why\n- Task two\n# Empty\n"
	sections := parseSections([]byte(body))
	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2 (empty one dropped)", len(sections))
	}
	if strings.Contains(string(sections[0].Body), "<script>") {
		t.Error("raw HTML must be escaped")
	}
	items := sections[1].Items
	if len(items) != 2 || items[0].Description == "" || items[1].Description != "" {
		t.Errorf("items = %+v", items)
	}
}
