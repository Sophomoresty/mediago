package feishu

import (
	"encoding/json"
	"html"
	"net/http"
	neturl "net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/util"
)

func extractFeishuDocTitle(body, token string) string {
	decoded := decodeFeishuEscapes(body)
	if token != "" {
		patterns := []*regexp.Regexp{
			regexp.MustCompile(`(?is)"title"\s*:\s*"([^"]+?)"[\s\S]{0,3000}?"token"\s*:\s*"` + regexp.QuoteMeta(token) + `"`),
			regexp.MustCompile(`(?is)"token"\s*:\s*"` + regexp.QuoteMeta(token) + `"[\s\S]{0,3000}?"title"\s*:\s*"([^"]+?)"`),
		}
		for _, re := range patterns {
			if m := re.FindStringSubmatch(decoded); len(m) > 1 {
				return decodeTextValue(m[1])
			}
		}
	}
	if m := titleAnyRe.FindStringSubmatch(decoded); len(m) > 1 {
		return decodeTextValue(m[1])
	}
	if m := htmlTitleRe.FindStringSubmatch(decoded); len(m) > 1 {
		return cleanText(m[1])
	}
	return ""
}

func feishuHeaders(rawURL string, jar http.CookieJar) map[string]string {
	h := map[string]string{
		"Referer": feishuReferer,
		"Accept":  "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	}
	if csrf := csrfToken(rawURL, jar); csrf != "" {
		h["x-csrftoken"] = csrf
	}
	if cookie := cookieHeader(rawURL, jar); cookie != "" {
		h["Cookie"] = cookie
	}
	return h
}

func csrfToken(rawURL string, jar http.CookieJar) string {
	for _, u := range feishuCookieURLs(rawURL) {
		for _, c := range jar.Cookies(u) {
			if c.Name == "_csrf_token" {
				return c.Value
			}
		}
	}
	return ""
}

func cookieHeader(rawURL string, jar http.CookieJar) string {
	seen := map[string]bool{}
	var parts []string
	for _, u := range feishuCookieURLs(rawURL) {
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

func feishuCookieURLs(rawURL string) []*neturl.URL {
	urls := []*neturl.URL{{Scheme: "https", Host: "www.feishu.cn", Path: "/"}, {Scheme: "https", Host: "internal-api-drive-stream.feishu.cn", Path: "/"}}
	if parsed, err := neturl.Parse(rawURL); err == nil && parsed.Host != "" {
		urls = append(urls, &neturl.URL{Scheme: firstNonEmpty(parsed.Scheme, "https"), Host: parsed.Host, Path: "/"})
	}
	return urls
}

func feishuOrigin(rawURL string) string {
	if u, err := neturl.Parse(rawURL); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return feishuReferer
}

func feishuSplitFilename(name, defaultFmt string) (string, string) {
	name = cleanName(decodeTextValue(name))
	defaultFmt = strings.TrimPrefix(strings.ToLower(defaultFmt), ".")
	if name == "" {
		return "untitled", defaultFmt
	}
	if idx := strings.LastIndex(name, "."); idx > 0 && idx < len(name)-1 {
		base := cleanName(name[:idx])
		ext := strings.ToLower(strings.TrimPrefix(name[idx+1:], "."))
		if knownFeishuFormats[ext] {
			return firstNonEmpty(base, name), ext
		}
	}
	return name, defaultFmt
}

func feishuFormatFromMime(mimeType, kind, source string) string {
	hint := strings.ToLower(strings.Join([]string{mimeType, kind, source}, " "))
	if strings.Contains(hint, "mpegurl") || strings.Contains(hint, "m3u8") {
		return "m3u8"
	}
	mapping := []struct{ needle, fmt string }{
		{"audio/mpeg", "mp3"}, {"audio/mp3", "mp3"}, {"audio/mp4", "m4a"}, {"audio/x-m4a", "m4a"},
		{"audio/aac", "aac"}, {"audio/wav", "wav"}, {"audio/x-wav", "wav"}, {"audio/flac", "flac"}, {"audio/ogg", "ogg"},
		{"video/mp4", "mp4"}, {"video/quicktime", "mov"}, {"video/x-msvideo", "avi"}, {"video/x-flv", "flv"}, {"video/webm", "webm"},
		{"application/pdf", "pdf"}, {"application/zip", "zip"}, {"application/x-rar", "rar"}, {"application/vnd.ms-powerpoint", "ppt"},
		{"application/vnd.openxmlformats-officedocument.presentationml.presentation", "pptx"},
		{"application/vnd.ms-excel", "xls"}, {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"},
		{"application/msword", "doc"}, {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docx"},
	}
	for _, m := range mapping {
		if strings.Contains(hint, m.needle) {
			return m.fmt
		}
	}
	if m := regexp.MustCompile(`(?i)\.(mp4|m4v|mov|flv|webm|avi|mp3|m4a|aac|wav|flac|ogg|pdf|docx?|pptx?|xlsx?|zip|rar|7z|html)(?:[?#&]|$)`).FindStringSubmatch(hint); len(m) > 1 {
		return strings.ToLower(m[1])
	}
	switch feishuMediaKindFromHint(kind, mimeType, source) {
	case "audio":
		return "mp3"
	case "video":
		return "mp4"
	}
	return ""
}

func feishuMediaKindFromHint(values ...string) string {
	hint := strings.ToLower(strings.Join(values, " "))
	for _, needle := range []string{"audio/", "audio_", "audio-", ".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg"} {
		if strings.Contains(hint, needle) {
			return "audio"
		}
	}
	for _, needle := range []string{"video/", "video_", "video-", ".mp4", ".m4v", ".mov", ".flv", ".webm", ".avi", ".m3u8"} {
		if strings.Contains(hint, needle) {
			return "video"
		}
	}
	for _, needle := range []string{"application/pdf", ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".zip", ".rar", ".7z"} {
		if strings.Contains(hint, needle) {
			return "file"
		}
	}
	return ""
}

func feishuKindFromFormat(format string) string {
	switch strings.TrimPrefix(strings.ToLower(format), ".") {
	case "mp3", "m4a", "aac", "wav", "flac", "ogg":
		return "audio"
	case "mp4", "m3u8", "m4v", "mov", "flv", "webm", "avi":
		return "video"
	case "pdf", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "zip", "rar", "7z", "html":
		return "file"
	}
	return ""
}

func extractJSONField(fragment string, fieldNames []string) string {
	for _, name := range fieldNames {
		key := regexp.QuoteMeta(name)
		patterns := []*regexp.Regexp{
			regexp.MustCompile(`(?is)"` + key + `"\s*:\s*"((?:\\.|[^"])*)"`),
			regexp.MustCompile(`(?is)"` + key + `"\s*:\s*(\d+(?:\.\d+)?)`),
			regexp.MustCompile(`(?is)` + key + `\s*=\s*["']([^"']+)["']`),
		}
		for _, re := range patterns {
			if m := re.FindStringSubmatch(fragment); len(m) > 1 {
				return m[1]
			}
		}
	}
	return ""
}

func jsonFindFirst(node any, keys ...string) string {
	keySet := map[string]bool{}
	for _, k := range keys {
		keySet[k] = true
	}
	var walk func(any, int) string
	walk = func(v any, depth int) string {
		if depth > 8 {
			return ""
		}
		switch x := v.(type) {
		case map[string]any:
			for _, k := range keys {
				if val, ok := x[k]; ok {
					if s := jsonScalarString(val); s != "" {
						return s
					}
				}
			}
			for k, val := range x {
				if keySet[k] {
					continue
				}
				if s := walk(val, depth+1); s != "" {
					return s
				}
			}
		case []any:
			for _, val := range x {
				if s := walk(val, depth+1); s != "" {
					return s
				}
			}
		}
		return ""
	}
	return walk(node, 0)
}

func jsonScalarString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func jsonFragmentAround(text string, index, leftLimit, rightLimit int) string {
	if index < 0 {
		index = 0
	}
	if index > len(text) {
		index = len(text)
	}
	start := index - leftLimit
	if start < 0 {
		start = 0
	}
	end := index + rightLimit
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func decodeTextValue(value string) string {
	value = strings.TrimSpace(html.UnescapeString(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, `\u`) || strings.Contains(value, `\x`) || strings.Contains(value, `\/`) {
		value = decodeFeishuEscapes(value)
	}
	return strings.TrimSpace(html.UnescapeString(value))
}

func decodeFeishuEscapes(s string) string {
	if s == "" {
		return ""
	}
	decoded, err := unicodeUnescape(s)
	if err != nil {
		decoded = s
	}
	decoded = strings.ReplaceAll(decoded, `\/`, `/`)
	decoded = strings.ReplaceAll(decoded, `\u002F`, `/`)
	decoded = strings.ReplaceAll(decoded, `\u002f`, `/`)
	decoded = strings.ReplaceAll(decoded, `\u0022`, `"`)
	return decoded
}

func normalizeFeishuURL(raw string) string {
	raw = strings.TrimSpace(decodeTextValue(raw))
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

func urlPath(raw string) string {
	if u, err := neturl.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}

func cleanName(name string) string {
	return util.SanitizeFilename(strings.Trim(name, ". "))
}

func cleanText(s string) string {
	s = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func extractFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	for _, v := range m[1:] {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cloneHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}

func dedupeFeishuItems(items []feishuItem) []feishuItem {
	seen := map[string]bool{}
	out := make([]feishuItem, 0, len(items))
	for _, item := range items {
		if item.URL != "" {
			item.URL = normalizeFeishuURL(item.URL)
		}
		if item.Token == "" && item.URL == "" {
			continue
		}
		if item.Name == "" {
			item.Name = firstNonEmpty(item.Token, path.Base(urlPath(item.URL)), "feishu_asset")
		}
		item.Name = cleanName(item.Name)
		item.Fmt = strings.TrimPrefix(strings.ToLower(item.Fmt), ".")
		if item.Fmt == "" {
			item.Fmt = feishuFormatFromMime(item.Mime, item.Kind, firstNonEmpty(item.Name, item.URL))
		}
		if item.Kind == "" {
			item.Kind = feishuKindFromFormat(item.Fmt)
		}
		key := item.Token + "|" + item.URL + "|" + item.Name + "|" + item.Fmt
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

// unicodeUnescape decodes Python-style `\uXXXX` escapes since the Python source
// captures URLs inside double-encoded JSON literals.
func unicodeUnescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+5 < len(s) && s[i] == '\\' && s[i+1] == 'u' {
			n, err := strconv.ParseUint(s[i+2:i+6], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteRune(rune(n))
			i += 6
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}
