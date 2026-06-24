package hqwx

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func cloneHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
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

func parseCookieHeader(cookie string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func codeOK(code *int) bool {
	return code != nil && *code == 0
}

func jsonSuccess(success bool, code *int, status responseStatus) bool {
	return success || codeOK(code) || codeOK(status.Code)
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case json.Number:
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
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func intString(v any) string {
	s := str(v)
	if s == "" || s == "0" {
		return ""
	}
	return s
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func listMaps(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func nowMillis() string { return strconv.FormatInt(time.Now().UnixMilli(), 10) }

var unsafeNameRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
	s = unsafeNameRe.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func mediaFormat(raw string) string {
	lower := strings.ToLower(raw)
	if strings.Contains(lower, ".m3u8") {
		return "m3u8"
	}
	u, err := url.Parse(raw)
	path := raw
	if err == nil {
		path = u.Path
	}
	if i := strings.LastIndex(path, "."); i >= 0 && i+1 < len(path) {
		ext := strings.ToLower(path[i+1:])
		switch ext {
		case "mp4", "flv", "pdf", "ppt", "pptx", "doc", "docx", "xls", "xlsx", "zip", "rar":
			return ext
		}
	}
	return "mp4"
}

func chooseList(v any) []map[string]any {
	if out := listMaps(v); len(out) > 0 {
		return out
	}
	m := asMap(v)
	for _, k := range []string{"list", "data", "result", "records", "rows", "children", "lessonList", "lessons", "productList"} {
		if out := listMaps(m[k]); len(out) > 0 {
			return out
		}
	}
	return nil
}

func firstMap(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		if child := asMap(m[k]); len(child) > 0 {
			return child
		}
	}
	return map[string]any{}
}

func splitIDs(raw string) []string {
	fields := regexp.MustCompile(`[,，\s]+`).Split(raw, -1)
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}
