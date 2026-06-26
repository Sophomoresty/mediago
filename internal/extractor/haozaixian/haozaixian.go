// Package haozaixian implements source-aligned Haozaixian course extraction.
package haozaixian

import (
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
	SYSTEM_COURSE_TYPE  = "1"
	SPECIAL_COURSE_TYPE = "2"
	AI_COURSE_TYPE      = "66"

	referer        = "https://www.haoke100.com"
	check_url      = "https://c3-jx-stable.zuoyebang.com/frontcourse/teach/course/pccoursefull?courseId=0&appId=winhaoke"
	order_list_url = "https://c3-sell.zuoyebang.com/order-ui/order/list/v2"

	system_course_list_url  = "https://c3-jx-stable.zuoyebang.com/mcourse/winhaoke/course/list"
	special_course_list_url = "https://c4-jx-stable.zuoyebang.com/teachcourse/index/courselist"
	course_full_url         = "https://c3-jx-stable.zuoyebang.com/frontcourse/teach/course/pccoursefull"
	ai_video_url            = "https://c4-jx-stable.zuoyebang.com/classme/student/aiclassroom/videoInfo"
	special_video_url       = "https://c4-jx-stable.zuoyebang.com/liveme/student/classroom/pre"
	system_video_url        = "https://c3-jx-stable.zuoyebang.com/liveme/student/classroom/pre"
	lesson_material_url     = "https://jx.zuoyebang.com/frontcourse/public/material/lessonmaterial"
	course_material_url     = "https://c4-jx-stable.zuoyebang.com/mcourse/winhaoke/matearial/course"
	file_material_url       = "https://c4-jx-stable.zuoyebang.com/mcourse/winhaoke/matearial/file"

	course_emphasis_detail_url = "https://c3-jx-stable.zuoyebang.com/frontcourse/public/courseemphasis/courseemphasisdetail"
	ai_course_info_url         = "https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getcourseinfo"
	ai_lesson_detail_url       = "https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getdetail"
	ai_video_by_round_url      = "https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getvideobyroundid"
	lesson_lecture_url         = "https://c3-jx-stable.zuoyebang.com/frontcourse/public/lecture/lessonlecture"
)

var patterns = []string{`\s*((https?://(?:[\w-]+\.)*haozaixian\.net.*?courseId=(?P<cid1>\d+).*?courseType=(?P<ctype1>[12]))|(https?://(?:[\w-]+\.)*haozaixian\.net.*?courseId=(?P<cid2>\d+))|(https?://(?:[\w-]+\.)*haozaixian\.net(?:[/?#].*)?)|(#小程序://好课在线))`}

func init() {
	extractor.Register(&Haozaixian{}, extractor.SiteInfo{Name: "Haozaixian", URL: "haozaixian.net", NeedAuth: true})
}

type Haozaixian struct{}

func (s *Haozaixian) Patterns() []string { return patterns }

type hzCtx struct {
	c       *util.Client
	headers map[string]string
	cookie  string
	quality string

	cid         string
	title       string
	courseType  string
	nCourseType string
	vc          string
	vcname      string
	host        string
	naSource    string
	courseList  []hzCourse
}

type hzCourse struct {
	CourseID, Title, TeacherName, CourseType, NCourseType string
	Raw                                                   map[string]any
}

type hzLesson struct {
	Name, LessonName, LessonID, LiveRoomID, Type, RoundID string
	Seq                                                   int
	Materials                                             []hzMaterial
}

type hzMaterial struct{ Name, URL, Kind string }
type aiVideo struct {
	URL      string
	Duration int
	IsMain   bool
}

func (s *Haozaixian) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("haozaixian requires login cookies")
	}
	x, err := newCtx(opts.Cookies, opts.Quality)
	if err != nil {
		return nil, err
	}
	if err := x.prepare(rawURL); err != nil {
		return nil, err
	}
	lessons, sources, err := x.getInfos()
	if err != nil {
		return nil, err
	}
	return x.mediaFromLessons(lessons, sources)
}

func newCtx(jar http.CookieJar, quality string) (*hzCtx, error) {
	cookie := cookieHeader(jar, []string{
		referer,
		check_url,
		"https://c3-jx-stable.zuoyebang.com/",
		"https://c4-jx-stable.zuoyebang.com/",
		"https://jx.zuoyebang.com/",
		"https://aiclass.zuoyebang.com/",
	})
	if cookie == "" {
		return nil, fmt.Errorf("haozaixian: empty cookie jar")
	}
	c := util.NewClient()
	c.SetCookieJar(jar)
	x := &hzCtx{
		c:          c,
		cookie:     cookie,
		quality:    quality,
		courseType: SPECIAL_COURSE_TYPE,
		vc:         "650",
		vcname:     "9.9.0",
		host:       "c4-jx-stable.zuoyebang.com",
		naSource:   "winhaoke",
		headers: map[string]string{
			"cookie":  cookie,
			"referer": referer,
		},
	}
	x.setCourseType(SPECIAL_COURSE_TYPE)
	return x, nil
}

func (x *hzCtx) setCourseType(courseType string) {
	if strings.TrimSpace(courseType) == SYSTEM_COURSE_TYPE {
		x.courseType = SYSTEM_COURSE_TYPE
		x.host = "c3-jx-stable.zuoyebang.com"
		x.vc = "640"
		x.vcname = "9.8.0"
	} else {
		x.courseType = SPECIAL_COURSE_TYPE
		x.host = "c4-jx-stable.zuoyebang.com"
		x.vc = "650"
		x.vcname = "9.9.0"
	}
	x.headers["referer"] = referer
	x.headers["cookie"] = x.cookie
	x.headers["User-Agent"] = fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36 a_irclass_vc/%s a_irclass_vcname/%s appId/winhaoke", x.vc, x.vcname)
}

func (x *hzCtx) checkCookie() error {
	if !strings.Contains(strings.ToLower(x.cookie), "zybuss") {
		return fmt.Errorf("haozaixian cookie check failed: missing zybuss")
	}
	x.setCourseType(SYSTEM_COURSE_TYPE)
	h := cloneHeaders(x.headers)
	h["cookie"] = x.cookie
	root, err := x.requestJSON(check_url, h)
	if err != nil {
		return err
	}
	if _, ok := root["errNo"]; !ok {
		return fmt.Errorf("haozaixian cookie check failed: missing errNo")
	}
	return nil
}

func (x *hzCtx) requestJSON(endpoint string, headers map[string]string) (map[string]any, error) {
	h := x.headers
	if headers != nil {
		h = headers
	}
	body, err := x.c.GetString(endpoint, h)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	return root, nil
}

func (x *hzCtx) postFormJSON(endpoint string, data []kv) (map[string]any, error) {
	h := cloneHeaders(x.headers)
	h["Content-Type"] = "application/x-www-form-urlencoded"
	resp, err := x.c.Post(endpoint, strings.NewReader(encodePairs(data)), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	return root, nil
}

func (x *hzCtx) prepare(rawURL string) error {
	if err := x.checkCookie(); err != nil {
		return err
	}
	cid, ctype := parseCourseRef(rawURL)
	if ctype == SYSTEM_COURSE_TYPE || ctype == SPECIAL_COURSE_TYPE {
		x.setCourseType(ctype)
	}
	if cid != "" {
		x.cid = cid
		if c := x.findCourse(cid, x.courseType); c.CourseID != "" {
			x.applyCourse(c)
		}
		if err := x.getTitle(); err != nil && x.title == "" {
			x.title = "好课在线课程" + cid
		}
		return nil
	}
	courses := append(x.buildCourseMap(SYSTEM_COURSE_TYPE), x.buildCourseMap(SPECIAL_COURSE_TYPE)...)
	if len(courses) == 0 {
		return fmt.Errorf("haozaixian: empty course list")
	}
	x.courseList = courses
	x.applyCourse(courses[0])
	return x.getTitle()
}

func (x *hzCtx) findCourse(cid, primary string) hzCourse {
	order := []string{primary}
	if primary == SYSTEM_COURSE_TYPE {
		order = append(order, SPECIAL_COURSE_TYPE)
	} else {
		order = append(order, SYSTEM_COURSE_TYPE)
	}
	for _, courseType := range order {
		courses := x.buildCourseMap(courseType)
		if len(courses) > 0 {
			x.courseList = courses
		}
		for _, c := range courses {
			if c.CourseID == cid {
				return c
			}
		}
	}
	return hzCourse{}
}

func (x *hzCtx) applyCourse(c hzCourse) {
	x.cid = c.CourseID
	x.title = c.Title
	x.nCourseType = c.NCourseType
	if c.CourseType == SYSTEM_COURSE_TYPE || c.CourseType == SPECIAL_COURSE_TYPE {
		x.setCourseType(c.CourseType)
	}
}

var (
	courseIDParamRe   = regexp.MustCompile(`(?:^|[?&])courseId=(\d+)`)
	courseTypeParamRe = regexp.MustCompile(`(?:^|[?&])courseType=([12])`)
)

func parseCourseRef(raw string) (string, string) {
	if u, err := url.Parse(strings.TrimSpace(raw)); err == nil {
		q := u.Query()
		cid := firstNonEmpty(q.Get("courseId"), q.Get("courseid"), q.Get("cid"))
		ctype := firstNonEmpty(q.Get("courseType"), q.Get("coursetype"))
		if cid != "" || ctype != "" {
			return cid, ctype
		}
	}
	cid, ctype := "", ""
	if m := courseIDParamRe.FindStringSubmatch(raw); len(m) == 2 {
		cid = m[1]
	}
	if m := courseTypeParamRe.FindStringSubmatch(raw); len(m) == 2 {
		ctype = m[1]
	}
	return cid, ctype
}
