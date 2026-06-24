package ckjr

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
)

func parseRoute(raw string) routeInfo {
	if m := routeRe.FindStringSubmatch(raw); m != nil {
		kind := routeKindFromPath(m[2], m[3])
		cfg := routeCfg[kind]
		cfg.Company = m[1]
		q, _ := url.ParseQuery(m[4])
		cfg.ID = firstNonEmpty(q.Get(cfg.IDKey), q.Get("courseId"), q.Get("liveId"), q.Get("extId"), q.Get("datumId"), q.Get("combosId"), q.Get("testId"))
		return cfg
	}
	kind := "video"
	cfg := routeCfg[kind]
	if strings.Contains(raw, "voice") {
		cfg = routeCfg["voice"]
	} else if strings.Contains(raw, "imgText") {
		cfg = routeCfg["imgText"]
	} else if strings.Contains(raw, "live") {
		cfg = routeCfg["live"]
	} else if strings.Contains(raw, "column") {
		cfg = routeCfg["column"]
	} else if strings.Contains(raw, "datum") {
		cfg = routeCfg["datum"]
	} else if strings.Contains(raw, "package") {
		cfg = routeCfg["package"]
	}
	u, _ := url.Parse(raw)
	if u != nil {
		q := u.Query()
		cfg.ID = firstNonEmpty(q.Get(cfg.IDKey), q.Get("courseId"), q.Get("liveId"), q.Get("extId"), q.Get("datumId"), q.Get("combosId"), q.Get("prodId"), q.Get("productId"), q.Get("id"))
	}
	if cfg.ID == "" {
		cfg.ID = extractFirst(idRe, raw)
	}
	return cfg
}

func resourceParams(r routeInfo) map[string]string {
	return map[string]string{
		r.IDKey:      r.ID,
		"id":         r.ID,
		"courseId":   r.ID,
		"prodId":     r.ID,
		"productId":  r.ID,
		"prodType":   r.ProdType,
		"courseType": r.CourseTyp,
		"type":       r.CourseTyp,
		"page":       "1",
		"pageNum":    "1",
		"pageSize":   "100",
		"limit":      "100",
		"size":       "100",
	}
}

func qcloudAuth(node map[string]any) map[string]string {
	appID := textValue(node, "app_id", "appId", "appID", "appid", "app")
	fileID := textValue(node, "fileID", "fileId", "file_id", "fileid", "vid")
	psign := textValue(node, "psign", "pSign", "p_sign", "playAuth", "sign", "token")
	if appID == "" || fileID == "" || psign == "" {
		return nil
	}
	return map[string]string{"app_id": appID, "file_id": fileID, "psign": psign}
}

func directMediaURL(v any) string {
	return findMediaURL(v)
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case string:
		u := normalizeMediaText(x)
		if isMediaURL(u) {
			return u
		}
	case map[string]any:
		for _, k := range []string{"playUrl", "playurl", "videoUrl", "video_url", "m3u8Url", "m3u8_url", "audioUrl", "audio_url", "downloadUrl", "fileUrl", "file_url", "url", "path", "src"} {
			if u := findMediaURL(x[k]); u != "" {
				return u
			}
		}
		for _, vv := range x {
			if u := findMediaURL(vv); u != "" {
				return u
			}
		}
	case []any:
		for _, vv := range x {
			if u := findMediaURL(vv); u != "" {
				return u
			}
		}
	}
	return ""
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	switch x := v.(type) {
	case map[string]any:
		out = append(out, x)
		for _, vv := range x {
			out = append(out, walkMaps(vv)...)
		}
	case []any:
		for _, vv := range x {
			out = append(out, walkMaps(vv)...)
		}
	}
	return out
}

func ckjrHeaders(raw string) map[string]string {
	h := map[string]string{"Accept": "application/json, text/plain, */*", "User-Agent": ckjrUA, "Referer": raw}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		h["Origin"] = u.Scheme + "://" + u.Host
	} else {
		h["Origin"] = url0
	}
	return h
}

func routeKindFromPath(path, courseKind string) string {
	if courseKind != "" {
		return courseKind
	}
	switch {
	case strings.Contains(path, "column"):
		return "column"
	case strings.Contains(path, "datum"):
		return "datum"
	case strings.Contains(path, "package"):
		return "package"
	case strings.Contains(path, "testPaper"):
		return "testPaper"
	case strings.Contains(path, "livePersonal"):
		return "livePersonal"
	case strings.Contains(path, "live"):
		return "live"
	}
	return "video"
}

func responseHasPayload(v any) bool {
	if m, ok := v.(map[string]any); ok {
		if code, ok := m["code"]; ok && fmt.Sprint(code) != "0" && fmt.Sprint(code) != "200" {
			return findMediaURL(v) != ""
		}
	}
	return len(walkMaps(v)) > 0
}

func dedupeEntries(in []*extractor.MediaInfo) []*extractor.MediaInfo {
	seen := map[string]bool{}
	var out []*extractor.MediaInfo
	for _, e := range in {
		key := e.Title
		if s := e.Streams["best"]; len(s.URLs) > 0 {
			key = s.URLs[0]
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

func normalizeMediaText(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	s = strings.ReplaceAll(s, `\/`, "/")
	s = strings.ReplaceAll(s, `\u002F`, "/")
	s = strings.ReplaceAll(s, `\u002f`, "/")
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	if m := mediaRe.FindStringSubmatch(s); m != nil {
		s = m[0]
	}
	return strings.TrimRight(s, `"' )],;`)
}

func isMediaURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "http") && (strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".m4v") || strings.Contains(lower, ".mov") || strings.Contains(lower, ".flv") || strings.Contains(lower, ".mp3") || strings.Contains(lower, ".m4a") || strings.Contains(lower, ".aac") || strings.Contains(lower, ".wav") || strings.Contains(lower, ".pdf"))
}

func pickFormat(mediaURL string) string {
	lower := strings.ToLower(mediaURL)
	switch {
	case strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".mp3") || strings.Contains(lower, ".m4a") || strings.Contains(lower, ".aac") || strings.Contains(lower, ".wav"):
		return "audio"
	case strings.Contains(lower, ".pdf"):
		return "pdf"
	}
	return "mp4"
}

func textValue(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
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

func extractFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}
