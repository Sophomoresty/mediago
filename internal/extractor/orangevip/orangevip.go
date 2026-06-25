// Package orangevip implements an extractor for orangevip.com courses.
package orangevip

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	referer        = "https://www.orangevip.com"
	userinfo_url   = "https://u.api.orangevip.com/Api/Index/getUserInfo"
	course_url     = "https://clapp.orangevip.com/otm/web/course/list"
	info_url       = "https://clapp.orangevip.com/otm/web/course/query/coursePeriod"
	video_play_url = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id=%s&token=%s&use_encrypt=0&render=jsonp"
	live_play_url  = "https://www.baijiayun.com/vod/video/getPlayUrl?vid=%s&render=jsonp&token=%s&use_encrypt=0"
	file_url       = "https://clapp.orangevip.com/otm/web/student/myCourseModelFile"
	price_url      = "https://www.orangevip.com/coursedetail/%s.html"
	token_url      = "https://clapp.orangevip.com/otm/web/course/v2/reviewPlayInfo"
	order_url      = "https://clapp.orangevip.com/otm/web/order/orderList"
)

var patterns = []string{`(?:[\w-]+\.)?orangevip\.com/`}

func init() {
	extractor.Register(&Orangevip{}, extractor.SiteInfo{Name: "Orangevip", URL: "orangevip.com", NeedAuth: true})
}

type Orangevip struct{}

func (s *Orangevip) Patterns() []string { return patterns }

type lesson struct{ VideoID, RoomID, LiveID, Name string }
type course struct{ ID, Title string }

type apiResp struct {
	CourseList        []map[string]any `json:"courseList"`
	CourseChapterList []map[string]any `json:"courseChapterList"`
	ChapterClass      []map[string]any `json:"chapterClass"`
	Data              any              `json:"data"`
	Files             []map[string]any `json:"files"`
	Orders            []map[string]any `json:"orders"`
}

var cidRe = regexp.MustCompile(`orangevip\.com/(?:clock/(\d+)|(?:my)?[cC]ourse[dD]etail/(\d+)|playcheckbjy/[^?]+/\?[^#]*?[cC]ourse[Ii]d=(\d+))`)

// errnoRe mirrors Orangevip_Base._check_cookie: a valid session returns
// `"errno":0` from getUserInfo. Source line: re.search('"errno"\\s*:\\s*0', resp).
var errnoRe = regexp.MustCompile(`"errno"\s*:\s*0`)

// checkCookie validates the session against u.api.orangevip.com/Api/Index/getUserInfo,
// matching Orangevip_Base._check_cookie. The cookie jar attaches the .orangevip.com
// cookies; a logged-in session yields `"errno":0`.
func checkCookie(c *util.Client, h map[string]string) bool {
	body, err := c.GetString(userinfo_url, h)
	if err != nil {
		return false
	}
	return errnoRe.MatchString(body)
}

func (s *Orangevip) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("orangevip requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := headers()
	if !checkCookie(c, h) {
		return nil, fmt.Errorf("orangevip: cookie validation failed (getUserInfo did not return \"errno\":0); login required")
	}
	cid := parseCID(rawURL)
	courses, _ := fetchCourses(c, h)
	if cid == "" && len(courses) > 0 {
		cid = courses[0].ID
	}
	if cid == "" {
		return nil, fmt.Errorf("orangevip: cannot parse courseModelId from URL and course list is empty")
	}
	title := courseTitle(courses, cid)
	if title == "" {
		title = pageTitle(c, h, cid)
	}
	if title == "" {
		title = "orangevip_" + cid
	}
	chapters, err := fetchCourseInfo(c, h, cid)
	if err != nil {
		return nil, fmt.Errorf("orangevip coursePeriod: %w", err)
	}
	lessons := parseLessons(chapters)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("orangevip: no coursePeriodList lessons in courseChapterList")
	}
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, le := range lessons {
		token := fetchToken(c, h, cid, le.VideoID)
		if token == "" {
			continue
		}
		videoURL, audioURL := resolveBaijiayun(c, h, le, token)
		if videoURL == "" || seen[videoURL] {
			continue
		}
		seen[videoURL] = true
		st := extractor.Stream{Quality: "best", URLs: []string{videoURL}, Format: formatOf(videoURL), AudioURL: audioURL, Headers: h}
		entries = append(entries, &extractor.MediaInfo{Site: "orangevip", Title: clean(fmt.Sprintf("[%d]--%s", i+1, first(le.Name, le.VideoID, le.LiveID))), Streams: map[string]extractor.Stream{"best": st}, Extra: map[string]any{"period_id": le.VideoID, "room_id": le.RoomID, "live_id": le.LiveID}})
	}
	files := fetchFiles(c, h, cid, "", 0)
	entries = append(entries, fileEntries(files, h)...)
	if len(entries) == 0 {
		return nil, fmt.Errorf("orangevip: no playable Baijiayun videos or courseware files found")
	}
	return &extractor.MediaInfo{Site: "orangevip", Title: clean(title), Entries: entries, Extra: map[string]any{"course_id": cid}}, nil
}

func fetchCourses(c *util.Client, h map[string]string) ([]course, error) {
	var out []course
	for p := 1; p < 10; p++ {
		body, err := c.PostForm(course_url, map[string]string{"showCount": "99", "currentPageForApp": fmt.Sprint(p)}, h)
		if err != nil {
			return out, err
		}
		var resp apiResp
		if json.Unmarshal([]byte(body), &resp) != nil || len(resp.CourseList) == 0 {
			break
		}
		for _, it := range resp.CourseList {
			if truthy(it["isExpire"]) || fmt.Sprint(it["totalCount"]) == "0" {
				continue
			}
			id := firstText(it, "guid", "courseModelId")
			if id != "" {
				out = append(out, course{ID: id, Title: firstText(it, "courseName", "title", "name")})
			}
		}
	}
	return out, nil
}

func fetchCourseInfo(c *util.Client, h map[string]string, cid string) ([]map[string]any, error) {
	body, err := c.PostForm(info_url, map[string]string{"courseModelId": cid}, h)
	if err != nil {
		return nil, err
	}
	var resp apiResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	return resp.CourseChapterList, nil
}

func parseLessons(chapters []map[string]any) []lesson {
	var out []lesson
	for ci, ch := range chapters {
		list, _ := ch["coursePeriodList"].([]any)
		for li, raw := range list {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			periodID := firstText(m, "guid")
			if periodID == "" {
				continue
			}
			name := clean(fmt.Sprintf("[%d.%d]--%s", ci+1, li+1, firstText(m, "coursePeriodTitle", "title", "name")))
			out = append(out, lesson{VideoID: periodID, RoomID: firstText(m, "roomId", "room_id"), LiveID: firstText(m, "videoId", "live_id"), Name: name})
		}
	}
	return out
}

func fetchToken(c *util.Client, h map[string]string, cid, periodID string) string {
	body, err := c.PostForm(token_url, map[string]string{"clientType": "1", "periodId": periodID, "courseId": cid}, h)
	if err != nil {
		return ""
	}
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return ""
	}
	return findFirst(v, "token")
}

func resolveBaijiayun(c *util.Client, h map[string]string, le lesson, token string) (string, string) {
	var videoURL string
	if le.RoomID != "" {
		if u, err := shared.BaijiayunResolvePlayback(c, le.RoomID, token, h); err == nil {
			videoURL = u
		}
	}
	if le.LiveID != "" {
		if u, err := shared.BaijiayunResolveVOD(c, le.LiveID, token, h); err == nil && u != "" {
			videoURL = u
		}
	}
	if videoURL == "" && le.RoomID != "" {
		body, err := c.GetString(fmt.Sprintf(video_play_url, url.QueryEscape(le.RoomID), url.QueryEscape(token)), h)
		if err == nil {
			videoURL = findFirstJSONP(body, "video_url", "url", "playback_url")
		}
	}
	return normalizeURL(videoURL), ""
}

// courseFile mirrors the dict produced by Orangevip_Course._get_file_list:
// {file_url, file_name, file_fmt, file_type, file_id}. file_type is the raw
// isFolder value; the source treats file_type == "1" as a folder.
type courseFile struct {
	URL, Name, Fmt, Type, ID string
}

// fetchFiles walks the courseware tree like Orangevip_Course._download_files:
// POST myCourseModelFile with {courseModelId, pguid}; for each node, file_type
// == "1" means a folder and we recurse with its file_id as the new pguid,
// otherwise it is a downloadable file. depth bounds runaway recursion.
func fetchFiles(c *util.Client, h map[string]string, cid, parentID string, depth int) []courseFile {
	if depth > 16 {
		return nil
	}
	body, err := c.PostForm(file_url, map[string]string{"courseModelId": cid, "pguid": parentID}, h)
	if err != nil {
		return nil
	}
	var resp apiResp
	if json.Unmarshal([]byte(body), &resp) != nil {
		return nil
	}
	var out []courseFile
	for _, f := range resp.Files {
		// _get_file_list: file_type stores the raw isFolder value; file_name guard.
		name := firstText(f, "fileName", "name", "title")
		if name == "" {
			continue
		}
		netURL := firstText(f, "netUrl", "file_url", "url")
		fileType := firstText(f, "isFolder", "file_type")
		fileID := firstText(f, "guid", "file_id")
		// _download_inner_files: file_type == "1" -> folder, recurse on file_id.
		if fileType == "1" {
			out = append(out, fetchFiles(c, h, cid, fileID, depth+1)...)
			continue
		}
		if netURL == "" {
			continue
		}
		out = append(out, courseFile{
			URL:  normalizeURL(netURL),
			Name: name,
			Fmt:  fileFmt(name, netURL),
			Type: fileType,
			ID:   fileID,
		})
	}
	return out
}

// fileFmt mirrors _get_file_list's format derivation: prefer the extension from
// the file name (rsplit('.', 1) when it yields two parts), else from the URL
// path before '?'. Lowercased.
func fileFmt(name, netURL string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && i < len(name)-1 {
		return strings.ToLower(name[i+1:])
	}
	if netURL != "" {
		path := strings.Split(netURL, "?")[0]
		if i := strings.LastIndex(path, "."); i >= 0 && i < len(path)-1 {
			return strings.ToLower(path[i+1:])
		}
	}
	return ""
}

// fileEntries turns courseware files into downloadable MediaInfo entries.
// _download_one_file routes by file_fmt (mp4 -> video, pdf/ppt/doc/... ->
// their download helper, fallback attach); each maps to a single-stream entry.
func fileEntries(files []courseFile, h map[string]string) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for _, f := range files {
		if f.URL == "" {
			continue
		}
		st := extractor.Stream{
			Quality: "file",
			URLs:    []string{f.URL},
			Format:  first(f.Fmt, formatOf(f.URL)),
			Headers: h,
		}
		out = append(out, &extractor.MediaInfo{
			Site:    "orangevip",
			Title:   clean(f.Name),
			Streams: map[string]extractor.Stream{"file": st},
			Extra:   map[string]any{"file_id": f.ID, "file_fmt": f.Fmt, "kind": "courseware"},
		})
	}
	return out
}

func parseCID(rawURL string) string {
	if m := cidRe.FindStringSubmatch(rawURL); m != nil {
		return first(m[1], m[2], m[3])
	}
	u, err := url.Parse(rawURL)
	if err == nil {
		return first(u.Query().Get("courseId"), u.Query().Get("courseid"), u.Query().Get("cid"), u.Query().Get("id"))
	}
	return ""
}

func pageTitle(c *util.Client, h map[string]string, cid string) string {
	body, err := c.GetString(fmt.Sprintf(price_url, url.PathEscape(cid)), h)
	if err != nil {
		return ""
	}
	return first(regexGroup(body, `"courseName"\s*:\s*"([^"]+)"`), regexGroup(body, `<title>(.*?)</title>`))
}

func headers() map[string]string {
	return map[string]string{"Referer": referer, "Origin": referer, "Accept": "application/json, text/plain, */*"}
}
func courseTitle(list []course, cid string) string {
	for _, c := range list {
		if c.ID == cid {
			return c.Title
		}
	}
	return ""
}
func firstText(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
func findFirst(v any, keys ...string) string {
	out := ""
	walk(v, func(m map[string]any) {
		if out == "" {
			out = firstText(m, keys...)
		}
	})
	return out
}
func walk(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, x := range t {
			walk(x, fn)
		}
	case []any:
		for _, x := range t {
			walk(x, fn)
		}
	}
}
func findFirstJSONP(text string, keys ...string) string {
	var v any
	body := strings.TrimSpace(text)
	if i := strings.Index(body, "("); i >= 0 && strings.HasSuffix(strings.TrimSuffix(body, ";"), ")") {
		body = body[i+1 : strings.LastIndex(body, ")")]
	}
	if json.Unmarshal([]byte(body), &v) != nil {
		return ""
	}
	return findFirst(v, keys...)
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func clean(s string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s), " .")
}
func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
func formatOf(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
func truthy(v any) bool {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	return s == "1" || s == "true" || s == "yes"
}
func regexGroup(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
