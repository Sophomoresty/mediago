// Package wallstreets implements a source-aligned extractor for wallstreets.cn courses.
package wallstreets

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer              = "https://wallstreets.cn/"
	order_url            = "https://wallstreets.cn/my/orders"
	index_url            = "https://wallstreets.cn/my/course/%s"
	course_list_url      = "https://wallstreets.cn/api/me/courses?title=&limit=12&offset=%d&type=%s"
	classroom_list_url   = "https://wallstreets.cn/my/classrooms"
	classroom_esbar_url  = "https://wallstreets.cn/esbar/my/classroom"
	classroom_course_url = "https://wallstreets.cn/classroom/%s/courses"
	info_url             = "https://wallstreets.cn/course/%s/task/list/render/default"
	source_info_url      = "https://wallstreets.cn/my/course/%s/material?type=material"
	token_url            = "https://wallstreets.cn/course/%s/task/%s/activity_show"
	video_play_url       = "https://play.qiqiuyun.net/sdk_api/play?resNo=%s&token=%s&ssl=1&sdkType=js&lang=zh-CN"

	modeHD      = 1
	modeSD      = 2
	modeOnlyPDF = 3
)

var patterns = []string{`(?:[\w-]+\.)?wallstreets\.cn/`}

func init() {
	extractor.Register(&Wallstreets{}, extractor.SiteInfo{Name: "Wallstreets", URL: "wallstreets.cn", NeedAuth: true})
}

type Wallstreets struct{}

func (s *Wallstreets) Patterns() []string { return patterns }

var (
	cidRe             = regexp.MustCompile(`(?i)wallstreets\.cn.*?/course/(\d+)`)
	queryCIDRe        = regexp.MustCompile(`[?&](?:cid|courseId)=([0-9]+)`)
	classroomIDRe     = regexp.MustCompile(`/classroom/(\d+)`)
	classroomCourseRe = regexp.MustCompile(`(?is)href="/(?:my/)?course/(\d+)"[^>]*>[\s\S]*?<a[^>]+href="/(?:my/)?course/\d+"[^>]*>([\s\S]*?)</a>`)
	titleRe           = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	taskRe            = regexp.MustCompile(`(?is)&quot;title&quot;:&quot;(.*?)&quot.*?&quot;taskId&quot;:&quot;(\d+)&quot;.*?&quot;type&quot;:&quot;(.*?)&quot;`)
	taskPlainRe       = regexp.MustCompile(`(?is)"title"\s*:\s*"(.*?)".*?"taskId"\s*:\s*"?(\d+)"?.*?"type"\s*:\s*"(.*?)"`)
	materialRe        = regexp.MustCompile(`(?is)<a\s*href\s*=\s*"(/course/\d+/material/\d+/download)".*?>(.*?)</a>`)
	tokenRe           = regexp.MustCompile(`data-token\s*=\s*"([^"]+)"`)
	resNoRe           = regexp.MustCompile(`data-file-global-id\s*=\s*"([^"]+)"`)
	playlistRe        = regexp.MustCompile(`"playlist"\s*:\s*"(http.*?)"`)
	versionRe         = regexp.MustCompile(`"version"\s*:\s*(\d+)`)
	variantRe         = regexp.MustCompile(`(?is)BANDWIDTH=(\d+).*?\n(http[^\r\n]+)`)
	keyURIRe          = regexp.MustCompile(`URI="(.*?)"`)
	tagRe             = regexp.MustCompile(`(?is)<[^>]+>`)
)

func (s *Wallstreets) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wallstreets requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	cookie := cookieString(opts.Cookies)
	h := headers(cookie, false)
	loggedIn := checkCookie(c, cookie)

	cid := parseCID(rawURL)
	courses := fetchCourseLists(c, cookie)
	title := ""
	if cid != "" {
		for _, course := range courses {
			if course.ID == cid {
				title = course.Title
				break
			}
		}
	} else if len(courses) > 0 {
		cid, title = courses[0].ID, courses[0].Title
	}
	if cid == "" {
		return nil, fmt.Errorf("cannot parse wallstreets course id from URL or enrolled course list")
	}
	if title == "" {
		title = fetchTitle(c, h, cid)
	}
	if title == "" {
		title = "wallstreets_" + cid
	}

	videos, files, err := fetchInfos(c, h, cid)
	if err != nil {
		return nil, err
	}
	mode := selectMode(opts.Quality)
	var entries []*extractor.MediaInfo
	if mode != modeOnlyPDF {
		for _, v := range videos {
			if entry := resolveVideo(c, h, cid, v, mode, cookie); entry != nil {
				entries = append(entries, entry)
			}
		}
	}
	for _, f := range files {
		if mode == modeOnlyPDF && strings.EqualFold(f.Format, "mp4") {
			continue
		}
		entries = append(entries, fileEntry(f, cookie))
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wallstreets course %s returned no downloadable video/file entries", cid)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Title < entries[j].Title })
	return &extractor.MediaInfo{Site: "Wallstreets", Title: sanitize(title), Entries: entries, Extra: map[string]any{"course_id": cid, "logged_in": loggedIn, "order_url": order_url}}, nil
}

type courseRef struct{ ID, Title string }
type videoInfo struct{ ID, Title, Kind string }
type fileInfo struct{ URL, Name, Format string }
type variant struct {
	URL       string
	Bandwidth int
}

func parseCID(raw string) string {
	return first(match1(raw, cidRe), match1(raw, queryCIDRe), match1(raw, regexp.MustCompile(`/course/(\d+)`)))
}

func headers(cookie string, jsonAccept bool) map[string]string {
	h := map[string]string{"Referer": referer, "referer": referer, "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}
	if cookie != "" {
		h["cookie"] = cookie
		h["Cookie"] = cookie
	}
	if jsonAccept {
		h["Accept"] = "application/vnd.edusoho.v2+json"
	}
	return h
}

func checkCookie(c *util.Client, cookie string) bool {
	body, err := c.GetString("https://wallstreets.cn", headers(cookie, false))
	if err != nil || !strings.Contains(body, "退出登录") {
		return false
	}
	_, err = c.GetString(fmt.Sprintf(course_list_url, 0, "learning"), headers(cookie, true))
	return err == nil
}

func fetchCourseLists(c *util.Client, cookie string) []courseRef {
	seen := map[string]bool{}
	var out []courseRef
	for _, typ := range []string{"learning", "learned", "expired"} {
		out = append(out, fetchCourseList(c, cookie, typ, seen)...)
	}
	fetchClassroomCourseList(c, cookie, &out, seen)
	return out
}

func fetchCourseList(c *util.Client, cookie, ctype string, seen map[string]bool) []courseRef {
	var out []courseRef
	offset := 0
	for i := 0; i < 99; i++ {
		api := fmt.Sprintf(course_list_url, offset, url.QueryEscape(ctype))
		body, err := c.GetString(api, headers(cookie, true))
		if err != nil || strings.TrimSpace(body) == "" {
			break
		}
		items := parseCourseListJSON(body)
		if len(items) == 0 {
			break
		}
		added := 0
		for _, item := range items {
			id := first(textAt(item, "id", "course_id", "courseId"), textAt(unwrapMap(item["course"]), "id"))
			title := first(textAt(item, "courseSetTitle", "title", "courseTitle", "courseName"), textAt(unwrapMap(item["courseSet"]), "title"))
			if appendCourseInfo(&out, seen, id, title) {
				added++
			}
		}
		if added == 0 {
			break
		}
		offset += len(items)
	}
	return out
}

func parseCourseListJSON(body string) []map[string]any {
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil
	}
	return listOfMaps(unwrapValue(payload, "data"))
}

func fetchClassroomCourseList(c *util.Client, cookie string, out *[]courseRef, seen map[string]bool) {
	h := headers(cookie, false)
	body, _ := c.GetString(classroom_list_url, h)
	ids := uniqueMatches(body, classroomIDRe)
	if len(ids) == 0 {
		body, _ = c.GetString(classroom_esbar_url, h)
		ids = uniqueMatches(body, classroomIDRe)
	}
	for _, id := range ids {
		page, err := c.GetString(fmt.Sprintf(classroom_course_url, url.PathEscape(id)), h)
		if err != nil {
			continue
		}
		for _, m := range classroomCourseRe.FindAllStringSubmatch(page, -1) {
			appendCourseInfo(out, seen, m[1], stripTags(m[2]))
		}
	}
}

func appendCourseInfo(out *[]courseRef, seen map[string]bool, id, title string) bool {
	id, title = strings.TrimSpace(id), sanitize(title)
	if id == "" || title == "" || seen[id] {
		return false
	}
	seen[id] = true
	*out = append(*out, courseRef{ID: id, Title: title})
	return true
}

func fetchTitle(c *util.Client, h map[string]string, cid string) string {
	body, err := c.GetString(fmt.Sprintf(index_url, url.PathEscape(cid)), h)
	if err != nil {
		return ""
	}
	m := titleRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return sanitize(strings.Split(m[1], "- 华尔街学堂 -")[0])
}

func fetchInfos(c *util.Client, h map[string]string, cid string) ([]videoInfo, []fileInfo, error) {
	body, err := c.GetString(fmt.Sprintf(info_url, url.PathEscape(cid)), h)
	if err != nil {
		return nil, nil, fmt.Errorf("wallstreets task list: %w", err)
	}
	matches := taskRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		matches = taskPlainRe.FindAllStringSubmatch(html.UnescapeString(body), -1)
	}
	var videos []videoInfo
	for _, m := range matches {
		kind := strings.TrimSpace(m[3])
		if kind != "video" && kind != "audio" {
			continue
		}
		id := strings.TrimSpace(m[2])
		if id == "" {
			continue
		}
		title := decodeEscapes(m[1])
		videos = append(videos, videoInfo{ID: id, Title: fmt.Sprintf("[%d]--%s", len(videos)+1, sanitize(title)), Kind: kind})
	}

	fileBody, ferr := c.GetString(fmt.Sprintf(source_info_url, url.PathEscape(cid)), h)
	if ferr != nil {
		fileBody = ""
	}
	var files []fileInfo
	for _, m := range materialRe.FindAllStringSubmatch(fileBody, -1) {
		name := sanitize(stripTags(m[2]))
		if name == "" {
			continue
		}
		format := fileExt(name)
		if format != "" {
			name = strings.TrimSuffix(name, "."+format)
		}
		files = append(files, fileInfo{URL: "https://wallstreets.cn" + m[1], Name: fmt.Sprintf("(%d)--%s", len(files)+1, name), Format: format})
	}
	return videos, files, nil
}

func resolveVideo(c *util.Client, h map[string]string, cid string, v videoInfo, mode int, cookie string) *extractor.MediaInfo {
	token, resNo := getToken(c, h, cid, v.ID)
	if token == "" || resNo == "" {
		return nil
	}
	playURL := ""
	version := 0
	masterURL := ""
	if v.Kind == "video" {
		playURL, version, masterURL = getM3U8URL(c, h, token, resNo, mode)
	}
	if playURL == "" {
		playURL = getAudioURL(c, h, token, resNo)
	}
	if playURL == "" {
		return nil
	}
	extra := map[string]any{"video_id": v.ID, "type": v.Kind, "res_no": resNo, "m3u8_version": version, "master_playlist": masterURL}
	format := pickFormat(playURL, v.Kind)
	if format == "m3u8" {
		extra["m3u8_url"] = playURL
		result := shared.PrepareQiqiuyunM3U8(c, playURL, shared.QiqiuyunM3U8Options{
			Headers: h,
			Referer: referer,
			Cookie:  cookie,
			Version: version,
			Mode:    mode,
		})
		if result.Text != "" {
			playURL = result.URL
			extra["source_type"] = "m3u8_text"
			extra["m3u8_text"] = result.Text
			if result.SourceURL != "" {
				extra["m3u8_url"] = result.SourceURL
			}
			if result.Meta != nil {
				extra["m3u8_meta"] = result.Meta
			}
		} else {
			extra["source_type"] = "m3u8_url"
			if meta := getM3U8Meta(c, h, playURL, version); len(meta) > 0 {
				extra["m3u8_meta"] = meta
			}
		}
	}
	return streamEntry(v.Title, playURL, format, cookie, extra)
}

func getToken(c *util.Client, h map[string]string, cid, vid string) (string, string) {
	body, err := c.GetString(fmt.Sprintf(token_url, url.PathEscape(cid), url.PathEscape(vid)), h)
	if err != nil {
		return "", ""
	}
	return match1(body, tokenRe), match1(body, resNoRe)
}

func getM3U8URL(c *util.Client, h map[string]string, token, resNo string, mode int) (string, int, string) {
	body, err := c.GetString(fmt.Sprintf(video_play_url, url.QueryEscape(resNo), url.QueryEscape(token)), h)
	if err != nil {
		return "", 0, ""
	}
	playlist := unescapeURL(match1(body, playlistRe))
	version, _ := strconv.Atoi(match1(body, versionRe))
	if playlist == "" {
		return "", version, ""
	}
	master, err := c.GetString(playlist, h)
	if err != nil {
		return playlist, version, playlist
	}
	variants := parseVariants(master, playlist)
	if len(variants) == 0 {
		return playlist, version, playlist
	}
	sort.SliceStable(variants, func(i, j int) bool { return variants[i].Bandwidth > variants[j].Bandwidth })
	idx := mode - 1
	if idx < 0 || idx >= len(variants) {
		idx = len(variants) - 1
	}
	return variants[idx].URL, version, playlist
}

func parseVariants(master, base string) []variant {
	var out []variant
	for _, m := range variantRe.FindAllStringSubmatch(master, -1) {
		bw, _ := strconv.Atoi(m[1])
		out = append(out, variant{URL: absURL(strings.TrimSpace(m[2]), base), Bandwidth: bw})
	}
	if len(out) > 0 {
		return out
	}
	lines := strings.Split(master, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "#EXT-X-STREAM-INF") {
			continue
		}
		bw, _ := strconv.Atoi(match1(line, regexp.MustCompile(`BANDWIDTH=(\d+)`)))
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, "#") {
				continue
			}
			out = append(out, variant{URL: absURL(next, base), Bandwidth: bw})
			break
		}
	}
	return out
}

func getAudioURL(c *util.Client, h map[string]string, token, resNo string) string {
	body, err := c.GetString(fmt.Sprintf(video_play_url, url.QueryEscape(resNo), url.QueryEscape(token)), h)
	if err != nil {
		return ""
	}
	return unescapeURL(match1(body, playlistRe))
}

func getM3U8Meta(c *util.Client, h map[string]string, m3u8URL string, version int) map[string]any {
	if m3u8URL == "" || !strings.Contains(strings.ToLower(m3u8URL), ".m3u8") {
		return nil
	}
	body, err := c.GetString(m3u8URL, h)
	if err != nil || body == "" {
		return nil
	}
	meta := map[string]any{"version": version, "bytes": len(body)}
	if keyURI := match1(body, keyURIRe); keyURI != "" {
		keyURL := absURL(keyURI, m3u8URL)
		keyText, _ := c.GetString(keyURL, h)
		meta["key_uri"] = keyURL
		meta["key_bytes"] = len(keyText)
		meta["key_decode"] = "qiqiuyun_key_decode"
	}
	return meta
}

func fileEntry(f fileInfo, cookie string) *extractor.MediaInfo {
	format := first(f.Format, pickFormat(f.URL, "file"))
	extra := map[string]any{"type": "file", "file_fmt": format}
	return streamEntry(sanitize(f.Name), f.URL, format, cookie, extra)
}

func streamEntry(title, rawURL, format, cookie string, extra map[string]any) *extractor.MediaInfo {
	headers := map[string]string{"Referer": referer}
	if cookie != "" {
		headers["Cookie"] = cookie
	}
	quality := "best"
	if extra["type"] == "file" {
		quality = "file"
	}
	return &extractor.MediaInfo{Site: "Wallstreets", Title: sanitize(title), Streams: map[string]extractor.Stream{quality: {Quality: quality, URLs: []string{rawURL}, Format: format, NeedMerge: format == "m3u8", Headers: headers}}, Extra: extra}
}

func cookieString(j http.CookieJar) string {
	seen := map[string]bool{}
	var parts []string
	for _, host := range []string{referer, "https://wallstreets.cn/", "https://play.qiqiuyun.net/"} {
		u, err := url.Parse(host)
		if err != nil {
			continue
		}
		for _, ck := range j.Cookies(u) {
			if ck.Name == "" || seen[ck.Name] {
				continue
			}
			seen[ck.Name] = true
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func uniqueMatches(body string, re *regexp.Regexp) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 || m[1] == "" || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}

func unwrapValue(v any, keys ...string) any {
	cur := v
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func listOfMaps(v any) []map[string]any {
	switch arr := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return arr
	case map[string]any:
		for _, k := range []string{"items", "list", "courses", "data"} {
			if out := listOfMaps(arr[k]); len(out) > 0 {
				return out
			}
		}
		return nil
	default:
		return nil
	}
}

func unwrapMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func textAt(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := fmt.Sprint(v)
			if strings.TrimSpace(s) != "" && s != "<nil>" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func decodeEscapes(s string) string {
	s = html.UnescapeString(s)
	q := `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	if out, err := strconv.Unquote(q); err == nil {
		return out
	}
	return s
}

func sanitize(s string) string {
	s = stripTags(s)
	s = regexp.MustCompile(`[\\/:*?"<>|]+`).ReplaceAllString(s, "_")
	s = strings.TrimSpace(s)
	if s == "" {
		return "wallstreets"
	}
	return s
}

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(strings.ReplaceAll(s, " ", " "))
	return strings.Join(strings.Fields(s), " ")
}

func fileExt(name string) string {
	m := regexp.MustCompile(`(?i)\.([a-z0-9]+)$`).FindStringSubmatch(strings.TrimSpace(name))
	if len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

func pickFormat(raw, kind string) string {
	low := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(low, "data:application/vnd.apple.mpegurl") || strings.HasPrefix(low, "data:application/x-mpegurl") || strings.HasPrefix(low, "#extm3u") {
		return "m3u8"
	}
	if ext := fileExt(rawURLPath(raw)); ext != "" {
		return ext
	}
	if kind == "audio" {
		return "mp3"
	}
	if kind == "file" {
		return "file"
	}
	return "m3u8"
}

func rawURLPath(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}

func selectMode(q string) int {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "2", "sd", "low", "标清":
		return modeSD
	case "3", "only_pdf", "pdf":
		return modeOnlyPDF
	default:
		return modeHD
	}
}

func unescapeURL(s string) string {
	s = html.UnescapeString(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\u0026`, "&")
	s = strings.ReplaceAll(s, `\u003d`, "=")
	return s
}

func absURL(raw, base string) string {
	s := unescapeURL(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	bu, err := url.Parse(base)
	if err != nil {
		return s
	}
	return bu.ResolveReference(u).String()
}

func match1(s string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
