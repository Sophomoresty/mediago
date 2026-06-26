package duanshu

import (
	"fmt"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func normalizeType(kind string) string {
	s := strings.ToLower(strings.TrimSpace(kind))
	switch s {
	case "text", "image_text", "graphic", "article":
		return "article"
	case "class", "cource", "series", "course":
		return "course"
	case "column":
		return "column"
	case "audio":
		return "audio"
	case "video":
		return "video"
	default:
		return "single"
	}
}

func collectContentItems(v any) []contentItem {
	var out []contentItem
	var walk func(any)
	walk = func(x any) {
		switch vv := x.(type) {
		case map[string]any:
			id := valueString(vv, "content_id", "contentId", "id")
			title := valueString(vv, "content_title", "title", "name")
			kind := normalizeType(firstNonEmpty(valueString(vv, "content_type", "type"), "single"))
			if id != "" && (hasAny(vv, "content_id", "contentId") || hasAny(vv, "content_type", "is_test")) {
				out = append(out, contentItem{ID: id, Title: title, Kind: kind, Test: truthy(vv["is_test"])})
			}
			for _, k := range []string{"content_list", "list", "items", "data", "response"} {
				if child, ok := vv[k]; ok {
					walk(child)
				}
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func collectClassItems(v any) []contentItem {
	var out []contentItem
	var walk func(any, string)
	walk = func(x any, prefix string) {
		switch vv := x.(type) {
		case map[string]any:
			title := firstNonEmpty(valueString(vv, "title", "name"), prefix)
			classID := valueString(vv, "class_id", "classId", "id")
			if classID != "" && (hasAny(vv, "class_id", "classId") || hasAny(vv, "course_id", "chapter_idx")) {
				out = append(out, contentItem{Class: classID, Title: title})
			}
			for _, k := range []string{"classes", "class_list", "classList", "contents", "children", "list", "data", "response"} {
				if child, ok := vv[k]; ok {
					walk(child, title)
				}
			}
		case []any:
			for _, child := range vv {
				walk(child, prefix)
			}
		}
	}
	walk(v, "")
	return out
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"url", "video_path", "video_url", "video_patch", "audio_url", "audio_path", "m3u8", "play_url", "playUrl"} {
			if s := normalizeURL(valueString(x, k)); isMediaURL(s) {
				return s
			}
		}
		for _, k := range []string{"play_data", "audio_data", "content", "data", "response"} {
			if child, ok := x[k]; ok {
				if s := findMediaURL(child); s != "" {
					return s
				}
			}
		}
		for _, child := range x {
			if s := findMediaURL(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := findMediaURL(child); s != "" {
				return s
			}
		}
	case string:
		if s := normalizeURL(x); isMediaURL(s) {
			return s
		}
	}
	return ""
}

func collectStringsByKeys(v any, keys ...string) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch vv := x.(type) {
		case map[string]any:
			for _, key := range keys {
				if s := valueString(vv, key); s != "" && !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
			for _, child := range vv {
				walk(child)
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func pickTitle(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if s := valueString(x, "title", "name", "content_title", "raw_title"); s != "" {
			return s
		}
		for _, child := range x {
			if s := pickTitle(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := pickTitle(child); s != "" {
				return s
			}
		}
	}
	return ""
}

func hasNextPage(v any, page int) bool {
	var found bool
	var walk func(any)
	walk = func(x any) {
		if found {
			return
		}
		m, ok := x.(map[string]any)
		if !ok {
			if a, ok := x.([]any); ok {
				for _, child := range a {
					walk(child)
				}
			}
			return
		}
		if p, ok := m["page"].(map[string]any); ok {
			last := intValue(p["last_page"])
			if last == 0 {
				last = intValue(p["total_pages"])
			}
			found = last > page
			return
		}
		for _, child := range m {
			walk(child)
		}
	}
	walk(v)
	return found
}

func mediaInfo(title, mediaURL string, headers map[string]string) *extractor.MediaInfo {
	format := "mp4"
	if strings.Contains(strings.ToLower(mediaURL), ".m3u8") {
		format = "m3u8"
	}
	if strings.Contains(strings.ToLower(mediaURL), ".mp3") {
		format = "mp3"
	}
	return &extractor.MediaInfo{Site: "duanshu", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers}}}
}

func valueString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func hasAny(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func normalizeURL(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	s = strings.ReplaceAll(s, `\/`, `/`)
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	return s
}

func isMediaURL(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "http") && (strings.Contains(low, ".mp4") || strings.Contains(low, ".m3u8") || strings.Contains(low, ".mp3") || strings.Contains(low, ".flv") || strings.Contains(low, ".m4a"))
}

func intValue(v any) int {
	var n int
	_, _ = fmt.Sscanf(fmt.Sprint(v), "%d", &n)
	return n
}

func truthy(v any) bool {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	return s != "" && s != "0" && s != "false" && s != "<nil>"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
