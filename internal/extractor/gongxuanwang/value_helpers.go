package gongxuanwang

import (
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

func dataMap(root map[string]any) map[string]any        { return asMap(root["data"]) }
func mapAt(m map[string]any, key string) map[string]any { return asMap(m[key]) }
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func firstMap(ms ...map[string]any) map[string]any {
	for _, m := range ms {
		if len(m) > 0 {
			return m
		}
	}
	return map[string]any{}
}
func listAt(m map[string]any, key string) []map[string]any {
	if l, ok := m[key].([]any); ok {
		return listMaps(l)
	}
	return nil
}
func listMaps(list []any) []map[string]any {
	out := []map[string]any{}
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func collectMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(v)
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case jsonNumber:
		return t.String()
	case float64:
		if math.Trunc(t) == t {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

type jsonNumber interface{ String() string }

func intVal(v any) int {
	s := str(v)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return int(f)
}
func firstPositiveInt(vals ...any) int {
	for _, v := range vals {
		if n := intVal(v); n > 0 {
			return n
		}
	}
	return 0
}

func cookieHeader(jar http.CookieJar, origins []string) string {
	seen := map[string]bool{}
	var parts []string
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil {
			continue
		}
		for _, c := range jar.Cookies(u) {
			if c.Name == "" || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func cookieValue(cookie, key string) string {
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

func cloneHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func clonePayload(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func compactPayload(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if str(v) != "" {
			out[k] = v
		}
	}
	return out
}
func filterAccessible(in []gxCourse, only bool) []gxCourse {
	if !only {
		return in
	}
	out := []gxCourse{}
	for _, c := range in {
		if c.Accessible {
			out = append(out, c)
		}
	}
	return out
}
func isAccessibleSKUCourse(m map[string]any) bool {
	for _, k := range []string{"isBuy", "hasBuy", "buyFlag", "purchased", "playbackAuthority", "isVipFree"} {
		if intVal(m[k]) > 0 {
			return true
		}
	}
	s := strings.ToLower(firstString(m, "costState"))
	return s == "1" || s == "true" || s == "paid" || s == "buy"
}
func courseIdentityValues(c gxCourse) map[string]bool {
	m := map[string]bool{}
	for _, v := range []string{c.CourseID, c.GoodsID, c.CourseSkuID, c.StudentGoodsID, firstString(c.Course, "id", "goodsId", "courseSkuId", "courseId", "studentGoodId", "studentGoodsId")} {
		if v != "" {
			m[v] = true
		}
	}
	return m
}
func stringSetHas(set map[string]bool, v string) bool { return set[v] }
func hasAnyKey(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}
func defaultSource(current, fallback string) string {
	if current != "" {
		return current
	}
	return fallback
}
func streamFormat(raw string) string {
	f := extFormat(raw)
	if f == "" {
		return "m3u8"
	}
	return f
}
func extFormat(raw string) string {
	ext := strings.TrimPrefix(path.Ext(parsedPath(raw)), ".")
	return strings.ToLower(ext)
}
func parsedPath(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}
func quoteFileURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Path = strings.ReplaceAll(u.EscapedPath(), "%2F", "/")
	return u.String()
}
func joinInts(vals []int, sep string) string {
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, sep)
}
