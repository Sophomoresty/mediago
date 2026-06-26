// Package huke88 implements source-aligned extraction for huke88.com (虎课网).
package huke88

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer             = "https://huke88.com/"
	login_check_url     = "https://huke88.com/course/174979.html"
	course_url          = "https://huke88.com/course/%s.html"
	study_url           = "https://huke88.com/person/study/%s.html?page=%s&per-page=30"
	purchased_study_url = "https://huke88.com/person/study/%s.html?type=6&page=%s&per-page=30"
	video_play_url      = "https://asyn.huke88.com/video/video-play"
	file_url            = "https://asyn.huke88.com/download/video-annex"
)

var (
	patterns = []string{`\s*((https?://(?:[\w-]+\.)*huke88\.com/course/(?P<cid>\d+)\.html(?:[/?#][^\s]*)?)|(?P<huke88>https?://(?:[\w-]+\.)*huke88\.com(?:[/?#][^\s]*)?)|(?P<huke88_name>huke88|虎课网|虎课))`}

	courseURLRe = regexp.MustCompile(`(?i)/course/(\d+)\.html`)
	careerURLRe = regexp.MustCompile(`(?i)/career/video/\d+-(\d+)\.html`)
)

func init() {
	extractor.Register(&Huke88{}, extractor.SiteInfo{Name: "Huke88", URL: "huke88.com", NeedAuth: true})
}

type Huke88 struct{}

func (h *Huke88) Patterns() []string { return patterns }

type huke88Ctx struct {
	c       *util.Client
	headers map[string]string
	cookie  string

	cid            string
	title          string
	uid            string
	paidCourseID   string
	csrf           string
	coursePageText string
	videoCache     map[string]map[string]any
	courseIDs      []string
	videoTitles    map[string]string
}

type courseRef struct {
	ID    string
	Title string
}

type hukeSource struct {
	Kind     string
	ID       string
	Name     string
	URL      string
	Format   string
	FileType string
	Raw      map[string]any
}

func (h *Huke88) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("huke88 requires login cookies")
	}
	x, err := newCtx(opts.Cookies)
	if err != nil {
		return nil, err
	}
	if err := x.prepare(rawURL); err != nil {
		return nil, err
	}
	sources, err := x.collectSources()
	if err != nil {
		return nil, err
	}
	return x.mediaFromSources(sources)
}

func newCtx(jar http.CookieJar) (*huke88Ctx, error) {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{referer, "https://asyn.huke88.com/"})
	if cookie == "" {
		return nil, fmt.Errorf("huke88 requires non-empty login cookie jar")
	}
	headers := map[string]string{
		"Accept":           "application/json, text/javascript, */*; q=0.01",
		"X-Requested-With": "XMLHttpRequest",
		"Origin":           "https://huke88.com",
		"Referer":          referer,
		"Cookie":           cookie,
	}
	x := &huke88Ctx{c: c, headers: headers, cookie: cookie, videoCache: map[string]map[string]any{}, videoTitles: map[string]string{}}
	if err := x.checkCookie(); err != nil {
		return nil, err
	}
	return x, nil
}

func (x *huke88Ctx) checkCookie() error {
	if !regexp.MustCompile(`(^|;)\s*_identity-usernew\s*=`).MatchString(x.cookie) {
		return fmt.Errorf("huke88: missing _identity-usernew cookie")
	}
	body, err := x.c.GetString(login_check_url, x.htmlHeader(referer))
	if err != nil {
		return fmt.Errorf("huke88 cookie check failed: %w", err)
	}
	if regexp.MustCompile(`Param\.is_login\s*=\s*["']?1`).MatchString(body) || regexp.MustCompile(`Param\.uid\s*=\s*\d+`).MatchString(body) {
		return nil
	}
	return fmt.Errorf("huke88 cookie check failed")
}

func (x *huke88Ctx) prepare(rawURL string) error {
	x.cid = parseCID(rawURL)
	if x.cid == "" {
		courses, err := x.courseList()
		if err != nil {
			return err
		}
		if len(courses) == 0 {
			return fmt.Errorf("cannot parse huke88 course id from URL: %s", rawURL)
		}
		x.cid = courses[0].ID
		x.title = cleanTitle(courses[0].Title)
	}
	page, err := x.getCoursePage(x.cid)
	if err != nil {
		return err
	}
	x.coursePageText = page
	if title := extractTitle(page); title != "" {
		x.title = title
	}
	if x.title == "" {
		x.title = "huke88_" + x.cid
	}
	x.uid = extractParam(page, "uid", x.uid)
	x.paidCourseID = extractPaidCourseID(page)
	x.csrf = extractCSRF(page, x.cookie)
	return nil
}

func parseCID(raw string) string {
	for _, re := range []*regexp.Regexp{courseURLRe, careerURLRe} {
		if m := re.FindStringSubmatch(raw); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}
