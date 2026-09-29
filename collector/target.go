package main

import (
	"net"
	"os"
	"path/filepath"

	"github.com/hirochachacha/go-smb2"
)

// target is where snapshots are copied: an SMB share or a local folder.
// Put must never leave a partially written file under name.
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

type smbTarget struct {
	share   *smb2.Share
	session *smb2.Session
	conn    net.Conn
}

func connectSMB(cfg config) (target, error) {
	conn, err := net.Dial("tcp", cfg.SMBHost)
	if err != nil {
		return nil, err
	}
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
	return &smbTarget{share: share, session: session, conn: conn}, nil
}

// Put writes a temp file and renames it into place. SMB rename will not
// overwrite, so the old file is removed first; the file is briefly absent but
// never half-written.
func (t *smbTarget) Put(name string, data []byte) error {
	tmp := "." + name + ".tmp"
	if err := t.share.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := t.share.Remove(name); err != nil && !os.IsNotExist(err) {
		t.share.Remove(tmp)
		return err
	}
	if err := t.share.Rename(tmp, name); err != nil {
		t.share.Remove(tmp)
		return err
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
