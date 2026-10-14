package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feds1337/vlessia/internal/store"
)

func TestRotatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	r := &rotatingFile{path: path, max: 100}
	if err := r.open(); err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("x", 39) + "\n")
	for i := 0; i < 5; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	r.f.Close()
	cur, _ := os.ReadFile(path)
	old, _ := os.ReadFile(path + ".old")
	if len(cur) > 100 || len(old) == 0 || len(cur)+len(old) > 5*len(line) {
		t.Fatalf("current %d bytes, old %d bytes", len(cur), len(old))
	}
}

// TestAppLibrary exercises the desktop App's server methods, which
// delegate to internal/core.
func TestAppLibrary(t *testing.T) {
	t.Setenv("VLESSIA_DATA", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp(st, log.New(io.Discard, "", 0))
	n, err := a.ImportText("vless://11111111-1111-1111-1111-111111111111@1.2.3.4:443#one\nvless://11111111-1111-1111-1111-111111111111@5.6.7.8:443#two")
	if err != nil || n != 2 {
		t.Fatalf("ImportText = %d, %v", n, err)
	}
	s := a.GetState()
	if len(s.Servers) != 2 || s.Selected != s.Servers[0].ID || s.Status.State != "disconnected" {
		t.Fatalf("state %+v", s)
	}
	if err := a.SelectServer(s.Servers[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteServer(s.Servers[1].ID); err != nil {
		t.Fatal(err)
	}
	if s := a.GetState(); len(s.Servers) != 1 || s.Selected != "" {
		t.Fatalf("after delete %+v", s)
	}
	if _, err := a.TestConnection(); err == nil {
		t.Error("TestConnection without a connection should fail")
	}
	if err := a.Connect(); err == nil {
		t.Error("Connect without a selected server should fail")
	}
}

func TestSaveRules(t *testing.T) {
	t.Setenv("VLESSIA_DATA", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp(st, log.New(io.Discard, "", 0))
	s := a.GetState().Settings
	s.ProxyRules = []string{" youtube.com ", "", "10.0.0.0/8"}
	if err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if got := a.GetState().Settings.ProxyRules; strings.Join(got, ",") != "youtube.com,10.0.0.0/8" {
		t.Fatalf("saved %q", got)
	}
	s.DirectRules = []string{"not a rule"}
	if err := a.SaveSettings(s); err == nil || !strings.Contains(err.Error(), "not a rule") {
		t.Fatalf("invalid rule: %v", err)
	}
	if got := a.GetState().Settings.DirectRules; len(got) != 0 {
		t.Fatalf("invalid rule was stored: %q", got)
	}
}
