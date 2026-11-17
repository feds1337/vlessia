package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerifiedDownload(t *testing.T) {
	const content = "verified installer"
	for _, tc := range []struct {
		name, body string
		size       int64
		sum        bool
		valid      bool
	}{
		{"valid", content, int64(len(content)), true, true},
		{"missing sums", content, int64(len(content)), false, false},
		{"tampered", "tampered installer", int64(len(content)), true, false},
		{"truncated", "short", int64(len(content)), true, false},
		{"oversize", content + "extra", int64(len(content)), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Release{setupURL: "https://github.com/setup.exe", setupName: "setup.exe", size: tc.size}
			if tc.sum {
				r.sumsURL = "https://github.com/SHA256SUMS.txt"
			}
			client := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				body := tc.body
				if strings.HasSuffix(req.URL.Path, "SHA256SUMS.txt") {
					body = fmt.Sprintf("%x  setup.exe\n", sha256.Sum256([]byte(content)))
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			dir := t.TempDir()
			path, err := Download(context.Background(), client, r, dir, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("path=%s err=%v", path, err)
			}
			if tc.valid {
				b, _ := os.ReadFile(path)
				if string(b) != content {
					t.Fatal("wrong downloaded file")
				}
				if _, err := Download(context.Background(), client, r, dir, nil); err != nil {
					t.Fatal("cached update", err)
				}
			} else {
				files, _ := filepath.Glob(filepath.Join(dir, "*"))
				if len(files) != 0 {
					t.Fatalf("unverified/partial files left: %v", files)
				}
			}
		})
	}
}
