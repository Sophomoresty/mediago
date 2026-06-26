package gaotu

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func parseIDs(raw string) ids {
	out := ids{Role: "3"}
	if u, err := url.Parse(raw); err == nil {
		qv := u.Query()
		out.Clazz = firstNonEmpty(qv.Get("clazzNumber"), qv.Get("clazzId"), qv.Get("courseId"), qv.Get("productSpuNumber"), qv.Get("cid"))
		out.Live = firstNonEmpty(qv.Get("clazzLessonNumber"), qv.Get("liveId"), qv.Get("lessonId"), qv.Get("videoId"), qv.Get("vid"))
		out.Room = firstNonEmpty(qv.Get("room_id"), qv.Get("roomId"))
		out.SID = firstNonEmpty(qv.Get("sid"), qv.Get("sessionId"))
		out.Role = firstNonEmpty(qv.Get("roleType"), qv.Get("user_role"), "3")
	}
	out.Clazz = firstNonEmpty(out.Clazz, rx(clazzRe, raw))
	out.Live = firstNonEmpty(out.Live, rx(liveRe, raw))
	out.Room = firstNonEmpty(out.Room, rx(roomRe, raw))
	return out
}

func rawPlaybackURL(id ids) string {
	return fmt.Sprintf("https://api.wenzaizhibo.com/web/playback/getPlaybackInfoV4?room_id=%s&user_role=%s", url.QueryEscape(id.Room), url.QueryEscape(firstNonEmpty(id.Role, "3")))
}

func queryValues(raw string) url.Values {
	if u, err := url.Parse(raw); err == nil {
		if u.RawQuery != "" {
			return u.Query()
		}
	}
	if idx := strings.Index(raw, "?"); idx >= 0 {
		v, _ := url.ParseQuery(raw[idx+1:])
		return v
	}
	v, _ := url.ParseQuery(raw)
	return v
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"url", "enc_url", "playUrl", "play_url", "pcUrl", "fileUrl", "m3u8"} {
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

func mediaInfo(title, mediaURL string, headers map[string]string) *extractor.MediaInfo {
	format := "mp4"
	if strings.Contains(strings.ToLower(mediaURL), ".m3u8") {
		format = "m3u8"
	}
	return &extractor.MediaInfo{Site: "gaotu", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers}}}
}

func collectStrings(v any, key string) []string {
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch vv := x.(type) {
		case map[string]any:
			if s := valueString(vv, key); s != "" {
				out = append(out, s)
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
		if s := valueString(x, "cardTitle", "clazzName", "clazzLessonName", "courseName", "name", "title"); s != "" {
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

func endpointsFor(raw string) gaotuEndpoints {
	low := strings.ToLower(raw)
	if strings.Contains(low, "gaotu100.com") {
		return gaotuEndpoints{
			referer:         "https://gaotu100.com",
			apiHost:         "api.gaotu100.com",
			interactiveHost: "interactive.gaotu100.com",
			pClient:         "2",
		}
	}
	if strings.Contains(low, "gtgz.cn") {
		return gaotuEndpoints{
			referer:         "https://www.gtgz.cn",
			apiHost:         "api.gtgz.cn",
			interactiveHost: "interactive.gtgz.cn",
			pClient:         "8",
		}
	}
	if strings.Contains(low, "naiyouxuexi.com") {
		return gaotuEndpoints{
			referer:         "https://www.naiyouxuexi.com",
			apiHost:         "api.naiyouxuexi.com",
			interactiveHost: "interactive.naiyouxuexi.com",
			pClient:         "18",
		}
	}
	return gaotuEndpoints{
		referer:         "https://www.gaotu.cn",
		apiHost:         "api.gaotu.cn",
		interactiveHost: "interactive.gaotu.cn",
		pClient:         "1",
	}
}

func q(s string) string { return url.QueryEscape(s) }

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

func rx(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
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
	return strings.HasPrefix(low, "http") && (strings.Contains(low, ".mp4") || strings.Contains(low, ".m3u8") || strings.Contains(low, ".flv") || strings.Contains(low, ".mp3"))
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
