// Icve_Course – mooc-old.icve.com.cn / course.icve.com.cn extraction.
//
// Source: Icve_Course.pyc.1shot.cdc.py
// API: courseware_index → getItemResourceDownloadUrl, plus zjy2 passLogin flow.
// Auth: requires cookie with token + UNTYXLCOOKIE (NeedAuth: true).
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
	courseURLDetail    = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_selectCourseDetails.action"
	courseURLCourseID  = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_getClassAndCourseIdByCode.action"
	courseURLCIDClass  = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_selectCourseId.action"
	courseURLInfos     = "https://course.icve.com.cn/learnspace/learn/learn/templateeight/courseware_index.action?params.courseId=%s"
	courseURLM3U8      = "https://course.icve.com.cn/learnspace/learn/learn/templateeight/content_video.action?params.courseId=%s&params.itemId=%s"
	courseURLResource  = "https://course.icve.com.cn/learnspace/learn/learnCourseItem/getItemResourceDownloadUrl.json"
	courseURLOutline   = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_getCourseOutline.action"
	courseURLYunpan    = "https://spoc-yunpan-sdk.icve.com.cn/api/downloadbyte?token=%s/%s&isView=true&metaId=%s"
)

// Source: Mooc_Config courses_re['Icve_Course']
var coursePatterns = []string{
	`\s*https?://course\.icve\.com\.cn/learnspace/learn/learn/templateeight/.*?course[Ii]d=(?P<cid>\w+)`,
	`\s*https?://mooc-old\.icve\.com\.cn/cms/courseDetails/index\.htm\?cid=(?P<class_code>\w+)`,
	`\s*https?://mooc-old\.icve\.com\.cn/cms/courseDetails/index\.htm\?class[Ii]d=(?P<class_id>\w+)`,
	`\s*https?://mooc-old\.icve\.com\.cn`,
}

var courseCIDRe = regexp.MustCompile(
	`(?i)(?:course\.icve\.com\.cn/.*?course[Ii]d=(\w+))|(?:mooc-old\.icve\.com\.cn/cms/courseDetails/index\.htm\?(?:cid|class[Ii]d)=(\w+))`,
)

func init() {
	extractor.Register(&IcveCourse{}, extractor.SiteInfo{Name: "IcveCourse", URL: "mooc-old.icve.com.cn", NeedAuth: true})
}

type IcveCourse struct{}

func (i *IcveCourse) Patterns() []string { return coursePatterns }

type courseCtx struct {
	c       *util.Client
	headers map[string]string
	mode    int
	cid     string
	title   string
	token   string
}

func (i *IcveCourse) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
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

	x := newCourseCtx(jar, modeFromQuality(opts.Quality))
	x.cid = parseCourseCID(rawURL)
	if x.cid == "" {
		return nil, fmt.Errorf("icve_course: cannot parse course id from URL")
	}

	if err := x.loadCourseInfo(); err != nil {
		return nil, err
	}

	items, err := x.loadCourseTree()
	if err != nil {
		return nil, err
	}

	return x.buildMedia(items)
}

func newCourseCtx(jar http.CookieJar, mode int) *courseCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Platform": `"Windows"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Google Chrome";v="115", "Chromium";v="115"`,
		"Referer":            referer,
		"cookie":             cookieHeader(jar, []string{referer + "/", "https://mooc-old.icve.com.cn/", "https://course.icve.com.cn/"}),
		"User-Agent":         util.RandomUA(),
	}
	return &courseCtx{c: c, headers: headers, mode: mode}
}

func parseCourseCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := courseCIDRe.FindStringSubmatch(raw); len(m) >= 3 {
		for _, g := range m[1:] {
			if strings.TrimSpace(g) != "" {
				return strings.TrimRight(strings.TrimSpace(g), "_")
			}
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, key := range []string{"courseId", "cid", "classId", "class_id"} {
		if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
			return v
		}
	}
	return ""
}

// loadCourseInfo gets course outline/details.
// Source: Icve_Course._get_title via portalMooc_getCourseOutline.action
func (x *courseCtx) loadCourseInfo() error {
	body, err := x.c.PostForm(courseURLOutline, map[string]string{"courseId": x.cid}, x.headers)
	if err != nil {
		return fmt.Errorf("icve_course: load course info: %w", err)
	}
	data := parseJSONMap(body)
	courseName := str(data["courseName"])
	if courseName == "" {
		courseName = str(mapAt(data, "data")["courseName"])
	}
	if courseName != "" {
		x.title = cleanTitle(courseName)
	}
	return nil
}

type courseItem struct {
	Name   string
	ItemID string
	Kind   string // "video" or "file"
}

// loadCourseTree fetches the courseware_index and builds items from the page's JSON.
// Source: Icve_Course._get_infos via courseware_index.action
func (x *courseCtx) loadCourseTree() ([]courseItem, error) {
	body, err := x.c.GetString(fmt.Sprintf(courseURLInfos, url.QueryEscape(x.cid)), x.headers)
	if err != nil {
		return nil, fmt.Errorf("icve_course: load tree: %w", err)
	}

	// The courseware_index returns HTML with embedded JSON. Try to parse JSON first.
	root := parseJSONMap(body)
	if len(root) == 0 {
		// Try extracting JSON from HTML
		jsonStr := regexExtract(`var\s+coursewareData\s*=\s*(\{[\s\S]*?\});`, body)
		if jsonStr != "" {
			root = parseJSONMap(jsonStr)
		}
	}

	items := listAt(root, "data")
	if len(items) == 0 {
		items = listAt(root, "chapterList")
	}

	return x.flattenCourseItems(items, nil), nil
}

func (x *courseCtx) flattenCourseItems(nodes []map[string]any, prefix []int) []courseItem {
	var out []courseItem
	videoCounter := 1
	fileCounter := 1
	for idx, node := range nodes {
		pos := idx + 1
		nextPrefix := append(append([]int{}, prefix...), pos)
		name := cleanTitle(str(node["Title"]))
		if name == "" {
			name = cleanTitle(str(node["name"]))
		}

		// Recurse children
		for _, childKey := range []string{"chapters", "knowleges", "cells", "children"} {
			children := listAt(node, childKey)
			if len(children) > 0 {
				out = append(out, x.flattenCourseItems(children, nextPrefix)...)
			}
		}

		itemID := str(node["Id"])
		if itemID == "" {
			itemID = str(node["id"])
		}
		if itemID == "" {
			continue
		}
		cellType := strings.ToLower(firstNonEmpty(str(node["CellType"]), str(node["cellType"]), str(node["fileType"])))
		switch cellType {
		case "video", "mp4", "flv", "mpg", "avi", "mov":
			idxs := append(append([]int{}, prefix...), videoCounter)
			videoCounter++
			out = append(out, courseItem{
				Name:   fmt.Sprintf("[%s]--%s", joinInts(idxs, "."), trimRStripMP4(name)),
				ItemID: itemID,
				Kind:   "video",
			})
		default:
			if cellType == "" {
				continue
			}
			idxs := append(append([]int{}, prefix...), fileCounter)
			fileCounter++
			out = append(out, courseItem{
				Name:   fmt.Sprintf("(%s)--%s", joinInts(idxs, "."), name),
				ItemID: itemID,
				Kind:   "file",
			})
		}
	}
	return out
}

// getResourceURL gets download URL via getItemResourceDownloadUrl.json.
// Source: Icve_Course uses _get_source_url which POSTs to viewDirectory,
// but also has a resource download endpoint.
func (x *courseCtx) getResourceURL(itemID string) string {
	body, err := x.c.PostForm(courseURLResource, map[string]string{
		"courseId": x.cid,
		"itemId":  itemID,
	}, x.headers)
	if err != nil {
		return ""
	}
	data := parseJSONMap(body)
	u := str(data["url"])
	if u == "" {
		u = str(mapAt(data, "data")["url"])
	}
	if u == "" {
		u = str(mapAt(data, "data")["downloadurl"])
	}
	// Strip query params per source
	if idx := strings.LastIndex(u, "?"); idx > 0 {
		u = u[:idx]
	}
	return u
}

// getVideoM3U8 tries the m3u8 endpoint for a video item.
// Source: Icve_Course.url_m3u8
func (x *courseCtx) getVideoM3U8(itemID string) string {
	body, err := x.c.GetString(fmt.Sprintf(courseURLM3U8, url.QueryEscape(x.cid), url.QueryEscape(itemID)), x.headers)
	if err != nil {
		return ""
	}
	data := parseJSONMap(body)
	u := str(data["url"])
	if u == "" {
		u = str(mapAt(data, "data")["url"])
	}
	return u
}

func (x *courseCtx) buildMedia(items []courseItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		var u string
		if item.Kind == "video" {
			// Try m3u8 first, fall back to resource
			u = x.getVideoM3U8(item.ItemID)
			if u == "" {
				u = x.getResourceURL(item.ItemID)
			}
		} else {
			u = x.getResourceURL(item.ItemID)
		}
		if u == "" {
			continue
		}
		if item.Kind == "video" && x.mode == ONLY_PDF {
			continue
		}
		ext := pickExt(u)
		if ext == "" {
			if item.Kind == "video" {
				ext = "mp4"
			} else {
				ext = "html"
			}
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
			Extra: map[string]any{"kind": item.Kind, "module": "course"},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icve_course: no playable entries")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{
		Site:    "icve",
		Title:   firstNonEmpty(x.title, x.cid, "icve_course"),
		Entries: entries,
		Extra:   map[string]any{"course_id": x.cid, "module": "course"},
	}, nil
}

// Ensure json import is used.
var _ = json.Marshal
