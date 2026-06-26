package xuelang

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func postJSON(c *util.Client, api string, payload map[string]any, h map[string]string) (map[string]any, error) {
	b, _ := json.Marshal(payload)
	resp, err := c.Post(api, bytes.NewReader(b), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return parseJSON(string(raw))
}
func parseJSON(body string) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("xuelang parse JSON: %w", err)
	}
	return root, nil
}
func headers(cookie string) map[string]string {
	return map[string]string{"cookie": cookie, "Cookie": cookie, "referer": refererURL, "Referer": refererURL, "User-Agent": defaultUA + " " + strconv.FormatInt(time.Now().UnixNano(), 36), "Content-Type": "application/json"}
}
func cookieHeader(jar http.CookieJar) string {
	parts := []string{}
	for _, raw := range []string{refererURL, "https://student-api.iyincaishijiao.com", "https://classroom.iyincaishijiao.com", "https://vod.bytedanceapi.com"} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func selectCourse(cs []course, cid string) course {
	for _, c := range cs {
		if cid == "" || c.id == cid {
			return c
		}
	}
	return course{}
}
func media(title string, pm playMedia, co course, l lesson) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "xuelang", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: []string{pm.videoURL}, Format: "m3u8", Size: pm.size, AudioURL: pm.audioURL, Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"course_id": co.id, "room_id": l.roomID, "video_id": pm.videoID, "key_id": pm.keyID}}
}

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}
func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"MainPlayUrl", "BackupPlayUrl", "play_url", "m3u8", "url", "preview_url"} {
			if u := val(m, k); strings.HasPrefix(u, "http") {
				return u
			}
		}
	}
	return ""
}
func mapAt(v any, key string) map[string]any {
	if m, ok := valueAt(v, key).(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func listUnder(v any, key string) []map[string]any { return listFrom(valueAt(v, key)) }
func listFrom(v any) []map[string]any {
	out := []map[string]any{}
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}
func valueAt(v any, key string) any {
	for _, m := range mapsUnder(v) {
		if x, ok := m[key]; ok {
			return x
		}
	}
	return nil
}
func mapsUnder(v any) []map[string]any {
	out := []map[string]any{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, y := range t {
				walk(y)
			}
		case []any:
			for _, y := range t {
				walk(y)
			}
		}
	}
	walk(v)
	return out
}
func val(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return asString(m[key])
}
func asString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		if math.Trunc(x) == x {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}
func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}
func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != "" && x != "0" && x != "false"
	}
	return false
}
func unique(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, x := range in {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
