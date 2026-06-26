// Package shanxiang implements an extractor for sx1211.com courses.
//
// API endpoints from decompiled Mooc/Courses/Shanxiang/:
//
//	https://www.sx1211.com/User/getAjaxCourseList
//	https://www.sx1211.com/course/study.html?id={cid}&skuId={sku_id}
//	https://www.sx1211.com/course/playbackView?id={playback_id}&skuId={sku_id}&scheduleId={schedule_id}
//	https://www.sx1211.com/course/docview.html?product_id={cid}&doc_id={doc_id}
//	https://view.csslcloud.net/replay/user/login
//	https://view.csslcloud.net/replay/video/play
//	https://view.csslcloud.net/replay/data/meta
package shanxiang

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlCourseList     = "https://www.sx1211.com/User/getAjaxCourseList"
	urlStudy          = "https://www.sx1211.com/course/study.html?id=%s&skuId=%s"
	urlPlayback       = "https://www.sx1211.com/course/playbackView?id=%s&skuId=%s&scheduleId=%s"
	urlDocview        = "https://www.sx1211.com/course/docview.html?product_id=%s&doc_id=%s"
	urlCsslLogin      = "https://view.csslcloud.net/replay/user/login"
	urlCsslPlay       = "https://view.csslcloud.net/replay/video/play"
	urlCsslMeta       = "https://view.csslcloud.net/replay/data/meta"
	urlCsslOrigin     = "https://view.csslcloud.net"
	csslDeviceType    = "h5-pc"
	csslDeviceVersion = "3.11.0"
	csslTpl           = 20
	csslTerminal      = 3
	urlReferer        = "https://www.sx1211.com/"
	urlLoginCheck     = "https://www.sx1211.com/user/course.html"
	coursePageLimit   = 100
)

var patterns = []string{`(?:[\w-]+\.)?(?:sx1211|shanxiangjiaoyu)\.com/|(?:shanxiang|山香教育|山香|sx1211)`}

func init() {
	extractor.Register(&Shanxiang{}, extractor.SiteInfo{Name: "Shanxiang", URL: "sx1211.com", NeedAuth: true})
}

type Shanxiang struct{}

func (s *Shanxiang) Patterns() []string { return patterns }

type playbackInfo struct {
	CourseID    string
	SKUId       string
	PlaybackID  string
	ScheduleID  string
	PlaybackURL string
	Title       string
}

func (s *Shanxiang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("shanxiang requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)

	info := parseInputURL(rawURL)
	if info.PlaybackID != "" {
		entry, err := resolvePlayback(c, info)
		if err != nil {
			return nil, err
		}
		return entry, nil
	}
	if info.CourseID == "" || info.SKUId == "" {
		course, err := fetchCourseFromList(c, info.CourseID)
		if err == nil && course.CourseID != "" {
			info.CourseID, info.SKUId, info.Title = course.CourseID, course.SKUId, course.Title
		}
	}
	if info.CourseID == "" || info.SKUId == "" {
		return nil, fmt.Errorf("cannot parse shanxiang course id and skuId from URL: %s", rawURL)
	}

	studyURL := fmt.Sprintf(urlStudy, info.CourseID, info.SKUId)
	body, err := c.GetString(studyURL, shanxiangHeaders(urlLoginCheck))
	if err != nil {
		return nil, fmt.Errorf("fetch shanxiang study page: %w", err)
	}
	title := firstNonEmpty(info.Title, extractStudyTitle(body), "shanxiang_"+info.CourseID)
	lessons := parseLessons(body, studyURL, info.CourseID, info.SKUId)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("shanxiang: no playback lessons found in study page")
	}

	entries := make([]*extractor.MediaInfo, 0, len(lessons))
	for _, lesson := range lessons {
		entry, err := resolvePlayback(c, lesson)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("shanxiang: playback pages found but no CSSLcloud streams resolved")
	}
	return &extractor.MediaInfo{Site: "shanxiang", Title: title, Entries: entries}, nil
}

type sxCourseResp struct {
	Success any `json:"success"`
	Data    struct {
		Rows []struct {
			ProductID   any    `json:"productid"`
			ID          any    `json:"id"`
			SKUId       any    `json:"skuid"`
			SKUId2      any    `json:"skuId"`
			ProductName string `json:"productname"`
			Name        string `json:"name"`
			Price       any    `json:"price"`
			MinPrice    any    `json:"minprice"`
			MaxPrice    any    `json:"maxprice"`
		} `json:"rows"`
		TotalPages    int `json:"totalPages"`
		NextPageIndex any `json:"nextPageIndex"`
	} `json:"data"`
}

func fetchCourseFromList(c *util.Client, wantCID string) (playbackInfo, error) {
	apiURL := urlCourseList + fmt.Sprintf("?productObjType=1&keywords=&p=1&isGift=-1&limit=%d", coursePageLimit)
	body, err := c.GetString(apiURL, shanxiangHeaders(urlReferer))
	if err != nil {
		return playbackInfo{}, err
	}
	var resp sxCourseResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return playbackInfo{}, err
	}
	for _, row := range resp.Data.Rows {
		cid := firstNonEmpty(anyString(row.ProductID), anyString(row.ID))
		if wantCID != "" && cid != wantCID {
			continue
		}
		sku := firstNonEmpty(anyString(row.SKUId), anyString(row.SKUId2))
		if cid != "" && sku != "" {
			return playbackInfo{CourseID: cid, SKUId: sku, Title: firstNonEmpty(row.ProductName, row.Name)}, nil
		}
	}
	return playbackInfo{}, fmt.Errorf("shanxiang course list has no matching course")
}

func resolvePlayback(c *util.Client, p playbackInfo) (*extractor.MediaInfo, error) {
	if p.PlaybackURL == "" {
		p.PlaybackURL = fmt.Sprintf(urlPlayback, p.PlaybackID, p.SKUId, p.ScheduleID)
	}
	h := shanxiangHeaders(p.PlaybackURL)
	body, err := c.GetString(p.PlaybackURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch shanxiang playback page: %w", err)
	}
	cc := parseCCInfo(body)
	accessID := firstNonEmpty(cc["userId"], cc["groupId"])
	roomID := firstNonEmpty(cc["roomId"], cc["liveId"])
	recordID := firstNonEmpty(cc["recordId"], cc["videoId"], p.PlaybackID)
	viewerName := firstNonEmpty(cc["viewername"], cc["viewerName"], cc["viewerId"])
	viewerToken := cc["viewertoken"]
	if accessID == "" || roomID == "" || recordID == "" || viewerToken == "" {
		return nil, fmt.Errorf("shanxiang: missing CSSLcloud fields userId/roomId/recordId/viewertoken")
	}
	play, err := shared.CssLcloudResolvePlayInfo(c, shared.CssLcloudPayload{
		LiveRoomID: roomID, AccessID: accessID, RecordID: recordID,
		UserID: accessID, ViewerName: viewerName, ViewerToken: viewerToken, Referer: p.PlaybackURL,
	})
	if err != nil {
		return nil, err
	}
	title := firstNonEmpty(p.Title, "shanxiang_"+recordID)
	extra := map[string]any{
		"course_id": p.CourseID, "sku_id": p.SKUId, "playback_id": p.PlaybackID,
		"schedule_id": p.ScheduleID, "account_id": accessID, "room_id": roomID, "record_id": recordID,
		"cssl_session_id": play.SessionID, "cssl_meta_url": urlCsslMeta,
	}
	if strings.Contains(strings.ToLower(play.VideoURL), ".m3u8") {
		if m3u8, err := c.GetString(play.VideoURL, map[string]string{"Referer": p.PlaybackURL}); err == nil {
			if rewritten, err := shared.CssLcloudRewriteM3U8Keys(c, m3u8, p.PlaybackURL); err == nil {
				extra["m3u8_text"] = rewritten
			} else {
				extra["m3u8_rewrite_error"] = err.Error()
			}
		}
	}
	return &extractor.MediaInfo{Site: "shanxiang", Title: title, Streams: csslStreams(play, p.PlaybackURL), Extra: extra}, nil
}

func parseInputURL(raw string) playbackInfo {
	out := playbackInfo{}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return out
	}
	q := u.Query()
	if strings.Contains(u.Path, "/course/playbackView") {
		out.PlaybackID = q.Get("id")
		out.SKUId = firstNonEmpty(q.Get("skuId"), q.Get("skuid"))
		out.ScheduleID = firstNonEmpty(q.Get("scheduleId"), q.Get("scheduleid"))
		out.PlaybackURL = raw
		return out
	}
	if strings.Contains(u.Path, "/course/study.html") || strings.Contains(u.Path, "/course/detail.html") {
		out.CourseID = q.Get("id")
		out.SKUId = firstNonEmpty(q.Get("skuId"), q.Get("skuid"))
		return out
	}
	if m := regexp.MustCompile(`/course/(\d+)`).FindStringSubmatch(u.Path); len(m) > 1 {
		out.CourseID = m[1]
	}
	return out
}

var (
	lessonLinkRe = regexp.MustCompile(`(?is)(?:href|data-url)\s*=\s*["']([^"']*/course/playbackView[^"']*)["']`)
	titleAttrRe  = regexp.MustCompile(`(?is)(?:title|data-title|aria-label)\s*=\s*["']([^"']{2,160})["']`)
	studyTitleRe = regexp.MustCompile(`(?is)<(?:h1|h2|h3|div)[^>]*(?:h-title|course-title|js-title-name)[^>]*>(.*?)</(?:h1|h2|h3|div)>|<title>(.*?)</title>`)
	ccPairRe     = regexp.MustCompile(`(?is)(userId|roomId|recordId|viewername|viewertoken|groupId)\s*:\s*["']([^"']*)["']`)
)

func parseLessons(body, base, cid, sku string) []playbackInfo {
	seen := map[string]bool{}
	var out []playbackInfo
	for _, m := range lessonLinkRe.FindAllStringSubmatchIndex(body, -1) {
		link := html.UnescapeString(body[m[2]:m[3]])
		abs := makeAbsolute(link, base)
		pi := parseInputURL(abs)
		if pi.CourseID == "" {
			pi.CourseID = cid
		}
		if pi.SKUId == "" {
			pi.SKUId = sku
		}
		key := pi.PlaybackID + ":" + pi.ScheduleID
		if pi.PlaybackID == "" || seen[key] {
			continue
		}
		seen[key] = true
		start, end := m[0]-500, m[1]+500
		if start < 0 {
			start = 0
		}
		if end > len(body) {
			end = len(body)
		}
		pi.Title = firstNonEmpty(extractContextTitle(body[start:end]), fmt.Sprintf("视频 %d", len(out)+1))
		out = append(out, pi)
	}
	return out
}

func parseCCInfo(text string) map[string]string {
	out := map[string]string{}
	for _, m := range ccPairRe.FindAllStringSubmatch(text, -1) {
		out[m[1]] = html.UnescapeString(m[2])
	}
	for _, pair := range [][2]string{{"userId", "userId"}, {"roomId", "roomId"}, {"recordId", "recordId"}, {"viewerName", "viewername"}, {"viewerId", "viewerId"}, {"liveId", "liveId"}, {"videoId", "videoId"}} {
		if out[pair[1]] != "" {
			continue
		}
		re := regexp.MustCompile(`(?is)id=["']` + regexp.QuoteMeta(pair[0]) + `["'][^>]*value=["']([^"']*)["']`)
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			out[pair[1]] = html.UnescapeString(m[1])
		}
	}
	return out
}

func csslStreams(play *shared.CssLcloudPlayInfo, referer string) map[string]extractor.Stream {
	streams := map[string]extractor.Stream{}
	list := play.VideoList
	if len(list) == 0 {
		list = []shared.CssLcloudStreamInfo{{URL: play.VideoURL}}
	}
	for i, v := range list {
		if v.URL == "" {
			continue
		}
		key := fmt.Sprintf("definition_%d", v.Definition)
		if v.Definition == 0 {
			key = fmt.Sprintf("video_%d", i+1)
		}
		streams[key] = extractor.Stream{Quality: key, URLs: []string{v.URL}, Format: pickFormat(v.URL), AudioURL: play.AudioURL, Headers: map[string]string{"Referer": referer}}
	}
	return streams
}

func shanxiangHeaders(referer string) map[string]string {
	return map[string]string{"X-Requested-With": "XMLHttpRequest", "Accept": "application/json, text/plain, */*", "Origin": "https://www.sx1211.com", "Referer": referer}
}
func extractStudyTitle(body string) string {
	if m := studyTitleRe.FindStringSubmatch(body); len(m) > 0 {
		return stripTags(firstNonEmpty(m[1], m[2]))
	}
	return ""
}
func extractContextTitle(s string) string {
	if m := titleAttrRe.FindStringSubmatch(s); len(m) > 1 {
		return stripTags(m[1])
	}
	return ""
}
func stripTags(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(html.UnescapeString(s), " "))
}
func makeAbsolute(raw, base string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		return raw
	}
	return b.ResolveReference(u).String()
}
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
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
