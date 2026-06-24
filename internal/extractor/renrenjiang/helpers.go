package renrenjiang

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
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
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
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
	p := strings.ToLower(strings.SplitN(strings.SplitN(u, "?", 2)[0], "#", 2)[0])
	if i := strings.LastIndex(p, "."); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return "mp4"
}
