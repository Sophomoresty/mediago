// Package open163 implements an extractor for open.163.com (网易公开课 VIP/free).
//
// API endpoints from decompiled Mooc/Courses/Open163/:
//
//	https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do
//	https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do
//	https://c.open.163.com/member/loginStatus.do
//	https://vip.open.163.com/courses/%s
//	https://open.163.com/newview/movie/free?pid=%s
package open163

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlMyOrders    = "https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do"
	urlCourseInfo  = "https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do"
	urlLoginStatus = "https://c.open.163.com/member/loginStatus.do"
	urlCoursePage  = "https://vip.open.163.com/courses/%s"
	urlFreePage    = "https://open.163.com/newview/movie/free?pid=%s"
	urlVipReferer  = "https://vip.open.163.com"
	urlFreeReferer = "https://open.163.com"
)

var patterns = []string{`(?:[\w-]+\.)?open\.163\.com/`}

func init() {
	extractor.Register(&Open163{}, extractor.SiteInfo{Name: "Open163", URL: "open.163.com", NeedAuth: true})
}

type Open163 struct{}

func (o *Open163) Patterns() []string { return patterns }

var (
	vipCourseRe = regexp.MustCompile(`/courses/([0-9A-Za-z]+)|courseId=([0-9A-Za-z]+)|cid=([0-9A-Za-z]+)`)
	freePidRe   = regexp.MustCompile(`(?:pid=|/free\?pid=)([0-9A-Za-z]+)`)
	freeMP4Re   = regexp.MustCompile(`"(https?:[^,]*?\.mp4[^"']*)"`)
	titleRe     = regexp.MustCompile(`<title>(.+?)</title>`)
)

func (o *Open163) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if pid := parseFreePID(rawURL); pid != "" {
		return o.extractFree(pid)
	}
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("open163 requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	if err := checkOpen163Cookie(c); err != nil {
		return nil, err
	}

	cid, courseUID := parseOpen163CourseIDs(rawURL)
	if cid == "" && courseUID == "" {
		// Fallback: fetch purchased courses from myOrders.do (source: Open163_App.prepare -> _select_my_course)
		return o.extractMyOrders(c)
	}
	course, err := loadOpen163Course(c, cid, courseUID)
	if err != nil {
		return nil, err
	}
	info := course.Data.CourseInfo
	title := sanitizeTitle(firstText(info.Title, info.Name, cid, courseUID, "open163"))
	entries := make([]*extractor.MediaInfo, 0)
	chapterTypes := []struct {
		items []open163Chapter
		kind  string
	}{
		{course.Data.MovieChapterList, "video"},
		{course.Data.AudioChapterList, "audio"},
	}
	for _, group := range chapterTypes {
		for chapterIndex, chapter := range group.items {
			chapterTitle := sanitizeTitle(firstText(chapter.Title, chapter.Name, "章节"))
			for contentIndex, content := range chapter.ContentList {
				item := buildOpen163Entry(chapterTitle, chapterIndex+1, contentIndex+1, content, group.kind)
				if item != nil {
					entries = append(entries, item)
				}
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("open163: no playable items in course %s", cid)
	}
	return &extractor.MediaInfo{Site: "open163", Title: title, Entries: entries, Extra: map[string]any{"course_id": cid, "course_uid": courseUID, "course_info": info}}, nil
}

func (o *Open163) extractFree(pid string) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	pageURL := fmt.Sprintf(urlFreePage, pid)
	body, err := c.GetString(pageURL, map[string]string{"Referer": urlFreeReferer})
	if err != nil {
		return nil, fmt.Errorf("open163 free page: %w", err)
	}
	title := pid
	if m := titleRe.FindStringSubmatch(body); len(m) > 1 {
		title = sanitizeTitle(strings.TrimSpace(strings.Split(m[1], "-")[0]))
	}
	parts := freeMP4Re.FindAllStringSubmatch(body, -1)
	if len(parts) == 0 {
		return nil, fmt.Errorf("open163 free page has no mp4 links for pid=%s", pid)
	}
	entries := make([]*extractor.MediaInfo, 0, len(parts))
	for i, m := range parts {
		u := normalizeFreeURL(m[1])
		if u == "" {
			continue
		}
		entries = append(entries, &extractor.MediaInfo{Site: "open163", Title: fmt.Sprintf("[%d]--%s", i+1, title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: mediaExt(u), Headers: map[string]string{"Referer": urlFreeReferer}}}})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("open163 free page: no decodable mp4 links")
	}
	return &extractor.MediaInfo{Site: "open163", Title: title, Entries: entries}, nil
}

func checkOpen163Cookie(c *util.Client) error {
	body, err := c.GetString(urlLoginStatus, map[string]string{"Referer": urlVipReferer, "Origin": urlVipReferer})
	if err != nil {
		return fmt.Errorf("open163 login check: %w", err)
	}
	var out struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return fmt.Errorf("open163 login check parse: %w", err)
	}
	if out.Code != 200 {
		return fmt.Errorf("open163 requires valid logged-in cookie (code=%d)", out.Code)
	}
	return nil
}

type open163CourseResp struct {
	Code int `json:"code"`
	Data struct {
		CourseInfo struct {
			ID          any    `json:"id"`
			CourseUID   any    `json:"courseUid"`
			Title       string `json:"title"`
			Name        string `json:"name"`
			OriginPrice any    `json:"originPrice"`
			BuyOrNot    any    `json:"buyOrNot"`
		} `json:"courseInfo"`
		MovieChapterList []open163Chapter `json:"movieChapterList"`
		AudioChapterList []open163Chapter `json:"audioChapterList"`
	} `json:"data"`
}

type open163Chapter struct {
	Title       string           `json:"title"`
	Name        string           `json:"name"`
	ContentList []open163Content `json:"contentList"`
}

type open163Content struct {
	Title         string             `json:"title"`
	Name          string             `json:"name"`
	MediaInfoList []open163MediaInfo `json:"mediaInfoList"`
	MediaSize     any                `json:"mediaSize"`
}

type open163MediaInfo struct {
	Type       string `json:"type"`
	EncryptURL string `json:"encryptUrl"`
	EncryptID  any    `json:"encryptId"`
	MediaURL   string `json:"mediaUrl"`
	URL        string `json:"url"`
	MediaSize  any    `json:"mediaSize"`
}

func loadOpen163Course(c *util.Client, cid, courseUID string) (*open163CourseResp, error) {
	variants := []map[string]string{}
	if cid != "" && courseUID != "" && cid != courseUID {
		variants = append(variants, map[string]string{"version": "1", "courseId": cid, "courseUid": courseUID})
	}
	if courseUID != "" {
		variants = append(variants, map[string]string{"version": "1", "courseUid": courseUID})
	}
	if cid != "" {
		variants = append(variants, map[string]string{"version": "1", "courseId": cid})
	}
	var lastErr error
	for _, form := range variants {
		body, err := c.PostForm(urlCourseInfo, form, map[string]string{"Referer": urlVipReferer, "Origin": urlVipReferer, "X-Requested-With": "XMLHttpRequest", "Accept": "application/json, text/plain, */*", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"})
		if err != nil {
			lastErr = err
			continue
		}
		var out open163CourseResp
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			lastErr = err
			continue
		}
		hasData := out.Data.CourseInfo.Title != "" || out.Data.CourseInfo.Name != "" || len(out.Data.MovieChapterList) > 0 || len(out.Data.AudioChapterList) > 0
		if out.Code == 200 && hasData {
			return &out, nil
		}
		lastErr = fmt.Errorf("open163 course info returned code=%d", out.Code)
	}
	return nil, fmt.Errorf("open163 load course data: %w", lastErr)
}

func buildOpen163Entry(chapterTitle string, chapterIndex, contentIndex int, content open163Content, kind string) *extractor.MediaInfo {
	title := sanitizeTitle(firstText(content.Title, content.Name, chapterTitle, kind))
	media := selectOpen163MediaInfo(content.MediaInfoList, kind)
	if media == nil {
		return nil
	}
	mediaSource := firstText(media.URL, media.MediaURL, media.EncryptURL)
	if kind == "audio" {
		mediaSource = firstText(media.EncryptURL, media.MediaURL, media.URL)
	}
	mediaURL := decodeOpen163MediaURL(mediaSource)
	if mediaURL == "" {
		return nil
	}
	stream := extractor.Stream{Quality: mediaQuality(media.Type), URLs: []string{mediaURL}, Format: mediaExt(mediaURL), Headers: map[string]string{"Referer": urlVipReferer}}
	if stream.Format == "m3u8" {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "open163", Title: fmt.Sprintf("[%d.%d]--%s", chapterIndex, contentIndex, title), Streams: map[string]extractor.Stream{kind: stream}, Extra: map[string]any{"media": media, "chapter_title": chapterTitle}}
}

func selectOpen163MediaInfo(list []open163MediaInfo, kind string) *open163MediaInfo {
	if len(list) == 0 {
		return nil
	}
	prefs := []string{"m3u8", "mp4"}
	if kind == "audio" {
		prefs = []string{"m4a", "mp3"}
	}
	quality := []string{"shd", "hd", "sd", "ld"}
	type candidate struct {
		score1 int
		score2 int
		info   *open163MediaInfo
	}
	best := candidate{score1: 99, score2: 99}
	for i := range list {
		m := &list[i]
		blob := strings.ToLower(strings.Join([]string{m.Type, m.EncryptURL, m.MediaURL, m.URL}, " "))
		s1 := len(prefs)
		for idx, p := range prefs {
			if strings.Contains(blob, p) {
				s1 = idx
				break
			}
		}
		s2 := len(quality)
		for idx, q := range quality {
			if strings.Contains(blob, q) {
				s2 = idx
				break
			}
		}
		if s1 < best.score1 || (s1 == best.score1 && s2 < best.score2) {
			best = candidate{score1: s1, score2: s2, info: m}
		}
	}
	return best.info
}

func decodeOpen163MediaURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http") || strings.HasPrefix(s, "#EXTM3U") {
		return s
	}
	pad := strings.Repeat("=", (4-len(s)%4)%4)
	decoded, err := base64.StdEncoding.DecodeString(s + pad)
	if err != nil {
		return s
	}
	if strings.HasPrefix(string(decoded), "http") {
		return string(decoded)
	}
	return s
}

func mediaExt(u string) string {
	lu := strings.ToLower(u)
	switch {
	case strings.Contains(lu, ".m3u8"):
		return "m3u8"
	case strings.Contains(lu, ".mp3"):
		return "mp3"
	case strings.Contains(lu, ".m4a"):
		return "m4a"
	default:
		return "mp4"
	}
}

func mediaQuality(t string) string {
	lu := strings.ToLower(t)
	for _, q := range []string{"shd", "hd", "sd", "ld"} {
		if strings.Contains(lu, q) {
			return q
		}
	}
	return "best"
}

func parseOpen163CourseIDs(rawURL string) (cid, courseUID string) {
	if m := vipCourseRe.FindStringSubmatch(rawURL); len(m) > 0 {
		for _, g := range m[1:] {
			if g != "" {
				if g[0] >= '0' && g[0] <= '9' {
					return g, g
				}
				return "", g
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		if v := q.Get("courseId"); v != "" {
			if v[0] >= '0' && v[0] <= '9' {
				return v, v
			}
			return "", v
		}
		if v := q.Get("pid"); v != "" {
			return "", v
		}
	}
	return "", ""
}

func parseFreePID(rawURL string) string {
	if m := freePidRe.FindStringSubmatch(rawURL); len(m) > 1 {
		return m[1]
	}
	if u, err := url.Parse(rawURL); err == nil {
		if v := u.Query().Get("pid"); v != "" {
			return v
		}
	}
	return ""
}

func firstText(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func stringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// sanitizeTitle normalises a title for filesystem use, matching the source
// winre_dir_sub / winre_sub behaviour: strip invalid chars, collapse whitespace,
// remove trailing dots/spaces, truncate to 32 runes.
func sanitizeTitle(s string) string {
	s = util.SanitizeFilename(s)
	// Collapse runs of whitespace (SanitizeFilename already replaces special chars with _)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, ". ")
	// Source WIN_LEN = 32
	runes := []rune(s)
	if len(runes) > 32 {
		s = string(runes[:32])
	}
	if s == "" {
		s = "untitled"
	}
	return s
}

// normalizeFreeURL applies url.QueryUnescape + unicode-escape decoding to a
// free-page MP4 URL, matching the source:
//
//	codecs.decode(parse.unquote(url), 'unicode_escape')
func normalizeFreeURL(raw string) string {
	// Step 1: URL percent-decode (parse.unquote)
	unquoted, err := url.QueryUnescape(raw)
	if err != nil {
		unquoted = raw
	}
	// Step 2: unicode-escape decode (codecs.decode(..., 'unicode_escape'))
	decoded := decodeUnicodeEscape(unquoted)
	return decodeOpen163MediaURL(decoded)
}

// decodeUnicodeEscape processes Python-style \uXXXX and \UXXXXXXXX escape
// sequences in a string.
func decodeUnicodeEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'u':
				if i+5 < len(s) {
					if v, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 6
						continue
					}
				}
			case 'U':
				if i+9 < len(s) {
					if v, err := strconv.ParseUint(s[i+2:i+10], 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 10
						continue
					}
				}
			case 'n':
				b.WriteByte('\n')
				i += 2
				continue
			case 't':
				b.WriteByte('\t')
				i += 2
				continue
			case '\\':
				b.WriteByte('\\')
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// --- myOrders.do purchased-course fallback ---
//
// Source: Open163_App._get_course_list (line 186-239)
// POST to order_url with {page: N, size: 99999}, paginate up to 9 pages.
// Filter items with status==2, dedup by (courseUid, productId).
// For each valid order, fetch course data and build entries.

type open163OrderResp struct {
	Code int `json:"code"`
	Data struct {
		Items []open163OrderItem `json:"items"`
	} `json:"data"`
}

type open163OrderItem struct {
	Status        any    `json:"status"`
	CourseUID     string `json:"courseUid"`
	ProductID     string `json:"productId"`
	ProductName   string `json:"productName"`
	ContentType   any    `json:"contentType"`
	DiscountPrice any    `json:"discountPrice"`
	ProductPrice  any    `json:"productPrice"`
}

// extractMyOrders fetches the purchased-course list from myOrders.do and
// returns each purchased course as a separate entry with its media items.
// This matches source Open163_App.prepare -> _select_my_course -> _get_course_list.
func (o *Open163) extractMyOrders(c *util.Client) (*extractor.MediaInfo, error) {
	headers := map[string]string{
		"Referer":          urlVipReferer,
		"Origin":           urlVipReferer,
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/plain, */*",
		"Content-Type":     "application/x-www-form-urlencoded;charset=UTF-8",
	}

	type dedupKey struct{ uid, pid string }
	seen := map[dedupKey]bool{}
	var courses []open163OrderItem

	// Paginate up to 9 pages (source: range(1, 10))
	for page := 1; page <= 9; page++ {
		form := map[string]string{
			"page": strconv.Itoa(page),
			"size": "99999",
		}
		body, err := c.PostForm(urlMyOrders, form, headers)
		if err != nil {
			break
		}
		var resp open163OrderResp
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			break
		}
		if resp.Code != 200 {
			break
		}
		items := resp.Data.Items
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			// Source: status must be 2 (paid)
			if !isStatus2(item.Status) {
				continue
			}
			uid := strings.TrimSpace(coalesceStr(item.CourseUID, item.ProductID))
			pid := strings.TrimSpace(item.ProductID)
			name := strings.TrimSpace(item.ProductName)
			if uid == "" || name == "" {
				continue
			}
			dk := dedupKey{uid, pid}
			if seen[dk] {
				continue
			}
			seen[dk] = true
			courses = append(courses, item)
		}
		if len(items) < 99999 {
			break
		}
	}

	if len(courses) == 0 {
		return nil, fmt.Errorf("open163: no purchased courses found in myOrders.do")
	}

	// For each purchased course, try to load its full data and build entries
	var allEntries []*extractor.MediaInfo
	for _, item := range courses {
		uid := strings.TrimSpace(coalesceStr(item.CourseUID, item.ProductID))
		pid := strings.TrimSpace(item.ProductID)
		courseTitle := sanitizeTitle(item.ProductName)

		// Determine cid and courseUID for loading course data
		// Source: cid = course_id (numeric), course_uid = courseUid (may be non-numeric)
		var cid, courseUID string
		if isNumeric(uid) {
			cid = uid
			courseUID = uid
		} else {
			courseUID = uid
		}
		if pid != "" && pid != uid && isNumeric(pid) {
			cid = pid
		}

		course, err := loadOpen163Course(c, cid, courseUID)
		if err != nil {
			// If we can't load course data, create a placeholder entry
			allEntries = append(allEntries, &extractor.MediaInfo{
				Site:  "open163",
				Title: courseTitle,
				Extra: map[string]any{
					"course_uid": uid,
					"product_id": pid,
					"purchased":  true,
					"price":      normalizeCentPrice(coalesceAny(item.DiscountPrice, item.ProductPrice)),
					"error":      err.Error(),
				},
			})
			continue
		}

		info := course.Data.CourseInfo
		title := sanitizeTitle(firstText(info.Title, info.Name, courseTitle))
		entries := make([]*extractor.MediaInfo, 0)
		chapterTypes := []struct {
			items []open163Chapter
			kind  string
		}{
			{course.Data.MovieChapterList, "video"},
			{course.Data.AudioChapterList, "audio"},
		}
		for _, group := range chapterTypes {
			for chapterIndex, chapter := range group.items {
				chapterTitle := sanitizeTitle(firstText(chapter.Title, chapter.Name, "章节"))
				for contentIndex, content := range chapter.ContentList {
					entry := buildOpen163Entry(chapterTitle, chapterIndex+1, contentIndex+1, content, group.kind)
					if entry != nil {
						entries = append(entries, entry)
					}
				}
			}
		}

		if len(entries) > 0 {
			allEntries = append(allEntries, &extractor.MediaInfo{
				Site:    "open163",
				Title:   title,
				Entries: entries,
				Extra: map[string]any{
					"course_uid": uid,
					"product_id": pid,
					"purchased":  true,
					"price":      normalizeCentPrice(coalesceAny(item.DiscountPrice, item.ProductPrice)),
				},
			})
		}
	}

	if len(allEntries) == 0 {
		return nil, fmt.Errorf("open163: purchased courses found but none have playable media")
	}

	return &extractor.MediaInfo{
		Site:    "open163",
		Title:   "open163-purchased-courses",
		Entries: allEntries,
	}, nil
}

// isStatus2 checks if a status value represents "paid" (status == 2).
// Source checks `status not in (2, '2')` to skip, so we accept int 2 or string "2".
func isStatus2(v any) bool {
	switch x := v.(type) {
	case float64:
		return x == 2
	case int:
		return x == 2
	case json.Number:
		n, _ := x.Int64()
		return n == 2
	case string:
		return x == "2"
	default:
		return fmt.Sprint(v) == "2"
	}
}

// normalizeCentPrice converts a price in cents to yuan, matching source
// _normalize_cent_price: if price >= 100, divide by 100.
func normalizeCentPrice(v any) float64 {
	var price float64
	switch x := v.(type) {
	case float64:
		price = x
	case int:
		price = float64(x)
	case json.Number:
		f, _ := x.Float64()
		price = f
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		price = f
	default:
		return 0
	}
	if price >= 100 {
		return math.Round(price) / 100
	}
	return price
}

// coalesceStr returns the first non-empty string.
func coalesceStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// coalesceAny returns the first non-nil, non-zero value.
func coalesceAny(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// isNumeric checks if a string consists entirely of digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
