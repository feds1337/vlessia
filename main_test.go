package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
