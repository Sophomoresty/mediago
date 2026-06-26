// Package chaoge implements an extractor for chaogejiaoyu.com courses.
package chaoge

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	courseListURL        = "https://chaogejiaoyu.com/user/index/getMyCourseListAjax"
	courseDetailURL      = "https://chaogejiaoyu.com/course/index/getCourseDetailAjax?id=%s&get_offline_info=0"
	seriesURL            = "https://chaogejiaoyu.com/course/index/getSeriesCourseListAjax?pid=%s&is_end=%d&page=%d&huifang_sort=1&page_size=1000"
	enterCourseURL       = "https://chaogejiaoyu.com/course/room/%s"
	courseFileURL        = "https://chaogejiaoyu.com/course/index/getCourseFileListAjax?course_id=%s"
	publicCourseURL      = "https://chaogejiaoyu.com/course/%s"
	publicGroupCourseURL = "https://chaogejiaoyu.com/course/%s"
	csslLoginURL         = "https://view.csslcloud.net/replay/user/login"
	csslPlayURL          = "https://view.csslcloud.net/replay/video/play"
	csslMetaURL          = "https://view.csslcloud.net/replay/data/meta"
	csslOrigin           = "https://view.csslcloud.net"
	refererURL           = "https://chaogejiaoyu.com/"
	originURL            = "https://chaogejiaoyu.com"
	loginCheckURL        = "https://chaogejiaoyu.com/user/index/getLoginUserInfo"
)

var (
	patterns      = []string{`(?:[\w-]+\.)?chaogejiaoyu\.com/`}
	queryIDRe     = regexp.MustCompile(`[?&](?:id|course_id)=(\d+)`)
	myCourseIDRe  = regexp.MustCompile(`/my/course/(\d+)`)
	roomIDRe      = regexp.MustCompile(`/course/room/(\d+)`)
	publicIDRe    = regexp.MustCompile(`/course/(\d+)`)
	ccInfoBlockRe = regexp.MustCompile(`(?s)let\s+ccInfo\s*=\s*\{([\s\S]*?)\}`)
	ccKeyValueRe  = regexp.MustCompile(`(\w+)\s*:\s*['"]([^'"]*)['"]`)
	titleCleanRe  = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
)

func init() {
	extractor.Register(&Chaoge{}, extractor.SiteInfo{Name: "Chaoge", URL: "chaogejiaoyu.com", NeedAuth: true})
}

type Chaoge struct{}

func (s *Chaoge) Patterns() []string { return patterns }

func (s *Chaoge) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("chaoge requires login cookies")
	}
	cid := parseCourseID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("chaoge: cannot parse course id from URL %q", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := baseHeaders(refererURL)
	if err := checkCookie(c, headers); err != nil {
		return nil, err
	}
	detail, title, err := fetchCourseDetail(c, cid, headers)
	if err != nil {
		return nil, err
	}
	items := collectCourseItems(detail, cid)
	items = append(items, fetchCourseFiles(c, headers, cid)...)
	items = append(items, fetchSeriesItems(c, headers, items)...)
	if len(items) == 0 {
		items = []map[string]any{{"id": cid, "course_id": cid, "title": title}}
	}

	seenVideo, seenFile := map[string]bool{}, map[string]bool{}
	var entries []*extractor.MediaInfo
	for _, item := range items {
		if fileEntry := resolveFileEntry(item); fileEntry != nil && !seenFile[fileEntry.Streams["file"].URLs[0]] {
			seenFile[fileEntry.Streams["file"].URLs[0]] = true
			entries = append(entries, fileEntry)
		}
		courseID := firstString(item, "course_id", "id")
		if courseID == "" || seenVideo[courseID] || !shouldTryVideo(item, courseID == cid) {
			continue
		}
		seenVideo[courseID] = true
		entry, err := resolveVideoEntry(c, headers, item, courseID)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("chaoge: no playable csslcloud or file entries found for course %s", cid)
	}
	if title == "" {
		title = "chaoge_" + cid
	}
	return &extractor.MediaInfo{Site: "chaoge", Title: title, Entries: entries}, nil
}

func parseCourseID(raw string) string {
	for _, re := range []*regexp.Regexp{queryIDRe, myCourseIDRe, roomIDRe, publicIDRe} {
		if m := re.FindStringSubmatch(raw); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func baseHeaders(referer string) map[string]string {
	return map[string]string{"Accept": "application/json, text/plain, */*", "Origin": originURL, "Referer": referer, "X-Requested-With": "XMLHttpRequest"}
}

func checkCookie(c *util.Client, headers map[string]string) error {
	body, err := c.GetString(loginCheckURL, headers)
	if err != nil {
		return fmt.Errorf("chaoge login check: %w", err)
	}
	var resp struct {
		Status int            `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return fmt.Errorf("chaoge login check parse: %w", err)
	}
	if resp.Status != 0 || len(resp.Data) == 0 {
		return fmt.Errorf("chaoge login check failed: status=%d", resp.Status)
	}
	return nil
}

func fetchCourseDetail(c *util.Client, cid string, headers map[string]string) (map[string]any, string, error) {
	body, err := c.GetString(fmt.Sprintf(courseDetailURL, url.QueryEscape(cid)), headers)
	if err != nil {
		return nil, "", fmt.Errorf("chaoge course detail: %w", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, "", fmt.Errorf("chaoge course detail parse: %w", err)
	}
	title := firstString(asMap(resp["data"]), "course_name", "title")
	return resp, cleanTitle(title), nil
}

func fetchCourseFiles(c *util.Client, headers map[string]string, cid string) []map[string]any {
	body, err := c.GetString(fmt.Sprintf(courseFileURL, url.QueryEscape(cid)), headers)
	if err != nil {
		return nil
	}
	var resp map[string]any
	if json.Unmarshal([]byte(body), &resp) != nil {
		return nil
	}
	data := asMap(resp["data"])
	var out []map[string]any
	for _, key := range []string{"file_seg_list", "file_list"} {
		out = append(out, listFromData(data[key])...)
	}
	return out
}

func fetchSeriesItems(c *util.Client, headers map[string]string, seeds []map[string]any) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	var queue []string
	for _, item := range seeds {
		if id := firstString(item, "id", "course_id"); id != "" && !seen[id] {
			seen[id], queue = true, append(queue, id)
		}
	}
	for len(queue) > 0 && len(out) < 300 {
		pid := queue[0]
		queue = queue[1:]
		for _, isEnd := range []int{0, 1} {
			for page := 1; page <= 200; page++ {
				body, err := c.GetString(fmt.Sprintf(seriesURL, url.QueryEscape(pid), isEnd, page), headers)
				if err != nil {
					break
				}
				var resp map[string]any
				if json.Unmarshal([]byte(body), &resp) != nil || intValue(resp["status"], -1) != 0 {
					break
				}
				items := listFromData(resp["data"])
				if len(items) == 0 {
					break
				}
				for _, item := range items {
					out = append(out, item)
					if id := firstString(item, "id", "course_id"); id != "" && !seen[id] && looksFolder(item) {
						seen[id], queue = true, append(queue, id)
					}
				}
			}
		}
	}
	return out
}

func resolveVideoEntry(c *util.Client, headers map[string]string, item map[string]any, courseID string) (*extractor.MediaInfo, error) {
	ccInfo, referer, err := parseCCInfo(c, headers, courseID)
	if err != nil {
		return nil, err
	}
	payload := shared.CssLcloudPayload{
		LiveRoomID:  firstNonEmpty(firstString(ccInfo, "liveRoomID", "liveRoomId", "liveid", "liveId", "roomid", "roomId"), firstString(item, "cc_live_id"), queryValue(firstString(item, "zhibo_url"), "liveRoomId", "liveid", "roomid")),
		UserID:      firstString(ccInfo, "userid", "userId", "uid"),
		AccessID:    firstString(ccInfo, "userId", "userid", "accessid", "accessId", "accountId"),
		RecordID:    firstNonEmpty(firstString(ccInfo, "recordId", "recordid", "replayId"), firstString(item, "cc_lubo_record_id")),
		ViewerName:  firstString(ccInfo, "viewername", "viewerName", "userName", "username"),
		ViewerToken: firstString(ccInfo, "viewertoken", "viewerToken", "userToken", "token"),
		Referer:     referer,
	}
	if payload.ViewerToken == "" && payload.UserID != "" && payload.LiveRoomID != "" {
		payload.ViewerToken = payload.UserID + ":" + payload.LiveRoomID
	}
	play, err := shared.CssLcloudResolvePlayInfo(c, payload)
	if err != nil {
		return nil, err
	}
	extra := map[string]any{"course_id": courseID, "cc_info": ccInfo, "source_login_url": csslLoginURL, "source_play_url": csslPlayURL, "source_meta_url": csslMetaURL}
	if manifest, err := rewriteManifestIfNeeded(c, play.VideoURL, referer); err != nil {
		return nil, err
	} else if manifest != "" {
		extra["m3u8_manifest"] = manifest
	}
	title := cleanTitle(firstNonEmpty(firstString(item, "course_name", "title", "name"), courseID))
	return &extractor.MediaInfo{Site: "chaoge", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{play.VideoURL}, Format: pickFormat(play.VideoURL), AudioURL: play.AudioURL, Headers: map[string]string{"Referer": referer}}}, Extra: extra}, nil
}

func resolveFileEntry(item map[string]any) *extractor.MediaInfo {
	fileURL := normalizeURL(firstString(item, "path", "url", "file_url", "file"))
	if fileURL == "" {
		return nil
	}
	title := cleanTitle(firstNonEmpty(firstString(item, "name", "title", "file_name"), fileURL[strings.LastIndex(fileURL, "/")+1:]))
	fmtName := firstNonEmpty(firstString(item, "ext", "suffix", "file_fmt"), fileExt(fileURL), "bin")
	return &extractor.MediaInfo{Site: "chaoge", Title: title, Streams: map[string]extractor.Stream{"file": {Quality: "source", URLs: []string{fileURL}, Format: fmtName, Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"type": "file"}}
}

func parseCCInfo(c *util.Client, headers map[string]string, courseID string) (map[string]any, string, error) {
	referer := fmt.Sprintf(enterCourseURL, url.QueryEscape(courseID))
	body, err := c.GetString(referer, headers)
	if err != nil {
		return nil, referer, fmt.Errorf("chaoge room page: %w", err)
	}
	m := ccInfoBlockRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return nil, referer, fmt.Errorf("chaoge: ccInfo not found for course %s", courseID)
	}
	info := map[string]any{}
	for _, kv := range ccKeyValueRe.FindAllStringSubmatch(m[1], -1) {
		v, _ := url.QueryUnescape(kv[2])
		info[kv[1]] = v
	}
	return info, referer, nil
}

func rewriteManifestIfNeeded(c *util.Client, videoURL, referer string) (string, error) {
	if !strings.Contains(strings.ToLower(videoURL), ".m3u8") {
		return "", nil
	}
	manifest, err := c.GetString(videoURL, map[string]string{"Referer": referer})
	if err != nil {
		return "", fmt.Errorf("chaoge csslcloud m3u8 fetch: %w", err)
	}
	if !strings.Contains(manifest, "#EXT-X-KEY") {
		return manifest, nil
	}
	return shared.CssLcloudRewriteM3U8Keys(c, manifest, referer)
}

func collectCourseItems(root map[string]any, cid string) []map[string]any {
	var out []map[string]any
	walkAny(root, func(m map[string]any) {
		if firstString(m, "id", "course_id") != "" || hasAny(m, "cc_live_id", "cc_lubo_record_id", "zhibo_url", "file_url", "path") {
			out = append(out, m)
		}
	})
	return append([]map[string]any{{"id": cid, "course_id": cid}}, out...)
}

func walkAny(v any, visit func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		visit(x)
		for _, c := range x {
			walkAny(c, visit)
		}
	case []any:
		for _, c := range x {
			walkAny(c, visit)
		}
	}
}
func listFromData(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	data := asMap(v)
	for _, k := range []string{"course_list", "file_list", "list", "dataList", "rows", "items"} {
		if arr, ok := data[k].([]any); ok {
			return listFromData(arr)
		}
	}
	return nil
}
func shouldTryVideo(m map[string]any, fallback bool) bool {
	return fallback || hasAny(m, "zhibo_url", "cc_live_id", "cc_lubo_record_id") || in(firstString(m, "room_type"), "10", "11") || in(firstString(m, "is_zhiboing"), "1", "2")
}
func looksFolder(m map[string]any) bool {
	return in(firstString(m, "group_status"), "3", "4") || len(listFromData(m)) > 0
}
func hasAny(m map[string]any, keys ...string) bool { return firstString(m, keys...) != "" }
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(fmt.Sprint(m[k])); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func intValue(v any, def int) int {
	var i int
	if _, err := fmt.Sscan(fmt.Sprint(v), &i); err == nil {
		return i
	}
	return def
}
func in(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}
func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		return strings.TrimRight(refererURL, "/") + u
	}
	return u
}
func fileExt(u string) string {
	p := strings.Split(strings.SplitN(u, "?", 2)[0], ".")
	if len(p) > 1 {
		return strings.ToLower(p[len(p)-1])
	}
	return ""
}
func queryValue(raw string, keys ...string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
}
