package cctalk

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
	CCTALK_BASE_URL          = "https://www.cctalk.com"
	CCTALK_CONTENT_API_V1    = "/webapi/content/v1"
	CCTALK_CONTENT_API_V11   = "/webapi/content/v1.1"
	CCTALK_CONTENT_API_V12   = "/webapi/content/v1.2"
	CCTALK_PCWEB_KEY         = "pcweb"
	CCTALK_TENANT_ID         = "10002"
	CCTALK_USER_AGENT        = "Mozilla/5.0 (Windows NT 6.2; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) QtWebEngine/5.9.7 Chrome/56.0.2924.122 Safari/537.36 CTPC/7.10.10.1"
	CCTALK_OCS_USER_AGENT    = "Hujiang/OCS/PC/Qt/Win"
	CCTALK_OCS_MATERIAL_HOST = "https://p1.ocs.hjfile.cn"

	my_group_list_url = "https://m.cctalk.com/webapi/content/v1.1/user/my_group_list?start=%d&limit=%d&sortType=1&keyword=%s"
	mycourse_url      = "https://m.cctalk.com/mycourse"
	mobile_url        = "https://m.cctalk.com"
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
	pathCourseRe = regexp.MustCompile(`/(?:m/|web/|school/)?course/(\d+)`)
	pathGroupRe  = regexp.MustCompile(`/(?:m/|web/)?group/(\d+)`)
	pathSeriesRe = regexp.MustCompile(`/(?:m/)?(?:program|series)/(\d+)|/group/(\d+)/series/(\d+)`)
	pathVideoRe  = regexp.MustCompile(`/v/(\d+)`)
	numberRe     = regexp.MustCompile(`(\d{6,})`)
)

func (ct *CCTalk) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("cctalk requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	ctx := &apiClient{c: c, headers: baseHeaders()}
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
	switch {
	case id.SeriesID != "":
		title = "cctalk_" + id.SeriesID
		structs = ctx.getSeriesStructs(id.SeriesID)
	case id.GroupID != "":
		title = "cctalk_" + id.GroupID
		structs = ctx.getGroupVideoList(id.GroupID, id.SeriesID)
	case id.CourseID != "":
		title = "cctalk_" + id.CourseID
		structs = ctx.getCourseStructs(id.CourseID)
	default:
		return nil, fmt.Errorf("cannot parse cctalk course/group/series/video id from URL")
	}

	entries := buildEntries(ctx, structs, id.SeriesID)
	if len(entries) == 0 {
		return nil, fmt.Errorf("cctalk: no playable video URL found in API response")
	}
	return &extractor.MediaInfo{Site: "cctalk", Title: util.SanitizeFilename(title), Entries: entries}, nil
}

type apiClient struct {
	c       *util.Client
	headers map[string]string
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

func (a *apiClient) getCourseDetail(courseID string) map[string]any {
	if strings.TrimSpace(courseID) == "" {
		return nil
	}
	endpoints := [][2]string{
		{fmt.Sprintf("/course/%s/course_detail", courseID), "v1.1"},
		{fmt.Sprintf("/course/%s/course_detail", courseID), "v1.2"},
		{fmt.Sprintf("/course/%s/detail", courseID), "v1.1"},
	}
	for _, ep := range endpoints {
		data := asMap(extractData(a.requestAPI(ep[0], nil, "", ep[1])))
		if len(data) > 0 {
			return data
		}
	}
	// fallback: params-based detail
	data := asMap(extractData(a.requestAPI("/course/detail", map[string]string{"courseId": courseID}, "", "v1.1")))
	if len(data) > 0 {
		return data
	}
	return nil
}

func (a *apiClient) getGroupInfo(groupID string) map[string]any {
	if strings.TrimSpace(groupID) == "" {
		return nil
	}
	for _, path := range []string{
		fmt.Sprintf("/webapi/im/v1.3/group/%s/info?isRichInfo=true", groupID),
		fmt.Sprintf("/webapi/im/v1.1/group/%s/baseinfo", groupID),
	} {
		data := asMap(extractData(a.requestJSON(CCTALK_BASE_URL+path, nil, "")))
		if len(data) > 0 {
			data["groupId"] = groupID
			if _, ok := data["courseId"]; !ok {
				data["courseId"] = groupID
			}
			if gn := textValue(data, "groupName"); gn != "" {
				if _, ok := data["courseName"]; !ok {
					data["courseName"] = gn
				}
			}
			return data
		}
	}
	return nil
}

func (a *apiClient) getSeriesInfo(seriesID string) map[string]any {
	if strings.TrimSpace(seriesID) == "" {
		return nil
	}
	for _, ep := range [][2]string{
		{fmt.Sprintf("/series/%s/get_series_info", seriesID), "v1.1"},
		{fmt.Sprintf("/series/%s/get_series_info", seriesID), "v1.2"},
	} {
		data := asMap(extractData(a.requestAPI(ep[0], nil, "", ep[1])))
		if len(data) > 0 {
			data["seriesId"] = seriesID
			return data
		}
	}
	return nil
}

func (a *apiClient) getGroupSeries(groupID string) []map[string]any {
	if strings.TrimSpace(groupID) == "" {
		return nil
	}
	var result []map[string]any
	seen := map[string]bool{}
	offset := 0
	for i := 0; i < 20; i++ {
		var page []any
		for _, version := range []string{"v1.2", "v1.1"} {
			data := extractData(a.requestAPI(
				fmt.Sprintf("/series/group/%s/series", groupID),
				map[string]string{"limit": "50", "start": fmt.Sprint(offset)},
				"", version,
			))
			if list := extractList(data); len(list) > 0 {
				page = list
				break
			}
		}
		for _, item := range page {
			m := asMap(item)
			if m == nil {
				continue
			}
			m["groupId"] = groupID
			if _, ok := m["courseId"]; !ok {
				m["courseId"] = firstNonEmpty(textValue(m, "seriesId"), textValue(m, "id"))
			}
			if _, ok := m["courseName"]; !ok {
				m["courseName"] = firstNonEmpty(textValue(m, "seriesName"), textValue(m, "name"), textValue(m, "title"))
			}
			key := firstNonEmpty(textValue(m, "seriesId"), textValue(m, "courseId"), textValue(m, "id"))
			if key != "" && !seen[key] {
				seen[key] = true
				result = append(result, m)
			}
		}
		if len(page) == 0 || len(page) < 50 {
			break
		}
		offset += len(page)
	}
	return result
}

func (a *apiClient) getLessonInfo(lessonID string) map[string]any {
	if strings.TrimSpace(lessonID) == "" {
		return nil
	}
	for _, source := range []string{"0", "1", "2", ""} {
		data := asMap(extractData(a.requestAPI("/course/get_lesson_info",
			map[string]string{"lessonId": lessonID, "source": source, "withCourse": "true"},
			"", "v1.1")))
		if len(data) > 0 {
			return data
		}
	}
	return nil
}

func (a *apiClient) getSubscribeCourses() []map[string]any {
	var result []map[string]any
	seen := map[string]bool{}

	// Phase 1: my_group_list
	offset := 0
	for i := 0; i < 20; i++ {
		body, err := a.c.GetString(
			fmt.Sprintf(my_group_list_url, offset, 20, ""),
			mergeSS(a.headers, map[string]string{"Referer": mycourse_url, "Origin": mobile_url}),
		)
		if err != nil {
			break
		}
		var raw map[string]any
		if json.Unmarshal([]byte(body), &raw) != nil {
			break
		}
		data := asMap(extractData(raw))
		page := extractList(data)
		for _, item := range page {
			m := asMap(item)
			if m == nil {
				continue
			}
			key := firstNonEmpty(textValue(m, "courseId", "seriesId", "groupId", "id"))
			if key != "" && !seen[key] {
				seen[key] = true
				result = append(result, m)
			}
		}
		hasMore := false
		if data != nil {
			if np, ok := data["nextPage"]; ok {
				if npInt, ok2 := np.(float64); ok2 && int(npInt) > offset {
					offset = int(npInt)
					hasMore = true
				}
			}
		}
		if len(page) == 0 || !hasMore {
			break
		}
	}

	// Phase 2: course_subscribe_list per type
	for _, courseType := range []string{"1", "2", "0"} {
		timeline := ""
		for i := 0; i < 20; i++ {
			params := map[string]string{"limit": "50", "timeline": timeline, "courseType": courseType}
			data := asMap(extractData(a.requestAPI("/user/course_subscribe_list", params, "", "v1.1")))
			page := extractList(data)
			for _, item := range page {
				m := asMap(item)
				if m == nil {
					continue
				}
				key := firstNonEmpty(textValue(m, "courseId", "seriesId", "groupId", "id"))
				if key != "" && !seen[key] {
					seen[key] = true
					result = append(result, m)
				}
			}
			nextTimeline := ""
			if data != nil {
				nextTimeline = firstNonEmpty(textValue(data, "nextTimeline", "next_timeline", "timeline"))
			}
			if len(page) == 0 || nextTimeline == "" || nextTimeline == timeline {
				break
			}
			timeline = nextTimeline
		}
	}

	return result
}

func (a *apiClient) getCourseStructs(courseID string) any {
	for _, version := range []string{"v1.1", "v1.2"} {
		data := extractData(a.requestAPI(fmt.Sprintf("/course/%s/course_structs", courseID), map[string]string{"orderType": "1"}, "", version))
		if list := extractList(data); len(list) > 0 {
			return list
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
			if list := extractList(data); len(list) > 0 {
				return list
			}
			if m := asMap(data); len(m) > 0 {
				return m
			}
		}
	}
	for _, version := range []string{"v1.1", "v1.2"} {
		data := extractData(a.requestAPI(fmt.Sprintf("/series/%s/content_list", seriesID), nil, "", version))
		if list := extractList(data); len(list) > 0 {
			return list
		}
	}
	return nil
}

func (a *apiClient) getGroupVideoList(groupID, seriesID string) any {
	data := extractData(a.requestAPI(fmt.Sprintf("/group/%s/new_video_list", groupID), map[string]string{
		"start": "0", "seriesId": seriesID, "searchKey": "", "limit": "50", "groupId": groupID,
	}, "", "v1.1"))
	return extractList(data)
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
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, node := range walkMaps(structs) {
		seriesID := firstNonEmpty(textValue(node, "seriesId"), fallbackSeries)
		videoID := firstNonEmpty(textValue(node, "videoId", "video_id", "contentId", "lessonId", "id", "bizId"))
		merged := node
		if findMediaURL(merged) == "" && !hasDownloadableResource(merged) && videoID != "" {
			merged = mergeMaps(merged, a.getVideoPlayInfo(videoID, seriesID))
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
		if stream, extra, ok := a.resolveOCSStream(coursewareInfo); ok {
			title := firstNonEmpty(textValue(item, "lessonName", "videoName", "contentName", "title", "name", "subject"), fallbackTitle)
			extra["tenantId"] = firstNonEmpty(textValue(coursewareInfo, "tenantId"), CCTALK_TENANT_ID)
			extra["courseware_info"] = coursewareInfo
			extra["playback_type"] = playbackType(item, extra)
			return &extractor.MediaInfo{Site: "cctalk", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": stream}, Extra: extra}, nil
		}
	} else if len(coursewareInfo) > 0 {
		ocsHeaders = ocsHeadersFor(coursewareInfo)
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
	u, _ := url.Parse(raw)
	path := raw
	if u != nil {
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
		for _, k := range []string{"items", "list", "lessonList", "videoList", "contentList", "records", "rows"} {
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
		case map[string]any:
			out = append(out, x)
			for _, k := range []string{"children", "childs", "nodes", "lessons", "lessonList", "items", "list", "contents", "contentList", "videoList", "videos", "recordList", "mediaList", "playList"} {
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
		for _, k := range []string{"videoUrl", "playUrl", "m3u8Url", "hlsUrl", "mediaUrl", "mediaURL", "mp4URL", "downloadUrl", "media_url", "fileUrl", "fileURL", "url"} {
			if u := findMediaURL(x[k]); u != "" {
				return u
			}
		}
		for _, k := range []string{"playInfo", "ocsInfo", "videoInfo", "mediaInfo", "coursewareInfo", "courseWareInfo", "contentInfo", "resourceInfo", "activityInfo", "lessonInfo", "detail", "raw"} {
			if u := findMediaURL(x[k]); u != "" {
				return u
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
	s = strings.TrimSpace(strings.ReplaceAll(s, `\/`, `/`))
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

func mergeSS(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
