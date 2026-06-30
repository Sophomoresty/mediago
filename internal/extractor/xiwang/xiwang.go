// Package xiwang implements an extractor for xiwang.com courses,
// including the Suyang (xi-xue.com) and Youke (wen-su.com) sub-brands.
package xiwang

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

// brandConfig holds the per-brand API endpoints, headers, and course type list.
// Xiwang_Course, Xiwang_Suyang, and Xiwang_Youke each define one of these.
type brandConfig struct {
	name       string // "xiwang" / "xiwang.suyang" / "xiwang.youke"
	referer    string
	checkLogin string
	courseURL  string
	infoURL    string
	videoPlay  string
	livePlay   string
	m3u8Play   string // format string with %s for appID, fid, bid
	pptList    string // format string with %s for planId
	fileURL    string
	priceURL   string // format string with %s for courseId

	appVersion string
	couTypes   []string // course types for _get_course_list pagination
	extraHdr   map[string]string
}

var xiwangBrand = brandConfig{
	name:       "xiwang",
	referer:    "https://www.xiwang.com",
	checkLogin: "https://api.xue.xiwang.com/login/V1/Web/checkLogin?X-Businessline-Id=30",
	courseURL:  "https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList",
	infoURL:    "https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyPlans/planListV2",
	videoPlay:  "https://studentlive.bcc.xiwang.com/v1/student/classroom/playback/enter",
	livePlay:   "https://lecturepie.bcc.xiwang.com/v1/student/classroom/playback/enter",
	m3u8Play:   "https://gslbsaturnbcc.saasw.vdyoo.com/v1/player/vodshow?appid=%s&fid=%s&bid=%s",
	pptList:    "https://studentlive.bcc.xiwang.com/v1/student/note/getTeacherNoteListV2?bizId=3&planId=%s",
	fileURL:    "https://i.bcc.xiwang.com/icenter/App/StudyCenter/MyPlans/getDatumListByType",
	priceURL:   "https://api.xue.xiwang.com/mall/detail/1/%s",
	appVersion: "60901",
	couTypes:   []string{"1", "2"},
	extraHdr:   map[string]string{"X-Businessline-Id": "30"},
}

var suyangBrand = brandConfig{
	name:       "xiwang.suyang",
	referer:    "https://www.xiwang.com",
	checkLogin: "https://api.xue.xi-xue.com/login/V1/Web/checkLogin?X-Businessline-Id=40",
	courseURL:  "https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList",
	infoURL:    "https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyPlans/planListV2",
	videoPlay:  "https://studentlive.bcc.xiwang.com/v1/student/classroom/playback/enter",
	livePlay:   "https://lecturepie.bcc.xiwang.com/v1/student/classroom/playback/enter",
	m3u8Play:   "https://gslbsaturnbcc.saasw.vdyoo.com/v1/player/vodshow?appid=%s&fid=%s&bid=%s",
	pptList:    "https://studentlive.bcc.xiwang.com/v1/student/note/getTeacherNoteListV2?bizId=3&planId=%s",
	fileURL:    "https://i.bcc.xiwang.com/icenter/App/StudyCenter/MyPlans/getDatumListByType",
	priceURL:   "https://api.xue.xiwang.com/mall/detail/1/%s",
	appVersion: "60902",
	couTypes:   []string{"7", "8"},
	extraHdr:   map[string]string{"X-Businessline-Id": "40", "appSource": "3"},
}

var youkeBrand = brandConfig{
	name:       "xiwang.youke",
	referer:    "https://www.xiwang.com",
	checkLogin: "https://api.xue.wen-su.com/login/V1/Web/checkLogin?X-Businessline-Id=30",
	courseURL:  "https://i.bcc.wen-su.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList",
	infoURL:    "https://i.bcc.wen-su.com/icenter-go/App/StudyCenter/MyPlans/planListV2",
	videoPlay:  "https://studentlive.bcc.wen-su.com/v1/student/classroom/playback/enter",
	livePlay:   "https://lecturepie.bcc.wen-su.com/v1/student/classroom/playback/enter",
	m3u8Play:   "https://gslbsaturnbcc.saasw.vdyoo.com/v1/player/vodshow?appid=%s&fid=%s&bid=%s",
	pptList:    "https://studentlive.bcc.wen-su.com/v1/student/note/getTeacherNoteListV2?bizId=3&planId=%s",
	fileURL:    "https://i.bcc.wen-su.com/icenter/App/StudyCenter/MyPlans/getDatumListByType",
	priceURL:   "https://api.xue.wen-su.com/mall/detail/1/%s",
	appVersion: "50900",
	couTypes:   []string{"1", "2"},
	extraHdr:   map[string]string{"X-Businessline-Id": "30"},
}

// cookieDomains returns the set of base URLs whose cookies the client should
// send. The list is brand-specific because Youke uses wen-su.com domains.
func (b *brandConfig) cookieDomains() []string {
	out := []string{b.referer}
	for _, u := range []string{b.checkLogin, b.courseURL, b.infoURL, b.videoPlay, b.livePlay} {
		parsed, err := url.Parse(u)
		if err != nil {
			continue
		}
		origin := parsed.Scheme + "://" + parsed.Host
		dup := false
		for _, o := range out {
			if o == origin {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, origin)
		}
	}
	return out
}

var xiwangPatterns = []string{`(?:[\w-]+\.)?(?:xiwang\.com|bcc\.xiwang\.com)/`}
var suyangPatterns = []string{`(?:[\w-]+\.)?xi-xue\.com`}
var youkePatterns = []string{`(?:[\w-]+\.)?wen-su\.com`}

var idRe = regexp.MustCompile(`(?i)(?:courseId|course_id|cid|id|planId)=([0-9]+)|/(?:course|detail|mall)/(?:\d+/)?([0-9]+)`)
var loginOKRe = regexp.MustCompile(`"(?:stat|status)"\s*:\s*1`)

func init() {
	extractor.Register(&Xiwang{brand: xiwangBrand}, extractor.SiteInfo{Name: "Xiwang", URL: "xiwang.com", NeedAuth: true})
	extractor.Register(&Xiwang{brand: suyangBrand}, extractor.SiteInfo{Name: "XiwangSuyang", URL: "xi-xue.com", NeedAuth: true})
	extractor.Register(&Xiwang{brand: youkeBrand}, extractor.SiteInfo{Name: "XiwangYouke", URL: "wen-su.com", NeedAuth: true})
}

type Xiwang struct {
	brand brandConfig
}

func (s *Xiwang) Patterns() []string {
	switch s.brand.name {
	case "xiwang.suyang":
		return suyangPatterns
	case "xiwang.youke":
		return youkePatterns
	default:
		return xiwangPatterns
	}
}

type course struct{ id, title, stuCouID, typ string }
type lesson struct{ id, title, bizID string }

// coursewareFile represents a single downloadable file within a courseware category.
type coursewareFile struct {
	name string
	url  string
}

func (s *Xiwang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xiwang requires login cookies")
	}
	b := &s.brand
	if b.checkLogin == "" {
		b = &xiwangBrand
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := brandHeaders(b, opts.Cookies)
	body, err := c.GetString(b.checkLogin, h)
	if err != nil {
		return nil, fmt.Errorf("xiwang checkLogin: %w", err)
	}
	if !loginOKRe.MatchString(body) {
		return nil, fmt.Errorf("xiwang checkLogin rejected cookie")
	}
	cid := firstMatch(idRe, rawURL)
	courses, err := fetchCoursesBrand(c, h, b)
	if err != nil {
		return nil, err
	}
	co := selectCourse(courses, cid)
	if co.id == "" {
		return nil, fmt.Errorf("xiwang course %q not found in course list", cid)
	}
	price := 999.0
	if p, ok := fetchPriceBrand(c, h, b, co); ok {
		price = p
	}
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}

	if !xiwangOnlyFilesMode(opts.Quality) {
		lessons, err := fetchLessonsBrand(c, h, b, co)
		if err != nil {
			return nil, err
		}
		for _, l := range lessons {
			for _, u := range resolveLessonBrand(c, h, b, co, l) {
				if u == "" || seen[u] {
					continue
				}
				seen[u] = true
				entries = append(entries, media(b.referer, firstNonEmpty(l.title, "plan_"+l.id), u, map[string]any{"planId": l.id, "bizId": l.bizID, "stuCouId": co.stuCouID}))
			}
			for i, p := range pptImagesBrand(c, h, b, co, l) {
				if p == "" || seen[p] {
					continue
				}
				seen[p] = true
				entries = append(entries, image(b.referer, fmt.Sprintf("%s_ppt_%d", firstNonEmpty(l.title, "plan_"+l.id), i+1), p, map[string]any{"planId": l.id}))
			}
		}
	}

	// Fetch courseware files via getDatumListByType (mirrors _get_infos file_dict path).
	fileCategories := fetchCoursewareFiles(c, h, b, co)
	for catName, files := range fileCategories {
		for _, f := range files {
			if f.url == "" || seen[f.url] {
				continue
			}
			seen[f.url] = true
			title := firstNonEmpty(f.name, catName)
			entries = append(entries, fileEntry(b.referer, title, f.url, map[string]any{"category": catName, "stuCouId": co.stuCouID}))
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("xiwang: no playable m3u8/mp4, PPT, board image, or courseware URL resolved")
	}
	return &extractor.MediaInfo{Site: "xiwang", Title: firstNonEmpty(co.title, "xiwang_"+co.id), Entries: entries, Extra: map[string]any{"courseId": co.id, "stuCouId": co.stuCouID, "courseType": co.typ, "price": price, "purchased": true}}, nil
}

func xiwangOnlyFilesMode(quality string) bool {
	q := strings.ToLower(strings.TrimSpace(quality))
	q = strings.NewReplacer("-", "", "_", "", " ", "").Replace(q)
	switch q {
	case "2", "pdf", "onlypdf", "courseware", "material", "materials", "document", "documents", "file", "files", "课件", "资料":
		return true
	default:
		return false
	}
}

func fetchCoursesBrand(c *util.Client, h map[string]string, b *brandConfig) ([]course, error) {
	out := []course{}
	for _, couType := range b.couTypes {
		for pos := 1; pos <= 200; pos += 8 {
			root, err := postJSON(c, b.courseURL, map[string]string{"systemName": "pc-win", "appVersionNumber": b.appVersion, "position": fmt.Sprint(pos), "subjectId": "0", "couStatus": "0", "couType": couType}, h)
			if err != nil {
				return nil, err
			}
			list := append(listUnder(root, "learningCourses"), listUnder(root, "endedCourses")...)
			if len(list) == 0 {
				break
			}
			for _, m := range list {
				out = append(out, course{id: val(m, "courseId"), title: val(m, "courseName"), stuCouID: val(m, "stuCouId"), typ: firstNonEmpty(val(m, "type"), couType)})
			}
			if len(list) < 8 {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xiwang course list is empty")
	}
	return out, nil
}

func fetchLessonsBrand(c *util.Client, h map[string]string, b *brandConfig, co course) ([]lesson, error) {
	root, err := postJSON(c, b.infoURL, map[string]string{"courseId": co.id, "systemName": "pc-win", "appVersionNumber": b.appVersion, "type": co.typ, "stuCouId": co.stuCouID}, h)
	if err != nil {
		return nil, err
	}
	out := []lesson{}
	for _, m := range listUnder(root, "list") {
		id := firstNonEmpty(val(m, "planId"), val(m, "id"))
		if id == "" {
			continue
		}
		out = append(out, lesson{id: id, title: firstNonEmpty(val(m, "planName"), val(m, "name"), val(m, "title")), bizID: firstNonEmpty(val(m, "bizId"), val(m, "biz_id"), val(m, "type"), "3")})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xiwang plan list is empty")
	}
	return out, nil
}

func fetchPriceBrand(c *util.Client, h map[string]string, b *brandConfig, co course) (float64, bool) {
	if b.priceURL == "" || co.id == "" {
		return 0, false
	}
	body, err := c.GetString(fmt.Sprintf(b.priceURL, co.id), h)
	if err != nil {
		return 0, false
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return 0, false
	}
	if data := firstMapKey(root, "data"); data != nil {
		if priceModule := firstMapKey(data, "priceModule"); priceModule != nil {
			if p, ok := xiwangNormalizePrice(priceModule["price"]); ok {
				return p, true
			}
		}
	}
	for _, node := range mapsUnder(root) {
		if p, ok := xiwangNormalizePrice(node["price"]); ok {
			return p, true
		}
	}
	return 0, false
}

// fetchCoursewareFiles mirrors _get_infos file_dict path: POST to
// getDatumListByType with {couType, stuCouId}, parse result.data[] into a map
// of category_name -> [{file_name, file_url}, ...].
// Each top-level item in result.data has:
//   - name: category name
//   - files: recursive list of {name, url, files:[...]}
func fetchCoursewareFiles(c *util.Client, h map[string]string, b *brandConfig, co course) map[string][]coursewareFile {
	root, err := postJSON(c, b.fileURL, map[string]string{"couType": co.typ, "stuCouId": co.stuCouID}, h)
	if err != nil {
		return nil
	}
	categories := listUnder(root, "data")
	if len(categories) == 0 {
		return nil
	}
	out := map[string][]coursewareFile{}
	for catIdx, cat := range categories {
		catName := val(cat, "name")
		if catName == "" {
			catName = fmt.Sprintf("category_%d", catIdx+1)
		}
		var entries []coursewareFile
		fileCounter := 0
		appendCoursewareFiles(asList(cat, "files"), catIdx+1, &fileCounter, &entries)
		if len(entries) > 0 {
			out[catName] = entries
		}
	}
	return out
}

func appendCoursewareFiles(files []map[string]any, catIndex int, counter *int, entries *[]coursewareFile) {
	for _, f := range files {
		fName := val(f, "name")
		fURL := val(f, "url")
		if fURL != "" {
			*counter = *counter + 1
			label := fmt.Sprintf("(%d.%d)--%s", catIndex, *counter, fName)
			*entries = append(*entries, coursewareFile{name: label, url: normalizeURL(fURL)})
		}
		appendCoursewareFiles(asList(f, "files"), catIndex, counter, entries)
	}
}

// resolveLessonBrand mirrors Xiwang_Course._get_video_url + _download_video:
// it POSTs (JSON body via request_json) to playback/enter with biz_id=3 first,
// and if the main video is empty retries with biz_id=2. The enter response
// yields beforeClassFileId / videoFile / afterClassFileId, each resolved to a
// real stream through the vodshow (m3u8) endpoint only when the fid is a string
// containing ".m3u8" or ".mp4" and app_id + liveTypeId are present.
func resolveLessonBrand(c *util.Client, h map[string]string, b *brandConfig, co course, l lesson) []string {
	var fallback []string
	for _, biz := range []string{"3", "2"} {
		before, video, after := videoURLsForBizBrand(c, h, b, co, l, biz)
		out := unique(append(append(before, video...), after...))
		// Source falls back to biz_id=2 only when the main video slot is empty.
		if len(video) > 0 {
			return out
		}
		if len(out) > 0 && len(fallback) == 0 {
			fallback = out
		}
	}
	return fallback
}

func videoURLsForBizBrand(c *util.Client, h map[string]string, b *brandConfig, co course, l lesson, biz string) (before, video, after []string) {
	root := fetchXiwangPlayInfoData(c, h, b, co, l, biz)
	configs := firstMapKey(root, "configs")
	if configs == nil {
		return nil, nil, nil
	}
	appID := val(configs, "appId")
	liveType := firstNonEmpty(val(firstMapKey(root, "planInfo"), "liveTypeId"), val(configs, "liveTypeId"))
	if appID == "" || liveType == "" {
		before = xiwangDirectMediaURLsForKeys(root, "before")
		video = xiwangDirectMediaURLsForKeys(root, "video")
		after = xiwangDirectMediaURLsForKeys(root, "after")
		return before, video, after
	}
	resolveFields := func(keys ...string) []string {
		var out []string
		for _, key := range keys {
			out = append(out, resolveXiwangMediaRef(c, h, b, val(configs, key), appID, liveType)...)
		}
		return unique(out)
	}
	before = resolveFields("beforeClassFileId", "beforeClassFile", "beforeVideoFile", "beforeFile", "beforeClassVideoFile")
	video = resolveFields("videoFile", "videoFileId", "videoUrl", "videoURL", "video", "mainVideoFile", "recordFile", "playbackFile", "videoPath")
	after = resolveFields("afterClassFileId", "afterClassFile", "afterVideoFile", "afterFile", "afterClassVideoFile")
	before = unique(append(before, xiwangDirectMediaURLsForKeys(root, "before")...))
	video = unique(append(video, xiwangDirectMediaURLsForKeys(root, "video")...))
	after = unique(append(after, xiwangDirectMediaURLsForKeys(root, "after")...))
	return before, video, after
}

func fetchXiwangPlayInfoData(c *util.Client, h map[string]string, b *brandConfig, co course, l lesson, biz string) map[string]any {
	api := b.videoPlay
	if biz != "3" {
		api = b.livePlay
	}
	root, err := postJSONBody(c, api, map[string]any{
		"acceptPlanVersion": 42,
		"bizId":             biz,
		"planId":            toInt(l.id),
		"stuCouId":          toInt(co.stuCouID),
	}, h)
	if err != nil {
		return nil
	}
	return root
}

func resolveXiwangMediaRef(c *util.Client, h map[string]string, b *brandConfig, ref, appID, liveType string) []string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	lower := strings.ToLower(ref)
	if strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") {
		if appID != "" && liveType != "" {
			if urls := m3u8URLsBrand(c, h, b, ref, appID, liveType); len(urls) > 0 {
				return urls
			}
		}
		if isHTTPURL(normalizeURL(ref)) {
			return []string{normalizeURL(ref)}
		}
	}
	if isHTTPURL(normalizeURL(ref)) && looksXiwangVideoURL(ref) {
		return []string{normalizeURL(ref)}
	}
	return nil
}

// m3u8URLsBrand mirrors _get_m3u8_urls: GET the vodshow endpoint and read
// content.addrs[].addr. The source formats the URL via str.format without
// percent-encoding, so the parameters are passed through verbatim.
func m3u8URLsBrand(c *util.Client, h map[string]string, b *brandConfig, fid, appID, bid string) []string {
	body, err := c.GetString(fmt.Sprintf(b.m3u8Play, appID, fid, bid), h)
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil
	}
	content, _ := root["content"].(map[string]any)
	addrs, _ := content["addrs"].([]any)
	out := []string{}
	for _, a := range addrs {
		if m, ok := a.(map[string]any); ok {
			if u := strings.TrimSpace(val(m, "addr")); u != "" {
				out = append(out, normalizeURL(u))
			}
		}
	}
	if len(out) == 0 {
		for _, m := range mapsUnder(root) {
			if u := strings.TrimSpace(val(m, "addr")); u != "" {
				out = append(out, normalizeURL(u))
			}
		}
	}
	return prioritizeReachableMediaURLs(unique(out), h, b)
}

// pptImagesBrand mirrors Xiwang_Course._get_ppt_url_list: primary
// getTeacherNoteListV2 data.picData[].pic_url, then playback metadata board
// page images for biz_id=3 and biz_id=2.
func pptImagesBrand(c *util.Client, h map[string]string, b *brandConfig, co course, l lesson) []string {
	body, err := c.GetString(fmt.Sprintf(b.pptList, l.id), h)
	out := []string{}
	if err == nil {
		var root map[string]any
		if json.Unmarshal([]byte(body), &root) == nil {
			data, _ := root["data"].(map[string]any)
			pics, _ := data["picData"].([]any)
			for _, p := range pics {
				if m, ok := p.(map[string]any); ok {
					if u := strings.TrimSpace(val(m, "pic_url")); u != "" {
						out = append(out, normalizeURL(u))
					}
				}
			}
		}
	}
	if len(out) > 0 {
		return unique(out)
	}
	for _, biz := range []string{"3", "2"} {
		playInfo := fetchXiwangPlayInfoData(c, h, b, co, l, biz)
		if playInfo == nil {
			continue
		}
		out = xiwangPageImageURLList(c, h, b, co, l, playInfo, biz)
		if len(out) > 0 {
			return unique(out)
		}
	}
	return out
}

func xiwangPageImageURLList(c *util.Client, h map[string]string, b *brandConfig, co course, l lesson, playInfo map[string]any, biz string) []string {
	resources := extractXiwangBoardResources(c, h, b, co, l, playInfo, biz)
	if len(resources.pages) == 0 {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	appendPage := func(pageKey string) {
		page := resources.pages[pageKey]
		if page == nil {
			return
		}
		u := normalizeURL(val(page, "imgUrl"))
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, item := range resources.timeline {
		appendPage(item.pageKey)
	}
	for _, key := range resources.order {
		appendPage(key)
	}
	return out
}

type xiwangBoardTimelineItem struct {
	at      float64
	pageKey string
}

type xiwangBoardResources struct {
	pages    map[string]map[string]any
	order    []string
	timeline []xiwangBoardTimelineItem
}

func extractXiwangBoardResources(c *util.Client, h map[string]string, b *brandConfig, co course, l lesson, playInfo map[string]any, biz string) xiwangBoardResources {
	metadata := fetchXiwangMetadata(c, h, b, co, l, playInfo, biz)
	res := xiwangBoardResources{pages: map[string]map[string]any{}}
	addPage := func(key string, page map[string]any, at float64) {
		if page == nil {
			return
		}
		img := normalizeURL(firstNonEmpty(val(page, "imgUrl"), val(page, "imgURL"), val(page, "imageUrl"), val(page, "pic_url"), val(page, "url")))
		if img == "" {
			return
		}
		key = firstNonEmpty(strings.TrimSpace(key), val(page, "dbKey"), val(page, "key"), val(page, "pageId"), img)
		if _, ok := res.pages[key]; !ok {
			res.order = append(res.order, key)
		}
		copyPage := map[string]any{}
		for k, v := range page {
			copyPage[k] = v
		}
		copyPage["imgUrl"] = img
		if copyPage["width"] == nil {
			copyPage["width"] = 960
		}
		if copyPage["height"] == nil {
			copyPage["height"] = 720
		}
		res.pages[key] = copyPage
		res.timeline = append(res.timeline, xiwangBoardTimelineItem{at: at, pageKey: key})
	}

	for _, event := range listUnder(metadata, "event") {
		category := toInt(val(event, "category"))
		if category != 50 {
			continue
		}
		props := firstMapKey(event, "properties")
		if props == nil {
			props = event
		}
		addPage(firstNonEmpty(val(props, "dbKey"), val(props, "key"), val(props, "pageId")), props, xiwangFloat(firstNonEmpty(val(event, "beginTime"), val(event, "timeStamp"), val(event, "timestamp"))))
	}
	for _, node := range mapsUnder(metadata) {
		if firstNonEmpty(val(node, "imgUrl"), val(node, "imgURL"), val(node, "imageUrl"), val(node, "pic_url")) != "" {
			addPage(firstNonEmpty(val(node, "dbKey"), val(node, "key"), val(node, "pageId")), node, xiwangFloat(firstNonEmpty(val(node, "beginTime"), val(node, "timeStamp"), val(node, "timestamp"))))
		}
	}
	for _, node := range mapsUnder(playInfo) {
		if firstNonEmpty(val(node, "imgUrl"), val(node, "imgURL"), val(node, "imageUrl"), val(node, "pic_url")) != "" {
			addPage(firstNonEmpty(val(node, "dbKey"), val(node, "key"), val(node, "pageId")), node, xiwangFloat(firstNonEmpty(val(node, "beginTime"), val(node, "timeStamp"), val(node, "timestamp"))))
		}
	}
	sortXiwangTimeline(res.timeline)
	return res
}

func fetchXiwangMetadata(c *util.Client, h map[string]string, b *brandConfig, co course, l lesson, playInfo map[string]any, biz string) map[string]any {
	configs := firstMapKey(playInfo, "configs")
	urls := firstMapKey(configs, "urls")
	metadataURL := firstNonEmpty(val(urls, "getMetadataUrl"), val(urls, "getFdMetadataUrl"), xiwangDefaultMetadataURL(b))
	root, err := postJSONBody(c, metadataURL, map[string]any{
		"planId":   toInt(l.id),
		"stuCouId": toInt(co.stuCouID),
		"bizId":    toInt(biz),
	}, h)
	if err != nil {
		return nil
	}
	if stat := val(root, "stat"); stat != "" && toInt(stat) != 1 {
		return nil
	}
	if data := firstMapKey(root, "data"); data != nil {
		return data
	}
	return root
}

func xiwangDefaultMetadataURL(b *brandConfig) string {
	if strings.Contains(b.videoPlay, "wen-su.com") {
		return "https://studentlive.bcc.wen-su.com/v1/student/playback/metadata/get"
	}
	return "https://studentlive.bcc.xiwang.com/v1/student/playback/metadata/get"
}

func xiwangDirectMediaURLsForKeys(root any, slot string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(u string) {
		u = normalizeURL(u)
		if !isHTTPURL(u) || !looksXiwangVideoURL(u) || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, node := range mapsUnder(root) {
		for k, v := range node {
			lk := strings.ToLower(k)
			switch slot {
			case "before":
				if !strings.Contains(lk, "before") {
					continue
				}
			case "after":
				if !strings.Contains(lk, "after") {
					continue
				}
			default:
				if strings.Contains(lk, "before") || strings.Contains(lk, "after") {
					continue
				}
				if !(strings.Contains(lk, "video") || strings.Contains(lk, "play") || strings.Contains(lk, "m3u8") || strings.Contains(lk, "mp4") || lk == "url" || strings.HasSuffix(lk, "url")) {
					continue
				}
			}
			collectXiwangStringURLs(v, add)
		}
	}
	return out
}

func collectXiwangStringURLs(v any, add func(string)) {
	switch x := v.(type) {
	case string:
		add(x)
	case []any:
		for _, it := range x {
			collectXiwangStringURLs(it, add)
		}
	case []string:
		for _, it := range x {
			add(it)
		}
	case map[string]any:
		for _, it := range x {
			collectXiwangStringURLs(it, add)
		}
	}
}

func prioritizeReachableMediaURLs(urls []string, h map[string]string, b *brandConfig) []string {
	if len(urls) < 2 {
		return urls
	}
	for i, u := range urls {
		if xiwangHeadOK(u, h, b) {
			if i == 0 {
				return urls
			}
			out := []string{u}
			out = append(out, urls[:i]...)
			out = append(out, urls[i+1:]...)
			return out
		}
	}
	return urls
}

func xiwangHeadOK(rawURL string, h map[string]string, b *brandConfig) bool {
	client, err := util.NewHTTPClient(5*time.Second, "")
	if err != nil {
		return false
	}
	req, err := http.NewRequest("HEAD", rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Referer", b.referer)
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "//")
}

func looksXiwangVideoURL(u string) bool {
	lower := strings.ToLower(u)
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4")
}

func xiwangNormalizePrice(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return 0, false
	}
	text = strings.NewReplacer(",", "", "￥", "", "¥", "").Replace(text)
	price, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return price, true
}

func xiwangFloat(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(value, 64)
	return f
}

func sortXiwangTimeline(items []xiwangBoardTimelineItem) {
	for i := 1; i < len(items); i++ {
		cur := items[i]
		j := i - 1
		for j >= 0 && items[j].at > cur.at {
			items[j+1] = items[j]
			j--
		}
		items[j+1] = cur
	}
}

func postJSONBody(c *util.Client, api string, data map[string]any, h map[string]string) (map[string]any, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	hdr := map[string]string{"Content-Type": "application/json"}
	for k, v := range h {
		hdr[k] = v
	}
	resp, err := c.Post(api, bytes.NewReader(payload), hdr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("xiwang HTTP %d from %s", resp.StatusCode, api)
	}
	var root map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return nil, fmt.Errorf("xiwang parse JSON: %w", err)
	}
	return root, nil
}

func toInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func postJSON(c *util.Client, api string, data map[string]string, h map[string]string) (map[string]any, error) {
	body, err := c.PostForm(api, data, h)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("xiwang parse JSON: %w", err)
	}
	return root, nil
}

func brandHeaders(b *brandConfig, jar http.CookieJar) map[string]string {
	h := map[string]string{"referer": b.referer, "Referer": b.referer, "Accept": "application/json, text/plain, */*"}
	for k, v := range b.extraHdr {
		h[k] = v
	}
	if ck := cookieHeaderBrand(b, jar); ck != "" {
		h["cookie"], h["Cookie"] = ck, ck
	}
	return h
}

func cookieHeaderBrand(b *brandConfig, jar http.CookieJar) string {
	parts := []string{}
	for _, raw := range b.cookieDomains() {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func selectCourse(cs []course, cid string) course {
	for _, c := range cs {
		if cid == "" || c.id == cid {
			return c
		}
	}
	return course{}
}
func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}
func firstMapKey(v any, key string) map[string]any {
	for _, m := range mapsUnder(v) {
		if x, ok := m[key].(map[string]any); ok {
			return x
		}
	}
	return nil
}
func listUnder(v any, key string) []map[string]any {
	out := []map[string]any{}
	for _, m := range mapsUnder(v) {
		if a, ok := m[key].([]any); ok {
			for _, x := range a {
				if mm, ok := x.(map[string]any); ok {
					out = append(out, mm)
				}
			}
		}
	}
	return out
}

// asList returns map[key] as a slice of map[string]any. Unlike listUnder
// it does not recurse into nested maps -- only the immediate value is checked.
func asList(m map[string]any, key string) []map[string]any {
	a, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := []map[string]any{}
	for _, x := range a {
		if mm, ok := x.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
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
func val(m map[string]any, k string) string {
	if m != nil {
		if v, ok := m[k]; ok && v != nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
func normalizeURL(u string) string {
	u = strings.TrimSpace(strings.ReplaceAll(u, `\/`, "/"))
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
func unique(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
func media(referer, title, u string, extra map[string]any) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "xiwang", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{u}, Format: formatOf(u), Headers: map[string]string{"Referer": referer}}}, Extra: extra}
}
func image(referer, title, u string, extra map[string]any) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "xiwang", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{u}, Format: "jpg", Headers: map[string]string{"Referer": referer}}}, Extra: extra}
}

// fileEntry creates a MediaInfo for a courseware file download. The format is
// inferred from the URL extension (stripping query parameters first), matching
// the _download_one_file extension dispatch in the source.
func fileEntry(referer, title, u string, extra map[string]any) *extractor.MediaInfo {
	ext := fileExtFromURL(u)
	return &extractor.MediaInfo{Site: "xiwang", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{u}, Format: ext, Headers: map[string]string{"Referer": referer}}}, Extra: extra}
}

// fileExtFromURL extracts the file extension from a URL after stripping query
// parameters. This mirrors the _download_one_file logic:
//
//	url.rsplit("?", 1)[0].rsplit(".", 1)[-1]
func fileExtFromURL(u string) string {
	// Strip query string
	if idx := strings.LastIndex(u, "?"); idx >= 0 {
		u = u[:idx]
	}
	// Get last dot-separated component
	if idx := strings.LastIndex(u, "."); idx >= 0 {
		ext := strings.ToLower(u[idx+1:])
		if ext != "" {
			return ext
		}
	}
	return "bin"
}

func formatOf(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
