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
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Repo is the GitHub repository releases are published to.
const Repo = "feds1337/vlessia"

var releaseTag = regexp.MustCompile("^v[0-9]+[.][0-9]+[.][0-9]+$")

// Release describes the latest published version.
type Release struct {
	Version   string `json:"version"`   // tag, e.g. "v0.2.0"
	NotesHTML string `json:"notesHtml"` // release description rendered from Markdown
	PageURL   string `json:"pageUrl"`

	setupURL  string
	setupName string
	size      int64
	sumsURL   string
	apks      []asset // Android packages
}

type asset struct {
	name, url string
	size      int64
}

// Latest fetches the latest non-prerelease release.
func Latest(ctx context.Context, client *http.Client) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Vlessia-updater")
	defer client.CloseIdleConnections()
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
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if !releaseTag.MatchString(r.TagName) {
		return nil, errors.New("неверная версия релиза")
	}
	base := "https://github.com/" + Repo
	rel := &Release{Version: r.TagName, NotesHTML: renderNotes(r.Body), PageURL: base + "/releases/tag/" + r.TagName}
	prefix := "Vlessia-" + r.TagName
	for _, a := range r.Assets {
		if a.URL != base+"/releases/download/"+r.TagName+"/"+a.Name || a.Size <= 0 {
			continue
		}
		switch {
		case a.Name == prefix+"-setup.exe":
			rel.setupURL, rel.setupName, rel.size = a.URL, a.Name, a.Size
		case a.Name == prefix+"-android-arm64-v8a.apk", a.Name == prefix+"-android-armeabi-v7a.apk",
			a.Name == prefix+"-android-x86_64.apk", a.Name == prefix+"-android-universal.apk":
			rel.apks = append(rel.apks, asset{a.Name, a.URL, a.Size})
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

// ForAndroid returns the release with the Android package for abi (e.g.
// "arm64-v8a", from Build.SUPPORTED_ABIS) as its installer, falling back to
// the universal package. The result cannot install if there is neither.
func (r *Release) ForAndroid(abi string) *Release {
	c := *r
	c.setupURL, c.setupName, c.size = "", "", 0
	var wants []string
	if abi != "" {
		wants = append(wants, "-"+abi+".apk")
	}
	for _, want := range append(wants, "-universal.apk") {
		for _, a := range r.apks {
			if strings.HasSuffix(strings.ToLower(a.name), strings.ToLower(want)) {
				c.setupURL, c.setupName, c.size = a.url, a.name, a.size
				return &c
			}
		}
	}
	return &c
}

// CanInstall reports whether the release has an installer to apply.
func (r *Release) CanInstall() bool { return r.setupURL != "" }

// Download verifies both the installer size and the published SHA-256.
func Download(ctx context.Context, client *http.Client, r *Release, dir string, progress func(done, total int64)) (string, error) {
	if !r.CanInstall() {
		return "", errors.New("в релизе нет установщика")
	}
	if r.sumsURL == "" {
		return "", errors.New("в релизе нет SHA256SUMS.txt")
	}
	if r.size <= 0 || r.size > 512<<20 || !filepath.IsLocal(r.setupName) || r.setupName != filepath.Base(r.setupName) {
		return "", errors.New("неверный файл обновления")
	}
	defer client.CloseIdleConnections()
	sums, err := fetchSums(ctx, client, r.sumsURL)
	if err != nil {
		return "", fmt.Errorf("не удалось получить контрольные суммы: %w", err)
	}
	want := sums[r.setupName]
	if want == "" {
		return "", fmt.Errorf("в SHA256SUMS.txt нет %s", r.setupName)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.setupName)
	if verifyFile(path, r.size, want) {
		if progress != nil && r.size > 0 {
			progress(r.size, r.size)
		}
		return path, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.setupURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Vlessia-updater")
	resp, err := downloadClient(client).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("загрузка: HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, "download-*.part")
	if err != nil {
		return "", err
	}
	part := f.Name()
	defer os.Remove(part)
	h := sha256.New()
	pw := &progressWriter{total: r.size, report: progress}
	n, err := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, r.size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if r.size > 0 && n != r.size {
		return "", fmt.Errorf("файл скачан не полностью (%d из %d байт)", n, r.size)
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
		return "", errors.New("контрольная сумма установщика не совпала")
	}
	if err := os.Rename(part, path); err != nil {
		return "", err
	}
	return path, nil
}

func verifyFile(path string, wantSize int64, wantSum string) bool {
	if wantSize <= 0 && wantSum == "" {
		return false
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || (wantSize > 0 && st.Size() != wantSize) {
		return false
	}
	if wantSum == "" {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantSum)
}

func fetchSums(ctx context.Context, client *http.Client, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Vlessia-updater")
	resp, err := downloadClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return ParseSums(io.LimitReader(resp.Body, 1<<20)), nil
}

func downloadClient(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !allowedAssetHost(req.URL) {
			return errors.New("недоверенный адрес обновления")
		}
		return nil
	}
	return &c
}

func allowedAssetHost(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

// ParseSums parses sha256sum output: "<hex>  <name>" (also "<hex> *<name>").
func ParseSums(r io.Reader) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			if _, err := hex.DecodeString(f[0]); err != nil {
				continue
			}
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
