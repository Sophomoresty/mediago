package xuetang

import (
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type courseLeaf struct {
	ID           string
	Name         string
	Type         string
	ChapterIndex int
	SectionIndex int
	LeafIndex    int
}

type leafSource struct {
	URL       string
	Title     string
	Size      int64
	Variants  []xuetangVariant
	Files     []xuetangFile
	Subtitles []extractor.Subtitle
	HTML      string
}

type xuetangVariant struct {
	URL       string
	Quality   string
	Format    string
	Size      int64
	NeedMerge bool
}

type xuetangFile struct {
	URL    string
	Title  string
	Format string
	Size   int64
}

func (s *leafSource) empty() bool {
	if s == nil {
		return true
	}
	return s.URL == "" && len(s.Variants) == 0 && len(s.Files) == 0 && s.HTML == ""
}

func xuetangOrigin(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.Split(host, "/")[0]
	switch {
	case strings.HasSuffix(host, "gradsmartedu.cn"):
		return "https://www.gradsmartedu.cn"
	case strings.HasSuffix(host, "cmgemooc.com"):
		return "https://www.xuetangx.com"
	default:
		return "https://www.xuetangx.com"
	}
}

func xuetangHeaders(jar http.CookieJar, rawURL, base string) map[string]string {
	h := map[string]string{
		"Accept":  "application/json, text/plain, */*",
		"Referer": firstNonEmpty(rawURL, base+"/"),
		"Origin":  base,
		"xtbz":    "xt",
	}
	if cookie := xuetangCookieHeader(jar, rawURL, base); cookie != "" {
		h["Cookie"] = cookie
		h["cookie"] = cookie
	}
	return h
}

func xuetangCookieHeader(jar http.CookieJar, rawURL, base string) string {
	if jar == nil {
		return ""
	}
	origins := []string{rawURL, base + "/", "https://www.xuetangx.com/", "https://next.xuetangx.com/", "https://degreecourse.xuetangx.com/", "https://www.gradsmartedu.cn/"}
	seen := map[string]bool{}
	var parts []string
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			continue
		}
		for _, c := range jar.Cookies(u) {
			if c.Name == "" || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func fetchCourseChapterPayload(c *util.Client, base string, h map[string]string, sign, cid string) (map[string]any, error) {
	chapterURL := fmt.Sprintf("%s/api/v1/lms/learn/course/chapter?cid=%s&sign=%s", base, url.QueryEscape(cid), url.QueryEscape(sign))
	root, err := getJSONMap(c, chapterURL, h)
	if err == nil && len(extractCourseLeaves(root)) > 0 {
		return root, nil
	}

	// Python retries the chapter API once after joining a free SKU. The first
	// chapter request can fail with 4xx before a free course is enrolled, so keep
	// this before the public course-detail fallback to preserve the source order.
	if joinFreeCourse(c, base, h, sign, cid) {
		if joinedRoot, joinedErr := getJSONMap(c, chapterURL, h); joinedErr == nil && len(extractCourseLeaves(joinedRoot)) > 0 {
			return joinedRoot, nil
		}
	}

	detailURL := fmt.Sprintf("%s/api/v1/lms/product/get_course_detail/?cid=%s", base, url.QueryEscape(cid))
	fallback, fallbackErr := getJSONMap(c, detailURL, h)
	if fallbackErr == nil && len(extractCourseLeaves(fallback)) > 0 {
		return fallback, nil
	}
	if err != nil {
		return nil, fmt.Errorf("course/chapter: %w", err)
	}
	return root, nil
}

func joinFreeCourse(c *util.Client, base string, h map[string]string, sign, cid string) bool {
	productID, skuID, err := getFreeProductSKU(c, base, h, sign, cid)
	if err != nil || productID == "" || skuID == "" {
		return false
	}
	joinURL := fmt.Sprintf("%s/api/v1/lms/order/entries_free_sku/%s/?sid=%s", base, url.PathEscape(productID), url.QueryEscape(skuID))
	root, err := getJSONMap(c, joinURL, h)
	if err != nil {
		return false
	}
	if success, ok := root["success"].(bool); ok {
		return success
	}
	if success, ok := anyMap(root["data"])["success"].(bool); ok {
		return success
	}
	code := strings.TrimSpace(jsonScalarString(root["code"]))
	return code == "0" || code == "200"
}

func getFreeProductSKU(c *util.Client, base string, h map[string]string, sign, cid string) (string, string, error) {
	productURL := fmt.Sprintf("%s/api/v1/lms/product/sku_pay_detail/?cid=%s&sign=%s", base, url.QueryEscape(cid), url.QueryEscape(sign))
	root, err := getJSONMap(c, productURL, h)
	if err != nil {
		return "", "", err
	}
	data := firstMap(root["data"], root)
	productID := jsonScalarString(firstPresent(data, "product_id", "productId"))
	for _, raw := range firstList(data["sku_info"], data["skuInfo"], data["skus"], data["sku_list"], data["skuList"]) {
		sku := anyMap(raw)
		if len(sku) == 0 || !isFreeSKU(sku) {
			continue
		}
		skuID := jsonScalarString(firstPresent(sku, "sku_id", "skuId", "id"))
		if skuID != "" {
			return productID, skuID, nil
		}
	}
	return productID, "", nil
}

func isFreeSKU(sku map[string]any) bool {
	for _, key := range []string{"current_price", "currentPrice", "price", "sale_price", "salePrice"} {
		if v, ok := sku[key]; ok && parsePriceNumber(v) == 0 {
			return true
		}
	}
	return false
}

func parsePriceNumber(v any) float64 {
	s := strings.TrimSpace(jsonScalarString(v))
	if s == "" {
		return -1
	}
	s = strings.TrimPrefix(strings.ReplaceAll(s, ",", ""), "¥")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return -1
	}
	return f
}

func getJSONMap(c *util.Client, apiURL string, h map[string]string) (map[string]any, error) {
	body, err := c.GetString(apiURL, h)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", apiURL, err)
	}
	return root, nil
}

func extractCourseLeaves(root map[string]any) []courseLeaf {
	data := anyMap(root["data"])
	content := firstList(data["content_data"], data["course_chapter"], data["chapter"], root["content_data"], root["course_chapter"])
	var out []courseLeaf
	for ci, chapterAny := range content {
		chapter := anyMap(chapterAny)
		chapterName := firstAnyString(chapter, "name", "title", "chapter_name")
		sections := firstList(chapter["section_leaf_list"], chapter["section_list"], chapter["children"], chapter["leaf_list"])
		if len(sections) == 0 {
			appendLeafIfPlayable(&out, chapter, chapterName, ci+1, 1, 1)
			continue
		}
		for si, sectionAny := range sections {
			section := anyMap(sectionAny)
			sectionName := firstAnyString(section, "name", "title", "section_name")
			leaves := firstList(section["leaf_list"], section["leaves"], section["children"])
			if len(leaves) == 0 {
				appendLeafIfPlayable(&out, section, firstNonEmpty(sectionName, chapterName), ci+1, si+1, 1)
				continue
			}
			for li, leafAny := range leaves {
				leaf := anyMap(leafAny)
				appendLeafIfPlayable(&out, leaf, firstNonEmpty(firstAnyString(leaf, "name", "title"), sectionName, chapterName), ci+1, si+1, li+1)
			}
		}
	}
	return out
}

func appendLeafIfPlayable(out *[]courseLeaf, node map[string]any, fallbackName string, chapterIndex, sectionIndex, leafIndex int) {
	if len(node) == 0 {
		return
	}
	id := firstAnyString(node, "id", "leaf_id", "leafId", "source_id", "sourceId")
	if id == "" {
		return
	}
	leafType := jsonScalarString(firstPresent(node, "leaf_type", "leafType", "type"))
	if leafType != "" && leafType != "0" && leafType != "2" && leafType != "3" && !looksPlayableNode(node) {
		return
	}
	*out = append(*out, courseLeaf{
		ID:           id,
		Name:         firstNonEmpty(firstAnyString(node, "name", "title"), fallbackName),
		Type:         leafType,
		ChapterIndex: chapterIndex,
		SectionIndex: sectionIndex,
		LeafIndex:    leafIndex,
	})
}

func looksPlayableNode(node map[string]any) bool {
	if _, ok := firstMediaURL(node, true); ok {
		return true
	}
	for _, k := range []string{"ccid", "content_info", "media", "leaf_list"} {
		if _, ok := node[k]; ok {
			return true
		}
	}
	return false
}

func resolveLeafSource(c *util.Client, base string, h map[string]string, sign, cid, leafID string, onlyFiles bool) *leafSource {
	apiURL := fmt.Sprintf("%s/api/v1/lms/learn/leaf_info/%s/%s/?sign=%s", base, url.PathEscape(cid), url.PathEscape(leafID), url.QueryEscape(sign))
	root, err := getJSONMap(c, apiURL, h)
	if err != nil {
		return nil
	}
	src := sourceFromLeafPayload(root)
	ccid := firstNonEmpty(findFirstKey(root, "ccid", "cc_id"), findFirstKey(root, "video_id", "videoId"))
	if ccid != "" && !onlyFiles {
		if variants := fetchPlayURLVariants(c, base, h, ccid); len(variants) > 0 {
			src.Variants = append(src.Variants, variants...)
		}
	}
	src.normalize()
	return src
}

func sourceFromLeafPayload(root map[string]any) *leafSource {
	src := &leafSource{}
	data := anyMap(root["data"])
	leafData := firstMap(data["leaf_data"], data["leaf"], data["data"], root["leaf_data"])
	src.Title = firstNonEmpty(firstAnyString(leafData, "name", "title"), firstAnyString(data, "name", "title"))

	content := firstMap(leafData["content_info"], leafData["contentInfo"], data["content_info"], data["contentInfo"], root["content_info"])
	media := firstMap(content["media"], data["media"], root["media"])
	if src.Title == "" {
		src.Title = firstAnyString(media, "name", "title")
	}
	if htmlText := findHTMLText(root); htmlText != "" {
		src.HTML = normalizeHTML(htmlText)
	}
	if mediaURL, ok := firstMediaURL(root, false); ok {
		src.Variants = append(src.Variants, xuetangVariant{URL: mediaURL, Quality: "source", Format: pickFormat(mediaURL), NeedMerge: pickFormat(mediaURL) == "m3u8"})
	}
	src.Files = append(src.Files, extractFiles(root)...)
	src.Subtitles = append(src.Subtitles, extractSubtitles(root)...)
	return src
}

func fetchPlayURLVariants(c *util.Client, base string, h map[string]string, ccid string) []xuetangVariant {
	playURL := fmt.Sprintf("%s/api/v1/lms/service/playurl/%s/?appid=10000", base, url.PathEscape(ccid))
	root, err := getJSONMap(c, playURL, h)
	if err != nil {
		return nil
	}
	data := anyMap(root["data"])
	sources := firstMap(data["sources"], root["sources"])
	var variants []xuetangVariant
	if len(sources) > 0 {
		keys := make([]string, 0, len(sources))
		for k := range sources {
			keys = append(keys, k)
		}
		sort.SliceStable(keys, func(i, j int) bool { return qualityRank(keys[i]) > qualityRank(keys[j]) })
		for _, key := range keys {
			for _, u := range mediaURLsFromAny(sources[key], false) {
				variants = append(variants, xuetangVariant{URL: u, Quality: key, Format: pickFormat(u), NeedMerge: pickFormat(u) == "m3u8"})
			}
		}
	}
	for _, u := range mediaURLsFromAny(data["video"], false) {
		variants = append(variants, xuetangVariant{URL: u, Quality: "video", Format: pickFormat(u), NeedMerge: pickFormat(u) == "m3u8"})
	}
	if len(variants) == 0 {
		for _, u := range mediaURLsFromAny(root, false) {
			variants = append(variants, xuetangVariant{URL: u, Quality: "source", Format: pickFormat(u), NeedMerge: pickFormat(u) == "m3u8"})
		}
	}
	return dedupeVariants(variants)
}

func mediaFromSource(base string, h map[string]string, title, leafID string, src *leafSource, onlyFiles bool) []*extractor.MediaInfo {
	if src == nil || src.empty() {
		return nil
	}
	headers := map[string]string{"Referer": base + "/"}
	if h["Cookie"] != "" {
		headers["Cookie"] = h["Cookie"]
	}
	var out []*extractor.MediaInfo
	streams := map[string]extractor.Stream{}
	if !onlyFiles {
		for _, v := range src.Variants {
			if v.URL == "" {
				continue
			}
			key := firstNonEmpty(v.Quality, v.Format, "default")
			for i := 2; streams[key].URLs != nil; i++ {
				key = fmt.Sprintf("%s_%d", firstNonEmpty(v.Quality, v.Format, "default"), i)
			}
			streams[key] = extractor.Stream{Quality: key, URLs: []string{v.URL}, Format: firstNonEmpty(v.Format, pickFormat(v.URL)), Size: v.Size, NeedMerge: v.NeedMerge || pickFormat(v.URL) == "m3u8", Headers: headers}
		}
		if len(streams) == 0 && src.URL != "" {
			format := pickFormat(src.URL)
			streams["default"] = extractor.Stream{Quality: "best", URLs: []string{src.URL}, Format: format, Size: src.Size, NeedMerge: format == "m3u8", Headers: headers}
		}
	}
	if len(streams) == 0 && src.HTML != "" {
		u := "data:text/html;charset=utf-8," + url.PathEscape(src.HTML)
		streams["document"] = extractor.Stream{Quality: "html", URLs: []string{u}, Format: "html", Headers: headers}
	}
	if len(streams) > 0 {
		out = append(out, &extractor.MediaInfo{Site: "xuetang", Title: util.SanitizeFilename(title), Streams: streams, Subtitles: src.Subtitles, Extra: map[string]any{"leaf_id": leafID}})
	}
	for i, f := range src.Files {
		if f.URL == "" {
			continue
		}
		format := firstNonEmpty(f.Format, pickFormat(f.URL))
		fileTitle := util.SanitizeFilename(firstNonEmpty(f.Title, fmt.Sprintf("%s_资料_%d", title, i+1)))
		out = append(out, &extractor.MediaInfo{
			Site:  "xuetang",
			Title: fileTitle,
			Streams: map[string]extractor.Stream{
				"file": {Quality: "file", URLs: []string{f.URL}, Format: format, Size: f.Size, Headers: headers},
			},
			Extra: map[string]any{"leaf_id": leafID, "kind": "file"},
		})
	}
	return out
}

func (s *leafSource) normalize() {
	if s == nil {
		return
	}
	s.Variants = dedupeVariants(s.Variants)
	if len(s.Variants) > 0 {
		s.URL = s.Variants[0].URL
		s.Size = s.Variants[0].Size
	}
	s.Files = dedupeFiles(s.Files)
}

func dedupeVariants(in []xuetangVariant) []xuetangVariant {
	seen := map[string]bool{}
	out := make([]xuetangVariant, 0, len(in))
	for _, v := range in {
		v.URL = normalizeMediaURL(v.URL)
		if v.URL == "" || seen[v.URL] || !isDirectMediaURL(v.URL) {
			continue
		}
		seen[v.URL] = true
		if v.Format == "" {
			v.Format = pickFormat(v.URL)
		}
		if v.Quality == "" {
			v.Quality = "source"
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return qualityRank(out[i].Quality) > qualityRank(out[j].Quality) })
	return out
}

func dedupeFiles(in []xuetangFile) []xuetangFile {
	seen := map[string]bool{}
	out := make([]xuetangFile, 0, len(in))
	for _, f := range in {
		f.URL = normalizeMediaURL(f.URL)
		if f.URL == "" || seen[f.URL] || isDirectMediaURL(f.URL) {
			continue
		}
		seen[f.URL] = true
		if f.Format == "" {
			f.Format = pickFormat(f.URL)
		}
		out = append(out, f)
	}
	return out
}

func extractFiles(v any) []xuetangFile {
	var out []xuetangFile
	for _, m := range walkMaps(v, 0) {
		title := firstAnyString(m, "name", "title", "file_name", "fileName", "filename", "resourceName")
		for _, key := range []string{"download_url", "downloadUrl", "file_url", "fileUrl", "url", "src", "pdf_url", "ppt_url", "doc_url", "attachment_url"} {
			u := normalizeMediaURL(jsonScalarString(m[key]))
			if u == "" || isDirectMediaURL(u) || !looksFileURL(u) {
				continue
			}
			out = append(out, xuetangFile{URL: u, Title: title, Format: pickFormat(u), Size: parseInt64(firstPresent(m, "size", "file_size", "fileSize"))})
		}
	}
	return out
}

func extractSubtitles(v any) []extractor.Subtitle {
	var out []extractor.Subtitle
	for _, m := range walkMaps(v, 0) {
		for _, key := range []string{"subtitle", "subtitle_url", "subtitleUrl", "caption", "caption_url", "captionUrl", "srt", "vtt"} {
			u := normalizeMediaURL(jsonScalarString(m[key]))
			if u == "" || !(strings.Contains(strings.ToLower(u), ".srt") || strings.Contains(strings.ToLower(u), ".vtt")) {
				continue
			}
			out = append(out, extractor.Subtitle{Language: firstNonEmpty(firstAnyString(m, "language", "lang"), "und"), URL: u, Format: fileExt(u)})
		}
	}
	return out
}

func firstMediaURL(v any, includeFiles bool) (string, bool) {
	for _, u := range mediaURLsFromAny(v, includeFiles) {
		return u, true
	}
	return "", false
}

func mediaURLsFromAny(v any, includeFiles bool) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(any, string, int)
	walk = func(value any, key string, depth int) {
		if depth > 8 {
			return
		}
		switch x := value.(type) {
		case map[string]any:
			for k, child := range x {
				walk(child, strings.ToLower(k), depth+1)
			}
		case []any:
			for _, child := range x {
				walk(child, key, depth+1)
			}
		case string:
			for _, u := range extractURLs(x) {
				if !includeFiles && !isDirectMediaURL(u) {
					continue
				}
				if includeFiles && !isDirectMediaURL(u) && !looksFileURL(u) {
					continue
				}
				if !seen[u] {
					seen[u] = true
					out = append(out, u)
				}
			}
		default:
			s := jsonScalarString(x)
			if strings.HasPrefix(s, "http") {
				walk(s, key, depth+1)
			}
		}
	}
	walk(v, "", 0)
	return out
}

var urlRe = regexp.MustCompile(`(?i)(?:https?:)?//[^\s<>"'\\]+?\.(?:m3u8|mp4|m4v|mov|flv|mp3|m4a|aac|wav|pdf|docx?|pptx?|xlsx?|zip|rar|7z|txt|csv|srt|vtt)(?:[^\s<>"'\\]*)?`)

func extractURLs(s string) []string {
	var out []string
	add := func(raw string) {
		u := normalizeMediaURL(raw)
		if u != "" {
			out = append(out, u)
		}
	}
	add(s)
	for _, m := range urlRe.FindAllString(s, -1) {
		add(m)
	}
	return out
}

func normalizeMediaURL(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, `\/`, "/")
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	if m := urlRe.FindString(s); m != "" {
		s = m
	}
	return strings.TrimRight(s, `"',;)]}`)
}

func looksFileURL(u string) bool {
	switch pickFormat(u) {
	case "pdf", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "zip", "rar", "7z", "txt", "csv":
		return true
	default:
		return false
	}
}

func findHTMLText(v any) string {
	for _, m := range walkMaps(v, 0) {
		for _, k := range []string{"text", "html", "content", "content_text", "contentText", "body", "description"} {
			s := strings.TrimSpace(jsonScalarString(m[k]))
			if s == "" {
				continue
			}
			lower := strings.ToLower(s)
			if strings.Contains(lower, "<p") || strings.Contains(lower, "<div") || strings.Contains(lower, "<img") || strings.Contains(lower, "<br") || strings.Contains(lower, "&nbsp;") {
				return s
			}
		}
	}
	return ""
}

func normalizeHTML(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<body") {
		return s
	}
	return "<html><body>" + s + "</body></html>"
}

func firstMap(values ...any) map[string]any {
	for _, v := range values {
		if m := anyMap(v); len(m) > 0 {
			return m
		}
	}
	return map[string]any{}
}

func firstList(values ...any) []any {
	for _, v := range values {
		switch t := v.(type) {
		case []any:
			if len(t) > 0 {
				return t
			}
		case []map[string]any:
			if len(t) > 0 {
				out := make([]any, 0, len(t))
				for _, m := range t {
					out = append(out, m)
				}
				return out
			}
		}
	}
	return nil
}

func anyMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func walkMaps(v any, depth int) []map[string]any {
	if depth > 8 {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		out := []map[string]any{t}
		for _, child := range t {
			out = append(out, walkMaps(child, depth+1)...)
		}
		return out
	case []any:
		var out []map[string]any
		for _, child := range t {
			out = append(out, walkMaps(child, depth+1)...)
		}
		return out
	default:
		return nil
	}
}

func firstAnyString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := jsonScalarString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func findFirstKey(v any, keys ...string) string {
	want := map[string]bool{}
	for _, k := range keys {
		want[strings.ToLower(k)] = true
	}
	for _, m := range walkMaps(v, 0) {
		for k, val := range m {
			if want[strings.ToLower(k)] {
				if s := jsonScalarString(val); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func firstPresent(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseInt64(v any) int64 {
	s := jsonScalarString(v)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int64(f)
}

func qualityRank(q string) int {
	m := regexp.MustCompile(`\d+`).FindString(q)
	if m == "" {
		return 0
	}
	n, _ := strconv.Atoi(m)
	return n
}

func fileExt(u string) string {
	parsed, err := url.Parse(u)
	rawPath := u
	if err == nil {
		rawPath = parsed.Path
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(rawPath)), ".")
	if ext != "" {
		return ext
	}
	if mt := strings.TrimSpace(mime.TypeByExtension(path.Ext(rawPath))); mt != "" {
		parts := strings.SplitN(mt, "/", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1])
		}
	}
	return "bin"
}
