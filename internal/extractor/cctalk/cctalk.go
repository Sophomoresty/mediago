package cctalk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	CCTALK_BASE_URL          = "https://www.cctalk.com"
	CCTALK_CONTENT_API_V1    = "/webapi/content/v1"
	CCTALK_CONTENT_API_V11   = "/webapi/content/v1.1"
	CCTALK_CONTENT_API_V12   = "/webapi/content/v1.2"
	CCTALK_PCWEB_KEY         = "pcweb"
	CCTALK_TENANT_ID         = "10002"
	CCTALK_USER_AGENT        = "Mozilla/5.0 (Windows NT 6.2; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) QtWebEngine/5.9.7 Chrome/56.0.2924.122 Safari/537.36 CTPC/7.10.10.1"
	CCTALK_OCS_USER_AGENT    = "Hujiang/OCS/PC/Qt/Win"
	CCTALK_OCS_MATERIAL_HOST = "https://p1.ocs.hjfile.cn"
)

var patterns = []string{`(?:[\w-]+\.)?cctalk\.com/|^(?:cctalk|ocsplayer)://`}
var cctalkOCSCurrentBases = []string{
	"https://courseware-ocs.hjapi.com/v5.5/",
	"https://courseware-ocs.hjapi.com/v5.4/",
	"https://courseware-ocs.hjapi.com/v5/",
	"https://courseware-ocs.hjapi.com/",
	"https://courseware-ocs1.hjapi.com/v5.5/",
	"https://courseware-ocs1.hjapi.com/v5.4/",
	"https://courseware-ocs1.hjapi.com/v5/",
	"https://courseware-ocs1.hjapi.com/",
}

func init() {
	extractor.Register(&CCTalk{}, extractor.SiteInfo{Name: "CCTalk", URL: "cctalk.com", NeedAuth: true})
}

type CCTalk struct{}

func (c *CCTalk) Patterns() []string { return patterns }

type ids struct{ CourseID, GroupID, SeriesID, VideoID string }

var (
	pathCourseRe  = regexp.MustCompile(`/(?:m/|web/|school/)?course/(\d+)`)
	pathGroupRe   = regexp.MustCompile(`/(?:m/|web/)?group/(\d+)`)
	pathSeriesRe  = regexp.MustCompile(`/(?:m/)?(?:program|series)/(\d+)|/group/(\d+)/series/(\d+)`)
	pathVideoRe   = regexp.MustCompile(`/v/(\d+)`)
	numberRe      = regexp.MustCompile(`(\d{6,})`)
	numberTokenRe = regexp.MustCompile(`\d+`)
)

func (ct *CCTalk) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("cctalk requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	ctx := &apiClient{c: c, headers: baseHeaders(), jar: opts.Cookies, listOnly: opts.ListOnly, quality: opts.Quality}
	if cookie := cookieHeaderFromJar(opts.Cookies); cookie != "" {
		ctx.cookieHeader = cookie
		ctx.headers["Cookie"] = cookie
	}
	if err := ctx.ensureLogin(); err != nil {
		return nil, err
	}
	id := parseIDs(rawURL)

	if id.VideoID != "" {
		info := ctx.getVideoPlayInfo(id.VideoID, id.SeriesID)
		entry, err := mediaFromMap(ctx, mergeMaps(map[string]any{"videoId": id.VideoID, "seriesId": id.SeriesID}, info), "cctalk_"+id.VideoID)
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	title := "cctalk"
	var structs any
	courseMeta := map[string]any{}
	switch {
	case id.SeriesID != "":
		if info := ctx.getSeriesInfo(id.SeriesID); len(info) > 0 {
			courseMeta = info
			title = firstNonEmpty(textValue(info, "courseName", "seriesName", "title", "name"), "cctalk_"+id.SeriesID)
		} else {
			title = "cctalk_" + id.SeriesID
		}
		structs = ctx.getSeriesStructs(id.SeriesID)
	case id.GroupID != "":
		if info := ctx.getGroupInfo(id.GroupID); len(info) > 0 {
			courseMeta = info
			title = firstNonEmpty(textValue(info, "courseName", "groupName", "title", "name"), "cctalk_"+id.GroupID)
		} else {
			title = "cctalk_" + id.GroupID
		}
		structs = ctx.getGroupVideoList(id.GroupID, id.SeriesID)
		if id.SeriesID == "" && len(walkMaps(structs)) == 0 {
			structs = ctx.getGroupSeriesStructs(id.GroupID, ctx.getGroupSeries(id.GroupID))
		}
	case id.CourseID != "":
		if info := ctx.getCourseDetail(id.CourseID); len(info) > 0 {
			courseMeta = info
			title = firstNonEmpty(textValue(info, "courseName", "title", "name"), "cctalk_"+id.CourseID)
		} else {
			title = "cctalk_" + id.CourseID
		}
		structs = ctx.getCourseStructs(id.CourseID)
	default:
		courses := ctx.getSubscribeCourses()
		if len(courses) > 0 {
			return courseListMedia("cctalk_my_courses", courses), nil
		}
		return nil, fmt.Errorf("cannot parse cctalk course/group/series/video id from URL")
	}

	entries, chapters := buildEntriesAndChapters(ctx, structs, id, courseMeta)
	if len(entries) == 0 {
		return nil, fmt.Errorf("cctalk: no playable video URL found in API response")
	}
	return &extractor.MediaInfo{Site: "cctalk", Title: util.SanitizeFilename(title), Entries: entries, Chapters: chapters, Extra: courseExtra(id, courseMeta)}, nil
}

type apiClient struct {
	c            *util.Client
	headers      map[string]string
	jar          http.CookieJar
	cookieHeader string
	listOnly     bool
	quality      string
}

func baseHeaders() map[string]string {
	return map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Hujiang-App-Key": CCTALK_PCWEB_KEY,
		"Origin":          CCTALK_BASE_URL,
		"Referer":         CCTALK_BASE_URL + "/",
		"User-Agent":      CCTALK_USER_AGENT,
	}
}

func (a *apiClient) apiURL(path, version string) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	prefix := map[string]string{"v1": CCTALK_CONTENT_API_V1, "v1.1": CCTALK_CONTENT_API_V11, "v1.2": CCTALK_CONTENT_API_V12}[version]
	if prefix == "" {
		prefix = CCTALK_CONTENT_API_V11
	}
	return CCTALK_BASE_URL + prefix + path
}

func (a *apiClient) requestAPI(path string, params map[string]string, method, version string) map[string]any {
	u := a.apiURL(path, version)
	if method != "post" && len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}
	return a.requestJSON(u, params, method)
}

func (a *apiClient) requestJSON(u string, data map[string]string, method string) map[string]any {
	var body string
	var err error
	if method == "post" {
		body, err = a.c.PostForm(u, data, a.headers)
	} else {
		body, err = a.c.GetString(u, a.headers)
	}
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal([]byte(body), &out) != nil {
		return nil
	}
	return out
}

func (a *apiClient) getCourseStructs(courseID string) any {
	for _, version := range []string{"v1.1", "v1.2"} {
		data := extractData(a.requestAPI(fmt.Sprintf("/course/%s/course_structs", courseID), map[string]string{"orderType": "1"}, "", version))
		if normalized := normalizeSeriesStructs(data, ""); len(walkMaps(normalized)) > 0 {
			if details := a.batchQueryLesson(courseID, normalized); len(details) > 0 {
				normalized = mergeStructsWithDetails(normalized, details)
			}
			return normalized
		}
		if m := asMap(data); len(m) > 0 {
			return m
		}
	}
	return nil
}

func (a *apiClient) getSeriesStructs(seriesID string) any {
	for _, endpoint := range []string{"/series/all_lesson_list", "/series/all_video_list"} {
		for _, version := range []string{"v1.2", "v1.1"} {
			data := extractData(a.requestAPI(endpoint, map[string]string{"showStudyTime": "true", "limit": "", "seriesId": seriesID}, "", version))
			if normalized := normalizeSeriesStructs(data, seriesID); len(walkMaps(normalized)) > 0 {
				return normalized
			}
		}
	}
	for _, version := range []string{"v1.1", "v1.2"} {
		data := extractData(a.requestAPI(fmt.Sprintf("/series/%s/content_list", seriesID), nil, "", version))
		if normalized := normalizeSeriesStructs(data, seriesID); len(walkMaps(normalized)) > 0 {
			return normalized
		}
	}
	return nil
}

func (a *apiClient) getGroupVideoList(groupID, seriesID string) any {
	groupID = strings.TrimSpace(groupID)
	seriesID = strings.TrimSpace(seriesID)
	if groupID == "" {
		return nil
	}
	var out []any
	seen := map[string]bool{}
	start := 0
	for i := 0; i < 20; i++ {
		data := extractData(a.requestAPI(fmt.Sprintf("/group/%s/new_video_list", groupID), map[string]string{
			"start": fmt.Sprint(start), "seriesId": seriesID, "searchKey": "", "limit": "50", "groupId": groupID,
		}, "", "v1.1"))
		page := extractList(data)
		if len(page) == 0 {
			break
		}
		for _, item := range page {
			m := asMap(item)
			if m == nil {
				out = append(out, item)
				continue
			}
			m["groupId"] = groupID
			if seriesID != "" {
				if _, ok := m["seriesId"]; !ok {
					m["seriesId"] = seriesID
				}
			}
			key := firstNonEmpty(textValue(m, "contentId", "videoId", "lessonId", "id", "bizId"), textValue(m, "title", "name", "videoName", "lessonName"))
			if key != "" {
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			out = append(out, m)
		}
		if len(page) < 50 {
			break
		}
		start += len(page)
	}
	sortGroupVideoItems(out)
	return normalizeSeriesStructs(map[string]any{"items": out}, seriesID)
}

func (a *apiClient) getVideoPlayInfo(videoID, seriesID string) map[string]any {
	out := map[string]any{}
	for _, version := range []string{"v1.1", "v1.2"} {
		data := asMap(extractData(a.requestAPI("/video/detail", map[string]string{"videoId": videoID, "seriesId": seriesID}, "", version)))
		out = mergeMaps(out, data)
	}
	for _, version := range []string{"v1.1", "v1.2"} {
		data := asMap(extractData(a.requestAPI("/video/play", map[string]string{"videoId": videoID}, "post", version)))
		out = mergeMaps(out, data)
	}
	return out
}

func buildEntries(a *apiClient, structs any, fallbackSeries string) []*extractor.MediaInfo {
	entries, _ := buildEntriesAndChapters(a, structs, ids{SeriesID: fallbackSeries}, nil)
	return entries
}

func buildEntriesFlat(a *apiClient, structs any, fallback ids, courseMeta map[string]any) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, node := range walkMaps(structs) {
		seriesID := firstNonEmpty(textValue(node, "seriesId", "series_id"), fallback.SeriesID)
		videoID := firstNonEmpty(textValue(node, "videoId", "video_id", "contentId", "content_id", "lessonId", "lesson_id", "id", "bizId"))
		lessonID := firstNonEmpty(textValue(node, "lessonId", "lesson_id"), videoID)
		merged := node
		if _, ok := merged["courseId"]; !ok && fallback.CourseID != "" {
			merged = mergeMaps(map[string]any{"courseId": fallback.CourseID}, merged)
		}
		if _, ok := merged["groupId"]; !ok && fallback.GroupID != "" {
			merged = mergeMaps(map[string]any{"groupId": fallback.GroupID}, merged)
		}
		if findMediaURL(merged) == "" && !hasDownloadableResource(merged) && lessonID != "" && a != nil && a.c != nil {
			if detail := a.getLessonInfo(lessonID); len(detail) > 0 {
				merged = mergeMaps(merged, detail)
			}
		}
		if findMediaURL(merged) == "" && !hasDownloadableResource(merged) && videoID != "" && a != nil && a.c != nil {
			merged = mergeMaps(merged, a.getVideoPlayInfo(videoID, seriesID))
		}
		if len(courseMeta) > 0 {
			merged = mergeMaps(courseMeta, merged)
		}
		for _, entry := range entriesFromMap(a, merged, fmt.Sprintf("课时%d", i+1)) {
			key := entryKey(entry)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry)
		}
	}
	return entries
}

func mediaFromMap(a *apiClient, item map[string]any, fallbackTitle string) (*extractor.MediaInfo, error) {
	mediaURL := normalizeMediaURL(findMediaURL(item))
	coursewareInfo := extractCoursewareInfo(item)
	ocsExtra := map[string]any{}
	ocsHeaders := baseHeaders()
	if a != nil && a.headers != nil {
		ocsHeaders = a.headers
	}
	if mediaURL == "" {
		if stream, extra, ok := buildEmbeddedOCSStream(item, coursewareInfo); ok {
			title := firstNonEmpty(textValue(item, "lessonName", "videoName", "contentName", "title", "name", "subject"), fallbackTitle)
			extra["tenantId"] = firstNonEmpty(textValue(coursewareInfo, "tenantId"), CCTALK_TENANT_ID)
			extra["courseware_info"] = coursewareInfo
			extra["playback_type"] = playbackType(item, extra)
			return &extractor.MediaInfo{Site: "cctalk", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": stream}, Extra: extra}, nil
		}
		if hasMeaningfulCoursewareInfo(coursewareInfo) {
			if stream, extra, ok := a.resolveOCSStream(coursewareInfo); ok {
				title := firstNonEmpty(textValue(item, "lessonName", "videoName", "contentName", "title", "name", "subject"), fallbackTitle)
				extra["tenantId"] = firstNonEmpty(textValue(coursewareInfo, "tenantId"), CCTALK_TENANT_ID)
				extra["courseware_info"] = coursewareInfo
				extra["playback_type"] = playbackType(item, extra)
				return &extractor.MediaInfo{Site: "cctalk", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": stream}, Extra: extra}, nil
			}
		}
		if stream, extra, ok := a.resolveProviderStream(item); ok {
			title := firstNonEmpty(textValue(item, "lessonName", "videoName", "contentName", "title", "name", "subject"), fallbackTitle)
			extra["tenantId"] = firstNonEmpty(textValue(coursewareInfo, "tenantId"), CCTALK_TENANT_ID)
			extra["courseware_info"] = coursewareInfo
			extra["playback_type"] = playbackType(item, extra)
			return &extractor.MediaInfo{Site: "cctalk", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": stream}, Extra: extra}, nil
		}
	} else if hasMeaningfulCoursewareInfo(coursewareInfo) {
		ocsHeaders = ocsHeadersFor(coursewareInfo)
		mediaURL = signOCSMediaURL(mediaURL, ocsHeaders)
		ocsExtra["courseware_info"] = coursewareInfo
	}
	if mediaURL == "" {
		return nil, classifyBlocked(item)
	}
	title := firstNonEmpty(textValue(item, "lessonName", "videoName", "contentName", "title", "name", "subject"), fallbackTitle)
	extra := map[string]any{"tenantId": firstNonEmpty(textValue(coursewareInfo, "tenantId"), textValue(item, "tenantId"), CCTALK_TENANT_ID)}
	for k, v := range ocsExtra {
		extra[k] = v
	}
	extra["playback_type"] = playbackType(item, extra)
	return &extractor.MediaInfo{
		Site:  "cctalk",
		Title: util.SanitizeFilename(title),
		Streams: map[string]extractor.Stream{
			"best": {Quality: "best", URLs: []string{mediaURL}, Format: pickFormat(mediaURL), Headers: ocsHeaders, NeedMerge: pickFormat(mediaURL) == "m3u8"},
		},
		Extra: extra,
	}, nil
}

func parseIDs(raw string) ids {
	var out ids
	u, err := url.Parse(raw)
	path := raw
	if err == nil && u != nil {
		path = u.Path
		q := u.Query()
		out.CourseID = firstNonEmpty(q.Get("courseId"), q.Get("course_id"), q.Get("cid"))
		out.SeriesID = firstNonEmpty(q.Get("sid"), q.Get("seriesId"), q.Get("series_id"))
		out.GroupID = firstNonEmpty(q.Get("groupId"), q.Get("group_id"), q.Get("gid"))
		out.VideoID = firstNonEmpty(q.Get("contentId"), q.Get("videoId"), q.Get("vid"))
	}
	out.CourseID = firstNonEmpty(out.CourseID, extractFirst(pathCourseRe, path))
	out.GroupID = firstNonEmpty(out.GroupID, extractFirst(pathGroupRe, path))
	out.SeriesID = firstNonEmpty(out.SeriesID, extractFirst(pathSeriesRe, path))
	out.VideoID = firstNonEmpty(out.VideoID, extractFirst(pathVideoRe, path))
	if out.VideoID == "" && (strings.HasPrefix(raw, "cctalk://") || strings.HasPrefix(raw, "ocsplayer://")) {
		out.VideoID = extractFirst(numberRe, raw)
	}
	return out
}

func extractData(v any) any {
	if m := asMap(v); m != nil {
		for _, k := range []string{"data", "Data", "result", "Result"} {
			if m[k] != nil {
				return m[k]
			}
		}
	}
	return v
}

func extractList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, k := range []string{"items", "list", "lessonList", "lesson_list", "videoList", "video_list", "contentList", "content_list", "records", "rows", "dataList", "recordList", "mediaList", "playList", "resourceList", "materialList"} {
			if l := extractList(x[k]); len(l) > 0 {
				return l
			}
		}
	}
	return nil
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, it := range x {
				walk(it)
			}
		case []map[string]any:
			for _, it := range x {
				walk(it)
			}
		case map[string]any:
			out = append(out, x)
			for _, k := range childKeys() {
				walk(x[k])
			}
		}
	}
	walk(v)
	return out
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case string:
		if looksMediaURL(x) {
			return x
		}
	case []any:
		for _, it := range x {
			if u := findMediaURL(it); u != "" {
				return u
			}
		}
	case map[string]any:
		for _, k := range []string{"videoUrl", "videoURL", "playUrl", "playURL", "m3u8Url", "m3u8URL", "hlsUrl", "hlsURL", "mediaUrl", "mediaURL", "mp4Url", "mp4URL", "downloadUrl", "downloadURL", "sourceUrl", "sourceURL", "resourceUrl", "resourceURL", "media_url", "play_url", "fileUrl", "fileURL", "url"} {
			if u := findMediaURL(x[k]); u != "" {
				return u
			}
		}
		for _, k := range []string{"playInfo", "play_info", "ocsInfo", "ocs_info", "videoInfo", "video_info", "mediaInfo", "media_info", "coursewareInfo", "courseWareInfo", "courseware_info", "contentInfo", "content_info", "resourceInfo", "resource_info", "activityInfo", "activity_info", "lessonInfo", "lesson_info", "detail", "raw"} {
			if u := findMediaURL(x[k]); u != "" {
				return u
			}
		}
		for _, nested := range x {
			switch nested.(type) {
			case []any, map[string]any:
				if u := findMediaURL(nested); u != "" {
					return u
				}
			}
		}
	}
	return ""
}

func looksMediaURL(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(strings.TrimSpace(s), "#EXTM3U") ||
		((strings.HasPrefix(l, "http") || strings.HasPrefix(l, "//") || strings.HasPrefix(l, "/")) &&
			(strings.Contains(l, ".m3u8") || strings.Contains(l, ".mp4") || strings.Contains(l, ".flv") || strings.Contains(l, ".m4a") || strings.Contains(l, ".mp3")))
}

func normalizeMediaURL(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, `\/`, `/`), `\u0026`, "&"), "&amp;", "&"))
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "/") {
		return CCTALK_BASE_URL + s
	}
	return s
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func mergeMaps(left, right map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range left {
		out[k] = v
	}
	for k, v := range right {
		if v != nil && v != "" {
			out[k] = v
		}
	}
	return out
}

func textValue(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := fmt.Sprint(m[k]); v != "" && v != "<nil>" {
			return v
		}
	}
	return ""
}

func extractFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

func pickFormat(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, ".m3u8") || strings.HasPrefix(strings.TrimSpace(s), "#EXTM3U") || strings.Contains(lower, "mpegurl") {
		return "m3u8"
	}
	for _, ext := range []string{"mp4", "flv", "m4a", "mp3", "pdf", "zip", "rar", "7z", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "html"} {
		if strings.Contains(lower, "."+ext) {
			return ext
		}
	}
	return "mp4"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sortGroupVideoItems(items []any) {
	if len(items) <= 1 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftKind, leftValue := groupVideoSortKey(items[i], i)
		rightKind, rightValue := groupVideoSortKey(items[j], j)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		if leftValue != rightValue {
			return leftValue < rightValue
		}
		return i < j
	})
}

func groupVideoSortKey(item any, fallback int) (int, int) {
	m := asMap(item)
	if m == nil {
		return 3, fallback
	}
	for _, key := range []string{"showIndex", "sort", "sortIndex", "orderIndex", "index", "seq", "sequence"} {
		if n := intFromAny(m[key]); n != 0 {
			return 1, n
		}
	}
	for _, key := range []string{"contentId", "videoId", "lessonId", "id", "bizId"} {
		if n := intFromAny(m[key]); n != 0 {
			return 2, n
		}
	}
	if n := lastIntInString(textValue(m, "videoName", "contentName", "lessonName", "title", "name")); n != 0 {
		return 2, n
	}
	return 3, fallback
}

func lastIntInString(s string) int {
	matches := numberTokenRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return 0
	}
	return intFromAny(matches[len(matches)-1])
}
