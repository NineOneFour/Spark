package main

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

// memFS is an in-memory smbFS with SMB rename semantics: no overwrite.
type memFS struct {
	files        map[string][]byte
	failRenameTo string // rename into this name fails once
}

func newMemFS() *memFS { return &memFS{files: map[string][]byte{}} }

func (m *memFS) WriteFile(name string, data []byte, _ os.FileMode) error {
	m.files[name] = data
	return nil
}

func (m *memFS) Remove(name string) error {
	if _, ok := m.files[name]; !ok {
		return fs.ErrNotExist
	}
	delete(m.files, name)
	return nil
}

func (m *memFS) Rename(oldpath, newpath string) error {
	data, ok := m.files[oldpath]
	if !ok {
		return fs.ErrNotExist
	}
	if newpath == m.failRenameTo {
		m.failRenameTo = ""
		return errors.New("injected rename failure")
	}
	if _, exists := m.files[newpath]; exists {
		return fs.ErrExist
	}
	delete(m.files, oldpath)
	m.files[newpath] = data
	return nil
}

func TestSMBPutReplacesExisting(t *testing.T) {
	m := newMemFS()
	m.files["m__a.md"] = []byte("old")
	if err := (&smbTarget{fs: m}).Put("m__a.md", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if string(m.files["m__a.md"]) != "new" || len(m.files) != 1 {
		t.Errorf("files = %v", m.files)
	}
}

func TestSMBPutNoPreviousFile(t *testing.T) {
	m := newMemFS()
	if err := (&smbTarget{fs: m}).Put("m__a.md", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if string(m.files["m__a.md"]) != "new" || len(m.files) != 1 {
		t.Errorf("files = %v", m.files)
	}
}

func TestSMBPutRenameFailureKeepsPreviousContent(t *testing.T) {
	m := newMemFS()
	m.files["m__a.md"] = []byte("old")
	m.failRenameTo = "m__a.md"
	err := (&smbTarget{fs: m}).Put("m__a.md", []byte("new"))
	if err == nil {
		t.Fatal("want error")
	}
	if got := string(m.files["m__a.md"]); got != "old" {
		t.Errorf("stored content = %q, want old", got)
	}
	if string(m.files[".m__a.md.tmp"]) != "new" {
		t.Error("new data should be kept in the temp file")
	}
}

func TestSMBBackupNameMatchesPruneLeftover(t *testing.T) {
	m := newMemFS()
	m.files[".m__a.md.old.tmp"] = []byte("x")
	dst := &fakeListTarget{names: []string{".m__a.md.old.tmp"}}
	if err := prune(dst, "m", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if len(dst.removed) != 1 {
		t.Errorf("removed = %v, want the backup file", dst.removed)
	}
}

type fakeListTarget struct {
	names, removed []string
}

func (f *fakeListTarget) Put(string, []byte) error { return nil }
func (f *fakeListTarget) List() ([]string, error)  { return f.names, nil }
func (f *fakeListTarget) Remove(n string) error    { f.removed = append(f.removed, n); return nil }
func (f *fakeListTarget) Close()                   {}

func TestConnectSMBDialTimeout(t *testing.T) {
	old := smbDialTimeout
	smbDialTimeout = 300 * time.Millisecond
	defer func() { smbDialTimeout = old }()

	start := time.Now()
	_, err := connectSMB(config{SMBHost: "10.255.255.1:445"})
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "network is unreachable") || strings.Contains(err.Error(), "no route") {
		t.Skip("sandbox has no route to a blackholed address: " + err.Error())
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("dial took %v with a 300ms timeout", d)
	}
}
