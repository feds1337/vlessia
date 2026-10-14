package core

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/feds1337/vlessia/internal/store"
)

func newLibrary(t *testing.T) *Library {
	t.Helper()
	t.Setenv("VLESSIA_DATA", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	return &Library{
		Store: st,
		Log:   log.New(io.Discard, "", 0),
		HTTP:  func(timeout time.Duration) *http.Client { return HTTPClient(nil, timeout) },
	}
}

func TestImportAndSelect(t *testing.T) {
	l := newLibrary(t)
	changes := 0
	l.Changed = func() { changes++ }
	n, err := l.ImportText("vless://11111111-1111-1111-1111-111111111111@1.2.3.4:443#one\nvless://11111111-1111-1111-1111-111111111111@5.6.7.8:443#two")
	if err != nil || n != 2 {
		t.Fatalf("ImportText = %d, %v", n, err)
	}
	if changes == 0 {
		t.Error("Changed was not called")
	}
	if s := l.Selected(); s == nil || s.Name != "one" {
		t.Fatalf("first imported server should be selected, got %+v", s)
	}
	if _, err := l.ImportText("not a link"); err == nil {
		t.Error("garbage was imported")
	}
}

func TestSubscriptionUpdateKeepsSelection(t *testing.T) {
	links := []string{
		"vless://11111111-1111-1111-1111-111111111111@1.2.3.4:443#a",
		"vless://11111111-1111-1111-1111-111111111111@5.6.7.8:443#b",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=100; expire=0")
		io.WriteString(w, base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))))
	}))
	defer srv.Close()

	l := newLibrary(t)
	n, err := l.ImportText(srv.URL + "/sub")
	if err != nil || n != 2 {
		t.Fatalf("subscription = %d, %v", n, err)
	}
	var subID string
	l.Store.Read(func(d *store.Data) {
		subID = d.Subscriptions[0].ID
		if s := d.Subscriptions[0]; !s.HasUserInfo || s.Total != 100 {
			t.Errorf("traffic info not stored: %+v", s)
		}
	})

	// Select "b", then the provider reorders and adds a server.
	var b string
	l.Store.Read(func(d *store.Data) { b = d.Servers[1].ID })
	if err := l.Select(b); err != nil {
		t.Fatal(err)
	}
	links = []string{"vless://11111111-1111-1111-1111-111111111111@9.9.9.9:443#c", links[1], links[0]}
	if n, err := l.UpdateSubscription(subID); err != nil || n != 3 {
		t.Fatalf("update = %d, %v", n, err)
	}
	if s := l.Selected(); s == nil || s.Name != "b" {
		t.Fatalf("selection lost after update: %+v", s)
	}
}

func TestEffectiveMode(t *testing.T) {
	l := newLibrary(t)
	if _, err := l.ImportText("[Interface]\nPrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\nAddress = 10.0.0.2/32\n\n[Peer]\nPublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\nEndpoint = 1.2.3.4:51820\nAllowedIPs = 0.0.0.0/0\n"); err != nil {
		t.Fatal(err)
	}
	if mode := EffectiveMode(l.Selected(), "proxy"); mode != "tun" {
		t.Errorf("WireGuard runs in %q, want tun", mode)
	}
}

func TestImportQRFileAndWelcome(t *testing.T) {
	l := newLibrary(t)
	link := "vless://11111111-1111-1111-1111-111111111111@1.2.3.4:443#qr"
	m, err := qrcode.NewQRCodeWriter().Encode(link, gozxing.BarcodeFormat_QR_CODE, 300, 300, nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	if n, err := l.ImportFile(buf.Bytes(), "screenshot"); err != nil || n != 1 {
		t.Fatalf("ImportFile(qr png) = %d, %v", n, err)
	}
	if s := l.Selected(); s == nil || s.Name != "qr" {
		t.Fatalf("imported %+v", s)
	}
	if _, err := l.ImportFile([]byte("\x89PNG\r\n\x1a\nbroken"), "x"); err == nil {
		t.Error("a broken picture was imported")
	}

	if !l.WelcomePending() {
		t.Fatal("welcome should be pending on first run")
	}
	if err := l.SetWelcomeSeen(); err != nil || l.WelcomePending() {
		t.Fatalf("welcome still pending: %v", err)
	}
}
