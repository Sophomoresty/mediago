// Package ckjr implements 创客匠人 course extraction.
//
// Source alignment:
//
//	Mooc/Courses/Ckjr/Ckjr_Base.pyc.1shot.cdc.py
//	Mooc/Courses/Ckjr/Ckjr_Course.pyc.1shot.cdc.py
package ckjr

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	url0 = "https://kpapiop.ckjr001.com"
	url1 = "https://playvideo.qcloud.com/getplayinfo/v4/%s/%s"

	ckjrFromApp    = "oa"
	ckjrWebVersion = "202508141135"
	ckjrUA         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36 NetType/WIFI MicroMessenger/7.0.20.1781(0x6700143B) WindowsWechat(0x63090a13) UnifiedPCWindowsWechat(0xf254181c) XWEB/19339 Flue"
)

var patterns = []string{`(?:[\w-]+\.)?(?:ckjr001|nineteenj|gmp-office)\.(?:com|cn)/|/kpv2p/`}

func init() {
	extractor.Register(&Ckjr{}, extractor.SiteInfo{Name: "Ckjr", URL: "ckjr001.com", NeedAuth: true})
}

type Ckjr struct{}

func (s *Ckjr) Patterns() []string { return patterns }

type routeInfo struct {
	Kind      string
	ID        string
	IDKey     string
	ProdType  string
	CourseTyp string
	Company   string
}

type mediaCandidate struct {
	URL    string
	Title  string
	Format string
}

var (
	routeRe = regexp.MustCompile(`(?i)/kpv2p/([\w-]+).*#/?homePage/(course/(video|voice|imgText)|column/columnDetail|datum/datumDetail|live/(?:liveDetail|livePersonalDetail|liveRoom)|package/packageDetail|testPaper/testDetail)\?([^#\s]+)`)
	idRe    = regexp.MustCompile(`(?i)(?:courseId|extId|datumId|liveId|combosId|testId|prodId|productId|id)=([0-9]+)`)
	mediaRe = regexp.MustCompile(`(?i)(?:https?:)?//[^\s<>"'\\]+?\.(?:m3u8|mp4|m4v|mov|flv|mp3|m4a|aac|wav|pdf)(?:[^\s<>"'\\]*)?`)
)

var routeCfg = map[string]routeInfo{
	"video":        {Kind: "video", IDKey: "courseId", ProdType: "5", CourseTyp: "0"},
	"voice":        {Kind: "voice", IDKey: "courseId", ProdType: "5", CourseTyp: "1"},
	"imgText":      {Kind: "imgText", IDKey: "courseId", ProdType: "5", CourseTyp: "2"},
	"column":       {Kind: "column", IDKey: "extId", ProdType: "9", CourseTyp: "9"},
	"datum":        {Kind: "datum", IDKey: "datumId", ProdType: "8", CourseTyp: "8"},
	"live":         {Kind: "live", IDKey: "liveId", ProdType: "51", CourseTyp: "51"},
	"livePersonal": {Kind: "livePersonal", IDKey: "liveId", ProdType: "180", CourseTyp: "180"},
	"package":      {Kind: "package", IDKey: "combosId", ProdType: "61", CourseTyp: "61"},
	"testPaper":    {Kind: "testPaper", IDKey: "testId", ProdType: "125", CourseTyp: "125"},
}

func (s *Ckjr) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("ckjr requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := ckjrHeaders(rawURL)

	route := parseRoute(rawURL)
	if route.ID == "" {
		result, err := requestAPI(c, "/api/marketingAward/getMarketingAwardList", map[string]string{"name": "", "page": "1", "limit": "50", "prodType": "0"}, headers)
		if err != nil {
			return nil, err
		}
		entries := entriesFromPayload(c, result, headers, "ckjr")
		if len(entries) == 0 {
			return nil, fmt.Errorf("ckjr: no route id and no playable media in course list")
		}
		return &extractor.MediaInfo{Site: "ckjr", Title: "ckjr", Entries: entries}, nil
	}

	payloads := fetchRoutePayloads(c, route, headers)
	var entries []*extractor.MediaInfo
	for _, payload := range payloads {
		entries = append(entries, entriesFromPayload(c, payload, headers, route.ID)...)
	}
	entries = dedupeEntries(entries)
	if len(entries) == 1 {
		return entries[0], nil
	}
	if len(entries) > 1 {
		return &extractor.MediaInfo{Site: "ckjr", Title: util.SanitizeFilename("ckjr_" + route.ID), Entries: entries}, nil
	}
	return nil, fmt.Errorf("ckjr: no playable media URL in API response for %s=%s", route.IDKey, route.ID)
}

func fetchRoutePayloads(c *util.Client, r routeInfo, headers map[string]string) []any {
	params := resourceParams(r)
	paths := []string{
		fmt.Sprintf("/api/courses/%s", url.PathEscape(r.ID)),
		"/api/courses/detail",
		"/api/course/detail",
		"/api/prod/detail/info",
		fmt.Sprintf("/api/courses/%s/dirs", url.PathEscape(r.ID)),
		"/api/courses/dirs",
		"/api/course/dirs",
	}
	switch r.Kind {
	case "column":
		paths = append([]string{fmt.Sprintf("/api/column/detail/%s", url.PathEscape(r.ID)), "/api/column/detail", fmt.Sprintf("/api/columns/%s/dirs", url.PathEscape(r.ID)), "/api/columns/dirs"}, paths...)
	case "datum":
		paths = append([]string{"/api/datum/detail"}, paths...)
	case "live", "livePersonal":
		paths = append([]string{"/api/live/detail", "/api/live/playback/list"}, paths...)
	case "package":
		paths = append([]string{fmt.Sprintf("/api/combos/%s/dirs", url.PathEscape(r.ID))}, paths...)
	}
	var out []any
	for _, path := range paths {
		payload, err := requestAPI(c, path, params, headers)
		if err == nil && responseHasPayload(payload) {
			out = append(out, payload)
		}
	}
	return out
}

func requestAPI(c *util.Client, path string, params map[string]string, headers map[string]string) (any, error) {
	q := url.Values{}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	q.Set("fromApp", ckjrFromApp)
	q.Set("webversion", ckjrWebVersion)
	apiURL := url0 + path
	if strings.Contains(path, "?") {
		apiURL += "&" + q.Encode()
	} else {
		apiURL += "?" + q.Encode()
	}
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("ckjr GET %s: %w", path, err)
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("ckjr parse %s: %w", path, err)
	}
	return payload, nil
}

func entriesFromPayload(c *util.Client, payload any, headers map[string]string, fallbackTitle string) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for _, node := range walkMaps(payload) {
		cands := mediaFromNode(c, node, headers)
		for _, cand := range cands {
			if cand.URL == "" || seen[cand.URL] {
				continue
			}
			seen[cand.URL] = true
			title := firstNonEmpty(cand.Title, textValue(node, "lessonName", "dirName", "chapterName", "title", "name", "courseName", "prodName"), fallbackTitle)
			entries = append(entries, &extractor.MediaInfo{Site: "ckjr", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{
				"best": {Quality: "best", URLs: []string{cand.URL}, Format: cand.Format, Headers: headers},
			}})
		}
	}
	return entries
}

func mediaFromNode(c *util.Client, node map[string]any, headers map[string]string) []mediaCandidate {
	var out []mediaCandidate
	if u := directMediaURL(node); u != "" {
		out = append(out, mediaCandidate{URL: u, Format: pickFormat(u)})
	}
	if auth := qcloudAuth(node); auth != nil {
		if u, err := requestQCloud(c, auth, headers); err == nil && u != "" {
			out = append(out, mediaCandidate{URL: u, Format: pickFormat(u)})
		}
	}
	return out
}

func requestQCloud(c *util.Client, auth map[string]string, headers map[string]string) (string, error) {
	apiURL := fmt.Sprintf(url1, url.PathEscape(auth["app_id"]), url.PathEscape(auth["file_id"]))
	q := url.Values{"keyId": {"1"}, "psign": {auth["psign"]}}
	body, err := c.GetString(apiURL+"?"+q.Encode(), headers)
	if err != nil {
		return "", err
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", err
	}
	return findMediaURL(payload), nil
}
