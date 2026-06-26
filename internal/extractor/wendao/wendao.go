// Package wendao implements an extractor for wendao101.com courses.
package wendao

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	pcReferer            = "https://pc.wendao101.com/"
	pcOrigin             = "https://pc.wendao101.com"
	wapReferer           = "https://wap.wendao101.com/"
	wapOrigin            = "https://wap.wendao101.com"
	loginURL             = "https://wap.wendao101.com/#/pages_mine/myCourse/myCourse"
	apiHost              = "https://pc.wendao101.com/prod-api"
	wapAPIHost           = "https://wap.wendao101.com"
	appNameType          = 2
	defaultOrderPlatform = 0
	wapOrderPlatform     = 5
)

var patterns = []string{`(?:[\w-]+\.)?wendao101\.com/`}

func init() {
	extractor.Register(&Wendao{}, extractor.SiteInfo{Name: "Wendao", URL: "wendao101.com", NeedAuth: true})
}

type Wendao struct{}

func (s *Wendao) Patterns() []string { return patterns }

type wdSession struct{ token, openID string }
type wdCourse struct{ id, title string }
type wdLesson struct {
	title, id, url string
	typ            int
}

var cidRe = regexp.MustCompile(`(?i)(?:[?&#]|^)(?:id|courseId|course_id)=(\d+)|/(?:course|detail)/(\d+)`)

func (s *Wendao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wendao requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess := wdSession{token: tokenFromJar(opts.Cookies), openID: openIDFromJar(opts.Cookies)}
	if sess.token == "" {
		return nil, fmt.Errorf("wendao requires token/Admin-Token cookie or localStorage token")
	}
	if sess.openID == "" {
		sess.openID = sess.token
	}
	courseID := firstGroup(cidRe, rawURL)
	if courseID == "" {
		course, err := firstCourse(c, sess)
		if err != nil {
			return nil, err
		}
		courseID = course.id
	}
	detail, err := loadDetail(c, sess, courseID)
	if err != nil {
		return nil, err
	}
	lessons := lessonsFromDetail(detail)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("wendao: course detail has no downloadable lesson URLs")
	}
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}
	for _, les := range lessons {
		u := normalizeURL(les.url)
		if u == "" || seen[u] || !isMediaURL(u) {
			continue
		}
		seen[u] = true
		entries = append(entries, &extractor.MediaInfo{Site: "wendao", Title: firstNonEmpty(les.title, "lesson_"+les.id), Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{u}, Format: mediaFormat(u), Headers: headers(sess, false)}}})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wendao: no media lesson URL resolved")
	}
	return &extractor.MediaInfo{Site: "wendao", Title: detailTitle(detail, courseID), Entries: entries}, nil
}

func firstCourse(c *util.Client, sess wdSession) (wdCourse, error) {
	body := map[string]any{"appNameType": appNameType, "pageSize": 20, "pageNum": 1, "orderPlatform": wapOrderPlatform, "openId": sess.openID}
	data, err := requestData(c, sess, wapAPIHost, "/wap/home_page/course/purchased", body, true)
	if err != nil || data == nil {
		body["orderPlatform"] = defaultOrderPlatform
		data, err = requestData(c, sess, apiHost, "/home_page/course/purchased", body, false)
	}
	if err != nil {
		return wdCourse{}, err
	}
	for _, m := range mapsUnder(data) {
		id := firstNonEmpty(val(m, "courseId"), val(m, "course_id"), val(m, "id"))
		title := firstNonEmpty(val(m, "title"), val(m, "courseTitle"), val(m, "courseUploadTitle"), val(m, "name"))
		if id != "" {
			return wdCourse{id: id, title: title}, nil
		}
	}
	return wdCourse{}, fmt.Errorf("wendao: purchased course list is empty")
}

func loadDetail(c *util.Client, sess wdSession, courseID string) (map[string]any, error) {
	body := map[string]any{"needReferer": 1, "dataId": "", "platform": wapOrderPlatform, "appNameType": appNameType, "tempSeeSecret": "", "openId": sess.openID, "courseId": courseID}
	data, err := requestData(c, sess, wapAPIHost, "/wap/course/detail", body, true)
	if err != nil || len(mapsUnder(data)) == 0 {
		body["platform"] = defaultOrderPlatform
		data, err = requestData(c, sess, apiHost, "/course_detail/detail", body, false)
	}
	if err != nil {
		return nil, err
	}
	if m, ok := data.(map[string]any); ok {
		return m, nil
	}
	return nil, fmt.Errorf("wendao: detail response is not object")
}

func requestData(c *util.Client, sess wdSession, host, path string, body map[string]any, wap bool) (any, error) {
	root, err := requestJSON(c, sess, host, path, body, wap)
	if err != nil {
		return nil, err
	}
	code := fmt.Sprint(root["code"])
	if code != "0" && code != "200" && code != "<nil>" && code != "" {
		return nil, fmt.Errorf("wendao API code=%s", code)
	}
	if d, ok := root["data"]; ok {
		return d, nil
	}
	return root, nil
}
func requestJSON(c *util.Client, sess wdSession, host, path string, body map[string]any, wap bool) (map[string]any, error) {
	payload, _ := json.Marshal(body)
	apiURL := strings.TrimRight(host, "/") + "/" + strings.TrimLeft(path, "/")
	resp, err := c.Post(apiURL, bytes.NewReader(payload), headers(sess, wap))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("wendao parse JSON: %w", err)
	}
	return root, nil
}

func lessonsFromDetail(detail map[string]any) []wdLesson {
	lessons := []wdLesson{}
	for _, m := range mapsUnder(detail) {
		u := firstNonEmpty(val(m, "courseDirectoryUrl"), val(m, "studyFileUrl"), val(m, "videoUrl"), val(m, "audioUrl"), val(m, "fileUrl"), val(m, "url"))
		if u == "" {
			continue
		}
		lessons = append(lessons, wdLesson{title: firstNonEmpty(val(m, "directoryName"), val(m, "studyFileName"), val(m, "fileName"), val(m, "name"), val(m, "title")), id: firstNonEmpty(val(m, "id"), val(m, "courseDirectoryId"), val(m, "directoryId")), url: u, typ: toInt(m["directoryType"])})
	}
	return lessons
}
func detailTitle(detail map[string]any, cid string) string {
	for _, m := range mapsUnder(detail) {
		if t := firstNonEmpty(val(m, "title"), val(m, "courseTitle"), val(m, "courseUploadTitle"), val(m, "courseName")); t != "" {
			return t
		}
	}
	return "wendao_course_" + cid
}
func headers(sess wdSession, wap bool) map[string]string {
	h := map[string]string{"Content-Type": "application/json;charset=UTF-8", "Accept": "application/json, text/plain, */*"}
	if wap {
		h["Origin"], h["Referer"], h["token"] = wapOrigin, wapReferer, sess.openID
	} else {
		h["Origin"], h["Referer"], h["Authorization"], h["token"] = pcOrigin, pcReferer, "Bearer "+sess.token, sess.token
	}
	return h
}
func tokenFromJar(jar http.CookieJar) string {
	return cookieValue(jar, []string{"token", "Admin-Token", "adminToken", "Authorization", "authorization", "accessToken", "access_token", "Access-Token"})
}
func openIDFromJar(jar http.CookieJar) string {
	return cookieValue(jar, []string{"openId", "openid", "OpenId"})
}
func cookieValue(jar http.CookieJar, names []string) string {
	for _, raw := range []string{wapOrigin, pcOrigin} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				for _, n := range names {
					if strings.EqualFold(c.Name, n) && c.Value != "" {
						return c.Value
					}
				}
			}
		}
	}
	return ""
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
func val(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}
func firstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) == 0 {
		return ""
	}
	for _, v := range m[1:] {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return wapAPIHost + "/" + strings.TrimLeft(raw, "/")
}
func isMediaURL(u string) bool {
	l := strings.ToLower(u)
	return strings.Contains(l, ".mp4") || strings.Contains(l, ".m3u8") || strings.Contains(l, ".mp3") || strings.Contains(l, ".m4a") || strings.Contains(l, ".aac") || strings.Contains(l, ".wav")
}
func mediaFormat(u string) string {
	l := strings.ToLower(u)
	for _, ext := range []string{"m3u8", "mp3", "m4a", "aac", "wav"} {
		if strings.Contains(l, "."+ext) {
			return ext
		}
	}
	return "mp4"
}
func toInt(v any) int {
	var n int
	fmt.Sscanf(fmt.Sprint(v), "%d", &n)
	return n
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
