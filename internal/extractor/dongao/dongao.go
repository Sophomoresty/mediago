// Package dongao implements the course.dongao.com catalog / lecture extractor.
package dongao

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer                 = "https://course.dongao.com/"
	origin                  = "https://course.dongao.com"
	member_center_url       = "https://my.dongao.com/"
	login_check_url         = "https://serveapi.dongao.com/search/memberExamSubjectSeasonListV2"
	member_service_url      = "https://serveapi.dongao.com/search/memberServeExamList"
	stage_probe_url         = "https://course.dongao.com/v4/liveAndCourseList"
	stage_list_url          = "https://course.dongao.com/v4/liveAndCourseList"
	detail_infos_url        = "https://course.dongao.com/v4/liveAndCourseDetailInfos"
	live_number_list_url    = "https://course.dongao.com/live/v4/liveNumberList"
	live_linked_lecture_url = "https://course.dongao.com/live/v4/linkedLectureCatalog"
	catalog_url             = "https://course.dongao.com/catalog/%s"
	lecture_url             = "https://course.dongao.com/lecture/%s"
	device_verify_referer   = "https://my.dongao.com/qrcode/deviceVerify?redirectUrl=https://course.dongao.com/progress"
	device_verify_origin    = "https://my.dongao.com"
	urlDeviceVerify         = "https://my.dongao.com/qrcode/deviceVerify"
)

var patterns = []string{`(?:[\w-]+\.)?dongao\.com/`}

func init() {
	extractor.Register(&Dongao{}, extractor.SiteInfo{Name: "Dongao", URL: "dongao.com", NeedAuth: true})
}

type Dongao struct{}

func (d *Dongao) Patterns() []string { return patterns }

var (
	lectureIDRe = regexp.MustCompile(`(?i)(?:lectureId|lecture_id|listenLectureId|liveLectureId)=([A-Za-z0-9_-]+)|/lecture/(\w+)`)
	courseIDRe  = regexp.MustCompile(`(?i)(?:courseId|course_id|productId|courseID)=([A-Za-z0-9_-]+)|/catalog/(\w+)`)
	titleRe     = regexp.MustCompile(`(?is)<h1[^>]*>([\s\S]*?)</h1>|<h2[^>]*>([\s\S]*?)</h2>|<title[^>]*>([\s\S]*?)</title>|(?:lectureName|courseName)\s*[:=]\s*["']([^"']+)`)
	hrefLecture = regexp.MustCompile(`(?i)/lecture/(\w+)`)
)

type requestIDs struct {
	CourseID  string
	LectureID string
	SSID      string
	SID       string
	Lecturer  string
}

type lectureNode struct {
	ID    string
	Title string
}

func (d *Dongao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("dongao requires login cookies")
	}
	ids := parseIDs(rawURL)
	if ids.CourseID == "" && ids.LectureID == "" && ids.SSID == "" && ids.SID == "" {
		return nil, fmt.Errorf("cannot parse dongao course/lecture id from URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := map[string]string{
		"Accept":  "application/json, text/plain, */*",
		"Referer": referer,
		"Origin":  origin,
	}

	if ids.LectureID != "" {
		entry, err := resolveLecture(c, headers, ids.LectureID, "dongao_"+ids.LectureID)
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	entries, title, err := resolveCourse(c, headers, ids)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("dongao: no playable lectures found for course %s", ids.CourseID)
	}
	return &extractor.MediaInfo{Site: "dongao", Title: util.SanitizeFilename(firstNonEmpty(title, "dongao_"+ids.CourseID)), Entries: entries}, nil
}

func resolveCourse(c *util.Client, headers map[string]string, ids requestIDs) ([]*extractor.MediaInfo, string, error) {
	var title string
	var nodes []lectureNode
	var direct []*extractor.MediaInfo

	if ids.CourseID != "" {
		body, err := c.GetString(fmt.Sprintf(catalog_url, url.PathEscape(ids.CourseID)), headers)
		if err == nil {
			title = firstNonEmpty(parseTitle(body), ids.CourseID)
			if media := findMediaInText(body); media != "" {
				direct = append(direct, mediaInfo(title, media, headers))
			}
			payload := parseJSONText(body)
			if payload != nil {
				nodes = append(nodes, collectLectureNodes(payload, title)...)
				title = firstNonEmpty(pickTitle(payload), title)
			}
			nodes = append(nodes, collectLectureLinks(body, title)...)
		}
	}

	apiPayloads := requestCourseAPIs(c, headers, ids)
	for _, payload := range apiPayloads {
		title = firstNonEmpty(pickTitle(payload), title)
		if media := findMediaURL(payload); media != "" {
			direct = append(direct, mediaInfo(firstNonEmpty(pickTitle(payload), title, ids.CourseID), media, headers))
		}
		nodes = append(nodes, collectLectureNodes(payload, title)...)
	}
	if len(direct) > 0 {
		return direct, title, nil
	}
	if len(nodes) == 0 {
		return nil, title, nil
	}

	seen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.ID == "" || seen[node.ID] {
			continue
		}
		seen[node.ID] = true
		entry, err := resolveLecture(c, headers, node.ID, node.Title)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	return entries, title, nil
}

func resolveLecture(c *util.Client, headers map[string]string, lectureID, fallbackTitle string) (*extractor.MediaInfo, error) {
	playURL := fmt.Sprintf(lecture_url, url.PathEscape(lectureID))
	h := cloneHeaders(headers)
	h["Referer"] = referer
	h["Origin"] = origin
	body, err := c.PostForm(playURL, map[string]string{"playerType": "h5"}, h)
	if err != nil || strings.TrimSpace(body) == "" {
		body, err = c.GetString(playURL, h)
	}
	if err != nil {
		return nil, fmt.Errorf("fetch dongao lecture page: %w", err)
	}
	title := util.SanitizeFilename(firstNonEmpty(parseTitle(body), fallbackTitle, lectureID))

	// Try direct media URL first (plaintext flow)
	if media := findMediaInText(body); media != "" {
		return mediaInfo(title, media, h), nil
	}

	// Encrypted flow: extract player fields, build signed m3u8
	fields := extractPlayerFields(body)
	if len(fields) > 0 {
		// Look for m3u8 URL in the lecture HTML
		m3u8URL := findMediaInTextWithExt(body, ".m3u8")
		if m3u8URL == "" {
			// Try extracting from mainSource/videoSource pattern
			m3u8URL = extractM3U8FromPlayerData(body)
		}
		if m3u8URL != "" {
			m3u8Headers := cloneHeaders(h)
			m3u8Headers["Accept"] = "application/vnd.apple.mpegurl, application/x-mpegURL, */*"
			m3u8Headers["X-Requested-With"] = "XMLHttpRequest"
			m3u8Text, err := c.GetString(m3u8URL, m3u8Headers)
			if err == nil && strings.Contains(m3u8Text, "#EXTM3U") {
				signedM3U8, aesKey, _ := buildSignedM3U8(m3u8Text, fields)
				stream := extractor.Stream{
					Quality: "best",
					URLs:    []string{m3u8URL},
					Format:  "m3u8",
					Headers: m3u8Headers,
				}
				if len(aesKey) > 0 {
					_ = hex.EncodeToString(aesKey) // key material available for future inline-m3u8 use
				}
				_ = signedM3U8 // available for inline-manifest download
				return &extractor.MediaInfo{
					Site:    "dongao",
					Title:   title,
					Streams: map[string]extractor.Stream{"best": stream},
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("dongao: no media source in lecture %s", lectureID)
}

func requestCourseAPIs(c *util.Client, headers map[string]string, ids requestIDs) []any {
	form := map[string]string{
		"lecturerId": ids.Lecturer,
		"ssid":       ids.SSID,
		"sid":        ids.SID,
		"courseId":   ids.CourseID,
	}
	apiHeaders := cloneHeaders(headers)
	apiHeaders["Referer"] = device_verify_referer
	apiHeaders["Origin"] = device_verify_origin
	var out []any
	for _, api := range []string{stage_list_url, detail_infos_url, live_number_list_url, live_linked_lecture_url, stage_probe_url} {
		body, err := c.PostForm(api, form, apiHeaders)
		if err != nil {
			continue
		}
		var payload any
		if json.Unmarshal([]byte(body), &payload) == nil {
			out = append(out, payload)
		}
	}
	return out
}

func parseIDs(raw string) requestIDs {
	out := requestIDs{}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		out.CourseID = firstNonEmpty(q.Get("courseId"), q.Get("course_id"), q.Get("productId"), q.Get("courseID"))
		out.LectureID = firstNonEmpty(q.Get("lectureId"), q.Get("lecture_id"), q.Get("listenLectureId"), q.Get("liveLectureId"))
		out.SSID = firstNonEmpty(q.Get("ssid"), q.Get("sSubjectId"), q.Get("seasonId"))
		out.SID = firstNonEmpty(q.Get("sid"), q.Get("subjectId"))
		out.Lecturer = q.Get("lecturerId")
	}
	out.CourseID = firstNonEmpty(out.CourseID, rx(courseIDRe, raw))
	out.LectureID = firstNonEmpty(out.LectureID, rx(lectureIDRe, raw))
	return out
}

func parseJSONText(text string) any {
	trim := strings.TrimSpace(text)
	var payload any
	if strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[") {
		if json.Unmarshal([]byte(trim), &payload) == nil {
			return payload
		}
	}
	for _, candidate := range extractJSONObjects(text) {
		if json.Unmarshal([]byte(candidate), &payload) == nil {
			return payload
		}
	}
	return nil
}

func collectLectureLinks(text, title string) []lectureNode {
	seen := map[string]bool{}
	var out []lectureNode
	for _, m := range hrefLecture.FindAllStringSubmatch(text, -1) {
		if m[1] == "" || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, lectureNode{ID: m[1], Title: title})
	}
	return out
}

func parseTitle(text string) string {
	for _, m := range titleRe.FindAllStringSubmatch(text, -1) {
		for i := 1; i < len(m); i++ {
			if s := cleanText(m[i]); s != "" && s != "登录" && s != "学员验证" {
				return s
			}
		}
	}
	return ""
}

func rx(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}

func cleanText(s string) string {
	s = html.UnescapeString(s)
	s = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`[-_｜|]?\s*东奥.*$`).ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// findMediaInTextWithExt scans text for URLs ending with the given extension.
func findMediaInTextWithExt(text, ext string) string {
	urlRe := regexp.MustCompile(`https?://[^\s"'<>]+` + regexp.QuoteMeta(ext) + `[^\s"'<>]*`)
	if m := urlRe.FindString(text); m != "" {
		return m
	}
	return ""
}

// extractM3U8FromPlayerData looks for m3u8 URLs in player JSON or data attributes.
func extractM3U8FromPlayerData(text string) string {
	// Common patterns: "playUrl":"https://...m3u8", mainSource:"...", videoSource:"..."
	for _, pat := range []string{
		`(?i)(?:playUrl|mainSource|videoSource|url)["\s:=]*["'](https?://[^"']+\.m3u8[^"']*)`,
		`(?i)(?:source|src)["\s:=]*["'](https?://[^"']+\.m3u8[^"']*)`,
	} {
		re := regexp.MustCompile(pat)
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}
