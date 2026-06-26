// Package sanjieke implements an extractor for sanjieke.cn (三节课) courses.
package sanjieke

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

// Endpoints from decompiled Mooc/Courses/Sanjieke/:
const (
	urlReferer         = "https://study.sanjieke.cn/"
	urlOrigin          = "https://study.sanjieke.cn"
	urlClassroom       = "https://classroom.sanjieke.cn/my_course"
	urlClassroomOrigin = "https://classroom.sanjieke.cn"
	urlCourseList      = "https://service.sanjieke.cn/classroom/not_expired"
	urlCourseCatalog   = "https://web-api.sanjieke.cn/b-side/api/web/course/list"
	urlUserInfo        = "https://service.sanjieke.cn/user/info"
	urlStudyAPIRoot    = "https://web-api.sanjieke.cn/b-side/api/web/study/%s/%s"
	urlStudyInfo       = urlStudyAPIRoot + "/info"
	urlTree            = urlStudyAPIRoot + "/content/tree"
	urlSection         = urlStudyAPIRoot + "/content/%s"
	urlAttachmentList  = urlStudyAPIRoot + "/attachment/list"
	urlStudyPage       = "https://study.sanjieke.cn/course/%s/%s"
	urlVideoAuth       = "https://service.sanjieke.cn/video/master/auth"
	urlPublicProduct   = "https://www.sanjieke.cn/course/detail/sjk/%s"
	apiKey             = "cDpJh7SuWGFZCFfSjvByc34PNSBrNVrB"
	domainPrefix       = "cos"
	browserUA          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
)

var patterns = []string{`(?:[\w-]+\.)?sanjieke\.cn/`}

func init() {
	extractor.Register(&Sanjieke{}, extractor.SiteInfo{Name: "Sanjieke", URL: "sanjieke.cn", NeedAuth: true})
}

type Sanjieke struct{}

func (s *Sanjieke) Patterns() []string { return patterns }

type courseKey struct{ classID, courseID, projectID string }

type courseListResp struct {
	Code int `json:"code"`
	Data struct {
		List []courseItem `json:"list"`
	} `json:"data"`
}

type courseItem struct {
	ClassID     any    `json:"class_id"`
	CourseID    any    `json:"course_id"`
	StudyCourse any    `json:"study_course_id"`
	ProjectID   any    `json:"project_id"`
	ProjectID2  any    `json:"projectId"`
	StudyingURL string `json:"studying_url"`
}

type infoResp struct {
	Code int `json:"code"`
	Data struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	} `json:"data"`
}

type treeResp struct {
	Code int `json:"code"`
	Data struct {
		Tree     []node `json:"tree"`
		Nodes    []node `json:"nodes"`
		Children []node `json:"children"`
	} `json:"data"`
}

type contentResp struct {
	Code int `json:"code"`
	Data struct {
		Nodes        []node       `json:"nodes"`
		Children     []node       `json:"children"`
		VideoContent videoContent `json:"videoContent"`
	} `json:"data"`
}

type node struct {
	NodeID   any    `json:"nodeId"`
	ID       any    `json:"id"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Children []node `json:"children"`
}

type videoContent struct {
	ContentID any        `json:"contentId"`
	URL       string     `json:"url"`
	Items     []videoURL `json:"items"`
	Ratios    []videoURL `json:"resolutionRatioObjList"`
}

type videoURL struct {
	URL string `json:"url"`
}

func (s *Sanjieke) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("sanjieke requires login cookies")
	}
	key := parseCourseKey(rawURL)
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	if key.courseID == "" {
		found, err := fetchCourseList(c, opts.Cookies, key)
		if err == nil {
			key = found
		}
	}
	if key.courseID == "" {
		return nil, fmt.Errorf("cannot parse sanjieke course_id from URL: %s", rawURL)
	}
	if key.projectID == "" {
		key.projectID = "0"
	}

	h := studyHeaders(opts.Cookies, fmt.Sprintf(urlStudyPage, key.projectID, key.courseID))
	title := "sanjieke_" + key.courseID
	if body, err := c.GetString(fmt.Sprintf(urlStudyInfo, key.projectID, key.courseID), h); err == nil {
		var info infoResp
		if json.Unmarshal([]byte(body), &info) == nil && info.Code == 200 {
			title = firstNonEmpty(info.Data.Title, info.Data.Name, title)
		}
	}
	body, err := c.GetString(fmt.Sprintf(urlTree, key.projectID, key.courseID), h)
	if err != nil {
		return nil, fmt.Errorf("sanjieke content/tree: %w", err)
	}
	var tree treeResp
	if err := json.Unmarshal([]byte(body), &tree); err != nil {
		return nil, fmt.Errorf("sanjieke parse content/tree: %w", err)
	}
	if tree.Code != 200 {
		return nil, fmt.Errorf("sanjieke content/tree code=%d", tree.Code)
	}
	entries := collectEntries(c, opts.Cookies, key, append(append([]node{}, tree.Data.Tree...), append(tree.Data.Nodes, tree.Data.Children...)...), nil)
	if len(entries) == 0 {
		return nil, fmt.Errorf("sanjieke: no playable video nodes in course tree")
	}
	return &extractor.MediaInfo{Site: "sanjieke", Title: title, Entries: entries}, nil
}

func fetchCourseList(c *util.Client, jar http.CookieJar, want courseKey) (courseKey, error) {
	apiURL := urlCourseList + "?teacherId=&keyword=&sortDirection=&sortField=lastStudyAt&tab=all&page=1&limit=12"
	body, err := c.GetString(apiURL, classroomHeaders(jar))
	if err != nil {
		return courseKey{}, err
	}
	var resp courseListResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return courseKey{}, err
	}
	if resp.Code != 200 {
		return courseKey{}, fmt.Errorf("course list code=%d", resp.Code)
	}
	for _, item := range resp.Data.List {
		classID := anyString(item.ClassID)
		courseID := firstNonEmpty(anyString(item.CourseID), anyString(item.StudyCourse))
		if want.classID != "" && classID != want.classID {
			continue
		}
		if want.courseID != "" && courseID != want.courseID {
			continue
		}
		projectID := firstNonEmpty(anyString(item.ProjectID), anyString(item.ProjectID2), extractProjectID(item.StudyingURL), "0")
		if courseID != "" {
			return courseKey{classID: classID, courseID: courseID, projectID: projectID}, nil
		}
	}
	return courseKey{}, fmt.Errorf("course list has no matching sanjieke course")
}

func collectEntries(c *util.Client, jar http.CookieJar, key courseKey, nodes []node, prefix []string) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	for _, n := range nodes {
		name := firstNonEmpty(n.Name, n.Title)
		nextPrefix := append(append([]string{}, prefix...), name)
		nodeID := firstNonEmpty(anyString(n.NodeID), anyString(n.ID))
		if nodeID != "" {
			entries = append(entries, entriesFromContent(c, jar, key, nodeID, nextPrefix)...)
		}
		if len(n.Children) > 0 {
			entries = append(entries, collectEntries(c, jar, key, n.Children, nextPrefix)...)
		}
	}
	return entries
}

func entriesFromContent(c *util.Client, jar http.CookieJar, key courseKey, nodeID string, prefix []string) []*extractor.MediaInfo {
	referer := buildPageReferer(key, nodeID)
	body, err := c.GetString(fmt.Sprintf(urlSection, key.projectID, key.courseID, nodeID), studyHeaders(jar, referer))
	if err != nil {
		return nil
	}
	var resp contentResp
	if json.Unmarshal([]byte(body), &resp) != nil || resp.Code != 200 {
		return nil
	}
	var entries []*extractor.MediaInfo
	vc := resp.Data.VideoContent
	videoID := anyString(vc.ContentID)
	videoURL := pickVideoURL(vc)
	if videoURL == "" && videoID != "" {
		videoURL = authVideoURL(c, jar, videoID, referer)
	}
	if videoURL != "" {
		title := firstNonEmpty(strings.Join(nonEmpty(prefix), " / "), "sanjieke_"+nodeID)
		extra := map[string]any{"node_id": nodeID, "video_id": videoID, "referer": referer}
		if m3u8 := fetchMediaM3U8(c, jar, videoURL, referer); m3u8 != "" {
			extra["m3u8_text"] = m3u8
		}
		entries = append(entries, &extractor.MediaInfo{Site: "sanjieke", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{videoURL}, Format: pickFormat(videoURL), Headers: map[string]string{"Referer": referer}}}, Extra: extra})
	}
	children := append(append([]node{}, resp.Data.Nodes...), resp.Data.Children...)
	entries = append(entries, collectEntries(c, jar, key, children, prefix)...)
	return entries
}

func authVideoURL(c *util.Client, jar http.CookieJar, videoID, referer string) string {
	apiURL := urlVideoAuth + "?cid=" + url.QueryEscape(videoID)
	body, err := c.GetString(apiURL, studyHeaders(jar, referer))
	if err != nil {
		return ""
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil || resp.Code != 200 {
		return ""
	}
	return strings.TrimSpace(resp.Data.URL)
}

func fetchMediaM3U8(c *util.Client, jar http.CookieJar, mediaURL, referer string) string {
	if !strings.HasPrefix(mediaURL, "https://service.sanjieke.cn/video/media/") {
		return ""
	}
	body, err := c.GetString(mediaURL, mediaHeaders(jar, referer))
	if err != nil {
		return ""
	}
	var resp struct {
		Status   any               `json:"status"`
		M3U8Text string            `json:"m3u8Text"`
		KeyPairs map[string]string `json:"keyPairs"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ""
	}
	return resp.M3U8Text
}

func pickVideoURL(vc videoContent) string {
	for _, v := range append(vc.Ratios, vc.Items...) {
		if strings.TrimSpace(v.URL) != "" {
			return strings.TrimSpace(v.URL)
		}
	}
	return strings.TrimSpace(vc.URL)
}

var (
	classViewRe = regexp.MustCompile(`/course/view/cid/(\d+)/course_id/(\d+)`)
	studyRe     = regexp.MustCompile(`study\.sanjieke\.cn/(?:course|study)/(\d+)/(\d+)`)
)

func parseCourseKey(raw string) courseKey {
	var out courseKey
	if m := classViewRe.FindStringSubmatch(raw); len(m) > 2 {
		out.classID, out.courseID = m[1], m[2]
	}
	if m := studyRe.FindStringSubmatch(raw); len(m) > 2 {
		out.projectID, out.courseID = m[1], m[2]
	}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		out.classID = firstNonEmpty(out.classID, q.Get("cid"))
		out.courseID = firstNonEmpty(out.courseID, q.Get("course_id"), q.Get("courseId"))
		out.projectID = firstNonEmpty(out.projectID, q.Get("project_id"), q.Get("projectId"))
	}
	if out.projectID == "" {
		out.projectID = "0"
	}
	return out
}

func buildPageReferer(key courseKey, nodeID string) string {
	base := fmt.Sprintf(urlStudyPage, key.projectID, key.courseID)
	if nodeID != "" {
		return base + "/" + nodeID
	}
	return base
}
func studyHeaders(jar http.CookieJar, referer string) map[string]string {
	return authHeaders(jar, referer, urlOrigin)
}
func classroomHeaders(jar http.CookieJar) map[string]string {
	return authHeaders(jar, urlClassroom, urlClassroomOrigin)
}
func mediaHeaders(jar http.CookieJar, referer string) map[string]string {
	h := authHeaders(jar, referer, urlOrigin)
	h["Accept"] = "*/*"
	h["Sec-Fetch-Dest"] = "empty"
	h["Sec-Fetch-Mode"] = "cors"
	h["Sec-Fetch-Site"] = "same-site"
	return h
}

func authHeaders(jar http.CookieJar, referer, origin string) map[string]string {
	cookie := cookieHeader(jar, urlReferer, urlClassroom, urlUserInfo)
	h := map[string]string{"x-domain-prefix": domainPrefix, "sjk-apikey": apiKey, "User-Agent": browserUA, "X-Requested-With": "XMLHttpRequest", "Accept": "application/json, text/plain, */*", "Origin": origin, "Referer": referer, "cookie": cookie}
	if tok := sjkToken(cookie); tok != "" {
		h["Authorization"] = "Bearer " + tok
	}
	return h
}

func cookieHeader(jar http.CookieJar, rawURLs ...string) string {
	seen, vals := map[string]bool{}, []string{}
	for _, raw := range rawURLs {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				if !seen[c.Name] {
					seen[c.Name] = true
					vals = append(vals, c.Name+"="+c.Value)
				}
			}
		}
	}
	return strings.Join(vals, "; ")
}

func sjkToken(cookie string) string {
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && (kv[0] == "sjk_token" || kv[0] == "_sjk_jwt") {
			return strings.TrimSpace(kv[1])
		}
	}
	return ""
}
func extractProjectID(raw string) string {
	if m := regexp.MustCompile(`study\.sanjieke\.cn/(?:course|study)/(\d+)/\d+`).FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}
func anyString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func nonEmpty(vals []string) []string {
	out := vals[:0]
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
