package xsteach

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func courseMaps(root map[string]any, names []string) []map[string]any {
	out := []map[string]any{}
	for _, name := range names {
		if name == "body" {
			out = append(out, listFrom(root["body"])...)
		}
		out = append(out, listUnder(root, name)...)
	}
	return out
}
func normalizeCourse(item, detail map[string]any) xsCourse {
	if detail == nil {
		detail = map[string]any{}
	}
	return xsCourse{id: firstNonEmpty(val(item, "courseId"), val(item, "course_id"), val(item, "id"), val(item, "value"), val(detail, "courseId"), val(detail, "id")), title: firstNonEmpty(val(item, "name"), val(item, "label"), val(item, "courseName"), val(item, "title"), val(detail, "name"), val(detail, "courseName")), classScheduleID: firstNonEmpty(val(item, "classScheduleId"), val(item, "scheduleId"), val(item, "class_schedule_id"), val(detail, "classScheduleId")), lectureType: firstNonEmpty(val(item, "lectureType"), val(item, "lecture_type"), val(detail, "lectureType"))}
}
func mergeCourse(a, b xsCourse) xsCourse {
	a.title = firstNonEmpty(b.title, a.title)
	a.classScheduleID = firstNonEmpty(b.classScheduleID, a.classScheduleID)
	a.lectureType = firstNonEmpty(b.lectureType, a.lectureType)
	return a
}
func selectCourse(cs []xsCourse, cid string) xsCourse {
	for _, c := range cs {
		if cid == "" || c.id == cid {
			return c
		}
	}
	return xsCourse{}
}
func periodsFromBody(body any, scheduleID string, schedule map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, k := range []string{"periods", "directoryList", "periodList", "list", "items"} {
		out = append(out, flattenPeriods(valueAt(body, k))...)
	}
	if len(out) == 0 {
		out = flattenPeriods(body)
	}
	for _, p := range out {
		if scheduleID != "" {
			p["classScheduleId"] = scheduleID
		}
		if schedule != nil {
			p["_schedule_seq"] = schedule["scheduleSeq"]
			p["_schedule_begin"] = schedule["beginDate"]
		}
	}
	return uniquePeriods(out)
}
func flattenPeriods(v any) []map[string]any {
	out := []map[string]any{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, y := range t {
				walk(y)
			}
		case map[string]any:
			if looksPeriod(t) {
				out = append(out, t)
			}
			for _, k := range []string{"children", "childs", "childList", "childrens", "periods", "periodList", "lessonList", "lessons", "sectionList", "sections", "directoryList", "list", "items", "contents"} {
				walk(t[k])
			}
		}
	}
	walk(v)
	return out
}
func looksPeriod(m map[string]any) bool {
	if periodID(m) == "" {
		return false
	}
	for _, k := range []string{"periodStatus", "periodDuration", "isHasVideo", "videoUrl", "resourceUrl", "courseId", "resStatus", "lectureType", "homework", "teachCoachVideos"} {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}
func uniquePeriods(in []map[string]any) []map[string]any {
	out, seen := []map[string]any{}, map[string]bool{}
	for _, m := range in {
		key := firstNonEmpty(periodID(m), val(m, "name")+val(m, "beginDate")+firstMediaURL(m))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}
func periodID(m map[string]any) string { return firstNonEmpty(val(m, "id"), val(m, "periodId")) }
func mergeMap(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if asString(v) != "" {
			out[k] = v
		}
	}
	return out
}
func headers(jar http.CookieJar) map[string]string {
	ck := cookieHeader(jar)
	return map[string]string{"Accept": "application/json, text/plain, */*", "Origin": originURL, "Referer": refererURL, "User-Agent": defaultUserAgent, "cookie": ck, "Cookie": ck}
}
func cookieHeader(jar http.CookieJar) string {
	parts := []string{}
	for _, raw := range []string{refererURL, originURL} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}
func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"master_m3u8_url", "masterM3u8Url", "masterPlayList", "videoUrl", "playUrl", "m3u8Url", "hlsUrl", "mediaUrl", "source", "url", "addr", "fileUrl", "resourceUrl"} {
			if mm, ok := m[k].(map[string]any); ok {
				if u := normalizeURL(val(mm, "url")); u != "" {
					return u
				}
			}
			if u := normalizeURL(val(m, k)); u != "" {
				return u
			}
		}
	}
	return ""
}
func normalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "/") {
		base, _ := url.Parse(refererURL)
		ref, _ := url.Parse(s)
		return base.ResolveReference(ref).String()
	}
	if strings.HasPrefix(strings.ToLower(s), "http://") || strings.HasPrefix(strings.ToLower(s), "https://") {
		return s
	}
	return ""
}
func qcloudURL(appID, fileID string) string {
	u := strings.Replace(qcloudPlayAPI, "{}", url.PathEscape(appID), 1)
	return strings.Replace(u, "{}", url.PathEscape(fileID), 1)
}
func media(title, raw string, vi xsVideo) *extractor.MediaInfo {
	format := "mp4"
	if strings.Contains(strings.ToLower(raw), ".m3u8") {
		format = "m3u8"
	}
	return &extractor.MediaInfo{Site: "xsteach", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: []string{raw}, Format: format, Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"period_id": vi.periodID, "teach_coach_id": vi.teachCoachID}}
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
	case bool:
		if x {
			return "true"
		}
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
func codeIs(v any, want int) bool {
	s := asString(v)
	return s == strconv.Itoa(want) || (want == 0 && s == "")
}
func isFalse(v any) bool    { b, ok := v.(bool); return ok && !b }
func number(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
func unique(in []string) []string {
	out, seen := []string{}, map[string]bool{}
	for _, x := range in {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
