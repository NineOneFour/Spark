// Command collector scans source directories for spark.md files and copies
// each one to the Spark data directory as <machine-id>__<folder>.md, either
// over SMB or into a local folder.
//
// It runs once and exits; schedule it with a systemd timer, cron, or similar.
// Files for this machine that were not seen in this run are deleted.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const sparkFile = "spark.md"

// skipDirs are never descended into while scanning.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"build":        true,
	"dist":         true,
}

type config struct {
	MachineID string
	ScanRoots []string

	// SMB target, used when SMBHost is set.
	SMBHost     string
	SMBUser     string
	SMBPassword string
	SMBShare    string

	// Local target, used otherwise.
	TargetDir string
}

func main() {
	defaultConfig := ""
	if dir, err := os.UserConfigDir(); err == nil {
		defaultConfig = filepath.Join(dir, "spark", "collector.env")
	}
	configPath := flag.String("config", defaultConfig, "path to collector env file (optional; environment variables override it)")
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "config" })

	cfg, err := loadConfig(*configPath, explicit)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	projects := scan(cfg.ScanRoots)
	log.Printf("found %d spark.md file(s) under %s", len(projects), strings.Join(cfg.ScanRoots, ", "))

	var dst target
	if cfg.SMBHost != "" {
		dst, err = connectSMB(cfg)
	} else {
		dst, err = openLocal(cfg.TargetDir)
	}
	if err != nil {
		log.Fatalf("target: %v", err)
	}
	defer dst.Close()

	failed := false
	keep := map[string]bool{}
	for _, p := range projects {
		name := cfg.MachineID + "__" + p.folder + ".md"
		keep[strings.ToLower(name)] = true
		if err := copyTo(dst, p.path, name); err != nil {
			log.Printf("copy %s: %v", p.path, err)
			failed = true
			continue
		}
		log.Printf("copied %s -> %s", p.path, name)
	}

	// An empty scan more likely means a wrong or missing root than that every
	// project is gone, so never wipe this machine's cards on an empty scan.
	if len(projects) == 0 {
		log.Printf("no spark.md files found; skipping deletion")
	} else if err := prune(dst, cfg.MachineID, keep); err != nil {
		log.Printf("prune: %v", err)
		failed = true
	}

	if failed {
		os.Exit(1)
	}
}

var configKeys = []string{"MACHINE_ID", "SCAN_ROOT", "TARGET_DIR", "SMB_HOST", "SMB_SHARE", "SMB_USER", "SMB_PASSWORD"}

// loadConfig reads the env file if present, then lets environment variables
// override it. A missing file is only an error when it was passed explicitly.
func loadConfig(path string, explicit bool) (config, error) {
	vals, err := readEnvFile(path)
	if err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return config{}, err
	}
	for _, k := range configKeys {
		if v, ok := os.LookupEnv(k); ok {
			vals[k] = v
		}
	}

	cfg := config{
		MachineID:   vals["MACHINE_ID"],
		SMBHost:     vals["SMB_HOST"],
		SMBUser:     vals["SMB_USER"],
		SMBPassword: vals["SMB_PASSWORD"],
		SMBShare:    vals["SMB_SHARE"],
		TargetDir:   expandHome(vals["TARGET_DIR"]),
	}
	for _, root := range strings.Split(vals["SCAN_ROOT"], ",") {
		if root = strings.TrimSpace(root); root != "" {
			cfg.ScanRoots = append(cfg.ScanRoots, expandHome(root))
		}
	}
	if len(cfg.ScanRoots) == 0 {
		return config{}, errors.New("SCAN_ROOT is required")
	}

	if cfg.MachineID == "" {
		cfg.MachineID = "local"
	}
	if strings.Contains(cfg.MachineID, "__") || strings.ContainsAny(cfg.MachineID, `/\`) {
		return config{}, errors.New("MACHINE_ID must not contain __ or path separators")
	}

	if cfg.SMBHost != "" {
		for k, v := range map[string]string{"SMB_SHARE": cfg.SMBShare, "SMB_USER": cfg.SMBUser, "SMB_PASSWORD": cfg.SMBPassword} {
			if v == "" {
				return config{}, fmt.Errorf("%s is required when SMB_HOST is set", k)
			}
		}
		if _, _, err := net.SplitHostPort(cfg.SMBHost); err != nil {
			cfg.SMBHost = net.JoinHostPort(cfg.SMBHost, "445")
		}
	} else if cfg.TargetDir == "" {
		// Default to a projects folder next to the binary, where the web app
		// looks by default too.
		exe, err := os.Executable()
		if err != nil {
			return config{}, fmt.Errorf("TARGET_DIR is not set and the binary location is unknown: %w", err)
		}
		cfg.TargetDir = filepath.Join(filepath.Dir(exe), "projects")
	}
	return cfg, nil
}

func readEnvFile(path string) (map[string]string, error) {
	vals := map[string]string{}
	if path == "" {
		return vals, fs.ErrNotExist
	}
	f, err := os.Open(path)
	if err != nil {
		return vals, err
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
			return vals, fmt.Errorf("%s: invalid line %q", path, line)
		}
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return vals, sc.Err()
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

type project struct {
	path   string // local path to spark.md
	folder string // name of the folder containing it
}

// scan walks each root and returns one project per folder containing
// spark.md. It does not descend into a folder once it has found spark.md
// there. A root that cannot be read is logged and skipped.
func scan(roots []string) []project {
	var found []project
	for _, root := range roots {
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			log.Printf("scan %s: not a readable directory", root)
			continue
		}
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				log.Printf("scan %s: %v", path, err)
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path != root && (strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()]) {
				return fs.SkipDir
			}
			candidate := filepath.Join(path, sparkFile)
			if fi, err := os.Stat(candidate); err == nil && fi.Mode().IsRegular() {
				found = append(found, project{path: candidate, folder: filepath.Base(path)})
				return fs.SkipDir
			}
			return nil
		})
	}

	// SMB names are case-insensitive, so two folders that differ only by case
	// would overwrite each other. Keep the first by path and warn about the rest.
	sort.Slice(found, func(i, j int) bool { return found[i].path < found[j].path })
	seen := map[string]string{}
	var unique []project
	for _, p := range found {
		key := strings.ToLower(p.folder)
		if first, ok := seen[key]; ok {
			log.Printf("skipping %s: folder name clashes with %s", p.path, first)
			continue
		}
		seen[key] = p.path
		unique = append(unique, p)
	}
	return unique
}

func copyTo(dst target, localPath, name string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	return dst.Put(name, data)
}

// prune deletes this machine's files in the target that were not copied in
// this run, along with any temp files left behind by an interrupted run.
func prune(dst target, machineID string, keep map[string]bool) error {
	names, err := dst.List()
	if err != nil {
		return err
	}
	prefix := strings.ToLower(machineID + "__")
	for _, name := range names {
		lower := strings.ToLower(name)
		stale := strings.HasPrefix(lower, prefix) && strings.HasSuffix(lower, ".md") && !keep[lower]
		leftover := strings.HasPrefix(lower, "."+prefix) && strings.HasSuffix(lower, ".tmp")
		if !stale && !leftover {
			continue
		}
		if err := dst.Remove(name); err != nil {
			log.Printf("delete %s: %v", name, err)
			continue
		}
		log.Printf("deleted %s", name)
	}
	return nil
}
