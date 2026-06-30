// Package xsteach implements an extractor for xsteach.com courses.
package xsteach

import (
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL          = "https://www.xsteach.com/"
	originURL           = "https://www.xsteach.com"
	loginCheckURL       = "https://www.xsteach.com/api/user/my-course/list-v3"
	courseListURL       = "https://www.xsteach.com/api/user/my-course/list-v3"
	courseComboboxURL   = "https://www.xsteach.com/api/common/my-course-combobox"
	courseDetailURL     = "https://www.xsteach.com/api/course/course-detail"
	periodURL           = "https://www.xsteach.com/api/course/period"
	periodPlayListURL   = "https://www.xsteach.com/api/period/get-period-list"
	videoPlayURL        = "https://www.xsteach.com/api/vod/period/play"
	teachCoachPlayURL   = "https://www.xsteach.com/api/vod/teach-coach/play"
	livePlayURL         = "https://www.xsteach.com/api/live/enter/play"
	qcloudPlayAPI       = "https://playvideo.qcloud.com/getplayinfo/v4/{}/{}"
	defaultUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
	xsteachRSAPublicKey = "-----BEGIN PUBLIC KEY-----\nMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQC3pDA7GTxOvNbXRGMi9QSIzQEI\n+EMD1HcUPJSQSFuRkZkWo4VQECuPRg/xVjqwX1yUrHUvGQJsBwTS/6LIcQiSwYsO\nqf+8TWxGQOJyW46gPPQVzTjNTiUoq435QB0v11lNxvKWBQIZLmacUZ2r1APta7i/\nMY4Lx9XlZVMZNUdUywIDAQAB\n-----END PUBLIC KEY-----"
)

var patterns = []string{`(?:[\w-]+\.)?xsteach\.com/`}
var idRe = regexp.MustCompile(`(?i)xsteach\.com/course/(?:video|live)/(?:play/)?(\d+)|xsteach\.com/course/(?:detail/)?(\d+)|[?&](?:courseId|id)=(\d+)`)

func init() {
	extractor.Register(&Xsteach{}, extractor.SiteInfo{Name: "Xsteach", URL: "xsteach.com", NeedAuth: true})
}

type Xsteach struct{}

func (s *Xsteach) Patterns() []string { return patterns }

type xsCourse struct{ id, title, classScheduleID, lectureType string }
type xsVideo struct {
	periodID, teachCoachID, title, lectureType string
	raw                                        map[string]any
}

func (s *Xsteach) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xsteach requires login cookies")
	}
	onlyFiles := xsteachOnlyFilesMode(opts.Quality)
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := headers(opts.Cookies)
	if !strings.Contains(h["Cookie"], "xsteachID=") {
		return nil, fmt.Errorf("xsteach requires xsteachID cookie")
	}
	loginRoot, err := apiGet(c, loginCheckURL, map[string]string{"size": "1", "over": "1"}, h)
	if err != nil {
		return nil, fmt.Errorf("xsteach login check: %w", err)
	}
	if !codeIs(loginRoot["code"], 1) || loginRoot["body"] == nil {
		return nil, fmt.Errorf("xsteach login check rejected cookie")
	}
	cid := firstMatch(idRe, rawURL)
	courses, _ := fetchCourses(c, h)
	course := selectCourse(courses, cid)
	if course.id == "" && cid != "" {
		course = xsCourse{id: cid, title: "xsteach_" + cid}
	}
	if course.id == "" {
		return nil, fmt.Errorf("xsteach course %q not found in purchased course list", cid)
	}
	if detail := courseDetail(c, h, course.id); detail != nil {
		course.title = firstNonEmpty(val(detail, "name"), val(detail, "courseName"), val(detail, "title"), course.title)
		course.classScheduleID = firstNonEmpty(course.classScheduleID, val(detail, "classScheduleId"), val(detail, "scheduleId"))
		course.lectureType = firstNonEmpty(course.lectureType, val(detail, "lectureType"), val(detail, "lecture_type"))
	}
	periods := fetchPeriods(c, h, course)
	if len(periods) == 0 {
		return nil, fmt.Errorf("xsteach period list is empty for course %s", course.id)
	}
	entries, seen := []*extractor.MediaInfo{}, map[string]bool{}
	for _, p := range periods {
		for _, fi := range filesFromPeriod(p, course) {
			if fi.url == "" || seen[fi.url] || (onlyFiles && xsteachIsVideoFile(fi)) {
				continue
			}
			seen[fi.url] = true
			entries = append(entries, fileMedia(fi))
		}
		if onlyFiles {
			continue
		}
		for _, vi := range videosFromPeriod(p, course) {
			for _, src := range resolveVideoSources(c, h, vi, opts.Quality) {
				if src.URL == "" || seen[src.URL] {
					continue
				}
				seen[src.URL] = true
				entries = append(entries, mediaSource(firstNonEmpty(vi.title, "period_"+vi.periodID), src, vi))
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("xsteach: no playable qcloud/direct media URL resolved")
	}
	return &extractor.MediaInfo{Site: "xsteach", Title: firstNonEmpty(course.title, "xsteach_"+course.id), Entries: entries}, nil
}

func xsteachOnlyFilesMode(quality string) bool {
	mode := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(quality)))
	switch mode {
	case "4", "pdf", "onlypdf", "file", "files", "material", "materials", "courseware", "coursewares", "attachment", "attachments", "资料", "课件":
		return true
	default:
		return false
	}
}

func xsteachTargetQualityRank(quality string) int {
	mode := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(quality)))
	switch mode {
	case "3", "sd", "480", "480p", "standard", "标清":
		return 480
	case "2", "hd", "720", "720p", "高清":
		return 720
	case "1", "fhd", "fullhd", "1080", "1080p", "shd", "superhd", "超清", "蓝光":
		return 1080
	default:
		return 1080
	}
}

func xsteachIsVideoFile(f xsFile) bool {
	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(firstNonEmpty(f.format, fileFormat("", f.url)))), ".")
	switch format {
	case "mp4", "m3u8", "flv", "m4v", "mov", "avi", "wmv", "mkv", "webm":
		return true
	default:
		return false
	}
}

func fetchCourses(c *util.Client, h map[string]string) ([]xsCourse, error) {
	out, seen := []xsCourse{}, map[string]int{}
	for _, spec := range []struct {
		api    string
		params map[string]string
		lists  []string
	}{
		{courseComboboxURL, map[string]string{"over": "1"}, []string{"body"}},
		{courseListURL, map[string]string{"size": "100", "over": "1"}, []string{"records", "list", "body"}},
	} {
		root, err := apiGet(c, spec.api, spec.params, h)
		if err != nil {
			continue
		}
		for _, m := range courseMaps(root, spec.lists) {
			co := normalizeCourse(m, nil)
			if co.id == "" {
				continue
			}
			if i, ok := seen[co.id]; ok {
				out[i] = mergeCourse(out[i], co)
				continue
			}
			seen[co.id] = len(out)
			out = append(out, co)
		}
	}
	return out, nil
}

func courseDetail(c *util.Client, h map[string]string, cid string) map[string]any {
	root, err := apiGet(c, courseDetailURL, map[string]string{"id": cid}, h)
	if err != nil || !codeIs(root["code"], 1) {
		return nil
	}
	if m, ok := root["body"].(map[string]any); ok {
		return m
	}
	return nil
}

func fetchPeriods(c *util.Client, h map[string]string, co xsCourse) []map[string]any {
	base := periodBody(c, h, co.id, "")
	groups := []map[string]any{}
	schedules := listUnder(base, "schedules")
	if len(schedules) == 0 {
		groups = append(groups, periodsFromBody(base, co.classScheduleID, nil)...)
	} else {
		sort.SliceStable(schedules, func(i, j int) bool {
			return number(val(schedules[i], "scheduleSeq")) > number(val(schedules[j], "scheduleSeq"))
		})
		for _, sch := range schedules {
			sid := firstNonEmpty(val(sch, "id"), val(sch, "classScheduleId"), val(sch, "scheduleId"))
			if sid == "" {
				continue
			}
			groups = append(groups, periodsFromBody(periodBody(c, h, co.id, sid), sid, sch)...)
		}
	}
	out := []map[string]any{}
	for _, p := range groups {
		out = append(out, enrichWithPlayList(c, h, p)...)
	}
	return uniquePeriods(out)
}

func periodBody(c *util.Client, h map[string]string, cid, scheduleID string) map[string]any {
	params := map[string]string{"id": cid}
	if scheduleID != "" {
		params["classScheduleId"] = scheduleID
	}
	root, err := apiGet(c, periodURL, params, h)
	if err != nil || !codeIs(root["code"], 1) {
		return nil
	}
	if m, ok := root["body"].(map[string]any); ok {
		return m
	}
	return nil
}

func enrichWithPlayList(c *util.Client, h map[string]string, p map[string]any) []map[string]any {
	pid := periodID(p)
	out := []map[string]any{p}
	if pid == "" {
		return out
	}
	root, err := apiGet(c, periodPlayListURL, map[string]string{"periodId": pid}, h)
	if err != nil || !codeIs(root["code"], 1) {
		return out
	}
	body, _ := root["body"].(map[string]any)
	for _, q := range periodsFromBody(body, val(p, "classScheduleId"), p) {
		out = append(out, mergeMap(p, q))
	}
	return out
}

func requestPlayData(c *util.Client, h map[string]string, vi xsVideo) map[string]any {
	if vi.periodID == "" && vi.teachCoachID == "" {
		return nil
	}
	api, params := videoPlayURL, map[string]string{"periodId": vi.periodID}
	if vi.teachCoachID != "" {
		api, params = teachCoachPlayURL, map[string]string{"teachCoachId": vi.teachCoachID}
	}
	body := requestBody(c, h, api, params)
	if vi.teachCoachID == "" && (len(body) == 0 || (strings.EqualFold(vi.lectureType, "live") && firstMediaURL(body) == "")) {
		if live := requestBody(c, h, livePlayURL, map[string]string{"periodId": vi.periodID}); len(live) > 0 {
			body = live
		}
	}
	return body
}

func requestBody(c *util.Client, h map[string]string, api string, params map[string]string) map[string]any {
	root, err := apiGet(c, api, params, h)
	if err != nil || !codeIs(root["code"], 1) {
		return nil
	}
	if m, ok := root["body"].(map[string]any); ok {
		return m
	}
	return nil
}

type xsSource struct {
	URL   string
	Extra map[string]any
}

func resolveVideoSources(c *util.Client, h map[string]string, vi xsVideo, quality string) []xsSource {
	out := []xsSource{}
	play := requestPlayData(c, h, vi)
	if auth := qcloudAuth(play); auth != nil {
		if src := qcloudMediaSource(c, auth, quality); src.URL != "" {
			out = append(out, src)
		}
	}
	for _, v := range []any{play, vi.raw} {
		if u := firstMediaURL(v); u != "" {
			out = append(out, xsSource{URL: u, Extra: map[string]any{"source_type": "direct"}})
		}
	}
	return uniqueSources(out)
}

func resolveVideo(c *util.Client, h map[string]string, vi xsVideo) []string {
	out := []string{}
	for _, src := range resolveVideoSources(c, h, vi, "") {
		out = append(out, src.URL)
	}
	return unique(out)
}

func apiGet(c *util.Client, api string, params map[string]string, h map[string]string) (map[string]any, error) {
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		api += map[bool]string{true: "&", false: "?"}[strings.Contains(api, "?")] + q.Encode()
	}
	body, err := c.GetString(api, h)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("xsteach parse JSON: %w", err)
	}
	return root, nil
}

func qcloudMediaURL(c *util.Client, auth map[string]string) string {
	if src := qcloudMediaSource(c, auth, ""); src.URL != "" {
		return src.URL
	}
	return ""
}

func qcloudMediaURLFallback(c *util.Client, auth map[string]string) string {
	q := url.Values{"keyId": {"1"}, "psign": {auth["psign"]}}
	if iv := rsaOverlay(); iv != "" {
		q.Set("cipheredOverlayIv", iv)
	}
	if key := rsaOverlay(); key != "" {
		q.Set("cipheredOverlayKey", key)
	}
	body, err := c.GetString(qcloudURL(auth["app_id"], auth["file_id"])+"?"+q.Encode(), map[string]string{"Accept": "*/*", "Referer": refererURL, "User-Agent": defaultUserAgent})
	if err != nil {
		return ""
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil || !codeIs(root["code"], 0) {
		return ""
	}
	for _, m := range mapsUnder(root) {
		if ml, ok := m["masterPlayList"].(map[string]any); ok {
			if u := normalizeURL(val(ml, "url")); u != "" {
				return u
			}
		}
	}
	return firstMediaURL(root)
}

func qcloudAuth(v any) map[string]string {
	for _, m := range mapsUnder(v) {
		appID := firstNonEmpty(val(m, "appId"), val(m, "appID"), val(m, "app_id"))
		fileID := firstNonEmpty(val(m, "fileId"), val(m, "fileID"), val(m, "file_id"), val(m, "videoId"))
		psign := firstNonEmpty(val(m, "sign"), val(m, "psign"), val(m, "pSign"), val(m, "playAuth"))
		if appID != "" && fileID != "" && psign != "" {
			return map[string]string{"app_id": appID, "file_id": fileID, "psign": psign}
		}
	}
	return nil
}

func rsaOverlay() string {
	return rsaEncryptOverlay(newOverlayText())
}

func newOverlayText() string {
	raw := make([]byte, 16)
	if _, err := crand.Read(raw); err != nil {
		return ""
	}
	return hex.EncodeToString(raw)
}

func rsaEncryptOverlay(overlayText string) string {
	if overlayText == "" {
		return ""
	}
	block, _ := pem.Decode([]byte(xsteachRSAPublicKey))
	if block == nil {
		return ""
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return ""
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return ""
	}
	enc, err := rsa.EncryptPKCS1v15(crand.Reader, pub, []byte(overlayText))
	if err != nil {
		return ""
	}
	return hex.EncodeToString(enc)
}

func videosFromPeriod(p map[string]any, co xsCourse) []xsVideo {
	out := []xsVideo{}
	pid, teachID := periodID(p), firstNonEmpty(val(p, "teachCoachId"), val(p, "teach_coach_id"), val(p, "teachingAidsId"), val(p, "teaching_aids_id"))
	if pid != "" && !(teachID == "" && isFalse(p["isHasVideo"]) && firstMediaURL(p) == "") {
		out = append(out, xsVideo{periodID: pid, teachCoachID: teachID, title: firstNonEmpty(val(p, "name"), val(p, "title"), pid), lectureType: firstNonEmpty(val(p, "lectureType"), val(p, "lecture_type"), co.lectureType), raw: p})
	}
	for _, tc := range listUnder(p, "teachCoachVideos") {
		tid := firstNonEmpty(val(tc, "id"), val(tc, "teachCoachId"))
		if tid == "" || pid == "" {
			continue
		}
		raw := mergeMap(p, tc)
		raw["teachCoachId"] = tid
		out = append(out, xsVideo{periodID: pid, teachCoachID: tid, title: firstNonEmpty(val(tc, "name"), val(tc, "title"), tid), lectureType: firstNonEmpty(val(p, "lectureType"), co.lectureType), raw: raw})
	}
	return out
}
