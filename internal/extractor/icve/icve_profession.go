// Icve_Profession – zyk.icve.com.cn resource library extraction.
//
// Source: Icve_Profession.pyc.1shot.cdc.py
// API: course/trust/information → studyMoudleList → studyList (recursive),
//      courseContent/{sid} for individual resources, upload.icve.com.cn/status for transcoding.
// Auth: requires Bearer token via passLogin (NeedAuth: true).
package icve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	profURLCourse     = "https://zyk.icve.com.cn/prod-api/website/course/trust/information?courseId=%s"
	profURLContent    = "https://zyk.icve.com.cn/prod-api/teacher/courseContent/%s"
	profURLJoin       = "https://zyk.icve.com.cn/prod-api/teacher/courseInfoStudent/check/join?courseInfoId=%s"
	profURLInfos      = "https://zyk.icve.com.cn/prod-api/teacher/courseContent/studyMoudleList?courseInfoId=%s"
	profURLInnerInfos = "https://zyk.icve.com.cn/prod-api/teacher/courseContent/studyList?level=%d&parentId=%s&courseInfoId=%s"
	profURLSource     = "https://zyk.icve.com.cn/prod-api/teacher/courseContent/%s"
	profURLPassLogin  = "https://zyk.icve.com.cn/prod-api/auth/passLogin?token=%s"
	profURLCheckLogin = "https://zyk.icve.com.cn/prod-api/system/user/getInfo"
)

// Source: Mooc_Config courses_re['Icve_Profession']
var professionPatterns = []string{
	`\s*https?://zyk\.icve\.com\.cn/courseDetailed.*?id=(?P<cid1>[-\w]+)`,
	`\s*https?://zyk\.icve\.com\.cn/icve-study.*?id=(?P<mid1>[-\w]+)`,
	`\s*https?://zyk\.icve\.com\.cn/?$`,
}

var profCIDRe = regexp.MustCompile(
	`(?i)https?://zyk\.icve\.com\.cn/(?:courseDetailed|icve-study).*?id=([-\w]+)`,
)

func init() {
	extractor.Register(&IcveProfession{}, extractor.SiteInfo{Name: "IcveProfession", URL: "zyk.icve.com.cn", NeedAuth: true})
}

type IcveProfession struct{}

func (i *IcveProfession) Patterns() []string { return professionPatterns }

type profCtx struct {
	c          *util.Client
	headers    map[string]string
	mode       int
	cid        string // courseId from URL
	courseID    string // courseInfoId from course info
	title      string
	courseList  []profCourseItem
}

type profCourseItem struct {
	Name string
	ID   string
}

type profSourceItem struct {
	Name     string
	FileType string
	FileID   string
}

func (i *IcveProfession) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil {
		opts = &extractor.ExtractOpts{}
	}
	jar := opts.Cookies
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}

	resolved, err := resolveSmartEduURL(rawURL, jar)
	if err == nil && resolved != "" {
		rawURL = resolved
	}

	x := newProfCtx(jar, modeFromQuality(opts.Quality))
	x.cid = parseProfCID(rawURL)
	if x.cid == "" {
		return nil, fmt.Errorf("icve_profession: cannot parse course id from URL")
	}

	if err := x.loadTitle(); err != nil {
		return nil, err
	}
	if x.courseID == "" {
		return nil, fmt.Errorf("icve_profession: no courseInfoId found")
	}

	items, err := x.loadInfos()
	if err != nil {
		return nil, err
	}
	return x.buildMedia(items)
}

func newProfCtx(jar http.CookieJar, mode int) *profCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Platform": `"Windows"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Google Chrome";v="115", "Chromium";v="115"`,
		"Referer":            "https://zyk.icve.com.cn",
		"cookie":             cookieHeader(jar, []string{"https://zyk.icve.com.cn/", referer + "/"}),
		"User-Agent":         util.RandomUA(),
	}
	return &profCtx{c: c, headers: headers, mode: mode}
}

func parseProfCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := profCIDRe.FindStringSubmatch(raw); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, key := range []string{"id", "courseId"} {
		if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
			return v
		}
	}
	return ""
}

// loadTitle fetches course info to get title and courseInfoId.
// Source: Icve_Profession._get_title
func (x *profCtx) loadTitle() error {
	body, err := x.c.GetString(fmt.Sprintf(profURLCourse, url.QueryEscape(x.cid)), x.headers)
	if err != nil {
		return fmt.Errorf("icve_profession: load title: %w", err)
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")
	courseVo := mapAt(data, "courseVo")
	name := str(courseVo["name"])
	school := str(courseVo["schoolName"])
	if name != "" && school != "" {
		x.title = cleanTitle(name + "_" + school)
	} else if name != "" {
		x.title = cleanTitle(name)
	}

	// Extract courseInfo list to get courseInfoId
	courseInfos := listAt(data, "courseInfo")
	if len(courseInfos) > 0 {
		x.courseID = str(courseInfos[0]["id"])
		for idx, ci := range courseInfos {
			x.courseList = append(x.courseList, profCourseItem{
				Name: fmt.Sprintf("{%d}--%s", idx+1, cleanTitle(str(ci["name"]))),
				ID:   str(ci["id"]),
			})
		}
	}
	return nil
}

// loadInfos enumerates the course tree.
// Source: Icve_Profession._get_infos + _get_inner_infos
func (x *profCtx) loadInfos() ([]profSourceItem, error) {
	var allItems []profSourceItem

	courseIDs := []string{x.courseID}
	if len(x.courseList) > 1 {
		courseIDs = nil
		for _, cl := range x.courseList {
			courseIDs = append(courseIDs, cl.ID)
		}
	}

	for _, cInfoID := range courseIDs {
		body, err := x.c.GetString(fmt.Sprintf(profURLInfos, url.QueryEscape(cInfoID)), x.headers)
		if err != nil {
			continue
		}
		chapters := parseJSONMapList(body)
		sortBySort(chapters)
		for idx, chapter := range chapters {
			items := x.getInnerInfos(chapter, []int{idx + 1}, 1, cInfoID)
			allItems = append(allItems, items...)
		}
	}
	return allItems, nil
}

// getInnerInfos recursively builds the source list.
// Source: Icve_Profession._get_inner_infos
func (x *profCtx) getInnerInfos(item map[string]any, indexTup []int, levelNum int, courseInfoID string) []profSourceItem {
	var items []profSourceItem
	fileType := firstNonEmpty(str(item["fileType"]), "")
	id := str(item["id"])
	name := cleanTitle(str(item["name"]))

	if fileType == "父节点" || fileType == "子节点" || fileType == "文件夹" {
		level := min(levelNum, 2)
		var children []map[string]any
		if fileType == "文件夹" {
			children = listAt(item, "children")
		} else if id != "" {
			body, err := x.c.GetString(
				fmt.Sprintf(profURLInnerInfos, level, url.QueryEscape(id), url.QueryEscape(courseInfoID)),
				x.headers,
			)
			if err == nil {
				children = parseJSONMapList(body)
			}
		}
		if len(children) > 0 {
			sortBySort(children)
			for childIdx, child := range children {
				childPrefix := append(append([]int{}, indexTup...), childIdx+1)
				childItems := x.getInnerInfos(child, childPrefix, levelNum+1, courseInfoID)
				items = append(items, childItems...)
			}
		}
	} else if id != "" && fileType != "" {
		items = append(items, profSourceItem{
			Name:     fmt.Sprintf("(%s)--%s", joinInts(indexTup, "."), name),
			FileType: strings.TrimRight(fileType, "x"),
			FileID:   id,
		})
	}
	return items
}

// getVideoURL resolves transcoded video URL for a source.
// Source: Icve_Profession._get_video_url
func (x *profCtx) getVideoURL(sourceID string) string {
	body, err := x.c.GetString(fmt.Sprintf(profURLSource, url.QueryEscape(sourceID)), x.headers)
	if err != nil {
		return ""
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")
	if len(data) == 0 {
		return ""
	}

	fileGenURL := str(data["fileGenUrl"])
	urlShort := firstNonEmpty(str(data["urlShort"]), str(data["content"]))

	if fileGenURL != "" && urlShort != "" {
		statusBody, err := x.c.GetString(fmt.Sprintf(urlSourceStatus, strings.TrimLeft(urlShort, "/")), x.headers)
		if err == nil {
			status := parseJSONMap(statusBody)
			args := mapAt(status, "args")
			ac := &aiCtx{c: x.c, headers: x.headers, mode: x.mode}
			u := ac.selectTranscodedURL(fileGenURL, "mp4", map[string]any{"args": args})
			if u != "" {
				return u
			}
		}
	}

	fileURL := str(data["fileUrl"])
	if fileURL != "" && strings.HasPrefix(fileURL, "http") {
		return fileURL
	}
	return ""
}

// getSourceURL gets direct file URL for a source.
// Source: Icve_Profession._get_source_url
func (x *profCtx) getSourceURL(sourceID string) string {
	body, err := x.c.GetString(fmt.Sprintf(profURLSource, url.QueryEscape(sourceID)), x.headers)
	if err != nil {
		return ""
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")
	u := str(data["fileUrl"])
	if idx := strings.LastIndex(u, "?"); idx > 0 {
		u = u[:idx]
	}
	if u != "" && strings.HasPrefix(u, "http") {
		return u
	}
	return ""
}

func (x *profCtx) buildMedia(items []profSourceItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		isVideo := isVideoType(item.FileType)
		if isVideo && x.mode == ONLY_PDF {
			continue
		}
		var u string
		if isVideo {
			u = x.getVideoURL(item.FileID)
			if u == "" {
				u = x.getSourceURL(item.FileID)
			}
		} else {
			u = x.getSourceURL(item.FileID)
		}
		if u == "" {
			continue
		}
		ext := pickExt(u)
		if ext == "" {
			if isVideo {
				ext = "mp4"
			} else {
				ext = item.FileType
			}
		}
		if ext == "" {
			ext = "html"
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "icve",
			Title: item.Name,
			Streams: map[string]extractor.Stream{
				ext: {
					Quality:   ext,
					URLs:      []string{u},
					Format:    ext,
					NeedMerge: ext == "m3u8",
					Headers:   cloneHeaders(x.headers),
				},
			},
			Extra: map[string]any{"kind": item.FileType, "module": "profession"},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icve_profession: no playable entries")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{
		Site:    "icve",
		Title:   firstNonEmpty(x.title, x.cid, "icve_profession"),
		Entries: entries,
		Extra:   map[string]any{"course_id": x.cid, "module": "profession"},
	}, nil
}

func isVideoType(ft string) bool {
	ft = strings.ToLower(ft)
	switch ft {
	case "mp4", "video", "flv", "mpg", "avi", "mov", "m3u8":
		return true
	}
	return false
}

// parseJSONMapList parses text as a JSON array of objects.
// Falls back to extracting from a wrapper object with data/rows/list keys.
func parseJSONMapList(text string) []map[string]any {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "[") {
		var arr []any
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&arr); err == nil {
			return mapsFromAny(arr)
		}
	}
	root := parseJSONMap(text)
	for _, key := range []string{"data", "rows", "list"} {
		if arr := listAt(root, key); len(arr) > 0 {
			return arr
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure json import is referenced.
var _ = json.NewDecoder
