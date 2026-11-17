package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutomaticRecovery(t *testing.T) {
	for _, damage := range []string{"{broken", "null", "", "missing"} {
		t.Run(damage, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("VLESSIA_DATA", dir)
			s, err := Open()
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Update(func(d *Data) { d.Selected = "first" }); err != nil {
				t.Fatal(err)
			}
			if err := s.Update(func(d *Data) { d.Selected = "second" }); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.json")
			if damage == "missing" {
				err = os.Remove(path)
			} else {
				err = os.WriteFile(path, []byte(damage), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Open()
			if err != nil {
				t.Fatal(err)
			}
			if !restored.Recovered() || restored.data.Selected != "first" {
				t.Fatalf("not restored: %+v", restored)
			}
			if damage != "missing" {
				files, _ := filepath.Glob(path + ".corrupt-*")
				if len(files) != 1 {
					t.Fatal("damaged config was not preserved")
				}
			}
			if err := restored.Update(func(d *Data) { d.Selected = "third" }); err != nil {
				t.Fatal(err)
			}
			again, err := Open()
			if err != nil || again.Recovered() || again.data.Selected != "third" {
				t.Fatal("recovery did not persist", err)
			}
		})
	}
}

func TestInvalidConfigWithoutBackupPreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VLESSIA_DATA", dir)
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("bad"), 0600)
	if _, err := Open(); err == nil {
		t.Fatal("accepted damaged config")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "bad" {
		t.Fatal("damaged config overwritten")
	}
}

func TestFailedSaveDoesNotCommit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VLESSIA_DATA", dir)
	s, _ := Open()
	if err := s.Update(func(d *Data) { d.Selected = "saved" }); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "config.json.bak"))
	os.Mkdir(filepath.Join(dir, "config.json.bak"), 0700)
	if err := s.Update(func(d *Data) { d.Selected = "lost" }); err == nil {
		t.Fatal("expected backup write failure")
	}
	if s.data.Selected != "saved" {
		t.Fatal("failed save changed memory")
	}
	reopened, err := Open()
	if err != nil || reopened.data.Selected != "saved" {
		t.Fatal("failed save changed disk", err)
	}
}
