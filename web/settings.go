package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Settings live as JSON files in SparkRoot/Config, one file per concern.
// The web app writes only Config, plus pushed snapshots on remote.
const (
	scanRootsFile      = "scan_roots.json"
	projectTypesFile   = "project_types.json"
	priorityColorsFile = "priority_colors.json"
)

type projectType struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

var defaultProjectTypes = []projectType{
	{"key-project", "#7c3aed"},  // violet
	{"side-project", "#64748b"}, // slate
	{"experiment", "#db2777"},   // magenta
	{"just-for-fun", "#b8a67e"}, // tan
}

var defaultPriorityColors = map[string]string{
	"1": "#dc2626", // red
	"2": "#f59e0b", // amber
	"3": "#16a34a", // green
	"4": "#0d9488", // teal
	"5": "#2563eb", // blue
}

// Names and colors are written into generated CSS, so both are held to a
// strict shape rather than escaped.
var (
	typeNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	colorRe    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

func configDir(root string) string { return filepath.Join(root, "Config") }

// ensureSettings creates SparkRoot's folders and any missing settings file
// with its defaults. Existing files are never touched.
func ensureSettings(root string) error {
	for _, dir := range []string{filepath.Join(root, "Projects"), configDir(root)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return ensureFiles(root, map[string]any{
		projectTypesFile:   defaultProjectTypes,
		priorityColorsFile: defaultPriorityColors,
	})
}

// ensureFiles writes each missing Config file with its default value.
func ensureFiles(root string, defaults map[string]any) error {
	for name, v := range defaults {
		path := filepath.Join(configDir(root), name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := writeJSON(path, v); err != nil {
			return err
		}
		log.Printf("created %s with defaults", path)
	}
	return nil
}

// loadProjectTypes returns the valid entries of project_types.json. Bad
// entries are logged once and dropped, so one typo doesn't hide every card.
func loadProjectTypes(root string) ([]projectType, error) {
	var raw []projectType
	if err := readJSON(filepath.Join(configDir(root), projectTypesFile), &raw); err != nil {
		return nil, err
	}
	var types []projectType
	var bad []string
	for _, t := range raw {
		if !typeNameRe.MatchString(t.Name) || !colorRe.MatchString(t.Color) {
			bad = append(bad, fmt.Sprintf("%q %q", t.Name, t.Color))
			continue
		}
		types = append(types, t)
	}
	reportInvalid(projectTypesFile+" entries", invalidEntries(bad))
	return types, nil
}

// loadPriorityColors returns a color for each priority 1-5, falling back to
// the default for any that are missing or invalid.
func loadPriorityColors(root string) (map[string]string, error) {
	var raw map[string]string
	if err := readJSON(filepath.Join(configDir(root), priorityColorsFile), &raw); err != nil {
		return nil, err
	}
	colors := map[string]string{}
	var bad []string
	for p, def := range defaultPriorityColors {
		c, ok := raw[p]
		if ok && colorRe.MatchString(c) {
			colors[p] = c
			continue
		}
		if ok {
			bad = append(bad, fmt.Sprintf("%s: %q", p, c))
		}
		colors[p] = def
	}
	sort.Strings(bad)
	reportInvalid(priorityColorsFile+" entries", invalidEntries(bad))
	return colors, nil
}

func invalidEntries(bad []string) error {
	if len(bad) == 0 {
		return nil
	}
	return errors.New(strings.Join(bad, ", "))
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeJSON writes through a temp file and a rename, because the collector
// and other outside sources can read Config at any moment.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// writeFileAtomic writes data to path through a temp file and a rename, so a
// reader never sees half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
