// Package icourse163 implements an extractor for www.icourse163.org courses.
//
// API chain ported from decompiled Mooc/Courses/Mooc163/Icourse163/Icourse163_Mooc.pyc:
//  1. Course page         → title, currentTermId, member_id
//  2. getMocTermDto.dwr   → chapter / lesson / video unit tree (DWR text)
//  3. getLessonUnitLearnVo.dwr → direct mp4 URL (Shd/Hd/Sd) for each unit
//  4. resourceRpcBean.getResourceTokenV2.rpc + vod.study.163.com/eds/api/v1/vod/video
//     fallback chain (md5 signed) when no direct mp4 is exposed
//
// The main /course/CID-NNN[?tid=MMM] flow, kaopei/kaoyan term/live flow, and
// columnBean flow are implemented. textbook/youdao subsites use distinct
// products and remain outside this extractor.
package icourse163

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

// Constants ported verbatim from Icourse163_Mooc / Icourse163_Base.
const (
	srckey       = "2d58e2797ef54e928ea95c05ece03852"
	referer      = "https://www.icourse163.org"
	homeURL      = "https://www.icourse163.org/home.htm"
	infosURL     = "https://www.icourse163.org/dwr/call/plaincall/CourseBean.getMocTermDto.dwr"
	parseURL     = "https://www.icourse163.org/dwr/call/plaincall/CourseBean.getLessonUnitLearnVo.dwr"
	signatureURL = "https://www.icourse163.org/web/j/resourceRpcBean.getResourceTokenV2.rpc?csrfKey="
	videoInfoURL = "https://vod.study.163.com/eds/api/v1/vod/video"
	subURL       = "https://www.icourse163.org/mm-course/web/j/mocCourseBean.getVideoSubtitle.rpc?csrfKey="
	timestampURL = "https://acs.m.taobao.com/gw/mtop.common.getTimestamp/"

	kaoyanCourseURL   = "https://www.icourse163.org/course/kaoyan-"
	kaoyanNewInfosURL = "https://www.icourse163.org/web/j/courseBean.getLastLearnedMocTermDto.rpc?csrfKey="
	kaoyanTermURL     = "https://kaoyan.icourse163.org/course/terms/"
	kaoyanLiveURL     = "https://www.icourse163.org/live/"
	kaoyanPayURL      = "https://kaoyan.icourse163.org/web/j/kaoyanCourseBean.getKyCourseInfoBtStatusVo.rpc?csrfKey=%s"

	columnPageURL  = "https://www.icourse163.org/columns/"
	columnTermURL  = "https://www.icourse163.org/web/j/columnBean.getMocLessonBaseDtos.rpc?csrfKey="
	columnInfosURL = "https://www.icourse163.org/web/j/columnBean.getLessonUnitBaseVoByLessonId.rpc?csrfKey="
	columnAudioURL = "https://www.icourse163.org/web/j/columnBean.getArticleInfoVo.rpc?csrfKey="
)

// App "我的课程" (Icourse163_App) course-list API, from Icourse163_App source.
const (
	appMoocCourseListURL   = "https://www.icourse163.org/web/j/learnerCourseRpcBean.getMyLearnedCoursePanelList.rpc?csrfKey="
	appColumnCourseListURL = "https://www.icourse163.org/web/j/columnBean.getColumnInfoListForMember.rpc?csrfKey="
)

// Main-course pattern chosen to intersect with Mooc_Config.courses_re['Icourse163_Mooc']:
//
//	\s*https?://www\.icourse163\.org/(?P<mooc>.*?)((learn)|(course))/(?P<cid1>(?!kaopei-)[\%\w-]*-\d+)(.*?tid=(?P<tid1>\d+))?
//
// Kaoyan and Column sibling patterns are registered below and routed before
// the main-course parser.
var patterns = []string{
	`(?:www\.)?icourse163\.org/.*?(?:learn|course)/[%\w-]+-\d+`,
	`(?:www\.)?icourse163\.org/columns/\d+\.htm`,
	`(?:www\.)?icourse163\.org/column/learn/\d+(?:/.*?\.htm)?`,
	`kaoyan\.icourse163\.org/course/terms/\d+.*course[Ii]d=\d+`,
	`(?:www\.)?icourse163\.org/live/.*?\d+\.htm`,
	// App "我的课程" course-list (Icourse163_App)
	`(?:www\.)?icourse163\.org/(?:home\.htm|mycourse)`,
}

var moocURLRe = regexp.MustCompile(
	`^https?://www\.icourse163\.org/(?P<mooc>[^/]*?/?)(?:learn|course)/(?P<cid>[%\w-]+-\d+)(?:.*?tid=(?P<tid>\d+))?`,
)

var appURLRe = regexp.MustCompile(
	`^https?://(?:www\.)?icourse163\.org/(?:home\.htm|mycourse)`,
)

func init() {
	extractor.Register(&ICourse163{}, extractor.SiteInfo{
		Name:     "icourse163",
		URL:      "icourse163.org",
		NeedAuth: true,
	})
}

type ICourse163 struct{}

func (i *ICourse163) Patterns() []string { return patterns }

func (i *ICourse163) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("icourse163 requires login cookies (use --cookies or --cookies-from-browser)")
	}

	c := newClient(opts.Cookies)
	if column, ok := parseColumnURL(rawURL); ok {
		return extractColumn(c, column)
	}
	if ky, ok := parseKaoyanURL(rawURL); ok {
		return extractKaoyan(c, ky)
	}
	if appURLRe.MatchString(rawURL) {
		return extractAppCourseList(c)
	}

	m := moocURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, fmt.Errorf("cannot parse icourse163 URL: %s", rawURL)
	}
	moocPrefix := m[moocURLRe.SubexpIndex("mooc")]
	if moocPrefix != "" && !strings.HasSuffix(moocPrefix, "/") {
		moocPrefix += "/"
	}
	cid := m[moocURLRe.SubexpIndex("cid")]
	termID := m[moocURLRe.SubexpIndex("tid")]

	pageURL := fmt.Sprintf("https://www.icourse163.org/%scourse/%s", moocPrefix, cid)
	if termID != "" {
		pageURL += "?tid=" + termID
	}
	page, err := c.GetString(pageURL, headers())
	if err != nil {
		return nil, fmt.Errorf("fetch course page: %w", err)
	}

	if termID == "" {
		termID = match1(page, `currentTermId\s*:\s*"(\d+)"`)
	}
	if termID == "" {
		return nil, fmt.Errorf("cannot find termId for %s (course unavailable or not logged in)", cid)
	}

	title := titleFromPage(page, "icourse163_"+cid)
	memberID, err := fetchMemberID(c, page)
	if err != nil {
		return nil, err
	}

	chapters, err := fetchChapters(c, termID)
	if err != nil {
		return nil, fmt.Errorf("getMocTermDto: %w", err)
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters in course %s/%s (purchase required?)", cid, termID)
	}

	entries, err := entriesFromChapters(c, chapters, memberID)
	if err != nil {
		return nil, err
	}

	return &extractor.MediaInfo{
		Site:    "icourse163",
		Title:   title,
		Entries: entries,
	}, nil
}

// ---------- helpers ----------

func newClient(jar http.CookieJar) *util.Client {
	c := util.NewClient()
	u, _ := url.Parse(referer)
	jar.SetCookies(u, []*http.Cookie{{
		Name:   "NTESSTUDYSI",
		Value:  srckey,
		Path:   "/",
		Domain: ".icourse163.org",
	}})
	c.SetCookieJar(jar)
	return c
}

func headers() map[string]string { return map[string]string{"Referer": referer} }

func match1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func decodeJSON(body string, v any) error {
	dec := json.NewDecoder(bytes.NewBufferString(body))
	dec.UseNumber()
	return dec.Decode(v)
}

func titleFromPage(page, fallback string) string {
	if title := match1(page, `courseName\s*:\s*'([^']+)'`); title != "" {
		return sanitize(title)
	}
	if title := match1(page, `<meta\s+itemprop="name"\s+content="([^"]+)"\s*/?>`); title != "" {
		return sanitize(title)
	}
	return fallback
}

func memberIDFromPage(page string) string {
	if id := match1(page, `userId=(\d+)`); id != "" {
		return id
	}
	return match1(page, `id\s*:\s*"(\d+)",\s*nickName\s*:\s*"`)
}

func fetchMemberID(c *util.Client, page string) (string, error) {
	if memberID := memberIDFromPage(page); memberID != "" {
		return memberID, nil
	}
	home, err := c.GetString(homeURL, headers())
	if err != nil {
		return "", fmt.Errorf("fetch home for member id: %w", err)
	}
	return memberIDFromPage(home), nil
}

func entriesFromChapters(c *util.Client, chapters []chapter, memberID string) ([]*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	var firstErr error
	for ci, ch := range chapters {
		for li, ls := range ch.lessons {
			for ui, vu := range ls.videos {
				ps, err := fetchVideoStream(c, vu, memberID, false)
				if err != nil || ps.url == "" {
					if err != nil && firstErr == nil {
						firstErr = err
					}
					continue
				}
				name := fmt.Sprintf("%02d.%02d.%02d %s", ci+1, li+1, ui+1, sanitize(vu.name))
				entries = append(entries, mediaEntry(name, ps))
			}
		}
	}
	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no playable videos found (course locked or already ended): %w", firstErr)
		}
		return nil, fmt.Errorf("no playable videos found (course locked or already ended)")
	}
	return entries, nil
}

func mediaEntry(name string, ps pickedStream) *extractor.MediaInfo {
	return &extractor.MediaInfo{
		Site:  "icourse163",
		Title: name,
		Streams: map[string]extractor.Stream{
			ps.format: {
				Quality: ps.quality,
				URLs:    []string{ps.url},
				Format:  ps.format,
				Size:    ps.size,
				Headers: map[string]string{"Referer": referer},
			},
		},
		Subtitles: ps.subs,
	}
}

var sanitizeRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func sanitize(s string) string { return sanitizeRe.ReplaceAllString(strings.TrimSpace(s), "_") }

type chapter struct {
	id      string
	name    string
	lessons []lesson
}
type lesson struct {
	id     string
	name   string
	videos []videoUnit
}
type videoUnit struct {
	contentID, contentType, unitID, name, lessonID string
}

// Regex bodies match the Python source; %s is filled with the parent ID.
const (
	chapPat   = `homeworks=\w+;[\s\S]+?id=(\d+)[\s\S]+?name="([\s\S]+?)";`
	lessonFmt = `chapterId=%s[\s\S]+?contentType=1[\s\S]+?id=(\d+)[\s\S]+?isTestChecked=false[\s\S]+?name="([\s\S]+?)"[\s\S]+?test`
	videoFmt  = `contentId=(\d+)[\s\S]+?contentType=(1|7)[\s\S]+?id=(\d+)[\s\S]+?lessonId=%s[\s\S]+?name="([\s\S]+?)"`
)

var chapRe = regexp.MustCompile(chapPat)

func fetchChapters(c *util.Client, termID string) ([]chapter, error) {
	body, err := c.PostForm(infosURL, dwrData("getMocTermDto", map[string]string{
		"c0-param0": "number:" + termID,
		"c0-param1": "number:0",
		"c0-param2": "boolean:true",
	}), headers())
	if err != nil {
		return nil, err
	}

	var out []chapter
	for _, cm := range chapRe.FindAllStringSubmatch(body, -1) {
		ch := chapter{id: cm[1], name: cm[2]}
		lessonRe := regexp.MustCompile(fmt.Sprintf(lessonFmt, regexp.QuoteMeta(ch.id)))
		for _, lm := range lessonRe.FindAllStringSubmatch(body, -1) {
			ls := lesson{id: lm[1], name: lm[2]}
			videoRe := regexp.MustCompile(fmt.Sprintf(videoFmt, regexp.QuoteMeta(ls.id)))
			for _, vm := range videoRe.FindAllStringSubmatch(body, -1) {
				ls.videos = append(ls.videos, videoUnit{
					contentID:   vm[1],
					contentType: vm[2],
					unitID:      vm[3],
					name:        vm[4],
					lessonID:    ls.id,
				})
			}
			ch.lessons = append(ch.lessons, ls)
		}
		out = append(out, ch)
	}
	return out, nil
}

// dwrData returns a DWR plaincall form body. The constant fields here are
// identical to the Python *_data dicts in Icourse163_Mooc.
func dwrData(method string, override map[string]string) map[string]string {
	d := map[string]string{
		"batchId":         strconv.FormatInt(time.Now().UnixMilli(), 10),
		"callCount":       "1",
		"scriptSessionId": "${scriptSessionId}190",
		"c0-id":           "0",
		"c0-scriptName":   "CourseBean",
		"c0-methodName":   method,
	}
	for k, v := range override {
		d[k] = v
	}
	return d
}

type pickedStream struct {
	url, format, quality string
	size                 int64
	subs                 []extractor.Subtitle
}

func fetchVideoStream(c *util.Client, v videoUnit, memberID string, isLive bool) (pickedStream, error) {
	body, err := c.PostForm(parseURL, dwrData("getLessonUnitLearnVo", map[string]string{
		"c0-param0": "number:" + v.contentID,
		"c0-param1": "number:" + v.contentType,
		"c0-param2": "number:0",
		"c0-param3": "number:" + v.unitID,
	}), headers())
	if err != nil {
		return pickedStream{}, fmt.Errorf("getLessonUnitLearnVo: %w", err)
	}

	for _, q := range []string{"Shd", "Hd", "Sd"} {
		re := regexp.MustCompile(`mp4` + q + `Url="([^"]+\.mp4[^"]*)"`)
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return pickedStream{url: m[1], format: "mp4", quality: q, subs: subtitleFromSourceText(body)}, nil
		}
	}

	signID := v.unitID
	if signID == "" {
		signID = v.contentID
	}
	return fetchSignedVideoStream(c, signID, v.contentType, memberID, isLive)
}

func subtitleFromSourceText(body string) []extractor.Subtitle {
	subURL := match1(body, `name=".+?";[\s\S]*?url="(https?://[^"]+)"`)
	if subURL == "" {
		return nil
	}
	return []extractor.Subtitle{{Language: "zh", URL: subURL, Format: "srt"}}
}

func fetchSignedVideoStream(c *util.Client, signID, contentType, memberID string, isLive bool) (pickedStream, error) {
	if memberID == "" || signID == "" {
		return pickedStream{}, fmt.Errorf("no direct mp4 and cannot sign vod request")
	}

	tsBody, err := c.GetString(timestampURL, nil)
	ts := ""
	if err == nil {
		ts = match1(tsBody, `"t"\s*:\s*"(\d+)"`)
	}
	if ts == "" {
		ts = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	bizType := "1"
	if isLive {
		bizType = "101"
	}
	videoType := contentType
	sign := util.MD5(signID + bizType + ts + "88" + videoType + "mooc" + memberID)

	signBody, err := c.PostForm(signatureURL+srckey, map[string]string{
		"bizId":       signID,
		"bizType":     bizType,
		"contentType": videoType,
		"timestamp":   ts,
		"sign":        sign,
	}, headers())
	if err != nil {
		return pickedStream{}, fmt.Errorf("resourceRpcBean: %w", err)
	}

	var sig struct {
		Result struct {
			VideoSignDto struct {
				Signature string `json:"signature"`
				VideoID   any    `json:"videoId"`
			} `json:"videoSignDto"`
		} `json:"result"`
	}
	if err := decodeJSON(signBody, &sig); err != nil {
		return pickedStream{}, fmt.Errorf("parse signature: %w", err)
	}
	if sig.Result.VideoSignDto.Signature == "" {
		return pickedStream{}, fmt.Errorf("empty videoSignDto.signature")
	}

	vidBody, err := c.PostForm(videoInfoURL, map[string]string{
		"clientType": "1",
		"signature":  sig.Result.VideoSignDto.Signature,
		"videoId":    fmt.Sprint(sig.Result.VideoSignDto.VideoID),
	}, headers())
	if err != nil {
		return pickedStream{}, fmt.Errorf("vod.study.163: %w", err)
	}

	var vinfo struct {
		Result struct {
			Videos []struct {
				Format   string `json:"format"`
				Quality  int    `json:"quality"`
				VideoURL string `json:"videoUrl"`
				Size     int64  `json:"size"`
				E        bool   `json:"e"`
			} `json:"videos"`
			SrtCaptions []struct {
				URL  string `json:"url"`
				Lang string `json:"languageCode"`
			} `json:"srtCaptions"`
		} `json:"result"`
	}
	if err := decodeJSON(vidBody, &vinfo); err != nil {
		return pickedStream{}, fmt.Errorf("parse vod: %w", err)
	}

	best := struct {
		url, fmt string
		q        int
		size     int64
	}{q: -1}
	for _, preferred := range []string{"mp4", "hls"} {
		for _, vd := range vinfo.Result.Videos {
			if vd.E || vd.Format != preferred {
				continue
			}
			if vd.Quality > best.q {
				best.url = vd.VideoURL
				best.fmt = vd.Format
				best.q = vd.Quality
				best.size = vd.Size
			}
		}
		if best.url != "" {
			break
		}
	}
	if best.url == "" {
		return pickedStream{}, fmt.Errorf("no playable video in vod result")
	}

	out := pickedStream{url: best.url, format: "mp4", quality: strconv.Itoa(best.q), size: best.size}
	if best.fmt == "hls" {
		out.format = "m3u8"
	}
	for _, s := range vinfo.Result.SrtCaptions {
		out.subs = append(out.subs, extractor.Subtitle{Language: s.Lang, URL: s.URL, Format: "srt"})
	}
	return out, nil
}
