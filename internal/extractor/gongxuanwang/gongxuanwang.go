// Package gongxuanwang implements source-aligned Gongxuanwang course extraction.
package gongxuanwang

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
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	default_course_price           = 999
	referer                        = "https://www.gongxuanwang.com/"
	user_info_api                  = "https://newedu.gongxuanwang.com/api/v1/pc/getuserinfo"
	lms_course_list_api            = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageMicroLessonCourse"
	lms_course_detail_api          = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/microLessonCourseDetail?courseSkuId=%s"
	lms_period_vid_api             = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/getWebSectionPeriodVidVO?courseSkuId=%s"
	lms_vid_auth_api               = "https://lms.gongxuanwang.com/api/gxw-web-student/webLive/getVidAuthorization?userId=%s&vid=%s"
	lms_price_api                  = "https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/info"
	sku_course_list_api            = "https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/page"
	system_course_list_api         = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageSystem"
	system_course_detail_api       = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/systemCourseDetail"
	system_class_course_detail_api = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/systemClassCourseDetail"
	open_course_list_api           = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageOpenCourse"
	open_course_detail_api         = "https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/getOpenCourseDetail?courseSkuId=%s"
	legacy_course_list_api         = "https://newedu.gongxuanwang.com/api/v1/pc/coursemember?page=%d&prePage=%d"
	legacy_course_detail_api       = "https://newedu.gongxuanwang.com/api/v1/pc/courseDetails?course_id=%s"
	legacy_play_api                = "https://newedu.gongxuanwang.com/api/v1/pc/courseshow?type=live&media_id=%s&id=%s"
	polyv_secure_url               = "https://player.polyv.net/secure/{vid}.js"
	polyv_key_url                  = "https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}"
)

var patterns = []string{`\s*((?P<gxw_login>https?://www\.gongxuanwang\.com/login(?:[/?#].*)?)|(?P<gxw>https?://(?:[\w-]+\.)*gongxuanwang\.com(?:[/?#].*)?))`}

func init() {
	extractor.Register(&Gongxuanwang{}, extractor.SiteInfo{Name: "Gongxuanwang", URL: "gongxuanwang.com", NeedAuth: true})
}

type Gongxuanwang struct{}

func (s *Gongxuanwang) Patterns() []string { return patterns }

type gxCtx struct {
	c           *util.Client
	headers     map[string]string
	cid         string
	source      string
	title       string
	selected    gxCourse
	courseLists map[string][]gxCourse
	userInfo    map[string]any
}

type gxCourse struct {
	Source         string
	CourseID       string
	Title          string
	Course         map[string]any
	ClassID        string
	StudentGoodsID string
	GoodsID        string
	CourseSkuID    string
	Accessible     bool
}

type gxVideo struct {
	Name               string
	VideoID            string
	Source             string
	VID                string
	ClassID            string
	TimeArrangeID      string
	CourseCoursewareID string
	PeriodID           string
	MediaID            string
	LessonID           string
}

type gxFile struct{ Name, URL, Fmt string }

func (s *Gongxuanwang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("gongxuanwang requires login cookies")
	}
	x, err := newCtx(opts.Cookies, rawURL)
	if err != nil {
		return nil, err
	}
	videos, files, err := x.loadInfos()
	if err != nil {
		return nil, err
	}
	return x.mediaFromItems(videos, files)
}

func newCtx(jar http.CookieJar, rawURL string) (*gxCtx, error) {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{referer, "https://lms.gongxuanwang.com/", "https://newedu.gongxuanwang.com/"})
	token := cookieValue(cookie, "edu_token")
	if token == "" {
		return nil, fmt.Errorf("gongxuanwang: missing edu_token cookie")
	}
	h := map[string]string{
		"Origin":        "https://www.gongxuanwang.com",
		"Referer":       referer,
		"Accept":        "application/json, text/plain, */*",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"cookie":        cookie,
		"token":         token,
		"authorization": "Bearer " + token,
	}
	cid, source := parseCourseRef(rawURL)
	return &gxCtx{c: c, headers: h, cid: cid, source: source, courseLists: map[string][]gxCourse{}}, nil
}

func (x *gxCtx) getJSON(endpoint string) (map[string]any, error) {
	body, err := x.c.GetString(endpoint, x.headers)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	return root, nil
}

func (x *gxCtx) postJSON(endpoint string, payload map[string]any) (map[string]any, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	headers := cloneHeaders(x.headers)
	if headers["Content-Type"] == "" && headers["content-type"] == "" {
		headers["Content-Type"] = "application/json"
	}
	resp, err := x.c.Post(endpoint, bytes.NewReader(b), headers)
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

var (
	courseSkuIDRe = regexp.MustCompile(`courseSkuId=(\d+)`)
	courseIDRe    = regexp.MustCompile(`courseId=(\d+)`)
	goodsIDRe     = regexp.MustCompile(`goodsId=(\d+)`)
	legacyIDRe    = regexp.MustCompile(`course_id=(\d+)`)
	plainIDRe     = regexp.MustCompile(`[?&]id=(\d+)`)
)

func parseCourseRef(rawURL string) (string, string) {
	u, _ := url.Parse(strings.TrimSpace(rawURL))
	path := strings.ToLower(u.Path)
	source := ""
	switch {
	case strings.Contains(path, "publicclass"):
		source = "open"
	case strings.Contains(path, "coursedetail") || strings.Contains(path, "minicoursedetail") || strings.Contains(path, "videoplay"):
		source = "lms"
	case strings.Contains(path, "course"):
		source = "sku"
	case strings.HasSuffix(path, "/detail") || path == "/detail":
		source = "legacy"
	}
	for _, candidate := range []struct {
		re  *regexp.Regexp
		src string
	}{
		{courseSkuIDRe, defaultSource(source, "lms")},
		{courseIDRe, defaultSource(source, "lms")},
		{goodsIDRe, "sku"},
		{legacyIDRe, "legacy"},
		{plainIDRe, source},
	} {
		if m := candidate.re.FindStringSubmatch(rawURL); len(m) == 2 {
			return m[1], candidate.src
		}
	}
	return "", source
}

func (x *gxCtx) mediaFromItems(videos []gxVideo, files []gxFile) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	var lastErr error
	for _, v := range videos {
		entry, err := x.resolveVideo(v)
		if err != nil {
			lastErr = err
			continue
		}
		entries = append(entries, entry)
	}
	for _, f := range files {
		if f.URL == "" {
			continue
		}
		name := firstNonEmpty(f.Name, f.URL)
		fmtv := firstNonEmpty(f.Fmt, extFormat(f.URL), "pdf")
		entries = append(entries, &extractor.MediaInfo{Site: "gongxuanwang", Title: name, Streams: map[string]extractor.Stream{fmtv: {Quality: fmtv, URLs: []string{f.URL}, Format: fmtv, Headers: cloneHeaders(x.headers)}}, Extra: map[string]any{"kind": "file"}})
	}
	if len(entries) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("gongxuanwang: no playable video or file entries")
	}
	if len(entries) == 1 {
		if x.title != "" {
			entries[0].Extra["course_title"] = x.title
		}
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "gongxuanwang", Title: firstNonEmpty(x.title, x.cid, "gongxuanwang"), Entries: entries, Extra: map[string]any{"course_id": x.cid, "source": x.source}}, nil
}

func (x *gxCtx) resolveVideo(v gxVideo) (*extractor.MediaInfo, error) {
	playID := v.VideoID
	playData := map[string]any{}
	var err error
	if v.Source == "legacy" {
		playData, err = x.getLegacyPlayInfo(v)
	} else {
		playData, err = x.getLMSPlayInfo(v)
	}
	if err == nil && len(playData) > 0 {
		playID = firstNonEmpty(firstString(playData, "playUrl", "play_url", "url", "m3u8", "videoUrl", "video_url"), firstString(playData, "video_id", "videoId", "vid"), playID)
	}
	if playID == "" {
		return nil, fmt.Errorf("gongxuanwang: empty video_id for %s", v.Name)
	}

	mediaURL := playID
	extra := map[string]any{"source": v.Source, "video_id": v.VideoID, "vid": v.VID, "period_id": v.PeriodID}
	if !strings.HasPrefix(mediaURL, "http") {
		sec, err := shared.PolyvResolveSecure(x.c, mediaURL, x.headers)
		if err != nil {
			return nil, err
		}
		mediaURL, err = shared.PolyvPickBestManifest(sec)
		if err != nil {
			return nil, err
		}
		extra["polyv_token"] = sec.Data.Playsafe.Token
		if strings.Contains(mediaURL, ".m3u8") {
			if text, err := x.c.GetString(mediaURL, map[string]string{"Referer": referer}); err == nil {
				if rewritten, err := shared.PolyvRewriteM3U8Keys(x.c, text, sec.Data.Playsafe.Token, referer); err == nil {
					extra["m3u8_text"] = rewritten
				}
			}
		}
	}
	fmtv := streamFormat(mediaURL)
	return &extractor.MediaInfo{Site: "gongxuanwang", Title: firstNonEmpty(v.Name, v.VideoID), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{mediaURL}, Format: fmtv, NeedMerge: fmtv == "m3u8", Headers: cloneHeaders(x.headers)}}, Extra: extra}, nil
}

func (x *gxCtx) getLMSPlayInfo(v gxVideo) (map[string]any, error) {
	uid := x.getUserID()
	if uid == "" {
		return nil, fmt.Errorf("gongxuanwang: empty user id")
	}
	vid := firstNonEmpty(v.VID, v.VideoID)
	if v.ClassID != "" && v.TimeArrangeID != "" {
		vid = fmt.Sprintf("new_%s_%s_%s", v.ClassID, v.TimeArrangeID, firstNonEmpty(v.CourseCoursewareID, vid))
	}
	root, err := x.getJSON(fmt.Sprintf(lms_vid_auth_api, url.QueryEscape(uid), url.QueryEscape(vid)))
	if err != nil {
		return nil, err
	}
	return dataMap(root), nil
}

func (x *gxCtx) getLegacyPlayInfo(v gxVideo) (map[string]any, error) {
	mediaID := firstNonEmpty(v.MediaID, v.VideoID)
	root, err := x.getJSON(fmt.Sprintf(legacy_play_api, url.QueryEscape(mediaID), url.QueryEscape(v.LessonID)))
	if err != nil {
		return nil, err
	}
	if str(root["status"]) == "success" || intVal(root["code"]) == 200 {
		return dataMap(root), nil
	}
	return nil, fmt.Errorf("gongxuanwang legacy play failed: status=%v code=%v", root["status"], root["code"])
}

func (x *gxCtx) getUserID() string {
	if len(x.userInfo) == 0 {
		root, err := x.getJSON(user_info_api)
		if err == nil {
			x.userInfo = dataMap(root)
		}
	}
	return firstString(x.userInfo, "id")
}
