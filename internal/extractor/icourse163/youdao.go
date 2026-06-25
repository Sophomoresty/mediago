package icourse163

// Youdao / ke.study.163.com flow ported from decompiled
// Mooc/Courses/Mooc163/Study163/Study163_Youdao.pyc (Study163_Youdao +
// Youdao_Course). Course list / lesson / video resolution all run over plain
// HTTP with the member's login cookies:
//
//   1. {course_site}/course/detail/{cid}      -> window.courseTitle / window.lesson / window.isBuy
//   2. {course_site}/course/api/detail.json   -> course.lessonList (ke.study.163.com only)
//   3. {course_site}/course/live/lessons.json -> data[].list[].video.downloadUrl
//   4. {course_site}/course/video.json        -> result.videoUrl   (per-lesson fallback)
//   5. https://ke.youdao.com/course/detail/getLessonInfo2.json -> data.video.downloadUrl (youdao fallback)
//
// The "我的课程" picker (Youdao_Course._get_course_list / mycoursev3.json)
// is wired for ke.youdao.com/mycourse URLs, listing the user's purchased
// courses and returning them as entries with their resolved course URLs.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

// Constants ported verbatim from Study163_Youdao / Youdao_Course.
const (
	youdaoKeSite    = "https://ke.youdao.com"
	study163Site    = "https://ke.study.163.com"
	studyYoudaoSite = "https://ke.study.youdao.com"

	youdaoCourseURLFmt  = "%s/course/detail/%s"
	youdaoDetailURLFmt  = "%s/course/api/detail.json?courseId=%s"
	youdaoInfosURLFmt   = "%s/course/live/lessons.json?courseId=%s"
	youdaoVideoURLFmt   = "%s/course/video.json?lessonId=%s"
	youdaoNewVideoURL   = "https://ke.youdao.com/course/detail/getLessonInfo2.json?courseId=%s&lessonId=%s"
	youdaoCourseListURL = "https://ke.youdao.com/course/app/mycoursev3.json?courseStatus=%s&page=%s"
)

// Source patterns: courses_re['Youdao_Course'] and courses_re['Study163_Youdao'].
var youdaoPatterns = []string{
	`(?:www\.)?ke\.youdao\.com/.*?detail/\d+`,
	`live\.youdao.*?\.com/.*?course[Ii]d=\d+`,
	`ke\.study\.(?:163|youdao)\.com/.*?detail/\d+`,
	`.*\.study\.(?:163|youdao)\.com/.*?course[Ii]d=\d+`,
	// Youdao course-list (Youdao_Course._get_course_list)
	`(?:www\.)?ke\.youdao\.com/(?:mycourse|course/app)`,
}

var (
	youdaoKeRe       = regexp.MustCompile(`^https?://ke\.youdao\.com/.*?detail/(?P<cid>\d+)`)
	youdaoLiveRe     = regexp.MustCompile(`^https?://live\.youdao[^/]*\.com/.*?course[Ii]d=(?P<cid>\d+)`)
	study163DetRe    = regexp.MustCompile(`^https?://ke\.study\.(?:163|youdao)\.com/.*?detail/(?P<cid>\d+)`)
	study163CidRe    = regexp.MustCompile(`^https?://[^/]*\.study\.(?:163|youdao)\.com/.*?course[Ii]d=(?P<cid>\d+)`)
	youdaoCourseList = regexp.MustCompile(`^https?://ke\.youdao\.com/(?:mycourse|course/app)`)
)

func init() {
	extractor.Register(&Youdao163{}, extractor.SiteInfo{
		Name:     "youdao",
		URL:      "ke.youdao.com",
		NeedAuth: true,
	})
}

type Youdao163 struct{}

func (y *Youdao163) Patterns() []string { return youdaoPatterns }

type youdaoTarget struct {
	site string // course_site base, e.g. https://ke.youdao.com
	cid  string
	// youdao true => Youdao_Course (ke.youdao.com): no detail.json, uses
	// getLessonInfo2.json fallback. false => Study163_Youdao base flow.
	youdao bool
}

func parseYoudaoURL(rawURL string) (youdaoTarget, bool) {
	if m := youdaoKeRe.FindStringSubmatch(rawURL); m != nil {
		return youdaoTarget{site: youdaoKeSite, cid: m[youdaoKeRe.SubexpIndex("cid")], youdao: true}, true
	}
	if m := youdaoLiveRe.FindStringSubmatch(rawURL); m != nil {
		return youdaoTarget{site: youdaoKeSite, cid: m[youdaoLiveRe.SubexpIndex("cid")], youdao: true}, true
	}
	if m := study163DetRe.FindStringSubmatch(rawURL); m != nil {
		site := study163Site
		if strings.Contains(rawURL, ".study.youdao.com") {
			site = studyYoudaoSite
		}
		return youdaoTarget{site: site, cid: m[study163DetRe.SubexpIndex("cid")]}, true
	}
	if m := study163CidRe.FindStringSubmatch(rawURL); m != nil {
		site := study163Site
		if strings.Contains(rawURL, ".study.youdao.com") {
			site = studyYoudaoSite
		}
		return youdaoTarget{site: site, cid: m[study163CidRe.SubexpIndex("cid")]}, true
	}
	return youdaoTarget{}, false
}

func (y *Youdao163) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("youdao requires login cookies (use --cookies or --cookies-from-browser)")
	}

	// Course-list route (Youdao_Course._get_course_list / mycoursev3.json)
	if youdaoCourseList.MatchString(rawURL) {
		c := util.NewClient()
		c.SetCookieJar(opts.Cookies)
		return extractYoudaoCourseList(c)
	}

	tgt, ok := parseYoudaoURL(rawURL)
	if !ok || tgt.cid == "" {
		return nil, fmt.Errorf("cannot parse youdao/study163 URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)

	pageURL := fmt.Sprintf(youdaoCourseURLFmt, tgt.site, tgt.cid)
	page, err := c.GetString(pageURL, youdaoHeaders(tgt.site))
	if err != nil {
		return nil, fmt.Errorf("fetch youdao course page: %w", err)
	}

	title := match1(page, `window\.courseTitle='(.*?)';`)
	if title == "" {
		title = "youdao_" + tgt.cid
	} else {
		title = sanitize(title)
	}

	lessons, err := y.lessonList(c, tgt, page)
	if err != nil {
		return nil, err
	}

	entries := y.videoEntries(c, tgt, lessons)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no playable youdao videos (course not purchased or login expired)")
	}

	return &extractor.MediaInfo{
		Site:    "youdao",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"course_id":   tgt.cid,
			"course_site": tgt.site,
			"source_api":  "Study163_Youdao",
		},
	}, nil
}

func youdaoHeaders(site string) map[string]string {
	return map[string]string{
		"Referer":    site + "/",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}
}

// youdaoLesson mirrors the flattened lesson dicts the Python code consumes.
type youdaoLesson struct {
	id          string
	title       string
	lessonType  int
	downloadURL string
	size        int64
}

var windowLessonRe = regexp.MustCompile(`window\.lesson\s*=\s*(\[[\s\S]*?\]);`)

func (y *Youdao163) lessonList(c *util.Client, tgt youdaoTarget, page string) ([]youdaoLesson, error) {
	var lessons []youdaoLesson
	if m := windowLessonRe.FindStringSubmatch(page); len(m) > 1 {
		lessons = flattenYoudaoLessons(parseYoudaoLessonArray(m[1]))
	}

	// Study163_Youdao base (platform != 'youdao') augments via detail.json.
	if !tgt.youdao {
		detURL := fmt.Sprintf(youdaoDetailURLFmt, tgt.site, tgt.cid)
		body, err := c.GetString(detURL, youdaoHeaders(tgt.site))
		if err == nil {
			det := flattenYoudaoLessons(parseDetailLessonList(body))
			if len(det) >= len(lessons) {
				lessons = det
			}
		}
	}
	if len(lessons) == 0 {
		return nil, fmt.Errorf("youdao lesson list empty (window.lesson / detail.json returned nothing)")
	}
	return lessons, nil
}

// rawYoudaoLesson is the on-wire lesson node; "list" nests sub-lessons.
type rawYoudaoLesson struct {
	ID    any    `json:"id"`
	Title string `json:"title"`
	Type  any    `json:"type"`
	Video struct {
		DownloadURL string `json:"downloadUrl"`
		Size        any    `json:"size"`
	} `json:"video"`
	List []rawYoudaoLesson `json:"list"`
}

func parseYoudaoLessonArray(body string) []rawYoudaoLesson {
	var arr []rawYoudaoLesson
	if err := decodeJSON(body, &arr); err != nil {
		return nil
	}
	return arr
}

func parseDetailLessonList(body string) []rawYoudaoLesson {
	var out struct {
		Course struct {
			LessonList []rawYoudaoLesson `json:"lessonList"`
		} `json:"course"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil
	}
	return out.Course.LessonList
}

// flattenYoudaoLessons mirrors the nested-list flatten in _get_lesson_list.
func flattenYoudaoLessons(nodes []rawYoudaoLesson) []youdaoLesson {
	var out []youdaoLesson
	for _, n := range nodes {
		if len(n.List) > 0 {
			out = append(out, flattenYoudaoLessons(n.List)...)
			continue
		}
		ls := youdaoLesson{
			id:          valueString(n.ID),
			title:       n.Title,
			downloadURL: n.Video.DownloadURL,
		}
		switch t := n.Type.(type) {
		case nil:
		default:
			ls.lessonType = parseInt(valueString(t))
		}
		if sz := valueString(n.Video.Size); sz != "" {
			ls.size = parseInt64(sz) / 1048576
		}
		out = append(out, ls)
	}
	return out
}

// videoEntries builds downloadable entries for video-type lessons (0,1,50),
// resolving the URL via video.json / getLessonInfo2.json when absent, exactly
// as Study163_Youdao._get_video_list + _get_video_url do.
func (y *Youdao163) videoEntries(c *util.Client, tgt youdaoTarget, lessons []youdaoLesson) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	idx := 1
	for _, ls := range lessons {
		if ls.lessonType != 0 && ls.lessonType != 1 && ls.lessonType != 50 {
			continue
		}
		videoURL := ls.downloadURL
		if videoURL == "" {
			videoURL = y.resolveVideoURL(c, tgt, ls.id)
		}
		if videoURL == "" {
			continue
		}
		name := fmt.Sprintf("%02d %s", idx, sanitize(ls.title))
		idx++
		format := formatFromURL(videoURL, "mp4")
		stream := extractor.Stream{
			Quality: "shd",
			URLs:    []string{videoURL},
			Format:  format,
			Size:    ls.size * 1048576,
			Headers: youdaoHeaders(tgt.site),
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:    "youdao",
			Title:   name,
			Streams: map[string]extractor.Stream{format: stream},
		})
	}
	return entries
}

func (y *Youdao163) resolveVideoURL(c *util.Client, tgt youdaoTarget, vid string) string {
	if vid == "" {
		return ""
	}
	// Base Study163 flow: course/video.json -> result.videoUrl
	body, err := c.GetString(fmt.Sprintf(youdaoVideoURLFmt, tgt.site, vid), youdaoHeaders(tgt.site))
	if err == nil {
		var out struct {
			Result struct {
				VideoURL string `json:"videoUrl"`
			} `json:"result"`
		}
		if decodeJSON(body, &out) == nil && out.Result.VideoURL != "" {
			return out.Result.VideoURL
		}
	}
	// Youdao_Course fallback: getLessonInfo2.json -> data.video.downloadUrl
	if tgt.youdao {
		body, err := c.GetString(fmt.Sprintf(youdaoNewVideoURL, tgt.cid, vid), youdaoHeaders(tgt.site))
		if err == nil {
			var out struct {
				Data struct {
					Video struct {
						DownloadURL string `json:"downloadUrl"`
					} `json:"video"`
				} `json:"data"`
			}
			if decodeJSON(body, &out) == nil {
				return out.Data.Video.DownloadURL
			}
		}
	}
	return ""
}

func parseInt(s string) int {
	n := 0
	neg := false
	for i, r := range s {
		if i == 0 && r == '-' {
			neg = true
			continue
		}
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		return -n
	}
	return n
}

func parseInt64(s string) int64 { return int64(parseInt(s)) }

// ---------- Youdao course-list flow ----------

// extractYoudaoCourseList implements Youdao_Course._get_course_list: fetch
// the user's purchased courses from mycoursev3.json (active + expired) and
// return each as a sub-entry with the resolved course detail URL.
//
// Source: Youdao_Course._get_course_list
func extractYoudaoCourseList(c *util.Client) (*extractor.MediaInfo, error) {
	var courses []youdaoCourseItem
	// Source calls _get_course_list() (active) + _get_course_list('expire')
	for _, status := range []string{"", "expire"} {
		items, err := fetchYoudaoCourseList(c, status)
		if err != nil {
			return nil, fmt.Errorf("youdao mycoursev3 (status=%q): %w", status, err)
		}
		courses = append(courses, items...)
	}
	if len(courses) == 0 {
		return nil, fmt.Errorf("no youdao courses found (login cookies may be invalid)")
	}

	entries := make([]*extractor.MediaInfo, 0, len(courses))
	for _, ci := range courses {
		courseURL := fmt.Sprintf(youdaoCourseURLFmt, youdaoKeSite, ci.courseID)
		entries = append(entries, &extractor.MediaInfo{
			Site:  "youdao",
			Title: sanitize(ci.title),
			Extra: map[string]any{
				"course_id":  ci.courseID,
				"course_url": courseURL,
				"source_api": "mycoursev3",
			},
		})
	}

	return &extractor.MediaInfo{
		Site:    "youdao",
		Title:   "我的有道精品课",
		Entries: entries,
		Extra: map[string]any{
			"source_api":  "Youdao_Course._get_course_list",
			"total_count": len(courses),
		},
	}, nil
}

type youdaoCourseItem struct {
	courseID string
	title    string
}

// fetchYoudaoCourseList paginates mycoursev3.json for the given status
// ("" = active, "expire" = expired), up to 32 pages matching the source
// range(1, 32).
//
// Source: Youdao_Course._get_course_list
func fetchYoudaoCourseList(c *util.Client, status string) ([]youdaoCourseItem, error) {
	var all []youdaoCourseItem
	for page := 1; page < 32; page++ {
		reqURL := fmt.Sprintf(youdaoCourseListURL, status, strconv.Itoa(page))
		body, err := c.GetString(reqURL, youdaoHeaders(youdaoKeSite))
		if err != nil {
			return nil, err
		}

		var out struct {
			Data struct {
				Data []struct {
					CourseID    any    `json:"courseId"`
					CourseTitle string `json:"courseTitle"`
				} `json:"data"`
			} `json:"data"`
		}
		if err := decodeJSON(body, &out); err != nil {
			break
		}
		if len(out.Data.Data) == 0 {
			break
		}
		for _, r := range out.Data.Data {
			all = append(all, youdaoCourseItem{
				courseID: valueString(r.CourseID),
				title:   r.CourseTitle,
			})
		}
	}
	return all, nil
}
