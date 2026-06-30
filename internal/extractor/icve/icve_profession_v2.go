// Icve_Profession_V2 – zjy2.icve.com.cn SPOC course extraction.
//
// Source: Icve_Profession_V2.pyc.1shot.cdc.py
// API: courseDesign/study/record → getStudyCellInfo, passLogin auth.
// Auth: requires Bearer token (NeedAuth: true).
package icve

import (
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
	profV2URLCourseList = "https://zjy2.icve.com.cn/prod-api/spoc/courseInfoStudent/myCourseList?pageNum=1&pageSize=999"
	profV2URLInfos      = "https://zjy2.icve.com.cn/prod-api/spoc/courseDesign/study/record?courseId=%s&courseInfoId=%s&parentId=%s&level=%d&classId=%s"
	profV2URLSource     = "https://zjy2.icve.com.cn/prod-api/spoc/courseDesign/getStudyCellInfo?id=%s&classId=%s"
	profV2URLPassLogin  = "https://zjy2.icve.com.cn/prod-api/auth/passLogin?token=%s"
	profV2URLCheckLogin = "https://zjy2.icve.com.cn/prod-api/system/user/getInfo"
)

// Source: Mooc_Config courses_re['Icve_Profession_V2']
var profV2Patterns = []string{
	`\s*https?://zjy2\.icve\.com\.cn/study/coursePreview/.*?(?:id|courseId)=(?P<cid>[-\w]+)`,
	`\s*https?://zjy2\.icve\.com\.cn`,
}

var profV2CIDRe = regexp.MustCompile(
	`(?i)https?://zjy2\.icve\.com\.cn/.*?(?:id|courseId)=([-\w]+)`,
)

func init() {
	extractor.Register(&IcveProfessionV2{}, extractor.SiteInfo{Name: "IcveProfessionV2", URL: "zjy2.icve.com.cn", NeedAuth: true})
}

type IcveProfessionV2 struct{}

func (i *IcveProfessionV2) Patterns() []string { return profV2Patterns }

type profV2Ctx struct {
	c            *util.Client
	headers      map[string]string
	mode         int
	cid          string // courseId
	courseInfoID string
	classID      string
	title        string
}

func (i *IcveProfessionV2) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
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

	x := newProfV2Ctx(jar, modeFromQuality(opts.Quality))

	// Parse courseId and classId from URL
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		x.cid = firstNonEmpty(q.Get("id"), q.Get("courseId"), q.Get("course_id"))
		x.courseInfoID = firstNonEmpty(q.Get("courseInfoId"), q.Get("course_info_id"))
		x.classID = firstNonEmpty(q.Get("classId"), q.Get("class_id"))
	}
	if x.cid == "" {
		if m := profV2CIDRe.FindStringSubmatch(rawURL); len(m) >= 2 {
			x.cid = strings.TrimSpace(m[1])
		}
	}

	// The V2 module requires login to list courses and pick one.
	// Without auth, try to extract from URL params.
	if err := x.loadCourseInfo(); err != nil {
		return nil, err
	}

	items, err := x.loadInfos()
	if err != nil {
		return nil, err
	}
	return x.buildMedia(items)
}

func newProfV2Ctx(jar http.CookieJar, mode int) *profV2Ctx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Platform": `"Windows"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Google Chrome";v="115", "Chromium";v="115"`,
		"Referer":            "https://zjy2.icve.com.cn",
		"cookie":             cookieHeader(jar, icveCookieOrigins("https://zjy2.icve.com.cn/")),
		"User-Agent":         util.RandomUA(),
	}
	_ = ensureICVEBearerAuth(c, headers, profV2URLPassLogin, profV2URLCheckLogin)
	return &profV2Ctx{c: c, headers: headers, mode: mode}
}

// loadCourseInfo fetches the course list and picks the first (or matching) course.
// Source: Icve_Profession_V2._get_course_list + _get_title
func (x *profV2Ctx) loadCourseInfo() error {
	if x.cid != "" && x.courseInfoID != "" && x.classID != "" {
		return nil
	}
	body, err := x.c.GetString(profV2URLCourseList, x.headers)
	if err != nil {
		// If listing fails (no auth), use URL-derived ids when complete enough.
		if x.cid != "" && x.courseInfoID != "" && x.classID != "" {
			return nil
		}
		return fmt.Errorf("icve_profession_v2: load course list: %w", err)
	}
	root := parseJSONMap(body)
	rows := listAt(root, "rows")
	for _, row := range rows {
		courseID := str(row["courseId"])
		courseInfoID := str(row["courseInfoId"])
		classID := str(row["classId"])
		title := str(row["courseName"])

		// If URL id matches courseId or courseInfoId, normalize to the API's
		// courseId while preserving URL-provided courseInfoId/classId.
		if x.cid == "" || x.cid == courseID || x.cid == courseInfoID || x.courseInfoID == courseInfoID {
			if courseID != "" {
				x.cid = courseID
			}
			x.courseInfoID = firstNonEmpty(x.courseInfoID, courseInfoID)
			x.classID = firstNonEmpty(x.classID, classID)
			x.title = cleanTitle(title)
			return nil
		}
	}
	// If no match, use the first course
	if len(rows) > 0 && x.courseInfoID == "" {
		row := rows[0]
		x.cid = str(row["courseId"])
		x.courseInfoID = str(row["courseInfoId"])
		x.classID = firstNonEmpty(x.classID, str(row["classId"]))
		x.title = cleanTitle(str(row["courseName"]))
	}
	return nil
}

// loadInfos enumerates the course design tree.
// Source: Icve_Profession_V2._get_infos + _get_inner_infos
func (x *profV2Ctx) loadInfos() ([]profSourceItem, error) {
	if x.cid == "" || x.courseInfoID == "" || x.classID == "" {
		return nil, fmt.Errorf("icve_profession_v2: missing courseId/courseInfoId/classId")
	}
	body, err := x.c.GetString(
		fmt.Sprintf(profV2URLInfos, url.QueryEscape(x.cid), url.QueryEscape(x.courseInfoID), "0", 1, url.QueryEscape(x.classID)),
		x.headers,
	)
	if err != nil {
		return nil, fmt.Errorf("icve_profession_v2: load infos: %w", err)
	}
	chapters := parseJSONMapList(body)
	var items []profSourceItem
	for idx, chapter := range chapters {
		subItems := x.getInnerInfos(chapter, []int{idx + 1}, 1)
		items = append(items, subItems...)
	}
	return items, nil
}

// getInnerInfos recursively builds the source list.
// Source: Icve_Profession_V2._get_inner_infos
func (x *profV2Ctx) getInnerInfos(item map[string]any, indexTup []int, levelNum int) []profSourceItem {
	var items []profSourceItem
	fileType := firstNonEmpty(str(item["fileType"]), "")
	id := str(item["id"])
	name := cleanTitle(str(item["name"]))

	if fileType == "父节点" || fileType == "子节点" || fileType == "文件夹" {
		var children []map[string]any
		if fileType == "文件夹" {
			children = listAt(item, "children")
		} else if id != "" {
			body, err := x.c.GetString(
				fmt.Sprintf(profV2URLInfos, url.QueryEscape(x.cid), url.QueryEscape(x.courseInfoID), url.QueryEscape(id), levelNum, url.QueryEscape(x.classID)),
				x.headers,
			)
			if err == nil {
				children = parseJSONMapList(body)
			}
		}
		if len(children) > 0 {
			for childIdx, child := range children {
				childPrefix := append(append([]int{}, indexTup...), childIdx+1)
				childItems := x.getInnerInfos(child, childPrefix, levelNum+1)
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

// getVideoURL resolves transcoded video for a V2 source.
// Source: Icve_Profession_V2._get_video_url – fileUrl is a JSON string inside data.
func (x *profV2Ctx) getVideoURL(sourceID string) string {
	body, err := x.c.GetString(
		fmt.Sprintf(profV2URLSource, url.QueryEscape(sourceID), url.QueryEscape(x.classID)),
		x.headers,
	)
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

// getSourceURL for V2 gets file URL for non-video sources.
func (x *profV2Ctx) getSourceURL(sourceID string) string {
	body, err := x.c.GetString(
		fmt.Sprintf(profV2URLSource, url.QueryEscape(sourceID), url.QueryEscape(x.classID)),
		x.headers,
	)
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

func (x *profV2Ctx) getSourcePayload(sourceID string) map[string]any {
	body, err := x.c.GetString(
		fmt.Sprintf(profV2URLSource, url.QueryEscape(sourceID), url.QueryEscape(x.classID)),
		x.headers,
	)
	if err != nil {
		return nil
	}
	data := mapAt(parseJSONMap(body), "data")
	if len(data) == 0 {
		return nil
	}
	return mergeICVEResourcePayload(data, data["fileUrl"])
}

func (x *profV2Ctx) buildMedia(items []profSourceItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		entries = append(entries, buildICVEResourceEntries(
			x.c,
			x.headers,
			x.mode,
			x.getSourcePayload(item.FileID),
			item.FileType,
			item.Name,
			"profession_v2",
		)...)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icve_profession_v2: no playable entries")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{
		Site:    "icve",
		Title:   firstNonEmpty(x.title, x.cid, "icve_profession_v2"),
		Entries: entries,
		Extra:   map[string]any{"course_id": x.cid, "module": "profession_v2"},
	}, nil
}
