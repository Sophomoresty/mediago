// Package icourse163 implements an extractor for www.icourse163.org courses.
//
// API chain ported from decompiled Mooc/Courses/Mooc163/Icourse163/Icourse163_Mooc.pyc:
//   1. Course page         → title, currentTermId, member_id
//   2. getMocTermDto.dwr   → chapter / lesson / video unit tree (DWR text)
//   3. getLessonUnitLearnVo.dwr → direct mp4 URL (Shd/Hd/Sd) for each unit
//   4. resourceRpcBean.getResourceTokenV2.rpc + vod.study.163.com/eds/api/v1/vod/video
//      fallback chain (md5 signed) when no direct mp4 is exposed
//
// Only the most common /course/CID-NNN[?tid=MMM] flow is implemented; kaopei
// (考研培优) URLs, column/textbook/youdao subsites use distinct flows and are
// rejected up front.
package icourse163

import (
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
)

// Patterns chosen to intersect with Mooc_Config.courses_re['Icourse163_Mooc']:
//   \s*https?://www\.icourse163\.org/(?P<mooc>.*?)((learn)|(course))/(?P<cid1>(?!kaopei-)[\%\w-]*-\d+)(.*?tid=(?P<tid1>\d+))?
// Go RE2 has no negative lookahead so the kaopei- exclusion is enforced in code.
var patterns = []string{
	`(?:www\.)?icourse163\.org/.*?(?:learn|course)/[%\w-]+-\d+`,
}

var urlRe = regexp.MustCompile(
	`^https?://www\.icourse163\.org/(?P<mooc>[^/]*?/?)(?:learn|course)/(?P<cid>[%\w-]+-\d+)(?:.*?tid=(?P<tid>\d+))?`,
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
	if strings.Contains(rawURL, "/learn/kaopei-") {
		return nil, fmt.Errorf("icourse163 kaopei- URLs use the Kaoyan flow which isn't implemented yet")
	}

	m := urlRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, fmt.Errorf("cannot parse icourse163 URL: %s", rawURL)
	}
	moocPrefix := m[urlRe.SubexpIndex("mooc")]
	if moocPrefix != "" && !strings.HasSuffix(moocPrefix, "/") {
		moocPrefix += "/"
	}
	cid := m[urlRe.SubexpIndex("cid")]
	termID := m[urlRe.SubexpIndex("tid")]

	c := newClient(opts.Cookies)

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

	title := match1(page, `<meta\s+itemprop="name"\s+content="([^"]+)"\s*/?>`)
	if title == "" {
		title = "icourse163_" + cid
	}

	memberID := match1(page, `id\s*:\s*"(\d+)",\s*nickName\s*:\s*"`)
	if memberID == "" {
		home, _ := c.GetString(homeURL, headers())
		memberID = match1(home, `id\s*:\s*"(\d+)",\s*nickName\s*:\s*"`)
	}

	chapters, err := fetchChapters(c, termID)
	if err != nil {
		return nil, fmt.Errorf("getMocTermDto: %w", err)
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters in course %s/%s (purchase required?)", cid, termID)
	}

	var entries []*extractor.MediaInfo
	for ci, ch := range chapters {
		for li, ls := range ch.lessons {
			for ui, vu := range ls.videos {
				ps, err := fetchVideoStream(c, vu, memberID)
				if err != nil || ps.url == "" {
					continue
				}
				name := fmt.Sprintf("%02d.%02d.%02d %s", ci+1, li+1, ui+1, sanitize(vu.name))
				entries = append(entries, &extractor.MediaInfo{
					Site:  "icourse163",
					Title: name,
					Streams: map[string]extractor.Stream{
						ps.format: {
							Quality: ps.quality,
							URLs:    []string{ps.url},
							Format:  ps.format,
							Headers: map[string]string{"Referer": referer},
						},
					},
					Subtitles: ps.subs,
				})
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no playable videos found (course locked or already ended)")
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
	subs                 []extractor.Subtitle
}

func fetchVideoStream(c *util.Client, v videoUnit, memberID string) (pickedStream, error) {
	body, _ := c.PostForm(parseURL, dwrData("getLessonUnitLearnVo", map[string]string{
		"c0-param0": "number:" + v.contentID,
		"c0-param1": "number:" + v.contentType,
		"c0-param2": "number:0",
		"c0-param3": "number:" + v.unitID,
	}), headers())

	for _, q := range []string{"Shd", "Hd", "Sd"} {
		re := regexp.MustCompile(`mp4` + q + `Url="([^"]+\.mp4[^"]*)"`)
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return pickedStream{url: m[1], format: "mp4", quality: q}, nil
		}
	}

	if memberID == "" || v.contentType != "1" {
		return pickedStream{}, fmt.Errorf("no direct mp4 and cannot sign vod request")
	}

	tsBody, _ := c.GetString(timestampURL, nil)
	ts := match1(tsBody, `"t"\s*:\s*"(\d+)"`)
	if ts == "" {
		ts = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	signID, bizType, videoType := v.contentID, "1", v.contentType
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
	if err := json.Unmarshal([]byte(signBody), &sig); err != nil {
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
				E        bool   `json:"e"`
			} `json:"videos"`
			SrtCaptions []struct {
				URL  string `json:"url"`
				Lang string `json:"languageCode"`
			} `json:"srtCaptions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(vidBody), &vinfo); err != nil {
		return pickedStream{}, fmt.Errorf("parse vod: %w", err)
	}

	best := struct {
		url, fmt string
		q        int
	}{q: -1}
	for _, vd := range vinfo.Result.Videos {
		if vd.E {
			continue
		}
		if vd.Quality > best.q {
			best.url = vd.VideoURL
			best.fmt = vd.Format
			best.q = vd.Quality
		}
	}
	if best.url == "" {
		return pickedStream{}, fmt.Errorf("no playable video in vod result")
	}

	out := pickedStream{url: best.url, format: "mp4", quality: strconv.Itoa(best.q)}
	if best.fmt == "hls" {
		out.format = "m3u8"
	}
	for _, s := range vinfo.Result.SrtCaptions {
		out.subs = append(out.subs, extractor.Subtitle{Language: s.Lang, URL: s.URL, Format: "srt"})
	}
	return out, nil
}
