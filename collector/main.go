// Command collector scans a root directory for spark.md files and uploads
// each one to the Spark Samba share as <machine-id>__<folder>.md.
//
// It runs once and exits; schedule it with a systemd timer, cron, or similar.
// Server files for this machine that were not seen in this run are deleted.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hirochachacha/go-smb2"
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
	MachineID   string
	ScanRoot    string
	SMBHost     string
	SMBUser     string
	SMBPassword string
	SMBShare    string
}

func main() {
	defaultConfig := ""
	if dir, err := os.UserConfigDir(); err == nil {
		defaultConfig = filepath.Join(dir, "spark", "collector.env")
	}
	configPath := flag.String("config", defaultConfig, "path to collector env file")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	projects, err := scan(cfg.ScanRoot)
	if err != nil {
		log.Fatalf("scan: %v", err)
	}
	log.Printf("found %d spark.md file(s) under %s", len(projects), cfg.ScanRoot)

	share, closeShare, err := connect(cfg)
	if err != nil {
		log.Fatalf("smb: %v", err)
	}
	defer closeShare()

	failed := false
	keep := map[string]bool{}
	for _, p := range projects {
		name := cfg.MachineID + "__" + p.folder + ".md"
		keep[strings.ToLower(name)] = true
		if err := upload(share, p.path, name); err != nil {
			log.Printf("upload %s: %v", p.path, err)
			failed = true
			continue
		}
		log.Printf("uploaded %s -> %s", p.path, name)
	}

	// An empty scan more likely means a wrong or missing root than that every
	// project is gone, so never wipe this machine's cards on an empty scan.
	if len(projects) == 0 {
		log.Printf("no spark.md files found; skipping deletion")
	} else if err := prune(share, cfg.MachineID, keep); err != nil {
		log.Printf("prune: %v", err)
		failed = true
	}

	if failed {
		os.Exit(1)
	}
}

func loadConfig(path string) (config, error) {
	vals := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return config{}, err
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
			return config{}, fmt.Errorf("%s: invalid line %q", path, line)
		}
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	if err := sc.Err(); err != nil {
		return config{}, err
	}

	cfg := config{
		MachineID:   vals["MACHINE_ID"],
		ScanRoot:    expandHome(vals["SCAN_ROOT"]),
		SMBHost:     vals["SMB_HOST"],
		SMBUser:     vals["SMB_USER"],
		SMBPassword: vals["SMB_PASSWORD"],
		SMBShare:    vals["SMB_SHARE"],
	}
	for k, v := range map[string]string{
		"MACHINE_ID": cfg.MachineID, "SCAN_ROOT": cfg.ScanRoot, "SMB_HOST": cfg.SMBHost,
		"SMB_USER": cfg.SMBUser, "SMB_PASSWORD": cfg.SMBPassword, "SMB_SHARE": cfg.SMBShare,
	} {
		if v == "" {
			return config{}, fmt.Errorf("%s: %s is required", path, k)
		}
	}
	if strings.Contains(cfg.MachineID, "__") || strings.ContainsAny(cfg.MachineID, `/\`) {
		return config{}, fmt.Errorf("MACHINE_ID must not contain __ or path separators")
	}
	if _, _, err := net.SplitHostPort(cfg.SMBHost); err != nil {
		cfg.SMBHost = net.JoinHostPort(cfg.SMBHost, "445")
	}
	return cfg, nil
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

// scan walks root and returns one project per folder containing spark.md.
// It does not descend into a folder once it has found spark.md there.
func scan(root string) ([]project, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	var found []project
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
	if err != nil {
		return nil, err
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
	return unique, nil
}

func connect(cfg config) (*smb2.Share, func(), error) {
	conn, err := net.Dial("tcp", cfg.SMBHost)
	if err != nil {
		return nil, nil, err
	}
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: cfg.SMBUser, Password: cfg.SMBPassword}}
	session, err := d.Dial(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	share, err := session.Mount(cfg.SMBShare)
	if err != nil {
		session.Logoff()
		conn.Close()
		return nil, nil, err
	}
	return share, func() {
		share.Umount()
		session.Logoff()
		conn.Close()
	}, nil
}

// upload writes to a temp file and renames it into place so the web app never
// reads a partial file. SMB rename will not overwrite, so the old file is
// removed first; the file is briefly absent but never half-written.
func upload(share *smb2.Share, localPath, name string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	tmp := "." + name + ".tmp"
	if err := share.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := share.Remove(name); err != nil && !os.IsNotExist(err) {
		share.Remove(tmp)
		return err
	}
	if err := share.Rename(tmp, name); err != nil {
		share.Remove(tmp)
		return err
	}
	return nil
}

// prune deletes this machine's files on the share that were not uploaded in
// this run, along with any temp files left behind by an interrupted run.
func prune(share *smb2.Share, machineID string, keep map[string]bool) error {
	entries, err := share.ReadDir(".")
	if err != nil {
		return err
	}
	prefix := strings.ToLower(machineID + "__")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		stale := strings.HasPrefix(lower, prefix) && strings.HasSuffix(lower, ".md") && !keep[lower]
		leftover := strings.HasPrefix(lower, "."+prefix) && strings.HasSuffix(lower, ".tmp")
		if !stale && !leftover {
			continue
		}
		if err := share.Remove(e.Name()); err != nil {
			log.Printf("delete %s: %v", e.Name(), err)
			continue
		}
		log.Printf("deleted %s", e.Name())
	}
	return nil
}
