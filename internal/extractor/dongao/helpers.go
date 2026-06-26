package dongao

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

var (
	directMediaRe = regexp.MustCompile(`(?i)https?:\\?/\\?/[^"'<>\s]+\.(?:m3u8|mp4|flv|mov|m4v|mp3|m4a)(?:\?[^"'<>\s]*)?`)
	kvMediaRe     = regexp.MustCompile(`(?is)(?:source|url|path|playUrl|playbackUrl|video_url)\s*[:=]\s*["']([^"']+)["']`)
)

func findMediaInText(text string) string {
	for _, m := range directMediaRe.FindAllString(text, -1) {
		if s := normalizeURL(m); isMediaURL(s) {
			return s
		}
	}
	for _, m := range kvMediaRe.FindAllStringSubmatch(text, -1) {
		if s := normalizeURL(m[1]); isMediaURL(s) {
			return s
		}
	}
	if payload := parseJSONText(text); payload != nil {
		if s := findMediaURL(payload); s != "" {
			return s
		}
	}
	return ""
}

func collectLectureNodes(v any, fallbackTitle string) []lectureNode {
	seen := map[string]bool{}
	var out []lectureNode
	var walk func(any, string)
	walk = func(x any, title string) {
		switch vv := x.(type) {
		case map[string]any:
			nextTitle := firstNonEmpty(valueString(vv, "lectureName", "lectureTitle", "title", "name", "videoName", "courseName"), title, fallbackTitle)
			id := valueString(vv, "lectureId", "lectureID", "listenLectureId", "liveNumberId", "liveLectureId", "id")
			if id != "" && !seen[id] && (hasAny(vv, "lectureId", "lectureID", "listenLectureId", "liveNumberId", "liveLectureId") || strings.Contains(strings.ToLower(nextTitle), "讲")) {
				seen[id] = true
				out = append(out, lectureNode{ID: id, Title: nextTitle})
			}
			for _, child := range vv {
				walk(child, nextTitle)
			}
		case []any:
			for _, child := range vv {
				walk(child, title)
			}
		}
	}
	walk(v, fallbackTitle)
	return out
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"source", "mainSource", "videoSource", "url", "path", "playUrl", "playbackUrl", "video_url", "m3u8"} {
			if s := normalizeURL(valueString(x, k)); isMediaURL(s) {
				return s
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

func pickTitle(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if s := valueString(x, "courseName", "lectureName", "lectureTitle", "name", "title", "videoName"); s != "" {
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

func extractJSONObjects(text string) []string {
	var out []string
	for _, marker := range []string{"courseCatalog", "liveAndCourseMap", "lectureList", "listenParam"} {
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		start := strings.LastIndex(text[:idx], "{")
		if start < 0 {
			continue
		}
		if obj := balancedJSON(text[start:]); obj != "" {
			out = append(out, obj)
		}
	}
	return out
}

func balancedJSON(s string) string {
	depth := 0
	inStr := byte(0)
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == inStr {
				inStr = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inStr = ch
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return ""
}

func mediaInfo(title, mediaURL string, headers map[string]string) *extractor.MediaInfo {
	format := "mp4"
	if strings.Contains(strings.ToLower(mediaURL), ".m3u8") {
		format = "m3u8"
	}
	return &extractor.MediaInfo{Site: "dongao", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers}}}
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
	s = strings.ReplaceAll(s, `\\/`, `/`)
	s = strings.ReplaceAll(s, `\/`, `/`)
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "/") {
		return origin + s
	}
	return s
}

func isMediaURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http") && (strings.Contains(low, ".m3u8") || strings.Contains(low, ".mp4") || strings.Contains(low, ".flv") || strings.Contains(low, ".mov") || strings.Contains(low, ".m4v") || strings.Contains(low, ".mp3") || strings.Contains(low, ".m4a"))
}

func cloneHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func appendQuery(raw string, query url.Values) string {
	if len(query) == 0 {
		return raw
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + query.Encode()
}
