package yizhiknow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var mediaExtRe = regexp.MustCompile(`(?i)\.(m3u8|mp4|m4v|mov|flv|mp3|m4a|aac|wav)(?:[?#]|$)`)

func parseJSON(body string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(fmt.Sprint(m[k])); s != "" && s != "<nil>" {
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

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func in(v string, vals ...string) bool {
	for _, x := range vals {
		if v == x {
			return true
		}
	}
	return false
}

func extractItems(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	m := asMap(v)
	for _, k := range []string{"lesson_list", "lessonList", "lesson", "lessons", "children", "list", "data"} {
		if out := extractItems(m[k]); len(out) > 0 {
			return out
		}
	}
	return nil
}

func joinIdx(idx []int) string {
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ".")
}

func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }

func normalizeMediaURL(v string) string {
	s := strings.Trim(strings.TrimSpace(v), `'"`)
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	if !strings.HasPrefix(s, "http") {
		return ""
	}
	if !mediaExtRe.MatchString(strings.ToLower(s)) {
		return ""
	}
	return s
}

func pickFormat(u string) string {
	low := strings.ToLower(u)
	if strings.Contains(low, ".m3u8") {
		return "m3u8"
	}
	if parsed, err := url.Parse(u); err == nil {
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(parsed.Path)), ".")
		if ext != "" {
			return ext
		}
	}
	return "mp4"
}

// materialFormat returns the file format for a material download URL.
func materialFormat(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return "bin"
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(parsed.Path)), ".")
	if ext != "" {
		return ext
	}
	return "bin"
}

// materialItem represents a downloadable courseware/material file.
type materialItem struct {
	URL  string
	Name string
}

// collectMaterialItems extracts downloadable material items from a lesson's
// study_material field. Matches the source _iter_material_items which walks
// the material structure extracting URL/name pairs from dicts, strings,
// and nested lists.
func collectMaterialItems(material any, defaultName string) []materialItem {
	if material == nil {
		return nil
	}
	var items []materialItem
	queue := []any{material}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item == nil {
			continue
		}
		switch v := item.(type) {
		case string:
			if v != "" {
				items = append(items, materialItem{URL: v, Name: defaultName})
			}
		case map[string]any:
			// Try known URL keys in priority order.
			u := firstNonEmpty(
				firstString(v, "url"),
				firstString(v, "file_url"),
				firstString(v, "download_url"),
				firstString(v, "material_url"),
				firstString(v, "oss_url"),
			)
			if u != "" {
				name := firstNonEmpty(
					firstString(v, "name"),
					firstString(v, "title"),
					firstString(v, "file_name"),
					defaultName,
				)
				items = append(items, materialItem{URL: u, Name: name})
			}
			// Queue nested containers for further traversal.
			for _, sub := range v {
				switch sub.(type) {
				case map[string]any, []any:
					queue = append(queue, sub)
				}
			}
		case []any:
			for _, sub := range v {
				queue = append(queue, sub)
			}
		}
	}
	return items
}
