// Package huatu implements source-aligned extraction for huatu.com (华图在线).
package huatu

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer          = "https://www.huatu.com/"
	check_url        = "https://ns.huatu.com/u/v3/member/user/icon"
	course_check_url = "https://ocfapi.huatu.com/api/user/my_course"

	my_course_url = "https://ocfapi.huatu.com/api/user/my_course"
	syllabus_url  = "https://ocfapi.huatu.com/api/goods/syllabusBuy"
	player_url    = "https://ocfapi.huatu.com/api/course/goods/get_player"
	vod_info_url  = "https://playvideo.vodplayvideo.net/getplayinfo/v4/%s/%s?psign=%s"

	USER_AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
	patterns = []string{`\s*((https?://(?:[\w-]+\.)*huatu\.com.*?goodsNum=\w+)|(?P<huatu_login>https?://v\.huatu\.com(?:[/?#].*)?)|(?P<huatu>https?://(?:[\w-]+\.)*huatu\.com(?:[/?#].*)?))`}

	goodsNumRe     = regexp.MustCompile(`(?i)(?:[?&#](?:goodsNum|goodsNo|goodsId|courseId|course_id)=)([A-Za-z0-9]+)`)
	courseDetailRe = regexp.MustCompile(`(?i)(?:^|/)(?:courseDetail|course-detail|course/detail)/(?P<cid>[^/?#&]+)/?(?P<title>[^?#]*)`)
	fallbackCIDRe  = regexp.MustCompile(`(?i)(?:goods(?:_|)(?:num|no|id)[=:]([A-Za-z0-9]+)|courseDetail/([A-Za-z0-9]+)|["']goodsNum["']\s*:\s*["']?([A-Za-z0-9]+)|["']goodsNo["']\s*:\s*["']?([A-Za-z0-9]+))`)
)

func init() {
	extractor.Register(&Huatu{}, extractor.SiteInfo{Name: "Huatu", URL: "huatu.com", NeedAuth: true})
}

type Huatu struct{}

func (h *Huatu) Patterns() []string { return patterns }

type huatuCtx struct {
	c       *util.Client
	headers map[string]string
	cookie  string
	token   string

	cid    string
	title  string
	target huatuTarget
}

type huatuTarget struct {
	CID       string
	Title     string
	StageID   string
	ModularID string
	LessonID  string
}

type apiResp struct {
	Code  int    `json:"code"`
	Msg   string `json:"msg"`
	Data  any    `json:"data"`
	Media any    `json:"media"`
}

func (h *Huatu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("huatu requires login cookies (use --cookies or --cookies-from-browser)")
	}
	x, err := newCtx(opts.Cookies)
	if err != nil {
		return nil, err
	}
	if err := x.prepare(rawURL); err != nil {
		return nil, err
	}
	items, err := x.collectItems()
	if err != nil {
		return nil, err
	}
	return x.mediaFromItems(items)
}

func newCtx(jar http.CookieJar) (*huatuCtx, error) {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{
		referer,
		"https://www.huatu.com/",
		"https://v.huatu.com/",
		"https://ocfapi.huatu.com/",
		"https://ns.huatu.com/",
	})
	if cookie == "" {
		return nil, fmt.Errorf("huatu requires non-empty login cookie jar")
	}
	cookie = normalizeCookieTokenAliases(cookie)
	token := cookieToken(cookie)
	headers := map[string]string{
		"terminal":         "3",
		"Channel-Terminal": "",
		"Channel-Alias":    "ht_pc",
		"Referer":          "https://www.huatu.com/htzx/user/index.shtml#/personalCenter/myCourses",
		"Origin":           "https://www.huatu.com",
		"Accept":           "application/json, text/plain, */*",
		"User-Agent":       USER_AGENT,
		"Cookie":           cookie,
		"cookie":           cookie,
	}
	applyTokenHeaders(headers, token)
	return &huatuCtx{c: c, headers: headers, cookie: cookie, token: token}, nil
}

func (x *huatuCtx) prepare(rawURL string) error {
	x.target = parseTarget(rawURL)
	x.cid = x.target.CID
	x.title = cleanName(x.target.Title)
	if x.cid == "" {
		course, err := x.firstCourse()
		if err != nil {
			return err
		}
		if len(course) == 0 {
			return fmt.Errorf("cannot parse huatu goodsNum/courseId from URL: %s", rawURL)
		}
		x.applyCourse(course)
		return nil
	}
	if course, _ := x.findCourse(x.cid); len(course) > 0 {
		x.applyCourse(course)
	}
	if x.title == "" {
		x.title = cleanName(x.cid)
	}
	return nil
}

func parseTarget(raw string) huatuTarget {
	out := huatuTarget{}
	if raw == "" || raw == "huatu" {
		return out
	}
	if decoded, err := url.QueryUnescape(raw); err == nil {
		raw = decoded
	}
	if m := goodsNumRe.FindStringSubmatch(raw); len(m) > 1 {
		out.CID = strings.TrimSpace(m[1])
	}
	if u, err := url.Parse(raw); err == nil {
		for _, values := range []url.Values{u.Query(), parseFragmentQuery(u.Fragment)} {
			if out.CID == "" {
				out.CID = firstQuery(values, "goodsNum", "goodsNo", "goodsId", "courseId", "course_id")
			}
			if out.Title == "" {
				out.Title = firstQuery(values, "coursesName", "courseName", "title", "name")
			}
			if out.StageID == "" {
				out.StageID = firstQuery(values, "stageId", "stage_id")
			}
			if out.ModularID == "" {
				out.ModularID = firstQuery(values, "modularId", "modular_id", "moduleId", "module_id")
			}
			if out.LessonID == "" {
				out.LessonID = firstQuery(values, "lessonId", "lesson_id", "clazzLessonId", "clazz_lesson_id")
			}
		}
		for _, candidate := range []string{u.Path, strings.Split(u.Fragment, "?")[0]} {
			if m := courseDetailRe.FindStringSubmatch(candidate); len(m) > 0 {
				if out.CID == "" {
					out.CID = strings.TrimSpace(m[courseDetailRe.SubexpIndex("cid")])
				}
				if out.Title == "" {
					out.Title = strings.Trim(strings.TrimSpace(m[courseDetailRe.SubexpIndex("title")]), "/")
				}
			}
		}
	}
	if out.CID == "" {
		if m := fallbackCIDRe.FindStringSubmatch(raw); len(m) > 1 {
			for _, g := range m[1:] {
				if strings.TrimSpace(g) != "" {
					out.CID = strings.TrimSpace(g)
					break
				}
			}
		}
	}
	out.Title = cleanName(out.Title)
	return out
}

func (x *huatuCtx) getJSON(endpoint string, params map[string]string, headers map[string]string, out any) (map[string]any, error) {
	if params != nil {
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		if strings.Contains(endpoint, "?") {
			endpoint += "&" + values.Encode()
		} else {
			endpoint += "?" + values.Encode()
		}
	}
	if headers == nil {
		headers = x.headers
	}
	body, err := x.c.GetString(endpoint, headers)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(body), out); err != nil {
			return nil, fmt.Errorf("decode %s: %w", endpoint, err)
		}
	}
	return root, nil
}

func successCode(v any) bool {
	s := str(v)
	return s == "10000" || s == "1000000"
}
