package subscription

import (
	"encoding/base64"
	"net/http"
	"testing"
)

func TestParseHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Subscription-Userinfo", "upload=1073741824; download=2.147483648e+09; total=107374182400; expire=1798761600")
	h.Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("Мой VPN 🚀")))
	h.Set("Announce", "base64:"+base64.StdEncoding.EncodeToString([]byte("Продлите до 1 ноября")))
	h.Set("Support-Url", "https://t.me/support")
	h.Set("Profile-Web-Page-Url", "https://panel.example.com/sub/abc")
	h.Set("Profile-Update-Interval", "6")

	info := Parse(h, "vless://x@1.2.3.4:443#a")
	want := Info{
		Title: "Мой VPN 🚀", Announce: "Продлите до 1 ноября",
		SupportURL: "https://t.me/support", WebPageURL: "https://panel.example.com/sub/abc",
		UpdateInterval: 6, Upload: 1 << 30, Download: 2 << 30, Total: 100 << 30, Expire: 1798761600,
		HasUserInfo: true,
	}
	if info != want {
		t.Fatalf("got  %+v\nwant %+v", info, want)
	}
}

func TestParseBodyMeta(t *testing.T) {
	body := "#profile-title: Body title\n" +
		"#subscription-userinfo: upload=0; download=5; total=10; expire=0\n" +
		"#announce: Привет\n" +
		"vless://x@1.2.3.4:443#a\n"
	h := http.Header{}
	h.Set("Profile-Title", "Header title") // headers win
	info := Parse(h, body)
	if info.Title != "Header title" || info.Announce != "Привет" || info.Download != 5 || info.Total != 10 || !info.HasUserInfo {
		t.Fatalf("got %+v", info)
	}
}

func TestParseFallbacks(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Disposition", `attachment; filename="my-sub.txt"`)
	h.Set("Support-Url", "javascript:alert(1)")
	h.Set("Subscription-Userinfo", "expire=1798761600000")
	info := Parse(h, "")
	if info.Title != "my-sub" || info.SupportURL != "" || info.Expire != 1798761600 {
		t.Fatalf("got %+v", info)
	}
}
