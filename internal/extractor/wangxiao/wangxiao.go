// Package wangxiao implements an extractor for k.wangxiao.cn (网校).
package wangxiao

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
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
	defaultBokeSiteID = "E601487AD12A3E06"

	modeFHD     = 1
	modeHD      = 2
	modeSD      = 3
	modeOnlyPDF = 4
)

var patterns = []string{`(?:[\w-]+\.)?wangxiao\.cn/(?:play|item|Course|player|user)|(?:[\w-]+\.)?bokecc\.com/`}

func init() {
	extractor.Register(&Wangxiao{}, extractor.SiteInfo{Name: "Wangxiao", URL: "wangxiao.cn", NeedAuth: true})
}

type Wangxiao struct{}

func (w *Wangxiao) Patterns() []string { return patterns }

type lessonRef struct {
	Title, URL, ActivityID, ProductID, SiteID, VideoID string
	FileURL, FileHTML, FileFmt, FileName               string
	Legacy                                             bool
}

type wangxiaoCourse struct {
	Title       string
	ProductID   string
	OrderNumber string
	ItemNum     string
	ItemURL     string
	ActivityID  string
	ContinueURL string
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

	if opts.ListOnly || isUserCourseURL(seed) {
		courses := parseCourseListPage(page)
		if len(courses) == 0 && !isUserCourseURL(seed) {
			courses, _ = fetchCourseList(c, headers)
		}
		return buildCourseListMedia(courses), nil
	}

	itemNum := firstNonEmpty(firstGroup(itemRe, seed), firstGroup(itemRe, page))
	meta := extractWangxiaoPageMeta(page)
	productID := firstNonEmpty(firstGroup(productRe, seed), firstGroup(productRe, page), stringFromMap(meta, "product_id"))
	refs := parseLessonRefs(page, seed, productID)
	if len(refs) == 0 {
		refs = []lessonRef{{Title: extractTitle(page), URL: seed, ActivityID: firstGroup(activityRe, seed), ProductID: productID, SiteID: extractSiteID(page), VideoID: extractVideoID(page), Legacy: strings.Contains(strings.ToLower(seed), "users.wangxiao.cn/player")}}
	}
	refs = append(refs, refsFromKeCatalog(c, page, seed, headers, opts.Cookies)...)
	if productID != "" {
		if userRefs, err := fetchUserClasshours(c, productID, extractOrderNumber(page), extractCourseID(page, seed), headers); err == nil {
			refs = append(refs, userRefs...)
		}
	}

	mode := selectMode(opts.Quality)
	entries := make([]*extractor.MediaInfo, 0, len(refs))
	seen := map[string]bool{}
	for i, ref := range refs {
		resolved, err := resolveRefEntries(c, ref, i+1, headers, mode)
		if err != nil {
			continue
		}
		for _, entry := range resolved {
			if entry == nil || len(entry.Streams) == 0 {
				continue
			}
			key := firstStreamURL(entry)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wangxiao: no playable BokeCC video resolved")
	}
	title := extractTitle(page)
	if title == "" {
		title = "wangxiao"
	}
	extra := map[string]any{}
	if productID != "" {
		extra["product_id"] = productID
	}
	if itemNum != "" {
		extra["item_num"] = itemNum
	}
	for _, key := range []string{"price", "raw_price", "title", "course_id", "course_num"} {
		if v, ok := meta[key]; ok && fmt.Sprint(v) != "" {
			extra[key] = v
		}
	}
	if len(extra) == 0 {
		extra = nil
	}
	return &extractor.MediaInfo{Site: "wangxiao", Title: title, Entries: entries, Extra: extra}, nil
}

func resolveRefEntries(c *util.Client, ref lessonRef, index int, headers map[string]string, mode int) ([]*extractor.MediaInfo, error) {
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
		ref.VideoID = extractVideoID(ref.URL)
	}
	var entries []*extractor.MediaInfo
	if ref.VideoID != "" && mode != modeOnlyPDF {
		play, err := resolveBokeCCWangxiao(c, ref.VideoID, ref.SiteID, ref.URL, mode)
		if err == nil && play.URL != "" {
			title := strings.TrimSpace(ref.Title)
			if title == "" {
				title = fmt.Sprintf("视频%d", index)
			}
			extra := map[string]any{
				"activity_id":    ref.ActivityID,
				"video_id":       ref.VideoID,
				"siteid":         ref.SiteID,
				"lesson_url":     ref.URL,
				"video_play_url": fmt.Sprintf(urlVideoPlay, ref.VideoID, ref.SiteID),
				"headers":        headers,
			}
			streamURL := play.URL
			format := formatFromURL(streamURL)
			if play.M3U8Text != "" {
				streamURL = shared.M3U8DataURL(play.M3U8Text)
				format = "m3u8"
				extra["m3u8_text"] = play.M3U8Text
				extra["m3u8_url"] = play.URL
				extra["source_type"] = "m3u8_text"
			}
			entries = append(entries, &extractor.MediaInfo{
				Site:  "wangxiao",
				Title: title,
				Streams: map[string]extractor.Stream{"default": {
					Quality:   "best",
					URLs:      []string{streamURL},
					Format:    format,
					NeedMerge: format == "m3u8",
					Headers:   map[string]string{"Referer": ref.URL},
				}},
				Extra: extra,
			})
		}
	}
	for j, fileURL := range resolveFileResourcesFromBody(c, ref, body, map[string]string{"Referer": ref.URL}) {
		name := strings.TrimSpace(ref.Title)
		if name == "" {
			name = fmt.Sprintf("资料%d", index)
		}
		if len(entries) > 0 || j > 0 {
			name = fmt.Sprintf("%s-资料%d", name, j+1)
		}
		format := firstNonEmpty(ref.FileFmt, resourceExt(fileURL))
		if mode == modeOnlyPDF && strings.EqualFold(format, "mp4") {
			continue
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "wangxiao",
			Title: name,
			Streams: map[string]extractor.Stream{"default": {
				Quality: "source",
				URLs:    []string{fileURL},
				Format:  format,
				Headers: map[string]string{"Referer": ref.URL},
			}},
			Extra: map[string]any{"activity_id": ref.ActivityID, "lesson_url": ref.URL, "type": "file", "source_url": ref.FileURL},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wangxiao: lesson has no cc vid or handout")
	}
	return entries, nil
}

func parseLessonRefs(text, pageURL, fallbackProductID string) []lessonRef {
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
		refs = append(refs, lessonRef{Title: title, URL: u, ActivityID: act, ProductID: firstNonEmpty(firstGroup(productRe, u), fallbackProductID), Legacy: strings.Contains(strings.ToLower(u), "users.wangxiao.cn/player")})
	}
	for _, m := range hrefRe.FindAllStringSubmatch(text, -1) {
		add(m[1], "")
	}
	classHourRe := regexp.MustCompile(`(?is)<[^>]+data-classhourid=["']([^"']+)["'][^>]*>`)
	titleAttrRe := regexp.MustCompile(`(?is)(?:title|data-title)=["']([^"']+)["']`)
	for _, m := range classHourRe.FindAllStringSubmatch(text, -1) {
		act := strings.TrimSpace(m[1])
		if act == "" {
			continue
		}
		title := ""
		if tm := titleAttrRe.FindStringSubmatch(m[0]); len(tm) > 1 {
			title = cleanText(tm[1])
		}
		if fallbackProductID != "" {
			add(fmt.Sprintf(urlPlay, act, fallbackProductID), title)
		} else if !seen[act] {
			seen[act] = true
			refs = append(refs, lessonRef{Title: title, URL: pageURL, ActivityID: act, ProductID: fallbackProductID})
		}
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

func refsFromKeCatalog(c *util.Client, page, pageURL string, headers map[string]string, jar http.CookieJar) []lessonRef {
	setmealID := firstGroup(setmealRe, page)
	if setmealID == "" {
		return nil
	}
	h := wangxiaoHeaders(pageURL)
	h["Content-Type"] = "application/json;charset=UTF-8"
	h["token"] = keAPIToken
	h["sessionId"] = cookieFromJar(jar, "sessionId", "k.wangxiao.cn", "ke.wangxiao.cn")
	h["source"] = "pc"
	payload, _ := json.Marshal(map[string]string{"id": setmealID})
	resp, err := c.Post(urlSku, bytes.NewReader(payload), h)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode >= 400 {
		return nil
	}
	body := string(raw)
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil || fmt.Sprint(root["code"]) != "0" {
		return nil
	}
	refs := []lessonRef{}
	productID := firstNonEmpty(firstGroup(productRe, pageURL), stringFromMap(extractWangxiaoPageMeta(page), "product_id"))
	walkJSON(root["data"], func(node map[string]any) {
		vid := firstString(node, "ccVideoId", "video_id", "videoId")
		act := firstString(node, "activityid", "activity_id", "activityId")
		fileURL := firstString(node, "jiangYi", "handout_url", "file_url", "fileUrl", "download_url", "downloadUrl")
		fileHTML := firstString(node, "jiangYiHtml", "file_html", "fileHtml", "html")
		if vid == "" && act == "" && fileURL == "" && fileHTML == "" {
			return
		}
		refs = append(refs, lessonRef{
			Title:      firstString(node, "title", "courseName", "name"),
			URL:        pageURL,
			ActivityID: act,
			ProductID:  productID,
			VideoID:    vid,
			SiteID:     firstString(node, "ccUserId", "siteid", "siteId"),
			FileURL:    normalizeURL(fileURL, pageURL),
			FileHTML:   fileHTML,
			FileFmt:    firstString(node, "file_fmt", "fileFmt", "file_type", "fileType"),
			FileName:   firstString(node, "file_name", "fileName", "title", "courseName", "name"),
		})
	})
	return refs
}

func isUserCourseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	path := strings.TrimRight(strings.ToLower(u.Path), "/")
	return host == "k.wangxiao.cn" && path == "/user"
}

func fetchCourseList(c *util.Client, headers map[string]string) ([]wangxiaoCourse, error) {
	body, err := c.GetString(userURL, headers)
	if err != nil {
		return nil, err
	}
	if isLoginPage(body) {
		return nil, fmt.Errorf("wangxiao requires valid cookies")
	}
	return parseCourseListPage(body), nil
}

func parseCourseListPage(text string) []wangxiaoCourse {
	blockRe := regexp.MustCompile(`(?is)<div\b[^>]*class=["'][^"']*\bcotent_box\b[^"']*["'][^>]*>.*?(?=<div\b[^>]*class=["'][^"']*\bcotent_box\b|</body>|$)`)
	openRe := regexp.MustCompile(`(?is)^<div\b[^>]*>`)
	titleRe := regexp.MustCompile(`(?is)<p\b[^>]*class=["'][^"']*\bplay_title\b[^"']*["'][^>]*>(.*?)</p>`)
	imgHrefRe := regexp.MustCompile(`(?is)<img\b[^>]*(?:data-href|href)=["']([^"']+)["'][^>]*>`)
	playHrefRe := regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']*activityid=[^"']+)["'][^>]*>`)
	var out []wangxiaoCourse
	for _, block := range blockRe.FindAllString(text, -1) {
		open := openRe.FindString(block)
		productID := strings.ToLower(attrValue(open, "data-id"))
		orderNumber := attrValue(open, "data-order")
		itemURL := ""
		if m := imgHrefRe.FindStringSubmatch(block); len(m) > 1 {
			itemURL = normalizeURL(html.UnescapeString(m[1]), refererURL)
		}
		continueURL := ""
		if m := playHrefRe.FindStringSubmatch(block); len(m) > 1 {
			continueURL = normalizeURL(html.UnescapeString(m[1]), refererURL)
		}
		title := ""
		if m := titleRe.FindStringSubmatch(block); len(m) > 1 {
			title = cleanText(m[1])
		}
		course := wangxiaoCourse{
			Title:       title,
			ProductID:   firstNonEmpty(productID, firstGroup(productRe, continueURL)),
			OrderNumber: orderNumber,
			ItemNum:     firstGroup(itemRe, itemURL),
			ItemURL:     itemURL,
			ActivityID:  firstGroup(activityRe, continueURL),
			ContinueURL: continueURL,
		}
		if course.ProductID == "" && course.ItemNum == "" && course.ActivityID == "" && course.Title == "" {
			continue
		}
		out = append(out, course)
	}
	return out
}

func buildCourseListMedia(courses []wangxiaoCourse) *extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(courses))
	for i, course := range courses {
		title := strings.TrimSpace(course.Title)
		if title == "" {
			title = fmt.Sprintf("课程%d", i+1)
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "wangxiao",
			Title: title,
			Extra: map[string]any{
				"product_id":   course.ProductID,
				"course_order": course.OrderNumber,
				"item_num":     course.ItemNum,
				"item_url":     course.ItemURL,
				"activity_id":  course.ActivityID,
				"continue_url": course.ContinueURL,
				"purchased":    true,
			},
		})
	}
	return &extractor.MediaInfo{Site: "wangxiao", Title: "wangxiao courses", Entries: entries, Extra: map[string]any{"count": len(entries), "list_only": true}}
}

func attrValue(tag, name string) string {
	if tag == "" || name == "" {
		return ""
	}
	re := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*["']([^"']*)["']`)
	if m := re.FindStringSubmatch(tag); len(m) > 1 {
		return html.UnescapeString(strings.TrimSpace(m[1]))
	}
	return ""
}

func extractWangxiaoPageMeta(page string) map[string]any {
	out := map[string]any{}
	var root map[string]any
	if m := pageDataRe.FindStringSubmatch(page); len(m) > 1 {
		_ = json.Unmarshal([]byte(m[1]), &root)
	}
	single, _ := root["singleInfo"].(map[string]any)
	if len(single) == 0 {
		return out
	}
	if title := strings.TrimSpace(fmt.Sprint(firstAny(single, "title", "name", "courseName"))); title != "" && title != "<nil>" {
		out["title"] = cleanText(title)
	}
	if id := strings.TrimSpace(fmt.Sprint(firstAny(single, "id", "productId", "productsId"))); id != "" && id != "<nil>" {
		out["product_id"] = strings.ToLower(id)
		out["course_id"] = id
	}
	if num := strings.TrimSpace(fmt.Sprint(firstAny(single, "num", "itemNum"))); num != "" && num != "<nil>" {
		out["course_num"] = num
	}
	if price, raw := extractWangxiaoPrice(single); raw != "" {
		out["price"] = price
		out["raw_price"] = raw
	}
	return out
}

func extractWangxiaoPrice(single map[string]any) (float64, string) {
	for _, key := range []string{"activityPrice", "nowPrice", "oriPrice", "price", "salePrice"} {
		if v, ok := single[key]; ok {
			raw := strings.TrimSpace(fmt.Sprint(v))
			if raw == "" || raw == "<nil>" {
				continue
			}
			if f, ok := parsePrice(raw); ok {
				return f, raw
			}
		}
	}
	return 0, ""
}

func parsePrice(raw string) (float64, bool) {
	raw = strings.ReplaceAll(raw, ",", "")
	m := regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`).FindString(raw)
	if m == "" {
		return 0, false
	}
	var f float64
	if _, err := fmt.Sscanf(m, "%f", &f); err != nil {
		return 0, false
	}
	return f, true
}

func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(m[key]))
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
	low := strings.ToLower(strings.TrimSpace(u))
	if strings.HasPrefix(low, "data:application/vnd.apple.mpegurl") || strings.HasPrefix(low, "data:application/x-mpegurl") || strings.HasPrefix(low, "#extm3u") || strings.Contains(low, ".m3u8") || strings.Contains(low, "format=m3u8") || strings.Contains(low, "type=m3u8") {
		return "m3u8"
	}
	return "mp4"
}

func selectMode(q string) int {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "4", "only_pdf", "only-pdf", "pdf", "file", "files", "资料":
		return modeOnlyPDF
	case "3", "sd", "low", "360", "标清":
		return modeSD
	case "2", "hd", "720", "高清":
		return modeHD
	default:
		return modeFHD
	}
}

// fetchUserClasshours fetches ProductsDirectory + GetClasshours to get the
// user's purchased course catalog. Source _parse_user_classhours: parse HTML
// <li> tags from GetClasshours response → lesson links.
func fetchUserClasshours(c *util.Client, productID, orderNumber, courseID string, headers map[string]string) ([]lessonRef, error) {
	h := cloneMap(headers)
	courseIDs := []string{}
	if courseID != "" {
		courseIDs = append(courseIDs, courseID)
	}

	// Step 1: ProductsDirectory → get ordernumber/course ids when they are not
	// embedded in the current page. The Python source always tries this owned
	// course-directory endpoint before GetClasshours.
	if orderNumber == "" || len(courseIDs) == 0 {
		dirURL := fmt.Sprintf(urlDirectory, productID, "")
		body, err := c.GetString(dirURL, h)
		if err == nil {
			if orderNumber == "" {
				orderNumber = extractOrderNumber(body)
			}
			courseIDs = append(courseIDs, extractCourseIDs(body)...)
		}
	}
	if len(courseIDs) == 0 {
		return nil, fmt.Errorf("wangxiao: cannot determine course id")
	}

	// Step 2: GetClasshours → parse lesson list
	var out []lessonRef
	for _, cid := range uniqueStrings(courseIDs) {
		classURL := fmt.Sprintf(urlClasshours, cid, productID)
		body, err := c.GetString(classURL, h)
		if err != nil {
			continue
		}
		out = append(out, parseClasshourLinks(body, productID)...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("wangxiao GetClasshours: no lessons")
	}
	return out, nil
}

// parseClasshourLinks extracts lesson links from GetClasshours HTML.
// Source _parse_user_classhours: finds <li> elements with lesson links.
func parseClasshourLinks(html, productID string) []lessonRef {
	refs := parseLessonRefs(html, refererURL, productID)
	for i := range refs {
		lower := strings.ToLower(refs[i].URL)
		if strings.Contains(lower, "player/") || strings.Contains(lower, "users.wangxiao.cn") {
			refs[i].Legacy = true
		}
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
	re := regexp.MustCompile(`https?://[^\s"'<>]+\.(?:pdf|docx?|pptx?|xlsx?|mp4|m4a|mp3|aac|wav|flv|mkv|zip|rar|7z|txt|csv|caj)[^\s"'<>]*`)
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

func firstStreamURL(entry *extractor.MediaInfo) string {
	if entry == nil {
		return ""
	}
	if stream, ok := entry.Streams["default"]; ok {
		for _, u := range stream.URLs {
			if u = strings.TrimSpace(u); u != "" {
				return u
			}
		}
	}
	for _, stream := range entry.Streams {
		for _, u := range stream.URLs {
			if u = strings.TrimSpace(u); u != "" {
				return u
			}
		}
	}
	return ""
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

type bokeCCPlay struct {
	URL      string
	M3U8Text string
	Quality  int
}

type bokeCCVariant struct {
	URL       string
	Quality   int
	MediaType int
}

type bokeCCXMLCopy struct {
	PlayURL   string `xml:"playurl"`
	BackupURL string `xml:"backupurl"`
	Quality   int    `xml:"quality"`
	MediaType int    `xml:"mediatype"`
}

type bokeCCXMLResponse struct {
	Copies []bokeCCXMLCopy `xml:"copy"`
}

func resolveBokeCCWangxiao(c *util.Client, vid, siteid, referer string, mode int) (bokeCCPlay, error) {
	if vid == "" || siteid == "" {
		return bokeCCPlay{}, fmt.Errorf("bokecc: missing vid or siteid")
	}
	api := fmt.Sprintf(urlVideoPlay, url.QueryEscape(vid), url.QueryEscape(siteid))
	body, err := c.GetString(api, map[string]string{"Referer": referer})
	if err != nil {
		return bokeCCPlay{}, fmt.Errorf("bokecc fetch: %w", err)
	}
	variants := parseBokeCCPayload(body, api)
	if len(variants) == 0 {
		return bokeCCPlay{}, fmt.Errorf("bokecc: no playable variants")
	}
	sort.SliceStable(variants, func(i, j int) bool { return variants[i].Quality > variants[j].Quality })
	chosen := chooseBokeVariant(variants, mode)
	play := bokeCCPlay{URL: chosen.URL, Quality: chosen.Quality}
	if formatFromURL(chosen.URL) == "m3u8" {
		if text, err := rewriteWangxiaoM3U8(c, chosen.URL, referer); err == nil && strings.TrimSpace(text) != "" {
			play.M3U8Text = text
		}
	}
	return play, nil
}

func parseBokeCCPayload(raw, base string) []bokeCCVariant {
	raw = strings.TrimSpace(raw)
	var out []bokeCCVariant
	var xmlResp bokeCCXMLResponse
	if xml.Unmarshal([]byte(raw), &xmlResp) == nil {
		for _, c := range xmlResp.Copies {
			u := firstNonEmpty(c.PlayURL, c.BackupURL)
			if u == "" {
				continue
			}
			if c.MediaType != 0 && c.MediaType != 1 {
				continue
			}
			out = append(out, bokeCCVariant{URL: normalizeURL(u, base), Quality: c.Quality, MediaType: c.MediaType})
		}
	}
	if len(out) > 0 {
		return uniqueBokeVariants(out)
	}

	raw = stripJSONP(raw)
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil
	}
	for _, m := range mapsUnder(v) {
		u := firstString(m, "playurl", "playUrl", "url")
		if u == "" {
			u = firstString(m, "backupurl", "backupUrl")
		}
		if u == "" {
			continue
		}
		mediaType := toIntAny(m["mediatype"])
		if mediaType != 0 && mediaType != 1 {
			continue
		}
		out = append(out, bokeCCVariant{
			URL:       normalizeURL(u, base),
			Quality:   toIntAny(firstAny(m, "quality", "definition", "bitrate")),
			MediaType: mediaType,
		})
	}
	return uniqueBokeVariants(out)
}

func chooseBokeVariant(variants []bokeCCVariant, mode int) bokeCCVariant {
	if len(variants) == 0 {
		return bokeCCVariant{}
	}
	idx := 0
	switch mode {
	case modeSD:
		idx = 2
	case modeHD:
		idx = 1
	}
	if idx >= len(variants) {
		idx = len(variants) - 1
	}
	return variants[idx]
}

func rewriteWangxiaoM3U8(c *util.Client, m3u8URL, referer string) (string, error) {
	text, err := c.GetString(m3u8URL, map[string]string{"Referer": referer})
	if err != nil {
		return "", err
	}
	if !strings.Contains(text, "#EXTM3U") {
		return text, nil
	}
	keyRe := regexp.MustCompile(`(?i)URI="([^"]+)"`)
	if m := keyRe.FindStringSubmatch(text); len(m) > 1 {
		keyURL := normalizeURL(m[1], m3u8URL)
		if key, err := c.GetBytes(keyURL, map[string]string{"Referer": referer}); err == nil && len(key) > 0 {
			if len(key) > 16 {
				key = key[:16]
			}
			text = strings.ReplaceAll(text, m[1], "data:application/octet-stream;base64,"+base64.StdEncoding.EncodeToString(key))
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(strings.ToLower(trimmed), "http") || strings.HasPrefix(strings.ToLower(trimmed), "data:") {
			continue
		}
		lines[i] = normalizeURL(trimmed, m3u8URL)
	}
	return strings.Join(lines, "\n"), nil
}

func resolveFileResourcesFromBody(c *util.Client, ref lessonRef, body string, headers map[string]string) []string {
	candidates := []string{}
	var out []string
	if strings.TrimSpace(ref.FileHTML) != "" {
		out = append(out, "data:text/html;charset=utf-8,"+url.PathEscape(ref.FileHTML))
	}
	if ref.FileURL != "" {
		candidates = append(candidates, normalizeURL(ref.FileURL, ref.URL))
	}
	linkRe := regexp.MustCompile(`(?is)(?:href|src|data-href)=["']([^"']*(?:DownHandOut|down\.aspx)[^"']*)["']|["']([^"']*(?:DownHandOut|down\.aspx)[^"']*)["']`)
	for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
		for _, group := range m[1:] {
			if group != "" {
				candidates = append(candidates, normalizeURL(html.UnescapeString(group), ref.URL))
			}
		}
	}
	if ref.ActivityID != "" {
		if ref.Legacy {
			candidates = append(candidates, fmt.Sprintf(urlPlayerDown, ref.ActivityID))
		} else {
			candidates = append(candidates, fmt.Sprintf(urlLiveHandout, ref.ActivityID))
		}
	}
	for _, candidate := range uniqueStrings(candidates) {
		if u := resolveFileCandidate(c, candidate, headers); u != "" {
			out = append(out, u)
		}
	}
	return uniqueStrings(out)
}

func resolveFileCandidate(c *util.Client, candidate string, headers map[string]string) string {
	if candidate == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(candidate), "data:") {
		return candidate
	}
	resp, err := c.Get(candidate, headers)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	finalURL := candidate
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if resp.StatusCode < 400 && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/json") {
		return finalURL
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	if u := extractFileURL(string(raw)); u != "" {
		return normalizeURL(html.UnescapeString(u), finalURL)
	}
	if resourceExt(finalURL) != "file" && !strings.Contains(strings.ToLower(finalURL), "downhandout") && !strings.Contains(strings.ToLower(finalURL), "down.aspx") {
		return finalURL
	}
	return ""
}

func resourceExt(u string) string {
	lower := strings.ToLower(u)
	if strings.HasPrefix(lower, "data:text/html") || strings.HasPrefix(lower, "data:application/xhtml") {
		return "html"
	}
	parsed, err := url.Parse(u)
	target := u
	if err == nil {
		target = parsed.Path
		if target == "" {
			target = parsed.RawQuery
		}
	}
	extRe := regexp.MustCompile(`(?i)\.([a-z0-9]{2,5})(?:$|[?#&])`)
	if m := extRe.FindStringSubmatch(target); len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return "file"
}

func cookieFromJar(jar http.CookieJar, name string, domains ...string) string {
	if jar == nil || name == "" {
		return ""
	}
	for _, domain := range domains {
		u, err := url.Parse("https://" + strings.TrimPrefix(domain, "https://"))
		if err != nil {
			continue
		}
		for _, c := range jar.Cookies(u) {
			if strings.EqualFold(c.Name, name) && c.Value != "" {
				return c.Value
			}
		}
	}
	return ""
}

func extractCourseID(text, fallback string) string {
	for _, s := range []string{fallback, text} {
		re := regexp.MustCompile(`(?i)(?:cid|courseid|courseId|CourseId|data-cid|data-id)\s*[=:]\s*["']?([A-Za-z0-9_-]+)`)
		if m := re.FindStringSubmatch(s); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func extractCourseIDs(text string) []string {
	re := regexp.MustCompile(`(?i)(?:cid|courseid|data-cid|data-id)\s*[=:]\s*["']?([A-Za-z0-9_-]+)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			out = append(out, m[1])
		}
	}
	return uniqueStrings(out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" && v != "<nil>" {
			return v
		}
	}
	return ""
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func uniqueBokeVariants(in []bokeCCVariant) []bokeCCVariant {
	seen := map[string]bool{}
	out := make([]bokeCCVariant, 0, len(in))
	for _, v := range in {
		if v.URL == "" || seen[v.URL] {
			continue
		}
		seen[v.URL] = true
		out = append(out, v)
	}
	return out
}

func stripJSONP(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		return raw
	}
	start := strings.IndexAny(raw, "{[")
	end := strings.LastIndexAny(raw, "}]")
	if start >= 0 && end > start {
		return raw[start : end+1]
	}
	return raw
}

func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func toIntAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(x), "%d", &n)
		return n
	default:
		var n int
		fmt.Sscanf(strings.TrimSpace(fmt.Sprint(x)), "%d", &n)
		return n
	}
}

func mapsUnder(v any) []map[string]any {
	out := []map[string]any{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(v)
	return out
}
