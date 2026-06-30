package luffycity

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type luffySource struct {
	URL, Type string
	Size      int64
	Headers   map[string]string
	Extra     map[string]any
}

func luffyCookieString(jar http.CookieJar) string {
	hosts := []string{"www.luffycity.com", "api.luffycity.com", "luffycity.com"}
	seen, parts := map[string]bool{}, []string{}
	for _, h := range hosts {
		for _, ck := range jar.Cookies(&url.URL{Scheme: "https", Host: h}) {
			if ck.Value != "" && !seen[ck.Name] {
				seen[ck.Name] = true
				parts = append(parts, ck.Name+"="+ck.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func cookieValue(cookie, name string) string {
	for _, p := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 && strings.EqualFold(kv[0], name) {
			v, _ := url.QueryUnescape(kv[1])
			return firstText(v, kv[1])
		}
	}
	return ""
}

func luffyAPIURL(p string, params map[string]string) string {
	if !strings.HasPrefix(p, "http") {
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		p = urlAPIBase + p
	}
	if len(params) == 0 {
		return p
	}
	u, err := url.Parse(p)
	if err != nil || u == nil {
		return ""
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func luffyAPIData(resp map[string]any) any {
	code := firstText(resp["code"])
	if code == "0" || code == "200" || boolOf(resp["success"]) || code == "" {
		if data, ok := resp["data"]; ok {
			return data
		}
	}
	if data, ok := resp["data"]; ok && code != "401" {
		return data
	}
	return map[string]any{}
}

func luffyTypeName(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "free":
		return "free"
	case "2", "degree", "vip", "employment":
		return "degree"
	case "11", "actual", "detail", "":
		return "actual"
	default:
		return "actual"
	}
}

func luffyIsVideo(m map[string]any) bool {
	t := strings.ToLower(firstText(m["section_type"], m["sectionType"], m["type"]))
	if t == "video" {
		return true
	}
	return firstText(m["vid"], m["video_id"], m["videoId"], m["play_url"], m["playUrl"], m["video_url"], m["videoUrl"]) != ""
}

func luffyMakeVideoItem(m map[string]any, prefix []int, canPlay bool) luffyItem {
	title := indexedTitle(prefix, firstText(m["name"], m["title"], m["course_name"], m["courseName"], m["section_name"], m["chapter_name"], m["id"]))
	return luffyItem{Kind: "video", Title: title, SectionID: firstText(m["id"], m["section_id"], m["sectionId"]), DirectURL: firstText(m["play_url"], m["playUrl"], m["video_url"], m["videoUrl"], m["url"]), CanPlay: canPlay || boolOf(m["is_buy"]) || boolOf(m["isBuy"]) || boolOf(m["freeTrail"]) || boolOf(m["free_trail"])}
}

func childMaps(v any) []map[string]any {
	m := mapAny(v)
	if len(m) > 0 {
		var out []map[string]any
		for _, k := range []string{"chapters", "sections", "children", "courses", "course_list", "courseList", "modules", "lessons", "list"} {
			out = append(out, records(m[k])...)
		}
		return out
	}
	return records(v)
}

func records(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, it := range x {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{x}
	}
	return nil
}

func luffyNormalizeURL(fileURL string, media bool) string {
	fileURL = strings.TrimSpace(fileURL)
	if fileURL == "" {
		return ""
	}
	if strings.HasPrefix(fileURL, "//") {
		return "https:" + fileURL
	}
	if strings.HasPrefix(fileURL, "/media/") {
		return urlCDN + fileURL
	}
	if strings.HasPrefix(fileURL, "/") {
		return strings.TrimRight(urlOrigin, "/") + fileURL
	}
	if strings.HasPrefix(fileURL, "http") {
		return fileURL
	}
	if media {
		return urlCDN + "/media/" + strings.TrimLeft(fileURL, "/")
	}
	u, err := url.Parse(urlOrigin + "/")
	if err != nil || u == nil {
		return ""
	}
	u.Path = path.Join(u.Path, fileURL)
	return u.String()
}

func luffyNormalizeMediaURL(v string) string {
	u := luffyNormalizeURL(v, true)
	lu := strings.ToLower(u)
	if strings.HasPrefix(lu, "data:application/vnd.apple.mpegurl") {
		return u
	}
	for _, ext := range []string{".m3u8", ".mp4", ".m4v", ".mov", ".flv", ".mp3", ".m4a", ".aac", ".wav"} {
		if strings.Contains(lu, ext) {
			return u
		}
	}
	return ""
}

func luffyCollectMedia(v any) []string {
	seen, out := map[string]bool{}, []string{}
	var walk func(any)
	walk = func(x any) {
		switch y := x.(type) {
		case string:
			if u := luffyNormalizeMediaURL(strings.Trim(y, `"'`)); u != "" && !seen[strings.ToLower(u)] {
				seen[strings.ToLower(u)] = true
				out = append(out, u)
			}
		case []any:
			for _, it := range y {
				walk(it)
			}
		case map[string]any:
			for _, it := range y {
				walk(it)
			}
		}
	}
	walk(v)
	return out
}

func luffyM3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func indexedTitle(prefix []int, title string) string {
	if title == "" {
		title = "video"
	}
	if len(prefix) == 0 {
		return title
	}
	parts := make([]string, len(prefix))
	for i, n := range prefix {
		parts[i] = strconv.Itoa(n)
	}
	return fmt.Sprintf("[%s]--%s", strings.Join(parts, "."), title)
}

func mapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func nested(v any, keys ...string) any {
	cur := v
	for _, k := range keys {
		cur = mapAny(cur)[k]
	}
	return cur
}
func boolOf(v any) bool   { b, _ := v.(bool); return b }
func numOf(v any) float64 { f, _ := strconv.ParseFloat(firstText(v), 64); return f }
func firstText(vals ...any) string {
	for _, v := range vals {
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
func mediaExt(u string) string {
	lu := strings.ToLower(u)
	if strings.Contains(lu, ".m3u8") || strings.HasPrefix(strings.TrimSpace(u), "#EXTM3U") || strings.HasPrefix(lu, "data:application/vnd.apple.mpegurl") {
		return "m3u8"
	}
	if strings.Contains(lu, ".pdx") {
		return "pdx"
	}
	if strings.Contains(lu, ".mp3") || strings.Contains(lu, ".m4a") || strings.Contains(lu, ".aac") || strings.Contains(lu, ".wav") {
		return "mp3"
	}
	if strings.Contains(lu, ".pdf") {
		return "pdf"
	}
	if strings.Contains(lu, ".ppt") {
		return "ppt"
	}
	if strings.Contains(lu, ".doc") {
		return "doc"
	}
	return "mp4"
}
