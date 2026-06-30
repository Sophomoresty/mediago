// Icve_Profession – zyk.icve.com.cn resource library extraction.
//
// Source: Icve_Profession.pyc.1shot.cdc.py
// API: course/trust/information → studyMoudleList → studyList (recursive),
//
//	courseContent/{sid} for individual resources, upload.icve.com.cn/status for transcoding.
//
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

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	profURLCourse     = "https://zyk.icve.com.cn/prod-api/website/course/trust/information?courseId=%s"
	profURLCourseList = "https://zyk.icve.com.cn/prod-api/teacher/courseList/myCourseList?pageNum=1&pageSize=999&flag=%d"
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
	c           *util.Client
	headers     map[string]string
	mode        int
	cid         string // courseId from URL
	courseID    string // courseInfoId from course info
	openCourse  string
	accessToken string
	title       string
	purchased   bool
	courseList  []profCourseItem
}

type profCourseItem struct {
	Name     string
	ID       string // courseInfoId when known
	CourseID string
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
	x.cid, x.openCourse = x.resolveURLCourseID(rawURL)
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
		"cookie":             cookieHeader(jar, icveCookieOrigins("https://zyk.icve.com.cn/")),
		"User-Agent":         util.RandomUA(),
	}
	accessToken := ensureICVEBearerAuth(c, headers, profURLPassLogin, profURLCheckLogin)
	return &profCtx{c: c, headers: headers, mode: mode, accessToken: accessToken}
}

func parseProfCID(raw string) string {
	cid, _, _, _ := parseProfURLTarget(raw)
	return cid
}

func parseProfURLTarget(raw string) (cid, mid, openCourse string, root bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err == nil && strings.EqualFold(u.Hostname(), "zyk.icve.com.cn") {
		q := u.Query()
		switch {
		case strings.Contains(strings.ToLower(u.Path), "coursedetailed"):
			cid = firstNonEmpty(q.Get("id"), q.Get("courseId"))
			openCourse = q.Get("openCourse")
			return
		case strings.Contains(strings.ToLower(u.Path), "icve-study"):
			mid = firstNonEmpty(q.Get("id"), q.Get("mid"))
			return
		case strings.Trim(u.Path, "/") == "":
			root = true
			return
		}
		cid = firstNonEmpty(q.Get("id"), q.Get("courseId"))
		openCourse = q.Get("openCourse")
		if cid != "" {
			return
		}
	}
	if m := profCIDRe.FindStringSubmatch(raw); len(m) >= 2 {
		cid = strings.TrimSpace(m[1])
	}
	return
}

func (x *profCtx) resolveURLCourseID(rawURL string) (string, string) {
	cid, mid, openCourse, root := parseProfURLTarget(rawURL)
	if cid == "" && mid != "" {
		cid = x.getCIDByMID(mid)
	}
	if cid == "" && root && x.accessToken != "" {
		courses := append(x.getCourseList(1), x.getCourseList(2)...)
		if len(courses) > 0 {
			cid = courses[0].CourseID
			if x.title == "" {
				x.title = cleanTitle(courses[0].Name)
			}
			x.purchased = true
		}
	}
	return cid, openCourse
}

func (x *profCtx) getCourseList(flag int) []profCourseItem {
	if x.accessToken == "" {
		return nil
	}
	body, err := x.c.GetString(fmt.Sprintf(profURLCourseList, flag), x.headers)
	if err != nil {
		return nil
	}
	root := parseJSONMap(body)
	rows := listAt(root, "rows")
	out := make([]profCourseItem, 0, len(rows))
	for _, row := range rows {
		courseID := str(row["courseId"])
		if courseID == "" {
			continue
		}
		name := firstNonEmpty(str(row["courseInfoName"]), str(row["courseName"]))
		if str(row["courseName"]) != "" && str(row["courseInfoName"]) != "" {
			name = str(row["courseName"]) + "-" + str(row["courseInfoName"])
		}
		out = append(out, profCourseItem{
			Name:     cleanTitle(name),
			ID:       str(row["courseInfoId"]),
			CourseID: courseID,
		})
	}
	return out
}

func (x *profCtx) getCIDByMID(mid string) string {
	if x.accessToken == "" || strings.TrimSpace(mid) == "" {
		return ""
	}
	body, err := x.c.GetString(fmt.Sprintf(profURLContent, url.QueryEscape(mid)), x.headers)
	if err != nil {
		return ""
	}
	return str(mapAt(parseJSONMap(body), "data")["courseId"])
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
		selected := courseInfos[0]
		selectedIdx := 0
		if x.openCourse != "" {
			for idx, ci := range courseInfos {
				if x.openCourse == str(ci["id"]) || x.openCourse == str(ci["courseInfoId"]) || x.openCourse == str(ci["openCourse"]) {
					selected = ci
					selectedIdx = idx
					break
				}
			}
		}
		x.courseID = firstNonEmpty(str(selected["id"]), str(selected["courseInfoId"]))
		selectedItem := func(idx int, ci map[string]any) profCourseItem {
			return profCourseItem{
				Name:     fmt.Sprintf("{%d}--%s", idx+1, cleanTitle(str(ci["name"]))),
				ID:       firstNonEmpty(str(ci["id"]), str(ci["courseInfoId"])),
				CourseID: firstNonEmpty(str(ci["courseId"]), x.cid),
			}
		}
		if x.openCourse != "" {
			x.courseList = append(x.courseList, selectedItem(selectedIdx, selected))
			return nil
		}
		for idx, ci := range courseInfos {
			x.courseList = append(x.courseList, profCourseItem{
				Name:     fmt.Sprintf("{%d}--%s", idx+1, cleanTitle(str(ci["name"]))),
				ID:       firstNonEmpty(str(ci["id"]), str(ci["courseInfoId"])),
				CourseID: firstNonEmpty(str(ci["courseId"]), x.cid),
			})
		}
	}
	return nil
}

func (x *profCtx) joinCourse(courseInfoID string) bool {
	if x.accessToken == "" || courseInfoID == "" {
		return false
	}
	body, err := x.c.GetString(fmt.Sprintf(profURLJoin, url.QueryEscape(courseInfoID)), x.headers)
	if err != nil {
		return false
	}
	ok := courseOKCodeRe.MatchString(body)
	if ok {
		x.purchased = true
	}
	return ok
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
		_ = x.joinCourse(cInfoID)
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
	payload := mergeICVEResourcePayload(data, data["fileUrl"])
	if u, _, kind := resolveICVEResourceMedia(x.c, x.headers, x.mode, payload, "mp4"); u != "" && kind == "video" {
		return u
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
	payload := mergeICVEResourcePayload(data, data["fileUrl"])
	u, _, _ := resolveICVEResourceMedia(x.c, x.headers, x.mode, payload, "")
	if idx := strings.LastIndex(u, "?"); idx > 0 {
		u = u[:idx]
	}
	if u != "" && strings.HasPrefix(u, "http") {
		return u
	}
	return ""
}

func (x *profCtx) getSourcePayload(sourceID string) map[string]any {
	body, err := x.c.GetString(fmt.Sprintf(profURLSource, url.QueryEscape(sourceID)), x.headers)
	if err != nil {
		return nil
	}
	data := mapAt(parseJSONMap(body), "data")
	if len(data) == 0 {
		return nil
	}
	return mergeICVEResourcePayload(data, data["fileUrl"])
}

func mergeICVEResourcePayload(data map[string]any, payloadValues ...any) map[string]any {
	payload := map[string]any{}
	for _, value := range payloadValues {
		switch v := value.(type) {
		case map[string]any:
			for k, val := range v {
				payload[k] = val
			}
		case string:
			for k, val := range parseICVEResourcePayload(v) {
				payload[k] = val
			}
		}
	}
	for k, v := range data {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	return payload
}

func (x *profCtx) buildMedia(items []profSourceItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		entries = append(entries, buildICVEResourceEntries(
			x.c,
			x.headers,
			x.mode,
			x.getSourcePayload(item.FileID),
			item.FileType,
			item.Name,
			"profession",
		)...)
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

func buildICVEResourceEntries(c *util.Client, headers map[string]string, mode int, payload map[string]any, fileType, baseName, module string) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	for _, res := range resolveICVEResourceMediaList(c, headers, mode, payload, fileType, baseName) {
		if res.URL == "" {
			continue
		}
		if res.Kind == "video" && mode == ONLY_PDF {
			continue
		}
		u := res.URL
		if res.Kind != "video" {
			if idx := strings.LastIndex(u, "?"); idx > 0 {
				u = u[:idx]
			}
		}
		ext := res.Ext
		if ext == "" {
			ext = pickExt(u)
		}
		if ext == "" {
			if res.Kind == "video" {
				ext = "mp4"
			} else {
				ext = firstNonEmpty(res.FileType, fileType)
			}
		}
		ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
		if ext == "" {
			ext = "html"
		}
		kind := firstNonEmpty(res.Kind, fileType, "file")
		entries = append(entries, &extractor.MediaInfo{
			Site:  "icve",
			Title: firstNonEmpty(res.Name, baseName),
			Streams: map[string]extractor.Stream{
				ext: {
					Quality:   ext,
					URLs:      []string{u},
					Format:    ext,
					NeedMerge: ext == "m3u8",
					Headers:   cloneHeaders(headers),
				},
			},
			Extra: map[string]any{"kind": kind, "file_type": firstNonEmpty(res.FileType, fileType), "module": module},
		})
	}
	return entries
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
