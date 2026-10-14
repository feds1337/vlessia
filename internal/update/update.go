// Package update checks GitHub releases for a newer version of the client
// and installs it with the NSIS installer from the release.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Repo is the GitHub repository releases are published to.
const Repo = "feds1337/vlessia"

// Release describes the latest published version.
type Release struct {
	Version string `json:"version"` // tag, e.g. "v0.2.0"
	Notes   string `json:"notes"`   // release description (markdown)
	PageURL string `json:"pageUrl"`

	setupURL  string
	setupName string
	size      int64
	sumsURL   string
}

// Latest fetches the latest non-prerelease release.
func Latest(ctx context.Context, client *http.Client) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Vlessia-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub ответил HTTP %d", resp.StatusCode)
	}
	var r struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, err
	}
	rel := &Release{Version: r.TagName, Notes: r.Body, PageURL: r.HTMLURL}
	for _, a := range r.Assets {
		switch {
		case strings.HasSuffix(strings.ToLower(a.Name), "-setup.exe"):
			rel.setupURL, rel.setupName, rel.size = a.URL, a.Name, a.Size
		case a.Name == "SHA256SUMS.txt":
			rel.sumsURL = a.URL
		}
	}
	return rel, nil
}

// Newer reports whether version latest is newer than current. Development
// builds ("dev") are never updated.
func Newer(current, latest string) bool {
	c, ok1 := parse(current)
	l, ok2 := parse(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// CanInstall reports whether the release has an installer to apply.
func (r *Release) CanInstall() bool { return r.setupURL != "" }

// Download fetches the installer into dir and verifies its size and, when
// the release publishes SHA256SUMS.txt, its checksum.
func Download(ctx context.Context, client *http.Client, r *Release, dir string, progress func(done, total int64)) (string, error) {
	if !r.CanInstall() {
		return "", errors.New("в релизе нет установщика")
	}
	want := ""
	if r.sumsURL != "" {
		sums, err := fetchSums(ctx, client, r.sumsURL)
		if err != nil {
			return "", fmt.Errorf("не удалось получить контрольные суммы: %w", err)
		}
		if want = sums[r.setupName]; want == "" {
			return "", fmt.Errorf("в SHA256SUMS.txt нет %s", r.setupName)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.setupName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.setupURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Vlessia-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("загрузка: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	pw := &progressWriter{total: r.size, report: progress}
	n, err := io.Copy(io.MultiWriter(f, h, pw), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if r.size > 0 && n != r.size {
		_ = os.Remove(path)
		return "", fmt.Errorf("файл скачан не полностью (%d из %d байт)", n, r.size)
	}
	if want != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
		_ = os.Remove(path)
		return "", errors.New("контрольная сумма установщика не совпала")
	}
	return path, nil
}

func fetchSums(ctx context.Context, client *http.Client, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Vlessia-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return ParseSums(io.LimitReader(resp.Body, 1<<20)), nil
}

// ParseSums parses sha256sum output: "<hex>  <name>" (also "<hex> *<name>").
func ParseSums(r io.Reader) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out
}

type progressWriter struct {
	done, total int64
	report      func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.report != nil {
		p.report(p.done, p.total)
	}
	return len(b), nil
}
