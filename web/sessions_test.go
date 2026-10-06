package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSessions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(configDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := loadSessions(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a1, _ := st.create("sam")
	a2, _ := st.create("sam")
	b, _ := st.create("lee")

	if u, ok := st.lookup(a1); !ok || u != "sam" {
		t.Fatalf("lookup = %q %v", u, ok)
	}
	if _, ok := st.lookup("made-up"); ok {
		t.Error("an unknown id was accepted")
	}

	// Only hashes reach the file, and a restart keeps the sessions.
	data, _ := os.ReadFile(st.path)
	if strings.Contains(string(data), a1) {
		t.Error("sessions.json holds a session id")
	}
	if st, _ = loadSessions(root, time.Hour); st.byHash[tokenHash(b)] == nil {
		t.Fatal("sessions lost on reload")
	}

	// Log out everywhere but here.
	if err := st.endAll("sam", a2); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.lookup(a1); ok {
		t.Error("other session survived endAll")
	}
	if _, ok := st.lookup(a2); !ok {
		t.Error("kept session ended")
	}
	if _, ok := st.lookup(b); !ok {
		t.Error("another account's session ended")
	}

	if err := st.end(a2); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.lookup(a2); ok {
		t.Error("ended session still works")
	}

	// Idle too long.
	st.byHash[tokenHash(b)].LastSeen = time.Now().Add(-2 * time.Hour)
	if _, ok := st.lookup(b); ok {
		t.Error("idle session still works")
	}

	// Recent use isn't written every time, older use is.
	c, _ := st.create("kim")
	seen := time.Now().Add(-time.Minute).UTC()
	st.byHash[tokenHash(c)].LastSeen = seen
	st.lookup(c)
	if !st.byHash[tokenHash(c)].LastSeen.Equal(seen) {
		t.Error("last seen bumped within lastSeenEvery")
	}
	st.byHash[tokenHash(c)].LastSeen = time.Now().Add(-10 * time.Minute)
	st.lookup(c)
	if time.Since(st.byHash[tokenHash(c)].LastSeen) > time.Minute {
		t.Error("last seen not bumped after lastSeenEvery")
	}
}
