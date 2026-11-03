package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hirochachacha/go-smb2"
)

// target is where snapshots are copied: an SMB share or a local folder.
// Put must never leave a partially written file under name. localTarget.Put
// replaces atomically; smbTarget.Put cannot (SMB rename will not overwrite),
// so it moves the old file aside first and never leaves a moment where neither
// the old nor the new version exists.
type target interface {
	Put(name string, data []byte) error
	List() ([]string, error) // file names, not directories
	Remove(name string) error
	Close()
}

type localTarget struct{ dir string }

func openLocal(dir string) (target, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return localTarget{dir: dir}, nil
}

// Put writes a temp file and renames it into place, which replaces the old
// file atomically.
func (t localTarget) Put(name string, data []byte) error {
	tmp := filepath.Join(t.dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(t.dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (t localTarget) List() ([]string, error) {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (t localTarget) Remove(name string) error { return os.Remove(filepath.Join(t.dir, name)) }

func (t localTarget) Close() {}

// Timeouts for the SMB connection. The run deadline covers every operation on
// the connection, so a hung share cannot stall the collector forever.
var (
	smbDialTimeout = 10 * time.Second
	smbRunTimeout  = 90 * time.Second
)

// smbFS is the part of *smb2.Share that Put needs.
type smbFS interface {
	WriteFile(name string, data []byte, perm os.FileMode) error
	Rename(oldpath, newpath string) error
	Remove(name string) error
}

type smbTarget struct {
	fs      smbFS
	share   *smb2.Share
	session *smb2.Session
	conn    net.Conn
}

func connectSMB(cfg config) (target, error) {
	conn, err := (&net.Dialer{Timeout: smbDialTimeout}).Dial("tcp", cfg.SMBHost)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(smbRunTimeout))
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: cfg.SMBUser, Password: cfg.SMBPassword}}
	session, err := d.Dial(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	share, err := session.Mount(cfg.SMBShare)
	if err != nil {
		session.Logoff()
		conn.Close()
		return nil, err
	}
	return &smbTarget{fs: share, share: share, session: session, conn: conn}, nil
}

// Put writes a temp file, moves the live file aside to a per-run backup,
// renames the temp file into place, then removes the backup. If the final
// rename fails, the backup is restored and the temp file is kept. The backup
// name ends in ".tmp" and starts with "." + name, so prune's leftover cleanup
// matches it if a run is interrupted.
func (t *smbTarget) Put(name string, data []byte) error {
	tmp := "." + name + ".tmp"
	bak := "." + name + ".old.tmp"
	if err := t.fs.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	t.fs.Remove(bak) // stale backup from an interrupted run
	hadOld := true
	if err := t.fs.Rename(name, bak); err != nil {
		if !os.IsNotExist(err) {
			t.fs.Remove(tmp)
			return err
		}
		hadOld = false
	}
	if err := t.fs.Rename(tmp, name); err != nil {
		if hadOld {
			if rerr := t.fs.Rename(bak, name); rerr != nil {
				return fmt.Errorf("%w (restoring the previous version failed: %v; it is at %s, new data at %s)", err, rerr, bak, tmp)
			}
		}
		return fmt.Errorf("%w (new data kept at %s)", err, tmp)
	}
	if hadOld {
		t.fs.Remove(bak)
	}
	return nil
}

func (t *smbTarget) List() ([]string, error) {
	entries, err := t.share.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (t *smbTarget) Remove(name string) error { return t.share.Remove(name) }

func (t *smbTarget) Close() {
	t.share.Umount()
	t.session.Logoff()
	t.conn.Close()
}
