// Package subscription extracts subscription metadata (traffic, expiry,
// title, announcement, links) from HTTP headers and the response body.
//
// Panels (Marzban, Remnawave, 3x-ui, Hiddify, ...) send it as headers, and
// some also embed it in the body as comment lines ("#announce: ...").
package subscription

import (
	"encoding/base64"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Info struct {
	Title          string
	Announce       string
	SupportURL     string
	WebPageURL     string
	UpdateInterval int // hours, 0 if not provided
	Upload         int64
	Download       int64
	Total          int64 // 0 means unlimited
	Expire         int64 // unix seconds, 0 means never
	HasUserInfo    bool
}

// Parse reads metadata from headers first; body comment lines fill in
// anything the headers did not provide. body must already be decoded.
func Parse(h http.Header, body string) Info {
	var info Info
	get := func(key string) string { return strings.TrimSpace(h.Get(key)) }
	apply(&info, get)

	meta := bodyMeta(body)
	fill := Info{}
	apply(&fill, func(key string) string { return meta[strings.ToLower(key)] })
	merge(&info, fill)

	if info.Title == "" {
		info.Title = dispositionFilename(get("Content-Disposition"))
	}
	return info
}

func apply(info *Info, get func(string) string) {
	info.Title = decodeValue(get("profile-title"))
	info.Announce = decodeValue(get("announce"))
	info.SupportURL = validURL(get("support-url"))
	info.WebPageURL = validURL(get("profile-web-page-url"))
	if n, err := strconv.ParseFloat(get("profile-update-interval"), 64); err == nil && n > 0 {
		info.UpdateInterval = int(math.Ceil(n))
	}
	if v := get("subscription-userinfo"); v != "" {
		parseUserInfo(v, info)
	}
}

func merge(dst *Info, src Info) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.Announce == "" {
		dst.Announce = src.Announce
	}
	if dst.SupportURL == "" {
		dst.SupportURL = src.SupportURL
	}
	if dst.WebPageURL == "" {
		dst.WebPageURL = src.WebPageURL
	}
	if dst.UpdateInterval == 0 {
		dst.UpdateInterval = src.UpdateInterval
	}
	if !dst.HasUserInfo && src.HasUserInfo {
		dst.Upload, dst.Download, dst.Total, dst.Expire = src.Upload, src.Download, src.Total, src.Expire
		dst.HasUserInfo = true
	}
}

// parseUserInfo parses "upload=1; download=2; total=3; expire=4". Values may
// be floats (some panels emit 1.07e+11).
func parseUserInfo(v string, info *Info) {
	for _, part := range strings.Split(v, ";") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil || f < 0 {
			continue
		}
		n := int64(f)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "upload":
			info.Upload = n
		case "download":
			info.Download = n
		case "total":
			info.Total = n
		case "expire":
			if n > 1e12 { // milliseconds
				n /= 1000
			}
			info.Expire = n
		default:
			continue
		}
		info.HasUserInfo = true
	}
}

// bodyMeta collects "#key: value" lines from the subscription body.
func bodyMeta(body string) map[string]string {
	meta := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "#")
		if !ok {
			continue
		}
		k, v, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if _, seen := meta[k]; !seen {
			meta[k] = strings.TrimSpace(v)
		}
	}
	return meta
}

// decodeValue handles the "base64:" prefix used for non-ASCII header values.
func decodeValue(v string) string {
	rest, ok := strings.CutPrefix(v, "base64:")
	if !ok {
		return v
	}
	rest = strings.TrimRight(strings.TrimSpace(rest), "=")
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(rest); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return v
}

func validURL(v string) string {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "tg") {
		return ""
	}
	return v
}

func dispositionFilename(v string) string {
	if v == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	name := params["filename"]
	if i := strings.LastIndex(name, "."); i > 0 && strings.EqualFold(name[i:], ".txt") {
		name = name[:i]
	}
	return name
}
