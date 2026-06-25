// Icve_Qun – qun.icve.com.cn community course extraction.
//
// Source: Icve_Qun.pyc.1shot.cdc.py
// API: process/getList → process/viewDirectory for individual resources.
// Structure: topics → cells → childCells (3 levels).
// Auth: requires auth cookie (NeedAuth: true).
package icve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	qunURLCourse      = "https://qun.icve.com.cn/api/zyqapi/course/getCourseData"
	qunURLJoin        = "https://qun.icve.com.cn/api/zyqapi/course/joinCourse"
	qunURLInfos       = "https://qun.icve.com.cn/study/process/getList"
	qunURLCourseInfos = "https://qun.icve.com.cn/api/zyqapi/course/getCourseProcess"
	qunURLSource      = "https://qun.icve.com.cn/study/process/viewDirectory"
)

// Icve_Qun URLs: qun.icve.com.cn/zyq/course/{id}
var qunPatterns = []string{
	`\s*https?://qun\.icve\.com\.cn/zyq/course/(?P<cid1>[-\w]+)`,
	`\s*https?://qun\.icve\.com\.cn/.*?(?:courseOpenId|courseId|courseid)=(?P<cid2>[-\w]+)`,
	`\s*https?://qun\.icve\.com\.cn`,
}

var qunCIDRe = regexp.MustCompile(
	`(?i)https?://qun\.icve\.com\.cn/zyq/course/([-\w]+)`,
)

func init() {
	extractor.Register(&IcveQun{}, extractor.SiteInfo{Name: "IcveQun", URL: "qun.icve.com.cn", NeedAuth: true})
}

type IcveQun struct{}

func (i *IcveQun) Patterns() []string { return qunPatterns }

type qunCtx struct {
	c       *util.Client
	headers map[string]string
	mode    int
	cid     string
	title   string
}

type qunSourceItem struct {
	Name   string
	FileID string
}

func (i *IcveQun) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
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

	x := newQunCtx(jar, modeFromQuality(opts.Quality))
	x.cid = parseQunCID(rawURL)
	if x.cid == "" {
		return nil, fmt.Errorf("icve_qun: cannot parse course id from URL")
	}

	if err := x.loadTitle(); err != nil {
		return nil, err
	}

	items, err := x.loadInfos()
	if err != nil {
		return nil, err
	}
	return x.buildMedia(items)
}

func newQunCtx(jar http.CookieJar, mode int) *qunCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Platform": `"Windows"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Google Chrome";v="115", "Chromium";v="115"`,
		"Referer":            "https://qun.icve.com.cn",
		"cookie":             cookieHeader(jar, []string{"https://qun.icve.com.cn/", referer + "/"}),
		"User-Agent":         util.RandomUA(),
	}
	return &qunCtx{c: c, headers: headers, mode: mode}
}

func parseQunCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := qunCIDRe.FindStringSubmatch(raw); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" && !strings.Contains(parts[i], ".") {
			return parts[i]
		}
	}
	return ""
}

// loadTitle fetches course data to get title.
// Source: Icve_Qun._get_title
func (x *qunCtx) loadTitle() error {
	body, err := x.c.PostForm(qunURLCourse, map[string]string{
		"courseOpenId": x.cid,
	}, x.headers)
	if err != nil {
		return fmt.Errorf("icve_qun: load title: %w", err)
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")
	courseData := mapAt(data, "courseData")
	title := str(courseData["Title"])
	if title != "" {
		x.title = cleanTitle(title)
	}
	return nil
}

// loadInfos fetches the process list and recurses through topics/cells/childCells.
// Source: Icve_Qun._get_infos + _get_inner_infos
func (x *qunCtx) loadInfos() ([]qunSourceItem, error) {
	// Try joinCourse first
	_, _ = x.c.PostForm(qunURLJoin, map[string]string{
		"courseOpenId": x.cid,
	}, x.headers)

	// Try process/getList
	body, err := x.c.PostForm(qunURLInfos, map[string]string{
		"courseOpenId": x.cid,
	}, x.headers)
	if err != nil {
		return nil, fmt.Errorf("icve_qun: load infos: %w", err)
	}
	root := parseJSONMap(body)
	list := listAt(root, "list")

	if len(list) == 0 {
		// Fallback: getCourseProcess
		body2, err2 := x.c.PostForm(qunURLCourseInfos, map[string]string{
			"courseOpenId": x.cid,
		}, x.headers)
		if err2 == nil {
			root2 := parseJSONMap(body2)
			list = listAt(root2, "data")
		}
	}

	var items []qunSourceItem
	for idx, item := range list {
		subItems := x.getInnerInfos(item, []int{idx + 1}, 1)
		items = append(items, subItems...)
	}
	return items, nil
}

// getInnerInfos recursively walks topics → cells → childCells.
// Source: Icve_Qun._get_inner_infos
func (x *qunCtx) getInnerInfos(item map[string]any, indexTup []int, level int) []qunSourceItem {
	var items []qunSourceItem
	// Determine children key based on level
	var childKey string
	switch level {
	case 1:
		childKey = "topics"
	case 2:
		childKey = "cells"
	default:
		childKey = "childCells"
	}

	title := strings.ReplaceAll(str(item["title"]), "&nbsp;", "")
	id := str(item["Id"])
	children := listAt(item, childKey)

	if len(children) > 0 {
		for childIdx, child := range children {
			childPrefix := append(append([]int{}, indexTup...), childIdx+1)
			subItems := x.getInnerInfos(child, childPrefix, level+1)
			items = append(items, subItems...)
		}
	} else if id != "" {
		items = append(items, qunSourceItem{
			Name:   fmt.Sprintf("(%s)--%s", joinInts(indexTup, "."), cleanTitle(title)),
			FileID: id,
		})
	}
	return items
}

// getSourceURL calls viewDirectory to get the resource download URL.
// Source: Icve_Qun._get_source_url
func (x *qunCtx) getSourceURL(cellID string) string {
	body, err := x.c.PostForm(qunURLSource, map[string]string{
		"cellId":       cellID,
		"courseOpenId": x.cid,
	}, x.headers)
	if err != nil {
		return ""
	}
	// Source: response has resUrl which is a JSON string containing urls.download
	root := parseJSONMap(body)
	resURL := str(root["resUrl"])
	if resURL == "" {
		return ""
	}
	inner := parseJSONMap(resURL)
	urls := mapAt(inner, "urls")
	dl := str(urls["download"])
	if idx := strings.LastIndex(dl, "?"); idx > 0 {
		dl = dl[:idx]
	}
	return dl
}

func (x *qunCtx) buildMedia(items []qunSourceItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		u := x.getSourceURL(item.FileID)
		if u == "" {
			continue
		}
		ext := pickExt(u)
		isVideo := isVideoType(ext)
		if isVideo && x.mode == ONLY_PDF {
			continue
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
			Extra: map[string]any{"module": "qun"},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icve_qun: no playable entries")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{
		Site:    "icve",
		Title:   firstNonEmpty(x.title, x.cid, "icve_qun"),
		Entries: entries,
		Extra:   map[string]any{"course_id": x.cid, "module": "qun"},
	}, nil
}

// Ensure json import is referenced.
var _ = json.NewDecoder
