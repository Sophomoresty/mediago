// Package open163 implements an extractor for open.163.com (网易公开课 VIP/free).
//
// API endpoints from decompiled Mooc/Courses/Open163/:
//
//	https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do
//	https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do
//	https://c.open.163.com/member/loginStatus.do
//	https://vip.open.163.com/courses/%s
//	https://open.163.com/newview/movie/free?pid=%s
package open163

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlMyOrders    = "https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do"
	urlCourseInfo  = "https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do"
	urlLoginStatus = "https://c.open.163.com/member/loginStatus.do"
	urlCoursePage  = "https://vip.open.163.com/courses/%s"
	urlFreePage    = "https://open.163.com/newview/movie/free?pid=%s"
	urlVipReferer  = "https://vip.open.163.com"
	urlFreeReferer = "https://open.163.com"
)

var patterns = []string{`(?:[\w-]+\.)?open\.163\.com/`}

func init() {
	extractor.Register(&Open163{}, extractor.SiteInfo{Name: "Open163", URL: "open.163.com", NeedAuth: true})
}

type Open163 struct{}

func (o *Open163) Patterns() []string { return patterns }

var (
	vipCourseRe = regexp.MustCompile(`/courses/([0-9A-Za-z]+)|courseId=([0-9A-Za-z]+)|cid=([0-9A-Za-z]+)`)
	freePidRe   = regexp.MustCompile(`(?:pid=|/free\?pid=)([0-9A-Za-z]+)`)
	freeMP4Re   = regexp.MustCompile(`"(https?:[^,]*?\.mp4[^"']*)"`)
	titleRe     = regexp.MustCompile(`<title>(.+?)</title>`)
)

func (o *Open163) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if pid := parseFreePID(rawURL); pid != "" {
		return o.extractFree(pid)
	}
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("open163 requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	if err := checkOpen163Cookie(c); err != nil {
		return nil, err
	}

	cid, courseUID := parseOpen163CourseIDs(rawURL)
	if cid == "" && courseUID == "" {
		return nil, fmt.Errorf("cannot parse open163 courseId from URL: %s", rawURL)
	}
	course, err := loadOpen163Course(c, cid, courseUID)
	if err != nil {
		return nil, err
	}
	info := course.Data.CourseInfo
	title := firstText(info.Title, info.Name, cid, courseUID, "open163")
	entries := make([]*extractor.MediaInfo, 0)
	chapterTypes := []struct {
		items []open163Chapter
		kind  string
	}{
		{course.Data.MovieChapterList, "video"},
		{course.Data.AudioChapterList, "audio"},
	}
	for _, group := range chapterTypes {
		for chapterIndex, chapter := range group.items {
			chapterTitle := firstText(chapter.Title, chapter.Name, "章节")
			for contentIndex, content := range chapter.ContentList {
				item := buildOpen163Entry(chapterTitle, chapterIndex+1, contentIndex+1, content, group.kind)
				if item != nil {
					entries = append(entries, item)
				}
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("open163: no playable items in course %s", cid)
	}
	return &extractor.MediaInfo{Site: "open163", Title: title, Entries: entries, Extra: map[string]any{"course_id": cid, "course_uid": courseUID, "course_info": info}}, nil
}

func (o *Open163) extractFree(pid string) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	pageURL := fmt.Sprintf(urlFreePage, pid)
	body, err := c.GetString(pageURL, map[string]string{"Referer": urlFreeReferer})
	if err != nil {
		return nil, fmt.Errorf("open163 free page: %w", err)
	}
	title := pid
	if m := titleRe.FindStringSubmatch(body); len(m) > 1 {
		title = strings.TrimSpace(strings.Split(m[1], "-")[0])
	}
	parts := freeMP4Re.FindAllStringSubmatch(body, -1)
	if len(parts) == 0 {
		return nil, fmt.Errorf("open163 free page has no mp4 links for pid=%s", pid)
	}
	entries := make([]*extractor.MediaInfo, 0, len(parts))
	for i, m := range parts {
		u := decodeOpen163MediaURL(m[1])
		if u == "" {
			continue
		}
		entries = append(entries, &extractor.MediaInfo{Site: "open163", Title: fmt.Sprintf("[%d]--%s", i+1, title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: mediaExt(u), Headers: map[string]string{"Referer": urlFreeReferer}}}})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("open163 free page: no decodable mp4 links")
	}
	return &extractor.MediaInfo{Site: "open163", Title: title, Entries: entries}, nil
}

func checkOpen163Cookie(c *util.Client) error {
	body, err := c.GetString(urlLoginStatus, map[string]string{"Referer": urlVipReferer, "Origin": urlVipReferer})
	if err != nil {
		return fmt.Errorf("open163 login check: %w", err)
	}
	var out struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return fmt.Errorf("open163 login check parse: %w", err)
	}
	if out.Code != 200 {
		return fmt.Errorf("open163 requires valid logged-in cookie (code=%d)", out.Code)
	}
	return nil
}

type open163CourseResp struct {
	Code int `json:"code"`
	Data struct {
		CourseInfo struct {
			ID          any    `json:"id"`
			CourseUID   any    `json:"courseUid"`
			Title       string `json:"title"`
			Name        string `json:"name"`
			OriginPrice any    `json:"originPrice"`
			BuyOrNot    any    `json:"buyOrNot"`
		} `json:"courseInfo"`
		MovieChapterList []open163Chapter `json:"movieChapterList"`
		AudioChapterList []open163Chapter `json:"audioChapterList"`
	} `json:"data"`
}

type open163Chapter struct {
	Title       string           `json:"title"`
	Name        string           `json:"name"`
	ContentList []open163Content `json:"contentList"`
}

type open163Content struct {
	Title         string             `json:"title"`
	Name          string             `json:"name"`
	MediaInfoList []open163MediaInfo `json:"mediaInfoList"`
	MediaSize     any                `json:"mediaSize"`
}

type open163MediaInfo struct {
	Type       string `json:"type"`
	EncryptURL string `json:"encryptUrl"`
	EncryptID  any    `json:"encryptId"`
	MediaURL   string `json:"mediaUrl"`
	URL        string `json:"url"`
	MediaSize  any    `json:"mediaSize"`
}

func loadOpen163Course(c *util.Client, cid, courseUID string) (*open163CourseResp, error) {
	variants := []map[string]string{}
	if cid != "" && courseUID != "" && cid != courseUID {
		variants = append(variants, map[string]string{"version": "1", "courseId": cid, "courseUid": courseUID})
	}
	if courseUID != "" {
		variants = append(variants, map[string]string{"version": "1", "courseUid": courseUID})
	}
	if cid != "" {
		variants = append(variants, map[string]string{"version": "1", "courseId": cid})
	}
	var lastErr error
	for _, form := range variants {
		body, err := c.PostForm(urlCourseInfo, form, map[string]string{"Referer": urlVipReferer, "Origin": urlVipReferer, "X-Requested-With": "XMLHttpRequest", "Accept": "application/json, text/plain, */*", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"})
		if err != nil {
			lastErr = err
			continue
		}
		var out open163CourseResp
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			lastErr = err
			continue
		}
		hasData := out.Data.CourseInfo.Title != "" || out.Data.CourseInfo.Name != "" || len(out.Data.MovieChapterList) > 0 || len(out.Data.AudioChapterList) > 0
		if out.Code == 200 && hasData {
			return &out, nil
		}
		lastErr = fmt.Errorf("open163 course info returned code=%d", out.Code)
	}
	return nil, fmt.Errorf("open163 load course data: %w", lastErr)
}

func buildOpen163Entry(chapterTitle string, chapterIndex, contentIndex int, content open163Content, kind string) *extractor.MediaInfo {
	title := firstText(content.Title, content.Name, chapterTitle, kind)
	media := selectOpen163MediaInfo(content.MediaInfoList, kind)
	if media == nil {
		return nil
	}
	mediaSource := firstText(media.URL, media.MediaURL, media.EncryptURL)
	if kind == "audio" {
		mediaSource = firstText(media.EncryptURL, media.MediaURL, media.URL)
	}
	mediaURL := decodeOpen163MediaURL(mediaSource)
	if mediaURL == "" {
		return nil
	}
	stream := extractor.Stream{Quality: mediaQuality(media.Type), URLs: []string{mediaURL}, Format: mediaExt(mediaURL), Headers: map[string]string{"Referer": urlVipReferer}}
	if stream.Format == "m3u8" {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "open163", Title: fmt.Sprintf("[%d.%d]--%s", chapterIndex, contentIndex, title), Streams: map[string]extractor.Stream{kind: stream}, Extra: map[string]any{"media": media, "chapter_title": chapterTitle}}
}

func selectOpen163MediaInfo(list []open163MediaInfo, kind string) *open163MediaInfo {
	if len(list) == 0 {
		return nil
	}
	prefs := []string{"m3u8", "mp4"}
	if kind == "audio" {
		prefs = []string{"m4a", "mp3"}
	}
	quality := []string{"shd", "hd", "sd", "ld"}
	type candidate struct {
		score1 int
		score2 int
		info   *open163MediaInfo
	}
	best := candidate{score1: 99, score2: 99}
	for i := range list {
		m := &list[i]
		blob := strings.ToLower(strings.Join([]string{m.Type, m.EncryptURL, m.MediaURL, m.URL}, " "))
		s1 := len(prefs)
		for idx, p := range prefs {
			if strings.Contains(blob, p) {
				s1 = idx
				break
			}
		}
		s2 := len(quality)
		for idx, q := range quality {
			if strings.Contains(blob, q) {
				s2 = idx
				break
			}
		}
		if s1 < best.score1 || (s1 == best.score1 && s2 < best.score2) {
			best = candidate{score1: s1, score2: s2, info: m}
		}
	}
	return best.info
}

func decodeOpen163MediaURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http") || strings.HasPrefix(s, "#EXTM3U") {
		return s
	}
	pad := strings.Repeat("=", (4-len(s)%4)%4)
	decoded, err := base64.StdEncoding.DecodeString(s + pad)
	if err != nil {
		return s
	}
	if strings.HasPrefix(string(decoded), "http") {
		return string(decoded)
	}
	return s
}

func mediaExt(u string) string {
	lu := strings.ToLower(u)
	switch {
	case strings.Contains(lu, ".m3u8"):
		return "m3u8"
	case strings.Contains(lu, ".mp3"):
		return "mp3"
	case strings.Contains(lu, ".m4a"):
		return "m4a"
	default:
		return "mp4"
	}
}

func mediaQuality(t string) string {
	lu := strings.ToLower(t)
	for _, q := range []string{"shd", "hd", "sd", "ld"} {
		if strings.Contains(lu, q) {
			return q
		}
	}
	return "best"
}

func parseOpen163CourseIDs(rawURL string) (cid, courseUID string) {
	if m := vipCourseRe.FindStringSubmatch(rawURL); len(m) > 0 {
		for _, g := range m[1:] {
			if g != "" {
				if g[0] >= '0' && g[0] <= '9' {
					return g, g
				}
				return "", g
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		if v := q.Get("courseId"); v != "" {
			if v[0] >= '0' && v[0] <= '9' {
				return v, v
			}
			return "", v
		}
		if v := q.Get("pid"); v != "" {
			return "", v
		}
	}
	return "", ""
}

func parseFreePID(rawURL string) string {
	if m := freePidRe.FindStringSubmatch(rawURL); len(m) > 1 {
		return m[1]
	}
	if u, err := url.Parse(rawURL); err == nil {
		if v := u.Query().Get("pid"); v != "" {
			return v
		}
	}
	return ""
}

func firstText(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func stringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}
