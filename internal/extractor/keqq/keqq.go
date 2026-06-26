// Package keqq implements an extractor for ke.qq.com (腾讯课堂) courses.
//
// API endpoints from decompiled Mooc/Courses/Keqq/:
//
//	https://ke.qq.com/cgi-proxy/user/user_center/get_plan_list?count=10&page={page}
//	https://ke.qq.com/course/{cid}#term_id={tid}
//	https://ke.qq.com/cgi-proxy/rec_video/describe_rec_video?course_id={cid}&file_id={vid}&header={head}&term_id={tid}&vod_type=0
//	https://ke.qq.com/cgi-bin/file/download?cid={cid}&term_id={tid}&taid={taid}&uin={uin}&cw_id={cw_id}&a_id={a_id}
//	https://ke.qq.com/cgi-bin/course/get_terms_detail?cid={cid}&term_id_list=%5B{tid}%5D&preload=1
package keqq

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlReferer    = "https://ke.qq.com"
	urlCourseList = "https://ke.qq.com/cgi-proxy/user/user_center/get_plan_list?count=10&page=%s"
	urlCourse     = "https://ke.qq.com/course/%s#term_id=%s"
	urlVideo      = "https://ke.qq.com/cgi-proxy/rec_video/describe_rec_video?course_id=%s&file_id=%s&header=%s&term_id=%s&vod_type=0"
	urlFile       = "https://ke.qq.com/cgi-bin/file/download?cid=%s&term_id=%s&taid=%s&uin=%s&cw_id=%s&a_id=%s"
	urlDetail     = "https://ke.qq.com/cgi-bin/course/get_terms_detail?cid=%s&term_id_list=%%5B%s%%5D&preload=1"
)

var patterns = []string{`(?:[\w-]+\.)?ke\.qq\.com/`}

func init() {
	extractor.Register(&Keqq{}, extractor.SiteInfo{Name: "Keqq", URL: "ke.qq.com", NeedAuth: false})
}

type Keqq struct{}

func (k *Keqq) Patterns() []string { return patterns }

var (
	courseIDRe = regexp.MustCompile(`(?i)(?:/course/([0-9A-Za-z]+)|/webcourse/([0-9A-Za-z]+)/([0-9A-Za-z]+)|[?&#](?:cid|course_id|courseId)=([0-9A-Za-z]+))`)
	termIDRe   = regexp.MustCompile(`(?i)[?&#]term_id=([0-9A-Za-z]+)`)
	taidRe     = regexp.MustCompile(`(?i)[?&#]taid=([0-9A-Za-z]+)`)
	vidRe      = regexp.MustCompile(`(?i)[?&#]vid=([0-9A-Za-z]+)`)
	nextDataRe = regexp.MustCompile(`(?s)<script\s+id="__NEXT_DATA__"\s+type="application/json">(\{.*?\})</script>`)
	titleRe    = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	m3u8ExtRe  = regexp.MustCompile(`(?i)\.m3u8|#EXTM3U`)
	fileExtRe  = regexp.MustCompile(`(?i)\.(pdf|pptx?|docx?)$`)
)

func (k *Keqq) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		c.SetCookieJar(opts.Cookies)
	}
	headers := keqqHeaders(opts)

	cid, tid, taid, vid := parseKeqqIDs(rawURL)
	if cid == "" || tid == "" {
		if courses, err := fetchKeqqCourseList(c, headers); err == nil && len(courses) > 0 {
			if cid == "" {
				cid = courses[0].CID
			}
			if tid == "" {
				tid = courses[0].TID
			}
			if taid == "" {
				taid = courses[0].TID
			}
		}
	}
	if cid == "" || tid == "" {
		return nil, fmt.Errorf("cannot parse keqq cid/tid from URL: %s", rawURL)
	}

	page, html, err := fetchKeqqCoursePage(c, cid, tid, headers)
	if err != nil {
		return nil, err
	}
	title := keqqPageTitle(page, html, cid)
	price, purchased := keqqPriceAndPurchased(page, tid)
	chapters := keqqMergedChapters(page, c, cid, tid, headers)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("keqq: empty chapter list for cid=%s tid=%s", cid, tid)
	}

	entries := make([]*extractor.MediaInfo, 0)
	for chapterIndex, chapter := range chapters {
		for i, video := range chapter.Videos {
			entry, err := buildKeqqVideoEntry(c, headers, cid, tid, chapterIndex+1, i+1, chapter.Title, video, opts)
			if err == nil {
				entries = append(entries, entry)
			}
		}
		for i, file := range chapter.Files {
			if entry := buildKeqqFileEntry(cid, tid, chapterIndex+1, i+1, chapter.Title, file, opts); entry != nil {
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("keqq: no playable items for cid=%s tid=%s", cid, tid)
	}
	if title == "" {
		title = "keqq_" + cid
	}
	return &extractor.MediaInfo{Site: "keqq", Title: title, Entries: entries, Extra: map[string]any{"cid": cid, "tid": tid, "taid": taid, "vid": vid, "price": price, "purchased": purchased}}, nil
}

func keqqHeaders(opts *extractor.ExtractOpts) map[string]string {
	headers := map[string]string{"referer": urlReferer, "Referer": urlReferer}
	if opts != nil && opts.Cookies != nil {
		if ck := cookieString(opts.Cookies, "https", "ke.qq.com"); ck != "" {
			headers["cookie"] = ck
		}
		if uin := cookieValue(opts.Cookies, "uin"); uin != "" {
			headers["uin"] = uin
		}
	}
	return headers
}

type keqqCourseRef struct {
	CID   string
	TID   string
	Title string
}

func fetchKeqqCourseList(c *util.Client, headers map[string]string) ([]keqqCourseRef, error) {
	var out []keqqCourseRef
	seen := map[string]bool{}
	for page := 1; page < 10; page++ {
		body, err := c.GetString(fmt.Sprintf(urlCourseList, strconv.Itoa(page)), headers)
		if err != nil {
			break
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			continue
		}
		result := mapAny(resp["result"])
		mapList := extractKeqqRecords(result["map_list"])
		if len(mapList) == 0 {
			break
		}
		for _, item := range mapList {
			for _, course := range extractKeqqRecords(item["map_courses"]) {
				cid := firstText(course["cid"])
				tid := firstText(course["term_id"])
				if cid == "" || tid == "" {
					continue
				}
				key := cid + ":" + tid
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, keqqCourseRef{CID: cid, TID: tid, Title: firstText(course["tname"], course["cname"], course["term_name"], course["course_name"])})
			}
		}
		if boolValue(result["is_end"]) {
			break
		}
	}
	return out, nil
}

type keqqPageData struct {
	Title string
	Next  map[string]any
}

func fetchKeqqCoursePage(c *util.Client, cid, tid string, headers map[string]string) (*keqqPageData, string, error) {
	body, err := c.GetString(fmt.Sprintf(urlCourse, url.PathEscape(cid), url.PathEscape(tid)), headers)
	if err != nil {
		return nil, "", fmt.Errorf("keqq course page: %w", err)
	}
	page := &keqqPageData{Title: ""}
	if m := titleRe.FindStringSubmatch(body); len(m) > 1 {
		page.Title = strings.TrimSpace(stripTags(m[1]))
	}
	if m := nextDataRe.FindStringSubmatch(body); len(m) > 1 {
		_ = json.Unmarshal([]byte(m[1]), &page.Next)
	}
	return page, body, nil
}

func keqqPageTitle(page *keqqPageData, html, cid string) string {
	title := strings.TrimSpace(page.Title)
	if title == "" {
		title = "keqq_" + cid
	}
	if page == nil || page.Next == nil {
		return title
	}
	courseInfo := nestedMap(page.Next, "props", "pageProps", "courseInfo")
	if courseInfo == nil {
		return title
	}
	if data := mapAny(courseInfo["data"]); len(data) > 0 {
		if basic := mapAny(data["basic_info"]); len(basic) > 0 {
			if s := firstText(basic["course_name"], basic["title"], basic["name"]); s != "" {
				title = s
			}
		}
	}
	return title
}

func keqqPriceAndPurchased(page *keqqPageData, tid string) (float64, bool) {
	if page == nil || page.Next == nil {
		return 0, false
	}
	courseInfo := nestedMap(page.Next, "props", "pageProps", "courseInfo")
	if courseInfo == nil {
		return 0, false
	}
	data := mapAny(courseInfo["data"])
	basic := mapAny(data["basic_info"])
	price := priceFromAny(basic["price"])
	purchased := false
	pay := mapAny(data["pay_market_info"])
	for _, term := range extractStrings(pay["user_term_pay_info_list"]) {
		if term == tid {
			purchased = true
			break
		}
	}
	return price, purchased
}

type keqqChapter struct {
	Title  string
	Videos []keqqVideo
	Files  []keqqFile
}

type keqqVideo struct {
	VideoID   string
	VideoName string
}

type keqqFile struct {
	FileInfo map[string]any
	FileName string
}

func keqqMergedChapters(page *keqqPageData, c *util.Client, cid, tid string, headers map[string]string) []keqqChapter {
	merged := map[string]keqqChapter{}
	if page != nil && page.Next != nil {
		courseInfo := nestedMap(page.Next, "props", "pageProps", "courseInfo")
		if courseInfo != nil {
			data := mapAny(courseInfo["data"])
			catalogMap := mapAny(data["catalogMap"])
			for _, node := range extractKeqqRecords(catalogMap[tid]) {
				chapter := parseKeqqChapter(node)
				if chapter.Title != "" {
					merged[chapter.Title] = mergeKeqqChapter(merged[chapter.Title], chapter)
				}
			}
		}
	}
	if detailChapters, err := fetchKeqqDetailChapters(c, cid, tid, headers); err == nil {
		for _, chapter := range detailChapters {
			merged[chapter.Title] = mergeKeqqChapter(merged[chapter.Title], chapter)
		}
	}
	out := make([]keqqChapter, 0, len(merged))
	for _, chapter := range merged {
		if len(chapter.Videos) > 0 || len(chapter.Files) > 0 {
			out = append(out, chapter)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

func mergeKeqqChapter(dst, src keqqChapter) keqqChapter {
	if dst.Title == "" {
		dst = src
		return dst
	}
	dst.Videos = append(dst.Videos, src.Videos...)
	dst.Files = append(dst.Files, src.Files...)
	return dst
}

func fetchKeqqDetailChapters(c *util.Client, cid, tid string, headers map[string]string) ([]keqqChapter, error) {
	body, err := c.GetString(fmt.Sprintf(urlDetail, url.QueryEscape(cid), url.QueryEscape(tid)), headers)
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	result := mapAny(resp["result"])
	terms := extractKeqqRecords(result["terms"])
	for _, term := range terms {
		if firstText(term["term_id"]) != tid {
			continue
		}
		chapterInfo := extractKeqqRecords(term["chapter_info"])
		out := make([]keqqChapter, 0, len(chapterInfo))
		for _, node := range chapterInfo {
			chapter := parseKeqqChapter(node)
			if chapter.Title != "" {
				out = append(out, chapter)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("keqq detail: no matching term")
}

func parseKeqqChapter(node map[string]any) keqqChapter {
	idx := firstText(node["index"], node["idx"], node["seq"], 1)
	name := stripName(firstText(node["name"], node["stage_name"], node["title"], "未命名"))
	chapter := keqqChapter{Title: fmt.Sprintf("{%s}--%s", idx, name)}
	tasks := extractKeqqRecords(node["task_info"])
	for _, task := range tasks {
		switch intValue(task["type"]) {
		case 1, 2:
			for _, id := range parseCommaIDs(task["resid_list"]) {
				if id == "" {
					continue
				}
				videoName := fmt.Sprintf("[%s]--%s", idx, stripName(firstText(task["name"], node["name"], "未命名")))
				chapter.Videos = append(chapter.Videos, keqqVideo{VideoID: id, VideoName: videoName})
			}
		case 3:
			fi := map[string]any{"taid": task["taid"], "cw_id": task["resid_list"], "a_id": task["aid"]}
			fileName := fmt.Sprintf("(%s)--%s", idx, stripName(firstText(task["file"], task["name"], "未命名")))
			chapter.Files = append(chapter.Files, keqqFile{FileInfo: fi, FileName: fileName})
		}
	}
	for _, childKey := range []string{"children", "course_sections", "sub_info", "list", "items"} {
		for _, child := range extractKeqqRecords(node[childKey]) {
			childChapter := parseKeqqChapter(child)
			chapter = mergeKeqqChapter(chapter, childChapter)
		}
	}
	return chapter
}

func buildKeqqVideoEntry(c *util.Client, headers map[string]string, cid, tid string, chapterIndex, videoIndex int, chapterTitle string, video keqqVideo, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	urlText, size, subtitle, err := getKeqqM3U8Info(c, headers, cid, tid, video.VideoID, opts)
	if err != nil {
		return nil, err
	}
	stream := extractor.Stream{Quality: "best", URLs: []string{urlText}, Format: mediaExt(urlText), Size: size, Headers: map[string]string{"Referer": urlReferer}}
	if stream.Format == "m3u8" {
		stream.NeedMerge = true
	}
	entry := &extractor.MediaInfo{Site: "keqq", Title: fmt.Sprintf("[%d.%d]--%s", chapterIndex, videoIndex, video.VideoName), Streams: map[string]extractor.Stream{"best": stream}, Extra: map[string]any{"cid": cid, "tid": tid, "video_id": video.VideoID, "chapter_title": chapterTitle}}
	if subtitle != "" {
		entry.Subtitles = []extractor.Subtitle{{Language: "zh", URL: subtitle, Format: "srt"}}
	}
	return entry, nil
}

func buildKeqqFileEntry(cid, tid string, chapterIndex, fileIndex int, chapterTitle string, file keqqFile, opts *extractor.ExtractOpts) *extractor.MediaInfo {
	fi := file.FileInfo
	aid := firstText(fi["a_id"])
	cwID := firstText(fi["cw_id"])
	taid := firstText(fi["taid"])
	if taid == "" || cwID == "" || aid == "" {
		return nil
	}
	uin := "0"
	if opts != nil && opts.Cookies != nil {
		if v := cookieValue(opts.Cookies, "uin"); v != "" {
			uin = v
		}
	}
	fileURL := fmt.Sprintf(urlFile, url.QueryEscape(cid), url.QueryEscape(tid), url.QueryEscape(taid), url.QueryEscape(uin), url.QueryEscape(cwID), url.QueryEscape(aid))
	entry := &extractor.MediaInfo{Site: "keqq", Title: fmt.Sprintf("[%d.%d]--%s", chapterIndex, fileIndex, file.FileName), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{fileURL}, Format: fileExt(file.FileName), Headers: map[string]string{"Referer": urlReferer}}}, Extra: map[string]any{"cid": cid, "tid": tid, "chapter_title": chapterTitle, "file_info": fi}}
	return entry
}

func getKeqqM3U8Info(c *util.Client, headers map[string]string, cid, tid, videoID string, opts *extractor.ExtractOpts) (string, int64, string, error) {
	head := keqqHeadPayload(headers)
	body, err := c.GetString(fmt.Sprintf(urlVideo, url.QueryEscape(cid), url.QueryEscape(videoID), url.QueryEscape(head), url.QueryEscape(tid)), headers)
	if err != nil {
		return "", 0, "", fmt.Errorf("keqq rec_video: %w", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", 0, "", fmt.Errorf("keqq rec_video parse: %w", err)
	}
	rec := mapAny(mapAny(resp["result"])["rec_video_info"])
	infos := normalizeKeqqInfos(rec["infos"])
	if len(infos) == 0 {
		return "", 0, "", fmt.Errorf("keqq rec_video: no infos")
	}
	choice := pickKeqqInfo(infos, opts)
	urlText := choice.URL
	if strings.Contains(urlText, "/drm/") {
		if token := keqqDRMToken(headers, cid, tid); token != "" {
			urlText = strings.Replace(urlText, "/drm/", "/drm/voddrm.token."+url.QueryEscape(token)+".", 1)
		}
	}
	subtitle := ""
	for _, sub := range extractKeqqRecords(rec["subtitles"]) {
		if strings.EqualFold(firstText(sub["type"]), "srt") {
			subtitle = firstText(sub["url"])
			break
		}
	}
	if subtitle == "" {
		if direct := firstText(rec["subtitles"]); direct != "" && strings.Contains(strings.ToLower(direct), ".srt") {
			subtitle = direct
		}
	}
	return urlText, choice.Size, subtitle, nil
}

type keqqInfo struct {
	URL  string
	Size int64
}

func normalizeKeqqInfos(v any) []keqqInfo {
	var out []keqqInfo
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			switch y := item.(type) {
			case []any:
				if len(y) >= 2 {
					out = append(out, keqqInfo{URL: firstText(y[0]), Size: int64FromAny(y[1])})
				}
			case map[string]any:
				out = append(out, keqqInfo{URL: firstText(y["url"], y["play_url"], y["m3u8"], y["path"]), Size: int64FromAny(firstText(y["size"], y["filesize"], y["rate"]))})
			}
		}
	case map[string]any:
		for _, key := range []string{"list", "items", "data"} {
			if recs := normalizeKeqqInfos(x[key]); len(recs) > 0 {
				out = append(out, recs...)
			}
		}
	}
	return out
}

func pickKeqqInfo(infos []keqqInfo, opts *extractor.ExtractOpts) keqqInfo {
	if len(infos) == 0 {
		return keqqInfo{}
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].Size > infos[j].Size })
	return infos[0]
}

func keqqDRMToken(headers map[string]string, cid, tid string) string {
	payload := fmt.Sprintf("cid=%s;term_id=%s;vod_type=0;platform=3", cid, tid)
	if cookie := headers["cookie"]; cookie != "" {
		payload += ";" + cookie
	}
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

func keqqHeadPayload(headers map[string]string) string {
	uin := headers["uin"]
	if uin == "" {
		uin = "0"
	}
	payload := fmt.Sprintf(`{"uin":"%s","srv_appid":201,"cli_appid":"ke","cli_info":{"cli_platform":103}}`, uin)
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

func parseKeqqIDs(rawURL string) (cid, tid, taid, vid string) {
	if m := courseIDRe.FindStringSubmatch(rawURL); len(m) > 0 {
		cid = firstText(m[1], m[2], m[4])
		tid = firstText(m[3])
	}
	if m := termIDRe.FindStringSubmatch(rawURL); len(m) > 1 {
		tid = firstText(tid, m[1])
	}
	if m := taidRe.FindStringSubmatch(rawURL); len(m) > 1 {
		taid = m[1]
	}
	if m := vidRe.FindStringSubmatch(rawURL); len(m) > 1 {
		vid = m[1]
	}
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		cid = firstText(cid, q.Get("cid"), q.Get("course_id"), q.Get("courseId"))
		tid = firstText(tid, q.Get("term_id"))
		taid = firstText(taid, q.Get("taid"))
		vid = firstText(vid, q.Get("vid"))
		if frag, err := url.Parse(u.Fragment); err == nil {
			fq := frag.Query()
			tid = firstText(tid, fq.Get("term_id"))
			taid = firstText(taid, fq.Get("taid"))
			vid = firstText(vid, fq.Get("vid"))
		}
	}
	return
}

func extractKeqqRecords(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"map_list", "map_courses", "terms", "chapter_info", "course_sections", "result", "data", "list", "items"} {
			if recs := extractKeqqRecords(x[key]); len(recs) > 0 {
				return recs
			}
		}
	}
	return nil
}

func nestedMap(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, key := range keys {
		next, ok := cur[key].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func mapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func stripTags(s string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
}

func stripName(s string) string {
	return strings.TrimSpace(stripTags(s))
}

func fileExt(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if m := fileExtRe.FindStringSubmatch(name); len(m) > 1 {
		return m[1]
	}
	return "dat"
}

func mediaExt(u string) string {
	lu := strings.ToLower(u)
	switch {
	case m3u8ExtRe.MatchString(lu):
		return "m3u8"
	case strings.Contains(lu, ".flv"):
		return "flv"
	case strings.Contains(lu, ".mp3"):
		return "mp3"
	default:
		return "mp4"
	}
}

func extractStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if s := firstText(item); s != "" {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, x...)
	}
	return out
}

func parseCommaIDs(v any) []string {
	return strings.FieldsFunc(firstText(v), func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
}

func priceFromAny(v any) float64 {
	s := firstText(v)
	if s == "" {
		return 0
	}
	if strings.Contains(s, ".") {
		f, _ := strconv.ParseFloat(s, 64)
		return f / 100
	}
	n, _ := strconv.ParseFloat(s, 64)
	if n > 1000 {
		return n / 100
	}
	return n
}

func int64FromAny(v any) int64 {
	s := firstText(v)
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func intValue(v any) int {
	s := firstText(v)
	n, _ := strconv.Atoi(s)
	return n
}

func boolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true") || x == "1"
	default:
		return false
	}
}

func cookieValue(jar http.CookieJar, name string) string {
	for _, host := range []string{"ke.qq.com"} {
		for _, ck := range jar.Cookies(&url.URL{Scheme: "https", Host: host}) {
			if ck.Name == name {
				return ck.Value
			}
		}
	}
	return ""
}

func cookieString(jar http.CookieJar, scheme, host string) string {
	cookies := jar.Cookies(&url.URL{Scheme: scheme, Host: host})
	parts := make([]string, 0, len(cookies))
	for _, ck := range cookies {
		if ck.Value != "" {
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	return strings.Join(parts, "; ")
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
	case json.Number:
		return strings.TrimSpace(x.String())
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return strings.TrimSpace(fmt.Sprint(x))
	default:
		return ""
	}
}
