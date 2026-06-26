// Package gaodun implements source-aligned Gaodun course and glive2-vod extraction.
package gaodun

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	course_url              = "https://apigateway.gaodun.com/ep-course/api/v2/front/space/vcourse/pc"
	info_url                = "https://apigateway.gaodun.com/ep-study/front/course/%s/syllabus"
	info_gradation_url      = "https://apigateway.gaodun.com/g-study/api/v1/front/gl/course/gradation/%s"
	info_glive_url          = "https://apigateway.gaodun.com/g-study/api/v1/front/course/%s/syllabus/glive/%s"
	info_syllabus_url       = "https://apigateway.gaodun.com/ep-study/front/course/%s/syllabus/%s"
	video_play_url          = "https://apigateway.gaodun.com/glive2-vod/api/v1/live/resource?code=%s&res=%s"
	live_token_url          = "https://apigateway.gaodun.com/glive2-vod/api/v1/vod/check?token=%s"
	live_play_url           = "https://apigateway.gaodun.com/glive2-vod/api/v1/live/resource?code=%s&res=%s"
	live_old_url            = "https://apigateway.gaodun.com/glive2-cloud-gateway/api/v1/live/record/info/%s"
	source_category_url     = "https://apigateway.gaodun.com/ep-course/api/v1/course/%s/handout/category"
	source_gradation_url    = "https://apigateway.gaodun.com/ep-course/api/v1/course/%s/gradation/handout"
	file_url                = "https://apigateway.gaodun.com/hermes/front/v1/download/resource?resource_id=%s&filename=%s"
	price_url               = "https://apigateway.gaodun.com/goodscenter/api/v3/vcourse/detailByIds?ids=%s"
	pc_token_url            = "https://apigateway.gaodun.com/glive2-cloud-gateway/api/v1/live/getPc"
	pe_token_url            = "https://apigateway.gaodun.com/glive2-cloud-gateway/api/v1/live/getPe"
	passport_glive_user_url = "https://apigateway.gaodun.com/passport/api/v3/get/glive-user-info"
)

var patterns = []string{`(?:[\w-]+\.)?gaodun\.com/|apigateway\.gaodun\.com/`}

func init() {
	extractor.Register(&Gaodun{}, extractor.SiteInfo{Name: "Gaodun", URL: "gaodun.com", NeedAuth: true})
}

type Gaodun struct{}

func (g *Gaodun) Patterns() []string { return patterns }

var (
	cidRe        = regexp.MustCompile(`(?i)(?:courseId|course_id|cid|ids?)=([A-Za-z0-9_-]+)|/(?:course|vcourse)/(\w+)`)
	videoIDRe    = regexp.MustCompile(`(?i)(?:videoId|video_id|vid|code|did)=([A-Za-z0-9_-]+)`)
	recordIDRe   = regexp.MustCompile(`(?i)(?:record_id|recordId)=([A-Za-z0-9_-]+)|/record/info/(\w+)`)
	syllabusIDRe = regexp.MustCompile(`(?i)(?:syllabus_id|syllabusId)=([A-Za-z0-9_-]+)`)
	tokenRe      = regexp.MustCompile(`(?i)(?:token)=([A-Za-z0-9._-]+)`)
)

type requestIDs struct {
	CourseID   string
	SyllabusID string
	VideoID    string
	RecordID   string
	Token      string
}

type videoNode struct {
	ID    string
	Title string
	Kind  string
}

type courseNode struct {
	ID    string
	Title string
}

type fileNode struct {
	ID     string
	Name   string
	URL    string
	Format string
}

func (g *Gaodun) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("gaodun requires login cookies")
	}
	ids := parseIDs(rawURL)
	if ids.CourseID == "" && ids.VideoID == "" && ids.RecordID == "" && ids.Token == "" && !strings.Contains(strings.ToLower(rawURL), "gaodun.com") {
		return nil, fmt.Errorf("cannot parse gaodun cid/video id from URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := map[string]string{
		"Accept":  "application/json, text/plain, */*",
		"Referer": "https://www.gaodun.com",
		"Origin":  "https://www.gaodun.com",
	}

	if ids.VideoID != "" || ids.RecordID != "" || ids.Token != "" {
		entry, err := resolveDirect(c, headers, ids, "gaodun_"+firstNonEmpty(ids.VideoID, ids.RecordID, ids.Token))
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	entries, title, err := resolveCourse(c, headers, ids)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("gaodun: no playable resource found for course %s", ids.CourseID)
	}
	return &extractor.MediaInfo{Site: "gaodun", Title: util.SanitizeFilename(firstNonEmpty(title, "gaodun_"+ids.CourseID)), Entries: entries}, nil
}

func resolveCourse(c *util.Client, headers map[string]string, ids requestIDs) ([]*extractor.MediaInfo, string, error) {
	if ids.CourseID == "" {
		courses, err := fetchGaodunCourses(c, headers)
		if err != nil {
			return nil, "", err
		}
		seen := map[string]bool{}
		var entries []*extractor.MediaInfo
		for _, course := range courses {
			if course.ID == "" || seen[course.ID] {
				continue
			}
			seen[course.ID] = true
			childIDs := ids
			childIDs.CourseID = course.ID
			childEntries, childTitle, err := resolveCourse(c, headers, childIDs)
			if err != nil || len(childEntries) == 0 {
				continue
			}
			entries = append(entries, &extractor.MediaInfo{
				Site:    "gaodun",
				Title:   util.SanitizeFilename(firstNonEmpty(childTitle, course.Title, "gaodun_"+course.ID)),
				Entries: childEntries,
				Extra:   map[string]any{"course_id": course.ID},
			})
		}
		return entries, "gaodun_courses", nil
	}

	apis := []string{
		fmt.Sprintf(info_url, url.PathEscape(ids.CourseID)),
		fmt.Sprintf(info_gradation_url, url.PathEscape(ids.CourseID)),
		fmt.Sprintf(source_category_url, url.PathEscape(ids.CourseID)),
		fmt.Sprintf(source_gradation_url, url.PathEscape(ids.CourseID)),
	}
	if ids.SyllabusID != "" {
		apis = append([]string{
			fmt.Sprintf(info_glive_url, url.PathEscape(ids.CourseID), url.PathEscape(ids.SyllabusID)),
			fmt.Sprintf(info_syllabus_url, url.PathEscape(ids.CourseID), url.PathEscape(ids.SyllabusID)),
		}, apis...)
	}

	var nodes []videoNode
	var direct []*extractor.MediaInfo
	var files []fileNode
	var title string
	if pricePayload := fetchGaodunPricePayload(c, headers, ids.CourseID); pricePayload != nil {
		title = firstNonEmpty(title, pickTitle(pricePayload))
	}
	for _, api := range apis {
		body, err := c.GetString(api, headers)
		if err != nil {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			continue
		}
		title = firstNonEmpty(title, pickTitle(payload))
		if strings.Contains(api, "/handout") {
			files = append(files, collectGaodunFiles(payload)...)
			continue
		}
		if u := findMediaURL(payload); u != "" {
			direct = append(direct, mediaInfo(firstNonEmpty(pickTitle(payload), ids.CourseID), u, headers))
		}
		nodes = append(nodes, collectVideoNodes(payload)...)
	}

	seen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(direct)+len(nodes)+len(files))
	entries = append(entries, direct...)
	for _, n := range nodes {
		if n.ID == "" || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		entry, err := resolveDirect(c, headers, requestIDs{VideoID: n.ID}, n.Title)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	entries = append(entries, resolveGaodunFiles(c, headers, files)...)
	return entries, title, nil
}

func resolveDirect(c *util.Client, headers map[string]string, ids requestIDs, fallbackTitle string) (*extractor.MediaInfo, error) {
	var payloads []any
	fetchJSON := func(api string) {
		body, err := c.GetString(api, headers)
		if err != nil {
			return
		}
		var payload any
		if json.Unmarshal([]byte(body), &payload) == nil {
			payloads = append(payloads, payload)
		}
	}

	if ids.RecordID != "" {
		fetchJSON(fmt.Sprintf(live_old_url, url.PathEscape(ids.RecordID)))
	}
	if ids.Token != "" {
		fetchJSON(fmt.Sprintf(live_token_url, url.QueryEscape(ids.Token)))
	}
	if ids.VideoID != "" {
		for _, mode := range []string{"FHD", "HD", "SD"} {
			fetchJSON(fmt.Sprintf(video_play_url, url.QueryEscape(ids.VideoID), mode))
			fetchJSON(fmt.Sprintf(live_play_url, url.QueryEscape(ids.VideoID), mode))
		}
	}

	for _, payload := range payloads {
		if u := findMediaURL(payload); u != "" {
			title := util.SanitizeFilename(firstNonEmpty(pickTitle(payload), fallbackTitle, "gaodun_"+firstNonEmpty(ids.VideoID, ids.RecordID, ids.Token)))
			entry := mediaInfo(title, u, headers)
			if strings.Contains(strings.ToLower(u), ".m3u8") {
				tokens := fetchGaodunTokens(c, headers)
				if len(tokens) > 0 {
					entry.Extra = tokens
				}
			}
			return entry, nil
		}
	}
	return nil, fmt.Errorf("gaodun: no path/playUrl from glive2-vod for %s", firstNonEmpty(ids.VideoID, ids.RecordID, ids.Token))
}

func fetchGaodunCourses(c *util.Client, headers map[string]string) ([]courseNode, error) {
	body, err := c.GetString(course_url, headers)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	return collectGaodunCourses(payload), nil
}

func fetchGaodunPricePayload(c *util.Client, headers map[string]string, courseID string) any {
	if courseID == "" {
		return nil
	}
	body, err := c.GetString(fmt.Sprintf(price_url, url.QueryEscape(courseID)), headers)
	if err != nil {
		return nil
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		return nil
	}
	return payload
}

func fetchGaodunTokens(c *util.Client, headers map[string]string) map[string]any {
	out := map[string]any{}
	if pc := fetchGaodunToken(c, headers, pc_token_url); pc != "" {
		out["pc_token"] = pc
	}
	if pe := fetchGaodunToken(c, headers, pe_token_url); pe != "" {
		out["pe_token"] = pe
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fetchGaodunToken(c *util.Client, headers map[string]string, api string) string {
	body, err := c.GetString(api, headers)
	if err != nil {
		return ""
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		return ""
	}
	return firstTokenText(payload)
}

func parseIDs(raw string) requestIDs {
	out := requestIDs{}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		out.CourseID = firstNonEmpty(q.Get("courseId"), q.Get("course_id"), q.Get("cid"), q.Get("ids"), q.Get("id"))
		out.SyllabusID = firstNonEmpty(q.Get("syllabus_id"), q.Get("syllabusId"))
		out.VideoID = firstNonEmpty(q.Get("videoId"), q.Get("video_id"), q.Get("vid"), q.Get("code"), q.Get("did"))
		out.RecordID = firstNonEmpty(q.Get("record_id"), q.Get("recordId"))
		out.Token = q.Get("token")
	}
	out.CourseID = firstNonEmpty(out.CourseID, rx(cidRe, raw))
	out.SyllabusID = firstNonEmpty(out.SyllabusID, rx(syllabusIDRe, raw))
	out.VideoID = firstNonEmpty(out.VideoID, rx(videoIDRe, raw))
	out.RecordID = firstNonEmpty(out.RecordID, rx(recordIDRe, raw))
	out.Token = firstNonEmpty(out.Token, rx(tokenRe, raw))
	return out
}

func collectGaodunCourses(v any) []courseNode {
	var out []courseNode
	seen := map[string]bool{}
	var walk func(any)
	walk = func(x any) {
		switch vv := x.(type) {
		case map[string]any:
			id := firstNonEmpty(valueString(vv, "saasCourseId", "course_id", "courseId", "ids", "id"))
			title := valueString(vv, "name", "title", "courseName")
			if id != "" && title != "" && !seen[id] {
				seen[id] = true
				out = append(out, courseNode{ID: id, Title: title})
			}
			for _, k := range []string{"result", "courseList", "children", "values", "list", "data"} {
				if child, ok := vv[k]; ok {
					walk(child)
				}
			}
			for _, child := range vv {
				walk(child)
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func collectVideoNodes(v any) []videoNode {
	var out []videoNode
	var walk func(any, string)
	walk = func(x any, prefix string) {
		switch vv := x.(type) {
		case map[string]any:
			title := firstNonEmpty(valueString(vv, "name", "title", "courseName", "syllabusName"), prefix)
			id := valueString(vv, "videoId", "video_id", "did", "code", "resourceId")
			kind := valueString(vv, "type", "resourceType", "videoType")
			if id != "" && (kind == "" || strings.Contains(strings.ToLower(kind), "video") || hasAny(vv, "resource", "path")) {
				out = append(out, videoNode{ID: id, Title: title, Kind: kind})
			}
			if res, ok := vv["resource"]; ok {
				walk(res, title)
			}
			for _, k := range []string{"children", "items", "list", "syllabus", "courseList", "result", "data"} {
				if child, ok := vv[k]; ok {
					walk(child, title)
				}
			}
		case []any:
			for _, child := range vv {
				walk(child, prefix)
			}
		}
	}
	walk(v, "")
	return out
}

func collectGaodunFiles(v any) []fileNode {
	var out []fileNode
	seen := map[string]bool{}
	var walk func(any, string)
	walk = func(x any, prefix string) {
		switch vv := x.(type) {
		case map[string]any:
			node := vv
			if res, ok := vv["resource"].(map[string]any); ok {
				node = res
			}
			name := firstNonEmpty(valueString(node, "name", "title", "fileName", "file_name"), valueString(vv, "name", "title", "fileName", "file_name"), prefix)
			rawURL := firstNonEmpty(firstDirectURL(node), firstDirectURL(vv))
			id := firstNonEmpty(valueString(node, "resourceId", "resource_id", "fileId", "file_id", "id"), valueString(vv, "resourceId", "resource_id", "fileId", "file_id", "id"))
			format := firstNonEmpty(valueString(node, "extension", "format", "file_fmt", "fileFmt", "suffix"), valueString(vv, "extension", "format", "file_fmt", "fileFmt", "suffix"))
			typ := strings.ToLower(firstNonEmpty(valueString(node, "type", "resourceType"), valueString(vv, "type", "resourceType")))
			if (rawURL != "" || id != "") && (typ == "" || typ == "file" || rawURL != "") {
				key := strings.Join([]string{id, rawURL, name}, "|")
				if !seen[key] {
					seen[key] = true
					out = append(out, fileNode{ID: id, Name: name, URL: rawURL, Format: strings.TrimPrefix(strings.ToLower(format), ".")})
				}
			}
			next := firstNonEmpty(name, prefix)
			for _, k := range []string{"children", "items", "list", "data", "result", "resource", "files", "fileList", "handouts"} {
				if child, ok := vv[k]; ok {
					walk(child, next)
				}
			}
		case []any:
			for _, child := range vv {
				walk(child, prefix)
			}
		}
	}
	walk(v, "")
	return out
}

func resolveGaodunFiles(c *util.Client, headers map[string]string, files []fileNode) []*extractor.MediaInfo {
	seen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(files))
	for i, file := range files {
		u := normalizeURL(file.URL)
		if u == "" && file.ID != "" {
			u = resolveGaodunFileURL(c, headers, file)
		}
		if !isHTTPURL(u) || seen[u] {
			continue
		}
		seen[u] = true
		name := firstNonEmpty(file.Name, fmt.Sprintf("gaodun_file_%02d", i+1))
		format := firstNonEmpty(file.Format, fileFormat(name, u), "bin")
		entries = append(entries, &extractor.MediaInfo{
			Site:  "gaodun",
			Title: util.SanitizeFilename(name),
			Streams: map[string]extractor.Stream{"file": {
				Quality: "source",
				URLs:    []string{u},
				Format:  format,
				Headers: headers,
			}},
			Extra: map[string]any{"kind": "file", "file_id": file.ID},
		})
	}
	return entries
}

func resolveGaodunFileURL(c *util.Client, headers map[string]string, file fileNode) string {
	if file.ID == "" {
		return ""
	}
	api := fmt.Sprintf(file_url, url.QueryEscape(file.ID), url.QueryEscape(file.Name))
	body, err := c.GetString(api, headers)
	if err != nil {
		return ""
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) == nil {
		if u := firstDownloadURL(payload); u != "" {
			return u
		}
	}
	return normalizeURL(body)
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"path", "playUrl", "play_url", "url", "m3u8", "m3u8Url", "file_url", "fileUrl"} {
			if s := normalizeURL(valueString(x, k)); isMediaURL(s) {
				return s
			}
		}
		for _, child := range x {
			if s := findMediaURL(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := findMediaURL(child); s != "" {
				return s
			}
		}
	case string:
		if s := normalizeURL(x); isMediaURL(s) {
			return s
		}
	}
	return ""
}

func firstDownloadURL(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"downloadUrl", "downloadURL", "fileUrl", "fileURL", "file_url", "url", "path", "resourceUrl", "resourceURL", "attachUrl", "attachmentUrl", "materialUrl", "handoutUrl", "pdfUrl", "pptUrl", "docUrl", "data"} {
			if s := normalizeURL(valueString(x, k)); isHTTPURL(s) {
				return s
			}
		}
		for _, child := range x {
			if s := firstDownloadURL(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := firstDownloadURL(child); s != "" {
				return s
			}
		}
	case string:
		if s := normalizeURL(x); isHTTPURL(s) {
			return s
		}
	}
	return ""
}

func firstDirectURL(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range []string{"downloadUrl", "downloadURL", "fileUrl", "fileURL", "file_url", "url", "path", "resourceUrl", "resourceURL", "attachUrl", "attachmentUrl", "materialUrl", "handoutUrl", "pdfUrl", "pptUrl", "docUrl"} {
		if s := normalizeURL(valueString(m, k)); isHTTPURL(s) {
			return s
		}
	}
	return ""
}

func firstTokenText(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if s := valueString(x, "token", "pcToken", "peToken", "key"); s != "" {
			return s
		}
		if result, ok := x["result"]; ok {
			if s, ok := result.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
			if s := firstTokenText(result); s != "" {
				return s
			}
		}
		for _, child := range x {
			if s := firstTokenText(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := firstTokenText(child); s != "" {
				return s
			}
		}
	case string:
		return ""
	}
	return ""
}

func mediaInfo(title, u string, headers map[string]string) *extractor.MediaInfo {
	format := "mp4"
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		format = "m3u8"
	}
	return &extractor.MediaInfo{Site: "gaodun", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: format, NeedMerge: format == "m3u8", Headers: headers}}}
}

func pickTitle(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if s := valueString(x, "courseName", "name", "title", "syllabusName"); s != "" {
			return s
		}
		for _, child := range x {
			if s := pickTitle(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := pickTitle(child); s != "" {
				return s
			}
		}
	}
	return ""
}

func valueString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func hasAny(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func rx(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}

func normalizeURL(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	s = strings.ReplaceAll(s, `\/`, `/`)
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	return s
}

func isMediaURL(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "http") && (strings.Contains(low, ".m3u8") || strings.Contains(low, ".mp4") || strings.Contains(low, ".flv") || strings.Contains(low, ".mp3"))
}

func isHTTPURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

func fileFormat(name, rawURL string) string {
	for _, s := range []string{name, rawURL} {
		if m := regexp.MustCompile(`\.([A-Za-z0-9]{1,8})(?:$|[?#])`).FindStringSubmatch(s); len(m) > 1 {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
