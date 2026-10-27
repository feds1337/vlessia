package update

import (
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0", "v0.1.1", true},
		{"v0.9.0", "v0.10.0", true}, // numeric, not lexical
		{"v1.0.0", "v0.9.9", false},
		{"v0.2.0", "v0.2.0", false},
		{"v0.2.0-beta", "v0.2.0", false}, // pre-release suffix is ignored
		{"dev", "v9.9.9", false},         // development builds never update
		{"v0.1.0", "garbage", false},
	} {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestParseSums(t *testing.T) {
	sums := ParseSums(strings.NewReader(
		"3f1c2a9e" + strings.Repeat("0", 56) + "  Vlessia-v0.2.0-setup.exe\n" +
			"AB" + strings.Repeat("1", 62) + " *Vlessia-v0.2.0-portable.exe\n" +
			"garbage line\n"))
	if len(sums) != 2 || sums["Vlessia-v0.2.0-portable.exe"] != "ab"+strings.Repeat("1", 62) {
		t.Fatalf("%v", sums)
	}
}

func TestForAndroid(t *testing.T) {
	r := &Release{setupURL: "setup", setupName: "Vlessia-v0.3.0-setup.exe", apks: []asset{
		{"Vlessia-v0.3.0-android-universal.apk", "u", 3},
		{"Vlessia-v0.3.0-android-arm64-v8a.apk", "a64", 1},
		{"Vlessia-v0.3.0-android-armeabi-v7a.apk", "a32", 2},
	}}
	for abi, want := range map[string]string{"arm64-v8a": "a64", "armeabi-v7a": "a32", "x86": "u", "": "u"} {
		if got := r.ForAndroid(abi); got.setupURL != want || !got.CanInstall() {
			t.Errorf("ForAndroid(%q) = %q, want %q", abi, got.setupURL, want)
		}
	}
	if r.setupURL != "setup" {
		t.Error("ForAndroid changed the original release")
	}
	if (&Release{setupURL: "setup"}).ForAndroid("arm64-v8a").CanInstall() {
		t.Error("a release without APKs must not install on Android")
	}
}
