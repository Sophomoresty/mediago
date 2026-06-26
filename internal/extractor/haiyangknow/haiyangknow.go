// Package haiyangknow implements source-aligned Haiyangknow course extraction.
package haiyangknow

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
	referer        = "https://user.haiyangknow.com/"
	origin         = "https://user.haiyangknow.com"
	api_host       = "https://user.haiyangknow.com/prod-api"
	aliyun_vod_url = "https://vod.{}.aliyuncs.com/?{}"
	aliyun_mts_url = "https://mts.{}.aliyuncs.com/?"
)

var patterns = []string{`\s*((?P<hy_name>haiyangknow|海洋知道)|(?P<hy_user>https?://user\.haiyangknow\.com(?:[/?#].*)?)|(?P<hy_host>https?://(?:[\w-]+\.)*haiyangknow\.com(?:[/?#].*)?)|(#小程序://海洋知道))`}

func init() {
	extractor.Register(&Haiyangknow{}, extractor.SiteInfo{Name: "Haiyangknow", URL: "haiyangknow.com", NeedAuth: true})
}

type Haiyangknow struct{}

func (s *Haiyangknow) Patterns() []string { return patterns }

type hyCtx struct {
	c            *util.Client
	headers      map[string]string
	cookie       string
	token        string
	quality      string
	loginType    string
	userInfo     map[string]any
	cid          string
	title        string
	platformType string
	apiCourseID  string
	selected     hyCourse
	courseList   []hyCourse
	playCache    map[string]map[string]any
	licenseCache map[string][]byte
	lessonSerial int
}

type hyCourse struct {
	ID, DraftID, Title, PlatformType, RawPlatformType string
	Course                                            map[string]any
	Purchased                                         bool
	Price                                             float64
}

type hyLesson struct {
	ID, Title, VideoName, MaterialName, MaterialPrefix string
	DocumentURL, Content                               string
	Raw                                                map[string]any
	Duration, MediaType                                any
}

type hySource struct {
	Name, URL, Format, Kind, HTML string
	Size                          int64
	NeedMerge                     bool
	Extra                         map[string]any
}

func (s *Haiyangknow) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("haiyangknow requires login cookies")
	}
	x, err := newCtx(opts.Cookies, opts.Quality)
	if err != nil {
		return nil, err
	}
	if err := x.prepare(rawURL); err != nil {
		return nil, err
	}
	sources, err := x.loadSources()
	if err != nil {
		return nil, err
	}
	return x.mediaFromSources(sources)
}

func newCtx(jar http.CookieJar, quality string) (*hyCtx, error) {
	cookie := cookieHeader(jar, []string{referer, api_host + "/", origin + "/"})
	token := extractToken(cookie)
	if token == "" {
		return nil, fmt.Errorf("haiyangknow: missing Admin-Token/token cookie")
	}
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Content-Type":  "application/json;charset=UTF-8",
		"Accept":        "application/json, text/plain, */*",
		"Origin":        origin,
		"Referer":       referer,
		"cookie":        cookie,
		"Cookie":        cookie,
		"Authorization": "Bearer " + token,
	}
	return &hyCtx{c: c, headers: headers, cookie: cookie, token: token, quality: quality, platformType: "1", playCache: map[string]map[string]any{}, licenseCache: map[string][]byte{}}, nil
}

func (x *hyCtx) prepare(rawURL string) error {
	if err := x.checkCookie(); err != nil {
		return err
	}
	x.cid = extractURLCourseID(rawURL)
	if x.cid != "" {
		if c := x.findCourse(x.cid); c.ID != "" {
			x.applyCourse(c)
		}
		if x.title == "" {
			x.title = "海洋知道课程" + x.cid
		}
		return nil
	}
	courses := x.getCourseList()
	if len(courses) == 0 {
		return fmt.Errorf("haiyangknow: empty course list")
	}
	x.applyCourse(courses[0])
	return nil
}

func (x *hyCtx) requestJSON(method, path string, params map[string]string, data map[string]any, extra map[string]string) (map[string]any, error) {
	raw := path
	if !strings.HasPrefix(raw, "http") {
		raw = strings.TrimRight(api_host, "/") + "/" + strings.TrimLeft(path, "/")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	headers := cloneStringMap(x.headers)
	for k, v := range extra {
		headers[k] = v
	}
	if headers["Cookie"] == "" && headers["cookie"] != "" {
		headers["Cookie"] = headers["cookie"]
	}
	method = strings.ToUpper(firstNonEmpty(method, "GET"))
	var body string
	if method == "GET" {
		body, err = x.c.GetString(u.String(), headers)
	} else {
		b, _ := json.Marshal(data)
		resp, postErr := x.c.Post(u.String(), bytes.NewReader(b), headers)
		if postErr != nil {
			return nil, postErr
		}
		defer resp.Body.Close()
		buf, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, u.String())
		}
		body = string(buf)
	}
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", u.String(), err)
	}
	return root, nil
}

func (x *hyCtx) requestAPIData(path string, params map[string]string, def any) any {
	root, err := x.requestJSON("GET", path, params, nil, nil)
	if err != nil || !isOKCode(root["code"]) {
		return def
	}
	if v, ok := root["data"]; ok {
		return v
	}
	return root
}

func (x *hyCtx) checkCookie() error {
	for _, typ := range []string{"1", "2", "3"} {
		root, err := x.requestJSON("GET", "/system/api/user/student/getStudentUserInfo", map[string]string{"type": typ}, nil, nil)
		if err != nil || !isOKCode(root["code"]) {
			continue
		}
		data := asMap(root["data"])
		if len(data) == 0 {
			continue
		}
		x.loginType = firstNonEmpty(str(data["loginType"]), typ)
		x.userInfo = data
		return nil
	}
	return fmt.Errorf("haiyangknow cookie check failed")
}

func (x *hyCtx) getCourseList() []hyCourse {
	if x.courseList != nil {
		return x.courseList
	}
	seen := map[string]bool{}
	var out []hyCourse
	pages := 1
	for page := 1; page <= pages && page <= 1000; page++ {
		data := x.requestAPIData("/system/api/user/learningCenter/getUserCoursePage", map[string]string{"platform": x.platformType, "pageSize": "100", "pageIndex": fmt.Sprint(page)}, map[string]any{})
		m := asMap(data)
		for _, rec := range extractRecords(data) {
			c := normalizeCourseInfo(rec)
			if c.ID != "" && !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c)
			}
		}
		if p := intVal(firstNonEmpty(str(m["pages"]), str(m["last_page"]))); p > 0 {
			pages = p
		}
	}
	x.courseList = out
	return out
}

func (x *hyCtx) findCourse(id string) hyCourse {
	id = strings.TrimSpace(id)
	for _, c := range x.getCourseList() {
		if c.ID == id || c.DraftID == id {
			return c
		}
	}
	return hyCourse{}
}

func (x *hyCtx) applyCourse(c hyCourse) {
	x.selected = c
	if c.ID != "" {
		x.cid = c.ID
	}
	if c.Title != "" {
		x.title = c.Title
	}
	if pt := resolvePlatformType(c); pt != "" {
		x.platformType = pt
	}
}

func normalizeCourseInfo(course map[string]any) hyCourse {
	id := firstString(course, "id", "courseId", "course_id")
	if id == "" {
		return hyCourse{}
	}
	return hyCourse{ID: id, DraftID: firstString(course, "draftId", "draft_id", "curriculumId"), Title: firstString(course, "title", "courseName", "name", "price"), PlatformType: firstString(course, "platformType", "platform"), RawPlatformType: firstString(course, "platform", "platformType"), Course: course, Purchased: true, Price: coursePrice(course)}
}

func resolvePlatformType(c hyCourse) string {
	m := c.Course
	for _, kv := range []struct{ key, val string }{{"dyCourseOrderId", "1"}, {"ksCourseOrderId", "2"}, {"wxCourseOrderId", "3"}} {
		if str(m[kv.key]) != "" {
			return kv.val
		}
	}
	for _, v := range []string{c.PlatformType, c.RawPlatformType, firstString(m, "platform", "platformType")} {
		s := strings.ToLower(strings.TrimSpace(v))
		switch s {
		case "1", "2", "3":
			return s
		case "dy", "douyin":
			return "1"
		case "ks", "kuaishou":
			return "2"
		case "wx", "wechat":
			return "3"
		}
	}
	return "1"
}

var numericPathRe = regexp.MustCompile(`(?i)/(?:course|video|detail|learn|curriculum)[^?#]*/(?P<cid>\d+)|/(?:course|video|detail|learn|curriculum)[^?#]*?(?P<cid>\d+)`)

func extractURLCourseID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if regexp.MustCompile(`^\d+$`).MatchString(raw) {
		return raw
	}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		for _, k := range []string{"courseId", "course_id", "curriculumId", "curriculum_id", "id", "draftId"} {
			if v := strings.TrimSpace(q.Get(k)); regexp.MustCompile(`^\d+$`).MatchString(v) {
				return v
			}
		}
	}
	m := numericPathRe.FindStringSubmatch(raw)
	for i, name := range numericPathRe.SubexpNames() {
		if name == "cid" && i < len(m) && m[i] != "" {
			return m[i]
		}
	}
	return ""
}
