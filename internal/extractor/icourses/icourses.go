// Package icourses implements source-aligned extraction for icourses.cn (爱课程).
package icourses

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer       = "https://www.icourses.cn"
	api_root      = "https://www.icourses.cn/prod/icourse-portal-api"
	user_info_url = api_root + "/userCenter/userinfo"
)

const (
	cuoc_detail_api   = "/course/getCourseDetailByVideo"
	cuoc_resource_api = "/course/getCourseResByVideo"

	mooc_detail_api      = "/course/getCourseDetailByShare"
	mooc_chapter_api     = "/course/getChapterListByShare"
	mooc_chapter_res_api = "/course/getResListByChapterId"
	mooc_other_res_api   = "/course/getOtherResListByShare"
	mooc_share_sub_api   = "/course/getShareCourseResSubList"
)

var moocCourseDocAPIs = []namedAPI{
	{"课程介绍", "/course/getCourseDescriptionRes"},
	{"教学大纲", "/course/getCourseTeachSyllabusRes"},
	{"教学日历", "/course/getCourseTeachCalendarRes"},
	{"考核方式", "/course/getCourseEvaluateWayRes"},
	{"学习指南", "/course/getCourseStudyGuideRes"},
}

var (
	patterns = []string{
		`\s*((https?://www\.icourses\.cn/sCourse/course_(?P<cid1>\d+)\.html)|(https?://www\.icourses\.cn/web/sword/portal/shareDetails\?.*?cId=(?P<cid2>\d+))|(https?://www\.icourses\.cn/shareCourseDetailModule/.*?courseId=(?P<cid3>\d+))|(https?://www\.icourses\.cn/shareCourseDetail\?.*?courseId=(?P<cid4>\d+)))`,
		`\s*((https?://www\.icourses\.cn/web/sword/portal/videoDetail\?.*?courseId=(?P<cid1>[\w-]*))|(https?://www\.icourses\.cn/videoCourseDetail\?.*?courseId=(?P<cid2>[\w-]+))|(https?://www\.icourses\.cn/videoCoursePlayer\?.*?courseId=(?P<cid3>[\w-]+)))`,
	}

	sCourseRe  = regexp.MustCompile(`(?i)/sCourse/course_(\d+)\.html`)
	courseIDRe = regexp.MustCompile(`(?i)(?:[?&](?:courseId|cId)=)([\w-]+)`)
)

func init() {
	extractor.Register(&Icourses{}, extractor.SiteInfo{Name: "Icourses", URL: "icourses.cn", NeedAuth: true})
}

type Icourses struct{}

func (i *Icourses) Patterns() []string { return patterns }

type namedAPI struct{ name, path string }

type targetKind string

const (
	kindCuoc targetKind = "cuoc"
	kindMooc targetKind = "mooc"
)

type icoursesCtx struct {
	c       *util.Client
	headers map[string]string
	cookie  string
	token   string

	cid    string
	kind   targetKind
	title  string
	detail map[string]any
}

type resource struct {
	Name      string
	URL       string
	Kind      string
	Ext       string
	MediaType string
	Category  string
	ResID     string
	Size      int64
}

type chapter struct {
	Name      string
	ID        string
	Resources []resource
	Units     []unit
}

type unit struct {
	Name      string
	ID        string
	Resources []resource
}

type moocInfo struct {
	Chapters []chapter
	Papers   []resource
	Sources  []resource
}

func (i *Icourses) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("icourses requires login cookies")
	}
	x := newCtx(opts.Cookies)
	kind, cid := parseTarget(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse icourses courseId from URL: %s", rawURL)
	}
	x.kind, x.cid = kind, cid
	if err := x.loadTitle(); err != nil {
		return nil, err
	}
	switch kind {
	case kindCuoc:
		resources, err := x.cuocResources()
		if err != nil {
			return nil, err
		}
		return x.mediaFromResources(resources)
	case kindMooc:
		info, err := x.moocInfo()
		if err != nil {
			return nil, err
		}
		return x.mediaFromMoocInfo(info)
	default:
		return nil, fmt.Errorf("icourses: unsupported course target")
	}
}

func newCtx(jar http.CookieJar) *icoursesCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookies := cookieMap(jar, []string{referer})
	token := firstNonEmpty(cookies["icourses_website_user_token"], cookies["icourses_token"])
	cookie := cookieHeaderFromMap(cookies)
	headers := map[string]string{"Referer": referer, "Accept": "application/json, text/plain, */*"}
	if cookie != "" {
		headers["Cookie"] = cookie
	}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return &icoursesCtx{c: c, cookie: cookie, token: token, headers: headers}
}

func parseTarget(raw string) (targetKind, string) {
	trimmed := strings.TrimSpace(raw)
	if m := sCourseRe.FindStringSubmatch(trimmed); len(m) > 1 {
		return kindMooc, m[1]
	}
	if u, err := url.Parse(trimmed); err == nil {
		q := u.Query()
		if cid := firstNonEmpty(q.Get("courseId"), q.Get("cId")); cid != "" {
			if strings.Contains(u.Path, "video") || strings.Contains(trimmed, "videoCourse") || strings.Contains(cid, "-") {
				return kindCuoc, cid
			}
			return kindMooc, cid
		}
	}
	if m := courseIDRe.FindStringSubmatch(trimmed); len(m) > 1 {
		if strings.Contains(trimmed, "video") || strings.Contains(m[1], "-") {
			return kindCuoc, m[1]
		}
		return kindMooc, m[1]
	}
	return kindMooc, ""
}

func (x *icoursesCtx) loadTitle() error {
	path := mooc_detail_api
	if x.kind == kindCuoc {
		path = cuoc_detail_api
	}
	data, err := x.apiGet(path, map[string]string{"courseId": x.cid})
	if err != nil {
		return err
	}
	x.detail = asMap(data)
	x.title = composeTitle(str(x.detail["courseName"]), str(x.detail["schoolName"]), str(x.detail["teacherName"]))
	if x.title == "" {
		x.title = "icourses_" + x.cid
	}
	return nil
}
