// Package wangxiao implements an extractor for k.wangxiao.cn (网校).
package wangxiao

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	refererURL        = "https://k.wangxiao.cn"
	userURL           = "https://k.wangxiao.cn/user/"
	urlPlay           = "https://k.wangxiao.cn/play?activityid=%s&productsid=%s"
	urlItem           = "https://k.wangxiao.cn/item/%s.html"
	urlSku            = "https://ke.wangxiao.cn/apis//products/skuSingleContent"
	keAPIToken        = "7209bbbc-cb34-438b-ad3b-742fa7fd9f2c"
	urlDirectory      = "https://k.wangxiao.cn/Course/ProductsDirectory?isfromusercenter=1&ProductsId=%s&ordernumber=%s"
	urlClasshours     = "https://k.wangxiao.cn/Course/GetClasshours?cid=%s&pid=%s"
	urlPlayer         = "https://users.wangxiao.cn/player/Index.aspx?Id=%s"
	urlPlayerDown     = "https://users.wangxiao.cn/player/down.aspx?Id=%s"
	urlLiveHandout    = "https://live.wangxiao.cn/LiveActivity/DownHandOut/?Id=%s"
	urlVideoPlay      = "https://p.bokecc.com/servlet/getvideofile?vid=%s&siteid=%s"
	defaultBokeSiteID = "A183AC83A2983CCC"
)

var patterns = []string{`(?:[\w-]+\.)?wangxiao\.cn/(?:play|item|Course|player|user)|(?:[\w-]+\.)?bokecc\.com/`}

func init() {
	extractor.Register(&Wangxiao{}, extractor.SiteInfo{Name: "Wangxiao", URL: "wangxiao.cn", NeedAuth: true})
}

type Wangxiao struct{}

func (w *Wangxiao) Patterns() []string { return patterns }

type lessonRef struct {
	Title, URL, ActivityID, ProductID, SiteID, VideoID string
	Legacy                                             bool
}

var (
	activityRe = regexp.MustCompile(`(?i)(?:activityid|[?&]Id)=([\w-]+)`)
	productRe  = regexp.MustCompile(`(?i)productsid=([\w-]+)`)
	itemRe     = regexp.MustCompile(`(?i)/item/(\d+)\.html`)
	setmealRe  = regexp.MustCompile(`(?i)(?:id=["']setmealId["'][^>]*value=["']([^"']+)|setmealId["']?\s*[:=]\s*["']?([\w-]+))`)
	siteIDRe   = regexp.MustCompile(`(?i)(?:siteid=([A-Z0-9]+)|["']siteid["']\s*[:=]\s*["']([A-Z0-9]+)["'])`)
	vidRe      = regexp.MustCompile(`(?i)(?:var\s+cc_vid\s*=\s*["']([A-Z0-9]+)["']|\bvid\s*=\s*["']([A-Z0-9]+)["']|["']vid["']\s*:\s*["']([A-Z0-9]+)["']|["']ccVideoId["']\s*:\s*["']([^"']+)["'])`)
	titleRe    = regexp.MustCompile(`(?is)<span[^>]+class=["'][^"']*course-title[^"']*["'][^>]*>(.*?)</span>|<title[^>]*>(.*?)</title>`)
	hrefRe     = regexp.MustCompile(`(?is)(?:href|data-href)=["']([^"']+)["']`)
	pageDataRe = regexp.MustCompile(`(?is)var\s+pageData\s*=\s*(\{.*?\})\s*;</script>`)
)

func (w *Wangxiao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wangxiao requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := wangxiaoHeaders(refererURL)

	seed := normalizeURL(rawURL, refererURL)
	if seed == "" {
		return nil, fmt.Errorf("wangxiao: empty URL")
	}
	page, err := c.GetString(seed, headers)
	if err != nil {
		return nil, fmt.Errorf("wangxiao fetch page: %w", err)
	}
	if isLoginPage(page) {
		return nil, fmt.Errorf("wangxiao requires valid NewPlatFormToken/token cookies")
	}

	refs := parseLessonRefs(page, seed)
	if len(refs) == 0 {
		refs = []lessonRef{{Title: extractTitle(page), URL: seed, ActivityID: firstGroup(activityRe, seed), ProductID: firstGroup(productRe, seed), SiteID: extractSiteID(page), VideoID: extractVideoID(page), Legacy: strings.Contains(strings.ToLower(seed), "users.wangxiao.cn/player")}}
	}
	refs = append(refs, refsFromKeCatalog(c, page, seed, headers)...)

	entries := make([]*extractor.MediaInfo, 0, len(refs))
	seen := map[string]bool{}
	for i, ref := range refs {
		entry, err := resolveRef(c, ref, i+1, headers)
		if err != nil || entry == nil || len(entry.Streams) == 0 {
			continue
		}
		key := entry.Streams["default"].URLs[0]
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wangxiao: no playable BokeCC video resolved")
	}
	title := extractTitle(page)
	if title == "" {
		title = "wangxiao"
	}
	return &extractor.MediaInfo{Site: "wangxiao", Title: title, Entries: entries}, nil
}

func resolveRef(c *util.Client, ref lessonRef, index int, headers map[string]string) (*extractor.MediaInfo, error) {
	if ref.ActivityID != "" {
		if ref.Legacy {
			ref.URL = fmt.Sprintf(urlPlayer, ref.ActivityID)
		} else if ref.ProductID != "" {
			ref.URL = fmt.Sprintf(urlPlay, ref.ActivityID, ref.ProductID)
		}
	}
	if ref.URL == "" {
		return nil, fmt.Errorf("wangxiao: empty lesson URL")
	}
	body, err := c.GetString(ref.URL, wangxiaoHeaders(ref.URL))
	if err != nil {
		return nil, err
	}
	if ref.VideoID == "" {
		ref.VideoID = extractVideoID(body)
	}
	if ref.SiteID == "" {
		ref.SiteID = extractSiteID(body)
	}
	if ref.SiteID == "" {
		ref.SiteID = defaultBokeSiteID
	}
	if ref.VideoID == "" {
		return nil, fmt.Errorf("wangxiao: lesson has no cc vid")
	}
	mediaURL, err := shared.BokeCCResolve(c, ref.VideoID, ref.SiteID, map[string]string{"Referer": ref.URL})
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(ref.Title)
	if title == "" {
		title = fmt.Sprintf("视频%d", index)
	}
	return &extractor.MediaInfo{Site: "wangxiao", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{mediaURL}, Format: formatFromURL(mediaURL), Headers: map[string]string{"Referer": ref.URL}}}, Extra: map[string]any{"activity_id": ref.ActivityID, "video_id": ref.VideoID, "siteid": ref.SiteID, "lesson_url": ref.URL, "video_play_url": fmt.Sprintf(urlVideoPlay, ref.VideoID, ref.SiteID), "headers": headers}}, nil
}

func parseLessonRefs(text, pageURL string) []lessonRef {
	refs := make([]lessonRef, 0)
	seen := map[string]bool{}
	add := func(u, title string) {
		u = normalizeURL(html.UnescapeString(u), pageURL)
		if u == "" || seen[u] {
			return
		}
		act := firstGroup(activityRe, u)
		item := firstGroup(itemRe, u)
		if act == "" && item == "" {
			return
		}
		seen[u] = true
		refs = append(refs, lessonRef{Title: title, URL: u, ActivityID: act, ProductID: firstGroup(productRe, u), Legacy: strings.Contains(strings.ToLower(u), "users.wangxiao.cn/player")})
	}
	for _, m := range hrefRe.FindAllStringSubmatch(text, -1) {
		add(m[1], "")
	}
	if m := pageDataRe.FindStringSubmatch(text); len(m) > 1 {
		var data map[string]any
		if json.Unmarshal([]byte(m[1]), &data) == nil {
			walkJSON(data, func(node map[string]any) {
				add(firstString(node, "lesson_url", "url", "href", "playUrl", "continue_url"), firstString(node, "title", "courseName", "name"))
			})
		}
	}
	return refs
}

func refsFromKeCatalog(c *util.Client, page, pageURL string, headers map[string]string) []lessonRef {
	setmealID := firstGroup(setmealRe, page)
	if setmealID == "" {
		return nil
	}
	h := wangxiaoHeaders(pageURL)
	h["content-type"] = "application/json;charset=UTF-8"
	h["token"] = keAPIToken
	h["source"] = "pc"
	body, err := c.PostForm(urlSku, map[string]string{"id": setmealID}, h)
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil || fmt.Sprint(root["code"]) != "0" {
		return nil
	}
	refs := []lessonRef{}
	walkJSON(root["data"], func(node map[string]any) {
		vid := firstString(node, "ccVideoId", "video_id", "videoId")
		act := firstString(node, "activityid", "activity_id", "activityId")
		if vid == "" && act == "" {
			return
		}
		refs = append(refs, lessonRef{Title: firstString(node, "title", "courseName", "name"), URL: pageURL, ActivityID: act, ProductID: firstGroup(productRe, pageURL), VideoID: vid, SiteID: firstString(node, "ccUserId", "siteid", "siteId")})
	})
	return refs
}

func extractTitle(text string) string {
	for _, m := range titleRe.FindAllStringSubmatch(text, -1) {
		for _, v := range m[1:] {
			if s := cleanText(v); s != "" {
				return s
			}
		}
	}
	return ""
}
func extractSiteID(text string) string  { return firstGroup(siteIDRe, text) }
func extractVideoID(text string) string { return firstGroup(vidRe, text) }

func firstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) == 0 {
		return ""
	}
	for _, v := range m[1:] {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func walkJSON(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, vv := range x {
			walkJSON(vv, fn)
		}
	case []any:
		for _, vv := range x {
			walkJSON(vv, fn)
		}
	}
}

func normalizeURL(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if u, err := url.Parse(raw); err == nil && u.IsAbs() {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		b, _ = url.Parse(refererURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return b.ResolveReference(u).String()
}

func wangxiaoHeaders(referer string) map[string]string {
	return map[string]string{"Referer": referer, "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}
}
func isLoginPage(text string) bool {
	return strings.Contains(text, "user.wangxiao.cn/login") || strings.Contains(text, "中大网校会员中心-登陆入口-中大网校") || strings.Contains(text, "/views/login/index.js")
}
func formatFromURL(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}

// fetchUserClasshours fetches ProductsDirectory + GetClasshours to get the
// user's purchased course catalog. Source _parse_user_classhours: parse HTML
// <li> tags from GetClasshours response → lesson links.
func fetchUserClasshours(c *util.Client, productID, orderNumber, courseID string, headers map[string]string) ([]lessonRef, error) {
	h := cloneMap(headers)
	// Step 1: ProductsDirectory → get ordernumber if missing
	if orderNumber == "" {
		dirURL := fmt.Sprintf(urlDirectory, productID, "")
		body, err := c.GetString(dirURL, h)
		if err == nil {
			orderNumber = extractOrderNumber(body)
		}
	}
	if orderNumber == "" {
		return nil, fmt.Errorf("wangxiao: cannot determine ordernumber")
	}

	// Step 2: GetClasshours → parse lesson list
	classURL := fmt.Sprintf(urlClasshours, courseID, productID)
	body, err := c.GetString(classURL, h)
	if err != nil {
		return nil, fmt.Errorf("wangxiao GetClasshours: %w", err)
	}
	return parseClasshourLinks(body, productID), nil
}

// parseClasshourLinks extracts lesson links from GetClasshours HTML.
// Source _parse_user_classhours: finds <li> elements with lesson links.
func parseClasshourLinks(html, productID string) []lessonRef {
	var refs []lessonRef
	liRe := regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	linkRe := regexp.MustCompile(`(?i)href=["']([^"']*player[^"']*)["']`)
	titleRe := regexp.MustCompile(`(?is)<(?:a|span)[^>]*>(.*?)</(?:a|span)>`)
	for _, li := range liRe.FindAllString(html, -1) {
		linkMatch := linkRe.FindStringSubmatch(li)
		if linkMatch == nil {
			continue
		}
		title := ""
		if tm := titleRe.FindStringSubmatch(li); len(tm) > 1 {
			title = cleanText(tm[1])
		}
		refs = append(refs, lessonRef{
			Title:      title,
			URL:        linkMatch[1],
			ActivityID: firstGroup(activityRe, linkMatch[1]),
			ProductID:  productID,
			Legacy:     strings.Contains(linkMatch[1], "users.wangxiao.cn"),
		})
	}
	return refs
}

// resolveFileResource fetches handout/file download URLs from lesson page.
// Source _resolve_file_resource: extracts file_url from lesson page JSON,
// downloads via DownHandOut endpoint.
func resolveFileResource(c *util.Client, activityID string, headers map[string]string) []string {
	if activityID == "" {
		return nil
	}
	// Try live handout endpoint
	handoutURL := fmt.Sprintf(urlLiveHandout, activityID)
	body, err := c.GetString(handoutURL, headers)
	if err == nil && body != "" {
		// Response may be a redirect URL or JSON with file_url
		if fileURL := extractFileURL(body); fileURL != "" {
			return []string{fileURL}
		}
	}
	return nil
}

func extractOrderNumber(html string) string {
	re := regexp.MustCompile(`(?i)ordernumber\s*[=:]\s*["']([^"']+)["']`)
	if m := re.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractFileURL(text string) string {
	re := regexp.MustCompile(`https?://[^\s"'<>]+\.(?:pdf|doc|docx|mp4|mp3|zip)[^\s"'<>]*`)
	if urls := re.FindAllString(text, -1); len(urls) > 0 {
		return urls[0]
	}
	return ""
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cleanText(s string) string {
	s = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
