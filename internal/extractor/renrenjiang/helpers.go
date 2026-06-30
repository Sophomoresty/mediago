package renrenjiang

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func parseCourseID(raw string) (string, string) {
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "/column") || strings.Contains(lower, "cid=") {
		return first(match1(raw, `(?:[?&#](?:id|cid)=)(\d+)`), match1(raw, `/column/(\d+)`)), "column"
	}
	if strings.Contains(lower, "/course") || strings.Contains(lower, "/activity") || strings.Contains(lower, "aid=") || strings.Contains(lower, "activityid=") {
		return first(match1(raw, `(?:[?&#](?:id|aid|activityid)=)(\d+)`), match1(raw, `/(?:course|activity)/(\d+)`)), "activity"
	}
	return "", ""
}
func authFromJar(j http.CookieJar) authInfo {
	var a authInfo
	for _, host := range []string{API_HOST + "/", REFERER, "https://h5.renrenjiang.cn/", "https://www.renrenjiang.cn/"} {
		u, _ := url.Parse(host)
		for _, ck := range j.Cookies(u) {
			payload := parsePayload(ck.Value)
			a.Token = first(a.Token, pickToken(payload), tokenFromString(ck.Name, ck.Value))
			a.UserID = first(a.UserID, textAt(payload, "user_id", "userId", "id"), textAt(unwrapMap(payload["user"]), "id", "user_id"))
		}
	}
	return a
}
func parsePayload(s string) map[string]any {
	s = strings.TrimSpace(s)
	if v, err := url.QueryUnescape(s); err == nil {
		s = v
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		return m
	}
	if vals, err := url.ParseQuery(s); err == nil && len(vals) > 0 {
		out := map[string]any{}
		for k, v := range vals {
			if len(v) > 0 {
				out[k] = v[0]
			}
		}
		return out
	}
	return map[string]any{}
}
func pickToken(m map[string]any) string {
	return first(textAt(m, "token", "access_token", "accessToken", "Authorization", "Admin-Token"), findTextInAny(m, "token"), findTextInAny(m, "access_token"), findTextInAny(m, "accessToken"))
}
func tokenFromString(name, val string) string {
	if strings.Contains(strings.ToLower(name), "token") || strings.EqualFold(name, "Authorization") {
		return strings.Trim(strings.TrimSpace(val), `'"`)
	}
	if m := regexp.MustCompile(`(?i)(?:access_token|accessToken|token|Authorization)\s*[:=]\s*"?([^";,\s]+)`).FindStringSubmatch(val); len(m) > 1 {
		return m[1]
	}
	return ""
}
func headers(token string) map[string]string {
	h := map[string]string{"Referer": REFERER, "Origin": ORIGIN, "Accept": "application/json, text/plain, */*"}
	if token != "" {
		h["Authorization"] = token
	}
	return h
}
func extractItems(v any, keys ...string) []map[string]any {
	if list, ok := v.([]any); ok {
		return maps(list)
	}
	m := unwrapMap(v)
	for _, k := range keys {
		if list, ok := m[k].([]any); ok {
			return maps(list)
		}
	}
	if d, ok := m["data"].(map[string]any); ok {
		for _, k := range keys {
			if list, ok := d[k].([]any); ok {
				return maps(list)
			}
		}
	}
	return nil
}
func maps(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, v := range in {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func unwrapMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		if d, ok := m["data"].(map[string]any); ok {
			return d
		}
		return m
	}
	return map[string]any{}
}
func textAt(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "<nil>" {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
func numAt(m map[string]any, key string) float64 {
	return safeFloat(m[key])
}
func safeFloat(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0
		}
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	default:
		f, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(x)), 64)
		return f
	}
}
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "1", "true", "yes", "ok", "paid", "pay", "subscribed":
			return true
		case "0", "false", "no", "fail", "null", "":
			return false
		}
		return false
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	default:
		return false
	}
}
func findTextInAny(v any, key string) string {
	switch t := v.(type) {
	case map[string]any:
		if s := textAt(t, key); s != "" {
			return s
		}
		for _, x := range t {
			if s := findTextInAny(x, key); s != "" {
				return s
			}
		}
	case []any:
		for _, x := range t {
			if s := findTextInAny(x, key); s != "" {
				return s
			}
		}
	}
	return ""
}
func findURLInAny(v any) string {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"hls_url", "stream_url", "rtmp_url", "play_url", "playUrl", "url"} {
			if u := textAt(t, k); strings.HasPrefix(u, "http") {
				return u
			}
		}
		for _, x := range t {
			if u := findURLInAny(x); u != "" {
				return u
			}
		}
	case []any:
		for _, x := range t {
			if u := findURLInAny(x); u != "" {
				return u
			}
		}
	}
	return ""
}
func mergeExtra(base map[string]any, more map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for k, v := range more {
		base[k] = v
	}
	return base
}
func match1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return ""
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func sanitize(s string) string {
	s = html.UnescapeString(strings.TrimSpace(s))
	return regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`).ReplaceAllString(s, "_")
}
func pickFormat(u string) string {
	raw := strings.TrimSpace(u)
	low := strings.ToLower(raw)
	if strings.HasPrefix(low, "#extm3u") ||
		strings.HasPrefix(low, "data:application/vnd.apple.mpegurl") ||
		strings.HasPrefix(low, "data:application/x-mpegurl") ||
		strings.Contains(low, ".m3u8") ||
		strings.Contains(low, "mpegurl") {
		return "m3u8"
	}
	p := strings.ToLower(strings.SplitN(strings.SplitN(raw, "?", 2)[0], "#", 2)[0])
	if i := strings.LastIndex(p, "."); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return "mp4"
}

func documentEntries(docs []map[string]any, defaultName string, h map[string]string) []*extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(docs))
	for i, doc := range docs {
		rawURL := first(textAt(doc, "file_url", "fileUrl", "url", "downloadUrl", "resourceUrl"), findURLInAny(doc))
		if rawURL == "" {
			continue
		}
		docURL := normalizeDocURL(rawURL)
		if docURL == "" {
			continue
		}
		format := pickFormat(docURL)
		title := first(textAt(doc, "name", "file_name", "fileName", "title"), defaultName, fmt.Sprintf("课件_%02d", i+1))
		title = strings.TrimSuffix(sanitize(title), "."+format)
		entries = append(entries, &extractor.MediaInfo{
			Site:  "renrenjiang",
			Title: title,
			Streams: map[string]extractor.Stream{"best": {
				Quality: "best",
				URLs:    []string{docURL},
				Format:  format,
				Headers: docHeaders(h),
			}},
			Extra: map[string]any{"type": "document", "raw": doc},
		})
	}
	return entries
}

func normalizeDocURL(raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(strings.Trim(raw, `"'`)))
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return ORIGIN + raw
	}
	return raw
}

func docHeaders(h map[string]string) map[string]string {
	out := map[string]string{"Referer": REFERER, "Origin": ORIGIN}
	for k, v := range h {
		if strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "Cookie") || strings.EqualFold(k, "cookie") {
			out[k] = v
		}
	}
	return out
}

func dedupeDocuments(docs []map[string]any) []map[string]any {
	seen := map[string]bool{}
	out := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		key := first(textAt(doc, "id"), textAt(doc, "file_url", "fileUrl", "url"), textAt(doc, "name", "file_name"))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, doc)
	}
	return out
}

func dedupeEntries(in []*extractor.MediaInfo) []*extractor.MediaInfo {
	seen := map[string]bool{}
	out := make([]*extractor.MediaInfo, 0, len(in))
	for _, entry := range in {
		if entry == nil {
			continue
		}
		key := entry.Title
		for _, stream := range entry.Streams {
			if len(stream.URLs) > 0 {
				key += "|" + stream.URLs[0]
				break
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	return out
}
