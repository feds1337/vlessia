package core

import (
	"bytes"
	"strings"
	"testing"

	"github.com/feds1337/vlessia/internal/store"
)

func TestBackupRestore(t *testing.T) {
	l := newLibrary(t)
	if _, err := l.ImportText("vless://11111111-1111-1111-1111-111111111111@127.0.0.1:443#backup"); err != nil {
		t.Fatal(err)
	}
	selected := l.Selected().ID
	var out bytes.Buffer
	if err := l.WriteBackup(&out); err != nil {
		t.Fatal(err)
	}
	if err := l.DeleteServer(selected); err != nil {
		t.Fatal(err)
	}
	if err := l.RestoreBackup(strings.NewReader(out.String()+"null"), false); err == nil || l.Selected() != nil {
		t.Fatal("invalid backup changed the store")
	}
	if err := l.RestoreBackup(&out, true); err != nil {
		t.Fatal(err)
	}
	if got := l.Selected(); got == nil || got.ID != selected {
		t.Fatalf("selection after restore: %+v", got)
	}
	l.Store.Read(func(d *store.Data) {
		if d.Settings.Mode != "tun" {
			t.Fatalf("Android mode = %q", d.Settings.Mode)
		}
	})
}
