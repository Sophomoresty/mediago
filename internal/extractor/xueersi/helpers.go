package xueersi

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func parseTarget(raw string) target {
	t := target{}
	if u, err := url.Parse(raw); err == nil {
		for _, q := range []url.Values{u.Query(), parseFragment(u.Fragment)} {
			t.courseID = firstNonEmpty(t.courseID, q.Get("courseId"), q.Get("course_id"), q.Get("cid"))
			t.stuCouID = firstNonEmpty(t.stuCouID, q.Get("stuCouId"), q.Get("stu_cou_id"), q.Get("stucouid"))
			t.courseType = firstNonEmpty(t.courseType, q.Get("couType"), q.Get("courseType"), q.Get("type"))
			t.planID = firstNonEmpty(t.planID, q.Get("planId"), q.Get("plan_id"))
		}
	}
	for _, m := range targetRe.FindAllStringSubmatch(raw, -1) {
		v := ""
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				v = m[i]
				break
			}
		}
		s := strings.ToLower(m[0])
		switch {
		case strings.Contains(s, "plan"):
			t.planID = firstNonEmpty(t.planID, v)
		case strings.Contains(s, "stu"):
			t.stuCouID = firstNonEmpty(t.stuCouID, v)
		case strings.Contains(s, "type") || strings.Contains(s, "coutype"):
			t.courseType = firstNonEmpty(t.courseType, v)
		default:
			t.courseID = firstNonEmpty(t.courseID, v)
		}
	}
	return t
}
func parseFragment(s string) url.Values {
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[i+1:]
	}
	v, _ := url.ParseQuery(strings.TrimLeft(s, "#?/&"))
	return v
}
func selectCourse(cs []course, t target) course {
	for _, c := range cs {
		if t.courseID != "" && t.stuCouID != "" && c.id == t.courseID && c.stuCouID == t.stuCouID {
			return c
		}
	}
	for _, c := range cs {
		if t.stuCouID != "" && c.stuCouID == t.stuCouID {
			return c
		}
	}
	for _, c := range cs {
		if t.courseID == "" || c.id == t.courseID {
			return c
		}
	}
	return course{}
}

func baseHeaders(cookie string) map[string]string {
	return map[string]string{"User-Agent": defaultUserAgent, "referer": refererURL, "Referer": refererURL, "cookie": cookie, "Cookie": cookie}
}
func courseHeaders(cookie string, isJSON bool, appVersion, appVersionNumber string) map[string]string {
	ct := "application/x-www-form-urlencoded"
	ua := "XueErSi Windows/9.98.0 (Windows 10; Student) (Curl)"
	if isJSON {
		ct = "application/json"
		ua = "XueErSi Windows/10.16.02 (Windows 10; Student) (Curl)"
	}
	h := baseHeaders(cookie)
	for k, v := range map[string]string{"Host": "i.xueersi.com", "Accept": "*/*", "Content-Type": ct, "User-Agent": ua, "X-Businessline-Id": "10", "appVersion": appVersion, "appVersionNumber": appVersionNumber, "systemName": "pc-win", "systemVersion": "Windows 10"} {
		h[k] = v
	}
	return h
}
func planHeaders(cookie string) map[string]string {
	return courseHeaders(cookie, false, "9.98.0", "99800")
}
func playbackHeaders(cookie, planID, stuCouID string) map[string]string {
	h := baseHeaders(cookie)
	for k, v := range map[string]string{"Accept": "*/*", "Content-Type": "application/json", "Host": "studentlive.xueersi.com", "User-Agent": "XueErSi Windows/10.15.01 (Windows 10; Student) (Curl)", "X-Businessline-Id": "10", "appVersionNumber": "101501", "doubleLivePlanId": planID, "doubleLiveStuId": stuCouID, "planId": planID, "stuCouId": stuCouID, "systemName": "pc-win", "startTime": strconv.FormatInt(time.Now().UnixMilli(), 10)} {
		if v != "" {
			h[k] = v
		}
	}
	return h
}
func cookieHeader(jar http.CookieJar) string {
	parts := []string{}
	for _, raw := range []string{refererURL, "https://api.xueersi.com", "https://i.xueersi.com", "http://i.xueersi.com", "http://studentlive.xueersi.com", "https://studentlive.xueersi.com"} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func media(title, raw string, co course, p plan) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "xueersi", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: []string{raw}, Format: "m3u8", Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"courseId": co.id, "stuCouId": co.stuCouID, "planId": p.id}}
}

func candidates(xs ...string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"addr", "url", "m3u8", "m3u8Url", "playUrl", "videoUrl", "fileUrl", "mediaUrl"} {
			if u := val(m, k); strings.HasPrefix(u, "http") {
				return u
			}
		}
	}
	return ""
}
func mapAt(v any, key string) map[string]any {
	if m, ok := valueAt(v, key).(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func listUnder(v any, key string) []map[string]any { return listFrom(valueAt(v, key)) }
func listFrom(v any) []map[string]any {
	out := []map[string]any{}
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}
func valueAt(v any, key string) any {
	for _, m := range mapsUnder(v) {
		if x, ok := m[key]; ok {
			return x
		}
	}
	return nil
}
func mapsUnder(v any) []map[string]any {
	out := []map[string]any{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, y := range t {
				walk(y)
			}
		case []any:
			for _, y := range t {
				walk(y)
			}
		}
	}
	walk(v)
	return out
}
func val(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return asString(m[key])
}
func asString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		if math.Trunc(x) == x {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}
func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}
