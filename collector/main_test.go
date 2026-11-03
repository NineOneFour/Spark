package main

import (
	"os"
	"path/filepath"
	"testing"
)

// memTarget is an in-memory target.
type memTarget struct {
	files     map[string][]byte
	removeErr map[string]error
}

func newMemTarget(names ...string) *memTarget {
	m := &memTarget{files: map[string][]byte{}, removeErr: map[string]error{}}
	for _, n := range names {
		m.files[n] = []byte("old")
	}
	return m
}

func (m *memTarget) Put(name string, data []byte) error { m.files[name] = data; return nil }
func (m *memTarget) List() ([]string, error) {
	var out []string
	for n := range m.files {
		out = append(out, n)
	}
	return out, nil
}
func (m *memTarget) Remove(name string) error {
	if err := m.removeErr[name]; err != nil {
		return err
	}
	delete(m.files, name)
	return nil
}
func (m *memTarget) Close() {}

func writeSpark(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sparkFile), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPartialScanDoesNotPrune(t *testing.T) {
	tmp := t.TempDir()
	dirA := filepath.Join(tmp, "dirA")
	writeSpark(t, filepath.Join(dirA, "a"))
	dirB := filepath.Join(tmp, "dirB") // never created: unreadable root

	dst := newMemTarget("m__a.md", "m__b.md")
	failed := run(config{MachineID: "m", ScanRoots: []string{dirA, dirB}}, dst)

	if !failed {
		t.Error("run should fail on an incomplete scan")
	}
	if _, ok := dst.files["m__b.md"]; !ok {
		t.Error("m__b.md was pruned by an incomplete scan")
	}
	if string(dst.files["m__a.md"]) != "x" {
		t.Error("m__a.md should still be copied")
	}
}

func TestSparkAtScanRootIsIncompleteAndNotPruned(t *testing.T) {
	tmp := t.TempDir()
	dirA := filepath.Join(tmp, "dirA")
	writeSpark(t, dirA)
	writeSpark(t, filepath.Join(dirA, "b"))

	dst := newMemTarget("m__b.md", "m__gone.md")
	failed := run(config{MachineID: "m", ScanRoots: []string{dirA}}, dst)

	if !failed {
		t.Error("run should fail when spark.md sits in a scan root")
	}
	if _, ok := dst.files["m__b.md"]; !ok {
		t.Error("m__b.md was deleted")
	}
	if _, ok := dst.files["m__gone.md"]; !ok {
		t.Error("nothing should be pruned on an incomplete scan")
	}
}

func TestCompleteScanPrunesAndSucceeds(t *testing.T) {
	tmp := t.TempDir()
	writeSpark(t, filepath.Join(tmp, "a"))
	dst := newMemTarget("m__gone.md", "other__keep.md")
	if failed := run(config{MachineID: "m", ScanRoots: []string{tmp}}, dst); failed {
		t.Error("run failed")
	}
	if _, ok := dst.files["m__gone.md"]; ok {
		t.Error("m__gone.md should be pruned")
	}
	if _, ok := dst.files["other__keep.md"]; !ok {
		t.Error("other machine's file was pruned")
	}
}

func TestPruneTable(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		keep     map[string]bool
		wantGone []string
		wantKept []string
	}{
		{"kept file stays", []string{"m__a.md"}, map[string]bool{"m__a.md": true}, nil, []string{"m__a.md"}},
		{"unkept file goes", []string{"m__a.md", "m__b.md"}, map[string]bool{"m__a.md": true}, []string{"m__b.md"}, []string{"m__a.md"}},
		{"other machine untouched", []string{"n__a.md"}, map[string]bool{}, nil, []string{"n__a.md"}},
		{"case-variant prefix is this machine", []string{"M__B.md"}, map[string]bool{}, []string{"M__B.md"}, nil},
		{"leftover tmp removed", []string{".m__a.md.tmp"}, map[string]bool{}, []string{".m__a.md.tmp"}, nil},
		{"other machine tmp untouched", []string{".n__a.md.tmp"}, map[string]bool{}, nil, []string{".n__a.md.tmp"}},
		{"non-md file untouched", []string{"m__notes.txt"}, map[string]bool{}, nil, []string{"m__notes.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := newMemTarget(tc.existing...)
			if err := prune(dst, "m", tc.keep); err != nil {
				t.Fatal(err)
			}
			for _, n := range tc.wantGone {
				if _, ok := dst.files[n]; ok {
					t.Errorf("%s should be removed", n)
				}
			}
			for _, n := range tc.wantKept {
				if _, ok := dst.files[n]; !ok {
					t.Errorf("%s should be kept", n)
				}
			}
		})
	}
}

func TestPruneReturnsRemoveError(t *testing.T) {
	dst := newMemTarget("m__a.md", "m__b.md")
	dst.removeErr["m__a.md"] = os.ErrPermission
	if err := prune(dst, "m", map[string]bool{}); err == nil {
		t.Error("want error when Remove fails")
	}
	if _, ok := dst.files["m__b.md"]; ok {
		t.Error("prune should continue past a failed Remove")
	}
}

func TestPruneFailureFailsRun(t *testing.T) {
	tmp := t.TempDir()
	writeSpark(t, filepath.Join(tmp, "a"))
	dst := newMemTarget("m__gone.md")
	dst.removeErr["m__gone.md"] = os.ErrPermission
	if !run(config{MachineID: "m", ScanRoots: []string{tmp}}, dst) {
		t.Error("run should fail when prune fails")
	}
}

func TestScanUnreadableRootIsIncomplete(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(file, []byte("x"), 0o644)
	_, complete := scan([]string{file})
	if complete {
		t.Error("a non-directory root must make the scan incomplete")
	}
}

func TestLocalTargetPutFailingRenameKeepsOld(t *testing.T) {
	dir := t.TempDir()
	dst, err := openLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the destination makes the rename fail.
	if err := os.MkdirAll(filepath.Join(dir, "m__a.md", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dst.Put("m__a.md", []byte("new")); err == nil {
		t.Fatal("want rename error")
	}
	if _, err := os.Stat(filepath.Join(dir, ".m__a.md.tmp")); !os.IsNotExist(err) {
		t.Error("temp file should be removed after a failed rename")
	}
}

func TestLocalTargetPutReplaces(t *testing.T) {
	dir := t.TempDir()
	dst, _ := openLocal(dir)
	for _, v := range []string{"one", "two"} {
		if err := dst.Put("m__a.md", []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(dir, "m__a.md"))
	if string(got) != "two" {
		t.Errorf("got %q", got)
	}
}
