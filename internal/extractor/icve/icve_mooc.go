// Icve_Mooc – www.icve.com.cn MOOC course extraction.
//
// Source: Icve_Mooc.pyc.1shot.cdc.py
// API: directoryList → viewDirectory (POST) to get download URLs.
// Auth: requires cookie from icve login (NeedAuth: true).
package icve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	moocURLHeadInfo = "https://www.icve.com.cn/Portal/courseinfo/getHeadInfo_upgrade"
	moocURLInfos    = "https://www.icve.com.cn/study/Directory/directoryList?courseId=%s"
	moocURLSource   = "https://www.icve.com.cn/study/directory/view"
	moocURLJoin     = "https://www.icve.com.cn/Portal/study/joinStudy"
)

var moocPatterns = []string{
	// Source: Mooc_Config courses_re – Icve_Mooc was not in courses_re, but the
	// module uses www.icve.com.cn URLs with courseId / courseid params.
	// Covers: portal_new/courseinfo, study/directory, Portal/courseinfo
	`\s*https?://www\.icve\.com\.cn/(?:portal_new/courseinfo|study/directory|Portal/courseinfo).*?(?:courseId|courseid)=(?P<cid1>[-\w]+)`,
	// Broad fallback: www.icve.com.cn with courseid= anywhere
	`\s*https?://www\.icve\.com\.cn/.*?(?:courseId|courseid)=(?P<cid2>[-\w]+)`,
}

var moocCIDRe = regexp.MustCompile(
	`(?i)https?://www\.icve\.com\.cn/.*?(?:courseId|courseid)=([-\w]+)`,
)

func init() {
	extractor.Register(&IcveMooc{}, extractor.SiteInfo{Name: "IcveMooc", URL: "www.icve.com.cn", NeedAuth: true})
}

type IcveMooc struct{}

func (i *IcveMooc) Patterns() []string { return moocPatterns }

type moocCtx struct {
	c       *util.Client
	headers map[string]string
	mode    int
	cid     string
	title   string
}

func (i *IcveMooc) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil {
		opts = &extractor.ExtractOpts{}
	}
	jar := opts.Cookies
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}

	// Resolve smartedu redirect
	resolved, err := resolveSmartEduURL(rawURL, jar)
	if err == nil && resolved != "" {
		rawURL = resolved
	}

	x := newMoocCtx(jar, modeFromQuality(opts.Quality))
	x.cid = parseMoocCID(rawURL)
	if x.cid == "" {
		return nil, fmt.Errorf("icve_mooc: cannot parse course id from URL")
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

func newMoocCtx(jar http.CookieJar, mode int) *moocCtx {
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
		"cookie":             cookieHeader(jar, []string{referer + "/", "https://www.icve.com.cn/"}),
		"User-Agent":         util.RandomUA(),
	}
	return &moocCtx{c: c, headers: headers, mode: mode}
}

func parseMoocCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := moocCIDRe.FindStringSubmatch(raw); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, key := range []string{"courseId", "courseid", "cid", "id"} {
		if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
			return v
		}
	}
	return ""
}

// loadTitle calls getHeadInfo_upgrade to get Title + ProjectName.
// Source: Icve_Mooc._get_title
func (x *moocCtx) loadTitle() error {
	body, err := x.c.PostForm(moocURLHeadInfo, map[string]string{"courseid": x.cid}, x.headers)
	if err != nil {
		return fmt.Errorf("icve_mooc: load title: %w", err)
	}
	data := parseJSONMap(body)
	list := mapAt(data, "list")
	title := str(list["Title"])
	project := str(list["ProjectName"])
	if title != "" && project != "" {
		x.title = cleanTitle(title + "_" + project)
	} else if title != "" {
		x.title = cleanTitle(title)
	}
	return nil
}

type moocItem struct {
	Name string
	ID   string
	Kind string // "video" or "file"
}

// loadInfos fetches the directory tree and builds flat items.
// Source: Icve_Mooc._get_infos + _get_inner_infos
func (x *moocCtx) loadInfos() ([]moocItem, error) {
	body, err := x.c.GetString(fmt.Sprintf(moocURLInfos, url.QueryEscape(x.cid)), x.headers)
	if err != nil {
		return nil, fmt.Errorf("icve_mooc: load infos: %w", err)
	}
	root := parseJSONMap(body)
	directory := listAt(root, "directory")
	var items []moocItem
	for secIdx, section := range directory {
		sec := mapAt(section, "section")
		_ = cleanTitle(str(sec["Title"]))
		chapters := listAt(section, "chapters")
		for chapIdx, chapter := range chapters {
			chap := mapAt(chapter, "chapter")
			_ = cleanTitle(str(chap["Title"]))
			cells := listAt(chapter, "cells")
			cellItems := collectMoocItems(cells, []int{secIdx + 1, chapIdx + 1})
			items = append(items, cellItems...)
			knowleges := listAt(chapter, "knowleges")
			for knIdx, kn := range knowleges {
				knObj := mapAt(kn, "knowlege")
				_ = cleanTitle(str(knObj["Title"]))
				knCells := listAt(kn, "cells")
				knItems := collectMoocItems(knCells, []int{secIdx + 1, chapIdx + 1, knIdx + 1})
				items = append(items, knItems...)
			}
		}
	}
	return items, nil
}

func collectMoocItems(cells []map[string]any, prefix []int) []moocItem {
	var items []moocItem
	videoCounter := 1
	fileCounter := 1
	for _, cell := range cells {
		cellType := strings.ToLower(str(cell["CellType"]))
		title := cleanTitle(str(cell["Title"]))
		id := str(cell["Id"])
		if id == "" {
			continue
		}
		if cellType == "video" {
			idxs := append(append([]int{}, prefix...), videoCounter)
			videoCounter++
			items = append(items, moocItem{
				Name: fmt.Sprintf("[%s]--%s", joinInts(idxs, "."), trimRStripMP4(title)),
				ID:   id,
				Kind: "video",
			})
		} else {
			idxs := append(append([]int{}, prefix...), fileCounter)
			fileCounter++
			items = append(items, moocItem{
				Name: fmt.Sprintf("(%s)--%s", joinInts(idxs, "."), title),
				ID:   id,
				Kind: "file",
			})
		}
	}
	return items
}

// getSourceURL calls viewDirectory to get the downloadurl for a cell.
// Source: Icve_Mooc._get_source_url
func (x *moocCtx) getSourceURL(cellID string) string {
	body, err := x.c.PostForm(moocURLSource, map[string]string{
		"enterType": "study",
		"courseId":   x.cid,
		"cellId":    cellID,
	}, x.headers)
	if err != nil {
		return ""
	}
	time.Sleep(100 * time.Millisecond) // source uses TIME_SLEEP = 3.6, we reduce
	data := parseJSONMap(body)
	inner := mapAt(data, "data")
	dl := str(inner["downloadurl"])
	// Strip query params per source
	if idx := strings.LastIndex(dl, "?"); idx > 0 {
		dl = dl[:idx]
	}
	return dl
}

func (x *moocCtx) buildMedia(items []moocItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		u := x.getSourceURL(item.ID)
		if u == "" {
			continue
		}
		ext := pickExt(u)
		isVideo := item.Kind == "video"
		if isVideo && x.mode == ONLY_PDF {
			continue
		}
		if ext == "" {
			if isVideo {
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
			Extra: map[string]any{"kind": item.Kind, "module": "mooc"},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icve_mooc: no playable entries")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{
		Site:    "icve",
		Title:   firstNonEmpty(x.title, x.cid, "icve_mooc"),
		Entries: entries,
		Extra:   map[string]any{"course_id": x.cid, "module": "mooc"},
	}, nil
}

// PostJSON helper for sending JSON body POSTs (used by multiple sub-modules).
func postJSON(c *util.Client, urlStr string, data any, headers map[string]string) (string, error) {
	bodyJSON, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	resp, err := c.Post(urlStr, strings.NewReader(string(bodyJSON)), h)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := readBody(resp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
