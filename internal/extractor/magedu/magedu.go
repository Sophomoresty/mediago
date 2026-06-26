// Package magedu implements an extractor for edu.magedu.com (马哥教育) courses.
package magedu

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlReferer       = "https://edu.magedu.com/person/home/0/course"
	urlOrigin        = "https://edu.magedu.com"
	urlAPIBase       = "https://edu.magedu.com/v1/api"
	urlKEAPIBase     = "https://edu.magedu.com/v1/api/ke"
	urlMarketAPIBase = "https://edu.magedu.com/v1/api/market"
	urlLoginCheck    = "https://edu.magedu.com/v1/api/ke/user/simpleInfo"
	urlCourseList    = "/v2/study/myList"
	urlDetail        = "/v2/curriculum/detail"
	urlOutline       = "/v2/curriculum/outline"
	urlOldDetail     = "/curriculum/detail"
	urlOldOutline    = "/curriculum/outline"
	urlMaterial      = "/leaningMaterial/getOne"
	urlPlaySafeToken = "/polyv/playsafe/token"
	mageduUA         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var patterns = []string{`(?:[\w-]+\.)?magedu\.com/`, `马哥教育`, `马哥`, `magedu`}

func init() {
	extractor.Register(&Magedu{}, extractor.SiteInfo{Name: "Magedu", URL: "magedu.com", NeedAuth: true})
}

type Magedu struct{}

func (m *Magedu) Patterns() []string { return patterns }

type mageduSession struct {
	Cookie, Token string
	Headers       map[string]string
}
type mageduCourse struct {
	ID, Title string
	Price     any
	Purchased bool
}
type mageduItem struct {
	Kind, Title, VideoID, SectionID, StorageID, FileURL, FileFmt string
	Size                                                         int64
}

var mageduIDRe = regexp.MustCompile(`(?i)(?:/course/(?:vip|detail)/([0-9]+)|/play/([0-9]+)|[?&](?:curriculumId|courseId|cid|id)=([0-9]+))`)

func (m *Magedu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("magedu requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess, err := mageduBuildSession(c, opts.Cookies)
	if err != nil {
		return nil, err
	}
	cid := parseMageduID(rawURL)
	courses := mageduFetchCourseList(c, sess)
	course := mageduPickCourse(courses, cid)
	if course.ID != "" {
		cid = course.ID
	}
	if cid == "" && len(courses) > 0 {
		course = courses[0]
		cid = course.ID
	}
	if cid == "" {
		return nil, fmt.Errorf("cannot parse magedu play/course id from URL: %s", rawURL)
	}
	detail := mageduDetail(c, sess, cid)
	title := firstText(course.Title, detail["title"], detail["name"], detail["courseName"], "马哥教育课程"+cid)
	items := mageduCollectItems(mageduOutline(c, sess, cid))
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for _, item := range items {
		if e, err := mageduBuildEntry(c, sess, cid, item); err == nil && e != nil {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("magedu: no playable entries for curriculumId=%s", cid)
	}
	return &extractor.MediaInfo{Site: "magedu", Title: title, Entries: entries, Extra: map[string]any{"curriculumId": cid, "purchased": course.Purchased, "price": firstText(course.Price, detail["price"])}}, nil
}

func mageduBuildSession(c *util.Client, jar http.CookieJar) (*mageduSession, error) {
	cookie := mageduCookieString(jar)
	token := firstText(cookieValue(cookie, "gupao_edu_college_token"), cookieValue(cookie, "token"))
	if token == "" {
		return nil, fmt.Errorf("magedu requires gupao_edu_college_token cookie")
	}
	headers := map[string]string{"token": token, "cookie": cookie, "Origin": urlOrigin, "Referer": urlReferer, "Accept": "application/json, text/plain, */*", "User-Agent": mageduUA}
	resp, err := mageduGetJSON(c, urlLoginCheck, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("magedu login check: %w", err)
	}
	if !mageduSuccess(resp) {
		return nil, fmt.Errorf("magedu requires valid login token")
	}
	return &mageduSession{Cookie: cookie, Token: token, Headers: headers}, nil
}

func mageduFetchCourseList(c *util.Client, sess *mageduSession) []mageduCourse {
	var out []mageduCourse
	seen := map[string]bool{}
	for page := 1; page < 100; page++ {
		resp, err := mageduGetJSON(c, mageduAPIURL(urlCourseList, urlKEAPIBase), map[string]string{"filter": "0", "pageSize": "100", "pageIndex": strconv.Itoa(page)}, sess.Headers)
		if err != nil {
			break
		}
		data := mageduData(resp)
		items := mageduCourseRecords(data)
		if len(items) == 0 {
			break
		}
		added := false
		for _, rec := range items {
			course := mageduNormalizeCourse(rec)
			if course.ID != "" && !seen[course.ID] {
				seen[course.ID] = true
				out = append(out, course)
				added = true
			}
		}
		if !added || page >= intOf(mapAny(data)["totalPage"]) && intOf(mapAny(data)["totalPage"]) > 0 {
			break
		}
	}
	return out
}

func mageduDetail(c *util.Client, sess *mageduSession, cid string) map[string]any {
	for _, p := range []string{urlDetail, urlOldDetail} {
		resp, err := mageduGetJSON(c, mageduAPIURL(p, urlKEAPIBase), map[string]string{"curriculumId": cid}, sess.Headers)
		if err == nil {
			if d := mapAny(mageduData(resp)); len(d) > 0 {
				return d
			}
		}
	}
	return map[string]any{}
}

func mageduOutline(c *util.Client, sess *mageduSession, cid string) map[string]any {
	for _, p := range []string{urlOutline, urlOldOutline} {
		resp, err := mageduGetJSON(c, mageduAPIURL(p, urlKEAPIBase), map[string]string{"curriculumId": cid}, sess.Headers)
		if err == nil {
			if d := mapAny(mageduData(resp)); len(d) > 0 {
				return d
			}
		}
	}
	return map[string]any{}
}

func mageduCollectItems(outline map[string]any) []mageduItem {
	var items []mageduItem
	roots := records(outline["outlineVOList"])
	if len(roots) == 0 {
		roots = []map[string]any{outline}
	}
	sortMagedu(roots)
	for i, root := range roots {
		prefix := []int{i + 1}
		items = append(items, mageduParseSections(records(root["sectionDetailList"]), prefix)...)
		chapters := records(root["chapterList"])
		sortMagedu(chapters)
		for j, ch := range chapters {
			items = append(items, mageduParseSections(records(ch["sectionDetailList"]), append(prefix, j+1))...)
		}
	}
	if len(items) == 0 {
		items = append(items, mageduParseSections(records(outline["sectionDetailList"]), []int{1})...)
	}
	return items
}

func mageduParseSections(sections []map[string]any, prefix []int) []mageduItem {
	var out []mageduItem
	sortMagedu(sections)
	for i, sec := range sections {
		if mageduHidden(sec) {
			continue
		}
		p := append(append([]int{}, prefix...), i+1)
		if firstText(sec["sectionType"]) != "2" {
			if item := mageduVideoItem(sec, p); item.VideoID != "" {
				out = append(out, item)
			}
		}
		if file := mageduInlineFile(sec, p); file.FileURL != "" {
			out = append(out, file)
		}
	}
	return out
}

func mageduBuildEntry(c *util.Client, sess *mageduSession, cid string, item mageduItem) (*extractor.MediaInfo, error) {
	if item.Kind == "file" {
		return &extractor.MediaInfo{Site: "magedu", Title: item.Title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: []string{item.FileURL}, Format: item.FileFmt, Headers: sess.Headers}}}, nil
	}
	playSafe := mageduPlaySafeToken(c, sess, cid, item.VideoID)
	sec, err := shared.PolyvResolveSecure(c, item.VideoID, mageduPolyvHeaders(sess, cid))
	if err != nil {
		return nil, err
	}
	manifest, err := shared.PolyvPickBestManifest(sec)
	if err != nil {
		return nil, err
	}
	return &extractor.MediaInfo{Site: "magedu", Title: item.Title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: []string{manifest}, Format: "m3u8", Size: item.Size, Headers: mageduPolyvHeaders(sess, cid)}}, Extra: map[string]any{"video_id": item.VideoID, "section_id": item.SectionID, "video_storage_id": item.StorageID, "playSafeToken": playSafe}}, nil
}

func mageduPlaySafeToken(c *util.Client, sess *mageduSession, cid, vid string) string {
	headers := cloneHeaders(sess.Headers)
	headers["Referer"] = fmt.Sprintf("https://edu.magedu.com/play/%s", cid)
	resp, err := mageduGetJSON(c, mageduAPIURL(urlPlaySafeToken, urlMarketAPIBase), map[string]string{"videoId": vid, "isWxa": "0"}, headers)
	if err != nil {
		return ""
	}
	data := mapAny(mageduData(resp))
	return firstText(data["token"], data["playSafe"], data["playSafeToken"])
}

func mageduGetJSON(c *util.Client, apiURL string, params map[string]string, headers map[string]string) (map[string]any, error) {
	if len(params) > 0 {
		u, _ := url.Parse(apiURL)
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		apiURL = u.String()
	}
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mageduAPIURL(path, base string) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(base, "/") + path
}
func parseMageduID(raw string) string {
	if m := mageduIDRe.FindStringSubmatch(raw); len(m) > 0 {
		for _, g := range m[1:] {
			if g != "" {
				return g
			}
		}
	}
	return ""
}
func mageduPickCourse(courses []mageduCourse, cid string) mageduCourse {
	for _, c := range courses {
		if cid == "" || c.ID == cid {
			return c
		}
	}
	return mageduCourse{}
}
func mageduPolyvHeaders(sess *mageduSession, cid string) map[string]string {
	return map[string]string{"Accept": "application/json, text/plain, */*", "Origin": urlOrigin, "Referer": fmt.Sprintf("https://edu.magedu.com/play/%s", cid), "User-Agent": sess.Headers["User-Agent"]}
}
