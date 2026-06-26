// Package cnmooc implements an extractor for cnmooc.sjtu.cn courses.
package cnmooc

import (
	"encoding/json"
	"fmt"
	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	origin      = "https://cnmooc.sjtu.cn"
	referer     = origin + "/"
	login_url   = origin + "/home/login.mooc"
	user_agent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	item_detail = "/item/detail.mooc"
)

var patterns = []string{`(?:[\w-]+\.)?cnmooc\.sjtu\.cn/`}

func init() {
	extractor.Register(&Cnmooc{}, extractor.SiteInfo{Name: "Cnmooc", URL: "cnmooc.sjtu.cn", NeedAuth: true})
}

type Cnmooc struct{}

func (c *Cnmooc) Patterns() []string { return patterns }
func (c *Cnmooc) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("cnmooc requires login cookies")
	}
	courseID, openID := parseIDs(rawURL)
	if openID == "" {
		return nil, fmt.Errorf("cannot parse cnmooc courseOpenId from URL")
	}
	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)
	detailURL := coursePage(courseID, openID)
	body, err := client.GetString(abs(detailURL), requestHeaders(referer, false))
	if err != nil {
		return nil, fmt.Errorf("cnmooc course page: %w", err)
	}
	if courseID == "" {
		courseID = hiddenValue(body, "courseId")
	}
	if openID == "" {
		openID = hiddenValue(body, "courseOpenId")
	}
	title := pageTitle(body)
	if title == "" {
		title = "CNMOOC课程" + first(courseID, openID)
	}
	pages := coursePages(courseID, openID)
	if len(pages) == 0 {
		pages = []string{detailURL}
	}
	seen := map[string]bool{}
	var entries []*extractor.MediaInfo
	for pi, page := range pages {
		text := body
		if page != detailURL {
			if t, e := client.GetString(abs(page), requestHeaders(referer, false)); e == nil {
				text = t
			} else {
				continue
			}
		}
		for ii, item := range extractPlayerItems(text) {
			mi := resolveItem(client, opts.Cookies, item, openID, fmt.Sprintf("%02d.%02d", pi+1, ii+1))
			if mi != nil && !seenKey(seen, mi.Streams["best"].URLs[0]) {
				entries = append(entries, mi)
			}
		}
		for li, link := range extractLinks(text) {
			if !isVideoURL(link.URL) {
				continue
			}
			mi := media(fmt.Sprintf("%02d.%02d %s", pi+1, li+1, sanitize(link.Title)), link.URL, nil, nil)
			if !seenKey(seen, link.URL) {
				entries = append(entries, mi)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("cnmooc: no playable video URLs found in pages or item/detail.mooc")
	}
	return &extractor.MediaInfo{Site: "cnmooc", Title: sanitize(title), Entries: entries, Extra: map[string]any{"course_id": courseID, "open_id": openID}}, nil
}

type itemInfo struct{ NodeID, ItemID, ItemType, Title, VideoURL string }
type linkInfo struct{ URL, Title string }

func resolveItem(c *util.Client, jar http.CookieJar, it itemInfo, openID, fallback string) *extractor.MediaInfo {
	cands := []string{it.VideoURL}
	var detail map[string]any
	if it.NodeID != "" && it.ItemID != "" {
		detail = requestItemDetail(c, jar, it.NodeID, it.ItemID, openID)
		cands = append(cands, mediaURLCandidates(detail, it)...)
	}
	for _, u := range cands {
		u = normalizeURL(u, origin)
		if !isVideoURL(u) {
			continue
		}
		title := first(it.Title, sourceTitle(detail), "CNMOOC视频"+fallback)
		extra := map[string]any{"node_id": it.NodeID, "item_id": it.ItemID, "item_type": it.ItemType}
		return media(sanitize(title), u, subtitleInfos(detail), extra)
	}
	return nil
}
func requestItemDetail(c *util.Client, jar http.CookieJar, nodeID, itemID, openID string) map[string]any {
	form := map[string]string{"nodeId": nodeID, "itemId": itemID}
	if tok := postoken(jar); tok != "" {
		form["postoken"] = tok
	}
	body, err := c.PostForm(abs(item_detail), form, requestHeaders(origin+"/portal/session/index/"+openID+".mooc", true))
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal([]byte(body), &out) != nil {
		return map[string]any{}
	}
	return out
}
func mediaURLCandidates(detail map[string]any, it itemInfo) []string {
	var out []string
	if it.VideoURL != "" {
		out = append(out, it.VideoURL)
	}
	if node, ok := detail["node"].(map[string]any); ok {
		out = append(out, valuesFor(node, "flvUrl", "flv_url", "url", "rsUrl", "mediaUrl", "fileUrl", "downloadUrl")...)
	}
	if mr, ok := detail["mediaResources"].(map[string]any); ok {
		out = append(out, valuesFor(mr, "currentUrl", "url", "videoUrl", "mediaUrl", "fileUrl", "downloadUrl")...)
		if list, ok := mr["mediaUrls"].([]any); ok {
			for _, v := range list {
				out = append(out, fmt.Sprint(v))
			}
		}
	}
	out = append(out, valuesFor(detail, "flvUrl", "flv_url", "url", "rsUrl", "videoUrl", "mediaUrl", "fileUrl", "downloadUrl")...)
	return splitDefinitions(out)
}
func extractPlayerItems(text string) []itemInfo {
	var out []itemInfo
	for _, obj := range objectRe.FindAllString(text, -1) {
		raw := parseJSObject(obj)
		nodeID, itemID := firstText(raw, "nodeId", "node_id"), firstText(raw, "itemId", "item_id")
		if nodeID == "" || itemID == "" {
			continue
		}
		out = append(out, itemInfo{NodeID: nodeID, ItemID: itemID, ItemType: firstText(raw, "itemType", "type"), Title: firstText(raw, "title", "name"), VideoURL: firstText(raw, "flvUrl", "rsUrl", "url", "video_url", "videoUrl", "mediaUrl")})
	}
	return out
}
func extractLinks(text string) []linkInfo {
	var out []linkInfo
	for _, m := range attrURLRe.FindAllStringSubmatch(text, -1) {
		out = append(out, linkInfo{URL: normalizeURL(html.UnescapeString(m[1]), origin), Title: cleanText(m[0])})
	}
	for _, m := range directURLRe.FindAllStringSubmatch(text, -1) {
		out = append(out, linkInfo{URL: normalizeURL(html.UnescapeString(m[1]), origin), Title: "CNMOOC视频"})
	}
	return out
}
func requestHeaders(ref string, ajax bool) map[string]string {
	if ref == "" {
		ref = referer
	}
	h := map[string]string{"User-Agent": user_agent, "Referer": ref, "Origin": origin, "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}
	if ajax {
		h["X-Requested-With"] = "XMLHttpRequest"
		h["Accept"] = "application/json, text/javascript, */*; q=0.01"
	}
	return h
}
func postoken(j http.CookieJar) string {
	if j == nil {
		return ""
	}
	u, _ := url.Parse(origin + "/")
	for _, ck := range j.Cookies(u) {
		if ck.Name == "cpstk" {
			if v, err := url.QueryUnescape(strings.TrimSpace(ck.Value)); err == nil {
				return v
			}
			return strings.TrimSpace(ck.Value)
		}
	}
	return ""
}
func coursePages(courseID, openID string) []string {
	if openID == "" {
		return nil
	}
	pages := []string{"/portal/session/index/" + openID + ".mooc", "/portal/session/unitNavigation/index/" + openID + ".mooc", "/portal/session/courseIntro/index/" + openID + ".mooc", "/portalPreview/session/unitNavigation/preview/" + openID + ".mooc", "/portalPreview/session/bulletin/preview/" + openID + ".mooc"}
	if courseID != "" {
		pages = append([]string{"/portal/course/" + courseID + "/" + openID + ".mooc"}, pages...)
	}
	return pages
}
func coursePage(courseID, openID string) string {
	if courseID != "" {
		return "/portal/course/" + courseID + "/" + openID + ".mooc"
	}
	return "/portal/session/index/" + openID + ".mooc"
}

var (
	courseRe    = regexp.MustCompile(`cnmooc\.sjtu\.cn/portal/course/(\d+)/(\d+)\.mooc`)
	sessionRe   = regexp.MustCompile(`cnmooc\.sjtu\.cn/portal(?:Preview)?/session/[^/\s]+/(?:index/|preview/)?(\d+)\.mooc`)
	previewRe   = regexp.MustCompile(`cnmooc\.sjtu\.cn/(?:course/preview|portal/payCourse-)(\d+)\.mooc`)
	inputReT    = regexp.MustCompile(`(?is)<input[^>]+(?:name|id)=["']%s["'][^>]*value=["']([^"']*)["']`)
	inputReV    = regexp.MustCompile(`(?is)<input[^>]+value=["']([^"']*)["'][^>]+(?:name|id)=["']%s["']`)
	objectRe    = regexp.MustCompile(`(?s)\{[^{}]*(?:nodeId|node_id)[^{}]*(?:itemId|item_id)[^{}]*\}`)
	kvRe        = regexp.MustCompile(`(?is)["']?([A-Za-z_][\w-]*)["']?\s*:\s*("(?:\\.|[^"])*"|'(?:\\.|[^'])*'|[^,}\s]+)`)
	attrURLRe   = regexp.MustCompile(`(?is)(?:data-url|src|href|rsurl|rs-url)=["']([^"']+)["']`)
	directURLRe = regexp.MustCompile(`(?is)(https?://[^"'\s<>]+\.(?:m3u8|mp4|flv|mp3)[^"'\s<>]*)`)
)

func parseIDs(raw string) (courseID, openID string) {
	if m := courseRe.FindStringSubmatch(raw); len(m) > 2 {
		return m[1], m[2]
	}
	if m := sessionRe.FindStringSubmatch(raw); len(m) > 1 {
		return "", m[1]
	}
	if m := previewRe.FindStringSubmatch(raw); len(m) > 1 {
		return "", m[1]
	}
	return "", ""
}
func hiddenValue(text, name string) string {
	for _, tpl := range []*regexp.Regexp{regexp.MustCompile(fmt.Sprintf(inputReT.String(), regexp.QuoteMeta(name))), regexp.MustCompile(fmt.Sprintf(inputReV.String(), regexp.QuoteMeta(name)))} {
		if m := tpl.FindStringSubmatch(text); len(m) > 1 {
			return strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	return ""
}
func pageTitle(text string) string {
	for _, pat := range []string{`(?is)<h1[^>]*>(.*?)</h1>`, `(?is)<h2[^>]*>(.*?)</h2>`, `(?is)<title[^>]*>(.*?)</title>`} {
		if m := regexp.MustCompile(pat).FindStringSubmatch(text); len(m) > 1 {
			if s := cleanText(m[1]); s != "" {
				return strings.TrimSpace(regexp.MustCompile(`(?i)[_-].*?CNMOOC.*$|好大学在线.*$`).ReplaceAllString(s, ""))
			}
		}
	}
	return ""
}
func parseJSObject(obj string) map[string]any {
	out := map[string]any{}
	for _, m := range kvRe.FindAllStringSubmatch(obj, -1) {
		out[m[1]] = unquoteJS(m[2])
	}
	return out
}
func unquoteJS(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\'`, `'`)
	return html.UnescapeString(s)
}
func valuesFor(m map[string]any, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "<nil>" {
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}
func splitDefinitions(in []string) []string {
	var out []string
	for _, s := range in {
		for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == '*' || r == '|' || r == ',' }) {
			if strings.TrimSpace(p) != "" {
				out = append(out, strings.TrimSpace(p))
			}
		}
	}
	return dedup(out)
}
func sourceTitle(detail map[string]any) string {
	if node, ok := detail["node"].(map[string]any); ok {
		return firstText(node, "title", "name")
	}
	return firstText(detail, "title", "name")
}
func subtitleInfos(detail map[string]any) []extractor.Subtitle {
	var out []extractor.Subtitle
	for _, u := range valuesFor(detail, "srtPath", "vttPath", "subtitleUrl", "rsUrl", "url", "fileUrl") {
		if isSubtitle(u) {
			out = append(out, extractor.Subtitle{Language: "字幕", URL: normalizeURL(u, origin), Format: ext(u)})
		}
	}
	return out
}
func media(title, u string, subs []extractor.Subtitle, extra map[string]any) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "cnmooc", Title: title, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: pickFormat(u), Headers: requestHeaders(referer, false)}}, Subtitles: subs, Extra: extra}
}
func abs(p string) string { return normalizeURL(p, origin) }
func normalizeURL(s, base string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, `\/`, `/`))
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err == nil && u.IsAbs() {
		return s
	}
	b, errB := url.Parse(base)
	r, errR := url.Parse(s)
	if errB != nil || errR != nil || b == nil || r == nil {
		return s
	}
	return b.ResolveReference(r).String()
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func firstText(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
func seenKey(seen map[string]bool, key string) bool {
	if seen[key] {
		return true
	}
	seen[key] = true
	return false
}
func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
func cleanText(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(html.UnescapeString(regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(s, " ")), " "))
}

var badName = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func sanitize(s string) string {
	s = badName.ReplaceAllString(strings.TrimSpace(s), "_")
	if s == "" {
		return "未命名视频"
	}
	return s
}
func ext(u string) string {
	p := strings.ToLower(strings.Split(strings.Split(u, "?")[0], "#")[0])
	if i := strings.LastIndex(p, "."); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return ""
}
func isVideoURL(u string) bool {
	e := ext(u)
	return e == "m3u8" || e == "mp4" || e == "flv" || e == "mp3"
}
func isSubtitle(u string) bool { e := ext(u); return e == "srt" || e == "vtt" }
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return ext(u)
}
