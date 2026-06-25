// Icve_Weike – www.icve.com.cn micro-lesson (微课) extraction.
//
// Source: Icve_Weike.pyc.1shot.cdc.py
// API: getWeikeInfo → microstudy/view for individual items.
// Extends Mooc pattern: www.icve.com.cn with weikeId or microstudy URLs.
// Auth: requires cookie (NeedAuth: true).
package icve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	weikeURLInfos  = "https://www.icve.com.cn/portal/weikeInfo/getWeikeInfo"
	weikeURLSource = "https://www.icve.com.cn/portal/microstudy/view"
)

// Weike URLs: weikeId= or microstudy paths
var weikePatterns = []string{
	`\s*https?://www\.icve\.com\.cn/.*?weikeId=(?P<cid1>[-\w]+)`,
	`\s*https?://www\.icve\.com\.cn/.*?microstudy.*?(?:courseId|courseid)=(?P<cid2>[-\w]+)`,
}

var weikeCIDRe = regexp.MustCompile(
	`(?i)https?://www\.icve\.com\.cn/.*?(?:weikeId|courseId|courseid)=([-\w]+)`,
)

func init() {
	extractor.Register(&IcveWeike{}, extractor.SiteInfo{Name: "IcveWeike", URL: "www.icve.com.cn/weike", NeedAuth: true})
}

type IcveWeike struct{}

func (i *IcveWeike) Patterns() []string { return weikePatterns }

type weikeCtx struct {
	c       *util.Client
	headers map[string]string
	mode    int
	cid     string
	title   string
}

type weikeItem struct {
	Name string
	ID   string
	Kind string // "video" or "file"
}

func (i *IcveWeike) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
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

	x := newWeikeCtx(jar, modeFromQuality(opts.Quality))
	x.cid = parseWeikeCID(rawURL)
	if x.cid == "" {
		return nil, fmt.Errorf("icve_weike: cannot parse weike id from URL")
	}

	items, err := x.loadInfos()
	if err != nil {
		return nil, err
	}
	return x.buildMedia(items)
}

func newWeikeCtx(jar http.CookieJar, mode int) *weikeCtx {
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
	return &weikeCtx{c: c, headers: headers, mode: mode}
}

func parseWeikeCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := weikeCIDRe.FindStringSubmatch(raw); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// loadInfos fetches weike info – title + activity list.
// Source: Icve_Weike._get_infos
func (x *weikeCtx) loadInfos() ([]weikeItem, error) {
	body, err := x.c.PostForm(weikeURLInfos, map[string]string{
		"weikeId": x.cid,
	}, x.headers)
	if err != nil {
		return nil, fmt.Errorf("icve_weike: load infos: %w", err)
	}
	root := parseJSONMap(body)
	courseInfo := mapAt(root, "courseInfo")
	title := str(courseInfo["Title"])
	if title != "" {
		x.title = cleanTitle(title)
	}

	activityList := listAt(root, "activetyList")
	var items []weikeItem
	videoCounter := 1
	fileCounter := 1
	for _, act := range activityList {
		cellType := strings.ToLower(str(act["CellType"]))
		actTitle := cleanTitle(str(act["Title"]))
		id := str(act["Id"])
		if id == "" {
			continue
		}
		if cellType == "video" {
			items = append(items, weikeItem{
				Name: fmt.Sprintf("[%d]--%s", videoCounter, trimRStripMP4(actTitle)),
				ID:   id,
				Kind: "video",
			})
			videoCounter++
		} else {
			items = append(items, weikeItem{
				Name: fmt.Sprintf("(%d)--%s", fileCounter, actTitle),
				ID:   id,
				Kind: "file",
			})
			fileCounter++
		}
	}
	return items, nil
}

// getSourceURL calls microstudy/view to get download URL.
// Source: Icve_Weike._get_source_url
func (x *weikeCtx) getSourceURL(cellID string) string {
	body, err := x.c.PostForm(weikeURLSource, map[string]string{
		"cellId": cellID,
	}, x.headers)
	if err != nil {
		return ""
	}
	time.Sleep(100 * time.Millisecond)
	root := parseJSONMap(body)
	// Source: response is data:[{downloadurl: "..."}]
	dataList := listAt(root, "data")
	if len(dataList) > 0 {
		dl := str(dataList[0]["downloadurl"])
		if idx := strings.LastIndex(dl, "?"); idx > 0 {
			dl = dl[:idx]
		}
		return dl
	}
	// Also try as single object
	data := mapAt(root, "data")
	dl := str(data["downloadurl"])
	if idx := strings.LastIndex(dl, "?"); idx > 0 {
		dl = dl[:idx]
	}
	return dl
}

func (x *weikeCtx) buildMedia(items []weikeItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		if item.Kind == "video" && x.mode == ONLY_PDF {
			continue
		}
		u := x.getSourceURL(item.ID)
		if u == "" {
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
			Extra: map[string]any{"kind": item.Kind, "module": "weike"},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icve_weike: no playable entries")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{
		Site:    "icve",
		Title:   firstNonEmpty(x.title, x.cid, "icve_weike"),
		Entries: entries,
		Extra:   map[string]any{"course_id": x.cid, "module": "weike"},
	}, nil
}

// Ensure json import is referenced.
var _ = json.NewDecoder
