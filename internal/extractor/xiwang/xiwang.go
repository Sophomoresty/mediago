// Package xiwang implements an extractor for xiwang.com courses.
package xiwang

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	refererURL    = "https://www.xiwang.com"
	checkLoginURL = "https://api.xue.xiwang.com/login/V1/Web/checkLogin?X-Businessline-Id=30"
	courseURL     = "https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList"
	infoURL       = "https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyPlans/planListV2"
	videoPlayURL  = "https://studentlive.bcc.xiwang.com/v1/student/classroom/playback/enter"
	livePlayURL   = "https://lecturepie.bcc.xiwang.com/v1/student/classroom/playback/enter"
	m3u8PlayURL   = "https://gslbsaturnbcc.saasw.vdyoo.com/v1/player/vodshow?appid=%s&fid=%s&bid=%s"
	pptListURL    = "https://studentlive.bcc.xiwang.com/v1/student/note/getTeacherNoteListV2?bizId=3&planId=%s"
	fileURL       = "https://i.bcc.xiwang.com/icenter/App/StudyCenter/MyPlans/getDatumListByType"
	priceURL      = "https://api.xue.xiwang.com/mall/detail/1/%s"
	appVersion    = "60901"
)

var patterns = []string{`(?:[\w-]+\.)?(?:xiwang\.com|bcc\.xiwang\.com)/`}
var idRe = regexp.MustCompile(`(?i)(?:courseId|course_id|cid|id|planId)=([0-9]+)|/(?:course|detail|mall)/(?:\d+/)?([0-9]+)`)
var loginOKRe = regexp.MustCompile(`"(?:stat|status)"\s*:\s*1`)

func init() {
	extractor.Register(&Xiwang{}, extractor.SiteInfo{Name: "Xiwang", URL: "xiwang.com", NeedAuth: true})
}

type Xiwang struct{}

func (s *Xiwang) Patterns() []string { return patterns }

type course struct{ id, title, stuCouID, typ string }
type lesson struct{ id, title, bizID string }

func (s *Xiwang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xiwang requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := headers(opts.Cookies)
	body, err := c.GetString(checkLoginURL, h)
	if err != nil {
		return nil, fmt.Errorf("xiwang checkLogin: %w", err)
	}
	if !loginOKRe.MatchString(body) {
		return nil, fmt.Errorf("xiwang checkLogin rejected cookie")
	}
	cid := firstMatch(idRe, rawURL)
	courses, err := fetchCourses(c, h)
	if err != nil {
		return nil, err
	}
	co := selectCourse(courses, cid)
	if co.id == "" {
		return nil, fmt.Errorf("xiwang course %q not found in course list", cid)
	}
	lessons, err := fetchLessons(c, h, co)
	if err != nil {
		return nil, err
	}
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}
	for _, l := range lessons {
		for _, u := range resolveLesson(c, h, co, l) {
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			entries = append(entries, media(firstNonEmpty(l.title, "plan_"+l.id), u, map[string]any{"planId": l.id, "bizId": l.bizID, "stuCouId": co.stuCouID}))
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("xiwang: no playable m3u8/mp4 URL resolved")
	}
	return &extractor.MediaInfo{Site: "xiwang", Title: firstNonEmpty(co.title, "xiwang_"+co.id), Entries: entries}, nil
}

func fetchCourses(c *util.Client, h map[string]string) ([]course, error) {
	out := []course{}
	for _, couType := range []string{"1", "2"} {
		for pos := 1; pos <= 200; pos += 8 {
			root, err := postJSON(c, courseURL, map[string]string{"systemName": "pc-win", "appVersionNumber": appVersion, "position": fmt.Sprint(pos), "subjectId": "0", "couStatus": "0", "couType": couType}, h)
			if err != nil {
				return nil, err
			}
			list := append(listUnder(root, "learningCourses"), listUnder(root, "endedCourses")...)
			if len(list) == 0 {
				break
			}
			for _, m := range list {
				out = append(out, course{id: val(m, "courseId"), title: val(m, "courseName"), stuCouID: val(m, "stuCouId"), typ: firstNonEmpty(val(m, "type"), couType)})
			}
			if len(list) < 8 {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xiwang course list is empty")
	}
	return out, nil
}

func fetchLessons(c *util.Client, h map[string]string, co course) ([]lesson, error) {
	root, err := postJSON(c, infoURL, map[string]string{"courseId": co.id, "systemName": "pc-win", "appVersionNumber": appVersion, "type": co.typ, "stuCouId": co.stuCouID}, h)
	if err != nil {
		return nil, err
	}
	out := []lesson{}
	for _, m := range listUnder(root, "list") {
		id := firstNonEmpty(val(m, "planId"), val(m, "id"))
		if id == "" {
			continue
		}
		out = append(out, lesson{id: id, title: firstNonEmpty(val(m, "planName"), val(m, "name"), val(m, "title")), bizID: firstNonEmpty(val(m, "bizId"), val(m, "biz_id"), val(m, "type"), "3")})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xiwang plan list is empty")
	}
	return out, nil
}

func resolveLesson(c *util.Client, h map[string]string, co course, l lesson) []string {
	out := []string{}
	for _, biz := range unique([]string{l.bizID, "3", "4"}) {
		api := videoPlayURL
		if biz != "3" {
			api = livePlayURL
		}
		root, err := postJSON(c, api, map[string]string{"acceptPlanVersion": "42", "bizId": biz, "planId": l.id, "stuCouId": co.stuCouID}, h)
		if err != nil {
			continue
		}
		if u := firstMediaURL(root); u != "" {
			out = append(out, u)
		}
		configs := firstMapKey(root, "configs")
		if configs == nil {
			continue
		}
		appID, liveType := val(configs, "appId"), firstNonEmpty(val(firstMapKey(root, "planInfo"), "liveTypeId"), val(configs, "liveTypeId"))
		for _, fid := range []string{val(configs, "beforeClassFileId"), val(configs, "videoFile"), val(configs, "afterClassFileId")} {
			if fid == "" || appID == "" || liveType == "" {
				continue
			}
			if strings.Contains(fid, ".m3u8") || strings.Contains(fid, ".mp4") || !strings.HasPrefix(fid, "http") {
				out = append(out, m3u8URLs(c, h, fid, appID, liveType)...)
			}
		}
	}
	return unique(out)
}

func m3u8URLs(c *util.Client, h map[string]string, fid, appID, bid string) []string {
	body, err := c.GetString(fmt.Sprintf(m3u8PlayURL, url.QueryEscape(appID), url.QueryEscape(fid), url.QueryEscape(bid)), h)
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil
	}
	out := []string{}
	for _, m := range mapsUnder(root) {
		if u := val(m, "addr"); isMediaURL(u) {
			out = append(out, normalizeURL(u))
		}
	}
	return out
}

func postJSON(c *util.Client, api string, data map[string]string, h map[string]string) (map[string]any, error) {
	body, err := c.PostForm(api, data, h)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("xiwang parse JSON: %w", err)
	}
	return root, nil
}

func headers(jar http.CookieJar) map[string]string {
	h := map[string]string{"X-Businessline-Id": "30", "referer": refererURL, "Referer": refererURL, "Accept": "application/json, text/plain, */*"}
	if ck := cookieHeader(jar); ck != "" {
		h["cookie"], h["Cookie"] = ck, ck
	}
	return h
}
func cookieHeader(jar http.CookieJar) string {
	parts := []string{}
	for _, raw := range []string{refererURL, "https://api.xue.xiwang.com", "https://i.bcc.xiwang.com", "https://studentlive.bcc.xiwang.com", "https://lecturepie.bcc.xiwang.com"} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func selectCourse(cs []course, cid string) course {
	for _, c := range cs {
		if cid == "" || c.id == cid {
			return c
		}
	}
	return course{}
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
func firstMapKey(v any, key string) map[string]any {
	for _, m := range mapsUnder(v) {
		if x, ok := m[key].(map[string]any); ok {
			return x
		}
	}
	return nil
}
func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"addr", "url", "m3u8", "m3u8Url", "playUrl", "videoUrl", "beforeClassFileId", "afterClassFileId", "videoFile"} {
			if u := normalizeURL(val(m, k)); isMediaURL(u) {
				return u
			}
		}
	}
	return ""
}
func listUnder(v any, key string) []map[string]any {
	out := []map[string]any{}
	for _, m := range mapsUnder(v) {
		if a, ok := m[key].([]any); ok {
			for _, x := range a {
				if mm, ok := x.(map[string]any); ok {
					out = append(out, mm)
				}
			}
		}
	}
	return out
}
func mapsUnder(v any) []map[string]any {
	out := []map[string]any{}
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
func val(m map[string]any, k string) string {
	if m != nil {
		if v, ok := m[k]; ok && v != nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
func isMediaURL(u string) bool { return strings.HasPrefix(u, "http") || strings.HasPrefix(u, "//") }
func normalizeURL(u string) string {
	u = strings.TrimSpace(strings.ReplaceAll(u, `\/`, "/"))
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
func unique(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
func media(title, u string, extra map[string]any) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "xiwang", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{u}, Format: formatOf(u), Headers: map[string]string{"Referer": refererURL}}}, Extra: extra}
}
func formatOf(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
