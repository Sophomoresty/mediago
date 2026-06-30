// Package mashibing implements an extractor for mashibing.com (马士兵教育) courses.
package mashibing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	urlReferer        = "https://www.mashibing.com"
	urlGateway        = "https://gateway.mashibing.com"
	urlLoginCheck     = "https://gateway.mashibing.com/uaa/user"
	urlCourse         = "https://gateway.mashibing.com/edu-course/studyProgress/ownCourse"
	urlCoursePackage  = "https://gateway.mashibing.com/edu-course/ownCourse/ownPackageList"
	urlInfoTmpl       = "https://gateway.mashibing.com/edu-course/systemCourse/course/%s"
	urlCourseWebTmpl  = "https://gateway.mashibing.com/edu-course/courseWeb/%s/pc"
	urlVideoInfo      = "https://gateway.mashibing.com/msb-video/ployv/getPloyvVideo"
	urlPlaySafe       = "https://gateway.mashibing.com/msb-video/ployv/playerSafe"
	urlPolyvJSONTmpl  = "https://player.polyv.net/secure/%s.json"
	urlSourceList     = "https://gateway.mashibing.com/edu-course/courseDocumentWeb/list/pc"
	urlSourceInfoTmpl = "https://gateway.mashibing.com/edu-course/courseDocumentWeb/%s/%s"
	urlPolyvKeyTmpl   = "https://hls.videocc.net/playsafe/%s/%s/%s_%s.key?token=%s"
	urlPolyvLibJS     = "https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js"
	mashibingUA       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0"
)

var patterns = []string{`(?:[\w-]+\.)?mashibing\.com/`, `mashibing`, `msb`, `码士集团`, `马士兵`}

func init() {
	extractor.Register(&Mashibing{}, extractor.SiteInfo{Name: "Mashibing", URL: "mashibing.com", NeedAuth: true})
}

type Mashibing struct{}

func (m *Mashibing) Patterns() []string { return patterns }

type mashibingSession struct {
	Cookie  string
	Headers map[string]string
}

type mashibingCourse struct {
	ID, Title string
	Price     any
	Purchased bool
}

type mashibingItem struct {
	Kind, Name, VideoID, FileURL, FileFmt, CourseID, SectionID string
	Size                                                       int64
	Raw                                                        map[string]any
}

var mashibingIDRe = regexp.MustCompile(`(?i)(?:/course/([0-9]+)|[?&](?:courseNo|courseId|cid|id)=([0-9]+))`)

func (m *Mashibing) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("mashibing requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess, err := mashibingBuildSession(c, opts.Cookies)
	if err != nil {
		return nil, err
	}

	cid := mashibingParseCourseID(rawURL)
	courses := mashibingFetchCourseList(c, sess)
	course := mashibingPickCourse(courses, cid)
	if course.ID != "" {
		cid = course.ID
	}
	if cid == "" && len(courses) > 0 {
		course = courses[0]
		cid = course.ID
	}
	if cid == "" {
		return nil, fmt.Errorf("cannot parse mashibing course id from URL: %s", rawURL)
	}

	detail := mashibingCourseDetail(c, sess, cid)
	title := mashibingFirstText(course.Title, detail["courseName"], detail["title"], "码士集团课程"+cid)
	items := mashibingBuildItems(c, sess, cid, detail)
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for _, item := range items {
		entry, err := mashibingBuildEntry(c, sess, item)
		if err == nil && entry != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("mashibing: no playable video/file entries for course=%s", cid)
	}
	return &extractor.MediaInfo{Site: "mashibing", Title: title, Entries: entries, Extra: map[string]any{"course_id": cid, "purchased": course.Purchased, "price": course.Price}}, nil
}

func mashibingBuildSession(c *util.Client, jar http.CookieJar) (*mashibingSession, error) {
	cookie := mashibingCookieString(jar)
	if cookie == "" {
		return nil, fmt.Errorf("mashibing requires cookie header")
	}
	headers := mashibingHeaders(cookie)
	body, err := c.GetString(urlLoginCheck, headers)
	if err != nil {
		return nil, fmt.Errorf("mashibing login check: %w", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("mashibing login check parse: %w", err)
	}
	if mashibingFirstText(resp["code"]) != "200" {
		return nil, fmt.Errorf("mashibing requires valid login cookie")
	}
	return &mashibingSession{Cookie: cookie, Headers: headers}, nil
}

func mashibingFetchCourseList(c *util.Client, sess *mashibingSession) []mashibingCourse {
	seen := map[string]bool{}
	out := []mashibingCourse{}
	for page := 1; page < 100; page++ {
		resp, err := mashibingGetJSON(c, urlCourse, map[string]string{"current": strconv.Itoa(page), "size": "100", "pageIndex": strconv.Itoa(page), "pageSize": "100"}, sess.Headers)
		if err != nil {
			break
		}
		data := mashibingMap(resp["data"])
		records := mashibingRecords(data["records"])
		if len(records) == 0 {
			break
		}
		added := false
		for _, rec := range records {
			id := mashibingFirstText(rec["relCourseNo"], rec["courseNo"], rec["courseId"])
			title := mashibingFirstText(rec["courseName"], rec["courseVersionName"])
			if id != "" && title != "" && !seen[id] {
				seen[id] = true
				out = append(out, mashibingCourse{ID: id, Title: title, Price: rec["price"], Purchased: true})
				added = true
			}
		}
		pages := mashibingInt(data["pages"])
		if !added || pages > 0 && page >= pages {
			break
		}
	}
	for page := 1; page < 100; page++ {
		resp, err := mashibingGetJSON(c, urlCoursePackage, map[string]string{"pageIndex": strconv.Itoa(page), "pageSize": "100"}, sess.Headers)
		if err != nil {
			break
		}
		data := mashibingMap(resp["data"])
		records := mashibingRecords(data["records"])
		if len(records) == 0 {
			break
		}
		added := false
		for _, rec := range records {
			id := mashibingFirstText(rec["courseNo"], rec["courseId"], rec["id"])
			title := mashibingFirstText(rec["courseName"], rec["name"], rec["title"])
			if id != "" && title != "" && !seen[id] {
				seen[id] = true
				out = append(out, mashibingCourse{ID: id, Title: title, Price: rec["price"], Purchased: true})
				added = true
			}
		}
		pages := mashibingInt(data["pages"])
		if !added || pages > 0 && page >= pages {
			break
		}
	}
	return out
}

func mashibingCourseDetail(c *util.Client, sess *mashibingSession, cid string) map[string]any {
	for _, apiURL := range []string{fmt.Sprintf(urlInfoTmpl, url.PathEscape(cid)), fmt.Sprintf(urlCourseWebTmpl, url.PathEscape(cid))} {
		resp, err := mashibingGetJSON(c, apiURL, nil, sess.Headers)
		if err != nil {
			continue
		}
		if data := mashibingMap(resp["data"]); len(data) > 0 {
			return data
		}
	}
	return map[string]any{}
}

func mashibingBuildEntry(c *util.Client, sess *mashibingSession, item mashibingItem) (*extractor.MediaInfo, error) {
	switch item.Kind {
	case "video":
		return mashibingBuildVideoEntry(c, sess, item)
	case "document":
		return mashibingBuildDocumentEntry(c, sess, item)
	default:
		if item.FileURL == "" {
			return nil, fmt.Errorf("mashibing: empty file url")
		}
		fmtName := strings.TrimPrefix(mashibingFirstText(item.FileFmt, mashibingFileExt(item.FileURL)), ".")
		return &extractor.MediaInfo{Site: "mashibing", Title: item.Name, Streams: map[string]extractor.Stream{"file": {Quality: "file", URLs: []string{item.FileURL}, Format: fmtName, Headers: sess.Headers}}, Extra: map[string]any{"file_url": item.FileURL}}, nil
	}
}

func mashibingBuildVideoEntry(c *util.Client, sess *mashibingSession, item mashibingItem) (*extractor.MediaInfo, error) {
	if item.VideoID == "" {
		return nil, fmt.Errorf("mashibing: empty polyv video id")
	}
	polyvHeaders := mashibingPolyvHeaders(sess)
	manifest, token := "", ""
	sec, err := shared.PolyvResolveSecure(c, item.VideoID, polyvHeaders)
	if err == nil {
		token = sec.Data.Playsafe.Token
		if u, pickErr := shared.PolyvPickBestManifest(sec); pickErr == nil {
			manifest = u
		}
	}
	info := mashibingPolyvInfo(c, item.VideoID, polyvHeaders)
	if manifest == "" {
		manifest = mashibingSelectPolyvURL(info, 1)
	}
	if token == "" {
		token = mashibingFirstText(info["playSafe"], info["token"], mashibingMap(info["playsafe"])["token"])
	}
	playSafe := ""
	if strings.Contains(strings.ToLower(manifest), ".m3u8") || strings.Contains(strings.ToLower(manifest), ".pdx") {
		playSafe = mashibingPlaySafeToken(c, sess, item.VideoID)
		token = mashibingFirstText(playSafe, token)
	}
	if manifest == "" {
		return nil, fmt.Errorf("mashibing polyv %s: empty manifest", item.VideoID)
	}
	manifest = mashibingNormalizeMediaURL(manifest)
	streamFormat := mashibingStreamFormat(manifest)
	streamURL := manifest
	extra := map[string]any{"video_id": item.VideoID, "playSafe": playSafe, "token": token, "polyv_info": info, "pdx_url": mashibingBuildPolyvPDXURL(manifest, info)}
	var streamExtra map[string]any
	if streamFormat == "pdx" {
		pdxURL := mashibingFirstText(mashibingBuildPolyvPDXURL(manifest, info), manifest)
		pdx, err := shared.PolyvResolvePDX(c, shared.PolyvPDXOptions{VideoID: item.VideoID, PDXURL: pdxURL, PlaySafeToken: token, Headers: polyvHeaders, Secure: sec})
		if err != nil {
			return nil, fmt.Errorf("mashibing polyv pdx %s: %w", item.VideoID, err)
		}
		streamURL = mashibingM3U8DataURL(pdx.M3U8Text)
		streamFormat = "m3u8"
		extra["source_type"] = "polyv_pdx"
		extra["m3u8_meta"] = pdx.M3U8Meta()
		streamExtra = map[string]any{"cryptor": "polyv_pdx", "m3u8_meta": pdx.M3U8Meta()}
		for k, v := range pdx.ExtraMap() {
			extra[k] = v
		}
	}
	if streamFormat == "m3u8" && token != "" && strings.HasPrefix(manifest, "http") {
		if text, e := c.GetString(manifest, polyvHeaders); e == nil && strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
			if rewritten, e := shared.PolyvRewriteM3U8Keys(c, text, token, urlReferer); e == nil {
				streamURL = mashibingM3U8DataURL(rewritten)
				extra["m3u8_text"] = rewritten
				extra["source_type"] = "m3u8_text"
			}
		}
	}
	return &extractor.MediaInfo{Site: "mashibing", Title: item.Name, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{streamURL}, Format: streamFormat, Size: item.Size, NeedMerge: streamFormat == "m3u8" || streamFormat == "pdx", Headers: polyvHeaders, Extra: streamExtra}}, Extra: extra}, nil
}

func mashibingBuildDocumentEntry(c *util.Client, sess *mashibingSession, item mashibingItem) (*extractor.MediaInfo, error) {
	if item.CourseID == "" || item.SectionID == "" {
		return nil, fmt.Errorf("mashibing: empty document ids")
	}
	apiURL := fmt.Sprintf(urlSourceInfoTmpl, url.PathEscape(item.CourseID), url.PathEscape(item.SectionID))
	resp, err := mashibingGetJSON(c, apiURL, nil, sess.Headers)
	if err != nil {
		return nil, err
	}
	data := mashibingMap(resp["data"])
	note := mashibingFirstText(data["noteContent"])
	if note == "" {
		return nil, fmt.Errorf("mashibing: empty noteContent")
	}
	htmlURL := "data:text/html;charset=utf-8," + url.PathEscape(mashibingMarkdownHTML(note, item.Name))
	return &extractor.MediaInfo{Site: "mashibing", Title: item.Name, Streams: map[string]extractor.Stream{"document": {Quality: "document", URLs: []string{htmlURL}, Format: "html", Headers: sess.Headers}}, Extra: map[string]any{"course_id": item.CourseID, "section_id": item.SectionID, "noteContent": note}}, nil
}

func mashibingGetJSON(c *util.Client, apiURL string, params map[string]string, headers map[string]string) (map[string]any, error) {
	if len(params) > 0 {
		u, err := url.Parse(apiURL)
		if err != nil {
			return nil, err
		}
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

func mashibingPostJSON(c *util.Client, apiURL string, payload map[string]any, headers map[string]string) (map[string]any, error) {
	b, _ := json.Marshal(payload)
	h := mashibingCloneHeaders(headers)
	h["Content-Type"] = "application/json;charset=UTF-8"
	resp, err := c.Post(apiURL, bytes.NewReader(b), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, apiURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
