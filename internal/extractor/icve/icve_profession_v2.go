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
	courseInfoID  string
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
		x.cid = firstNonEmpty(u.Query().Get("id"), u.Query().Get("courseId"))
		x.classID = u.Query().Get("classId")
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
		"cookie":             cookieHeader(jar, []string{"https://zjy2.icve.com.cn/", referer + "/"}),
		"User-Agent":         util.RandomUA(),
	}
	return &profV2Ctx{c: c, headers: headers, mode: mode}
}

// loadCourseInfo fetches the course list and picks the first (or matching) course.
// Source: Icve_Profession_V2._get_course_list + _get_title
func (x *profV2Ctx) loadCourseInfo() error {
	body, err := x.c.GetString(profV2URLCourseList, x.headers)
	if err != nil {
		// If listing fails (no auth), use URL-derived cid
		if x.cid != "" {
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

		// If cid matches or we have no cid, pick this one
		if x.cid == "" || x.cid == courseID || x.cid == courseInfoID {
			x.cid = courseID
			x.courseInfoID = courseInfoID
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

	// fileUrl in V2 is a JSON-encoded string
	fileURLStr := str(data["fileUrl"])
	ossGenURL := regexExtract(`"ossGenUrl"\s*:\s*"(.*?)"`, fileURLStr)
	urlField := regexExtract(`"url"\s*:\s*"(.*?)"`, fileURLStr)

	if ossGenURL != "" && urlField != "" {
		statusBody, err := x.c.GetString(fmt.Sprintf(urlSourceStatus, strings.TrimLeft(urlField, "/")), x.headers)
		if err == nil {
			status := parseJSONMap(statusBody)
			args := mapAt(status, "args")
			ac := &aiCtx{c: x.c, headers: x.headers, mode: x.mode}
			u := ac.selectTranscodedURL(ossGenURL, "mp4", map[string]any{"args": args})
			if u != "" {
				return u
			}
		}
	}

	// Fallback to ossOriUrl
	ossOriURL := regexExtract(`"ossOriUrl"\s*:\s*"(.*?)"`, fileURLStr)
	if ossOriURL != "" {
		ossOriURL = strings.SplitN(ossOriURL, "?", 2)[0]
		if strings.HasPrefix(ossOriURL, "http") {
			return ossOriURL
		}
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
	u := str(data["fileUrl"])
	// V2 fileUrl may be a JSON string or direct URL
	if strings.HasPrefix(u, "{") || strings.HasPrefix(u, "[") {
		ossOriURL := regexExtract(`"ossOriUrl"\s*:\s*"(.*?)"`, u)
		if ossOriURL != "" {
			u = strings.SplitN(ossOriURL, "?", 2)[0]
		} else {
			fileURLInner := regexExtract(`"fileUrl"\s*:\s*"(.*?)"`, u)
			if fileURLInner != "" {
				u = strings.SplitN(fileURLInner, "?", 2)[0]
			}
		}
	} else if idx := strings.LastIndex(u, "?"); idx > 0 {
		u = u[:idx]
	}
	if u != "" && strings.HasPrefix(u, "http") {
		return u
	}
	return ""
}

func (x *profV2Ctx) buildMedia(items []profSourceItem) (*extractor.MediaInfo, error) {
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
			Extra: map[string]any{"kind": item.FileType, "module": "profession_v2"},
		})
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
