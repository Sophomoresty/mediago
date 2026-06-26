// Package aishangke implements an extractor for loveshangke.com courses.
package aishangke

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
	courseListURL        = "https://loveshangke.com/user/index/getMyCourseListAjax"
	courseDetailURL      = "https://loveshangke.com/course/index/getCourseDetailAjax?id=%s"
	seriesURL            = "https://loveshangke.com/course/index/getMultipleSeriesCourseListAjax?pid=%s&is_end=%d&page=%d&tid=0&sid=0"
	enterCourseURL       = "https://loveshangke.com/course/index/enterCourse?course_id=%s"
	publicCourseURL      = "https://loveshangke.com/course/%s"
	publicGroupCourseURL = "https://loveshangke.com/course/g%s"
	csslLoginURL         = "https://view.csslcloud.net/replay/user/login"
	csslPlayURL          = "https://view.csslcloud.net/replay/video/play"
	csslOrigin           = "https://view.csslcloud.net"
	refererURL           = "https://loveshangke.com/"
	originURL            = "https://loveshangke.com"
	loginCheckURL        = "https://loveshangke.com/user/index/getLoginUserInfo"
)

var (
	patterns      = []string{`(?:[\w-]+\.)?loveshangke\.com/`}
	viewIDRe      = regexp.MustCompile(`[?&]id=(\d+)`)
	enterIDRe     = regexp.MustCompile(`[?&]course_id=(\d+)`)
	publicIDRe    = regexp.MustCompile(`/course/(?:g)?(\d+)`)
	ccInfoBlockRe = regexp.MustCompile(`(?s)let\s+ccInfo\s*=\s*\{([\s\S]*?)\}`)
	ccKeyValueRe  = regexp.MustCompile(`(\w+)\s*:\s*['"]([^'"]*)['"]`)
	titleCleanRe  = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
)

func init() {
	extractor.Register(&Aishangke{}, extractor.SiteInfo{Name: "Aishangke", URL: "loveshangke.com", NeedAuth: true})
}

type Aishangke struct{}

func (s *Aishangke) Patterns() []string { return patterns }

func (s *Aishangke) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("aishangke requires login cookies")
	}
	cid := parseCourseID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("aishangke: cannot parse course id from URL %q", rawURL)
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
	items = append(items, fetchSeriesItems(c, headers, items)...)
	if len(items) == 0 {
		items = []map[string]any{{"id": cid, "course_id": cid, "title": title}}
	}

	seen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for _, item := range items {
		courseID := firstString(item, "course_id", "id")
		if courseID == "" || seen[courseID] || !shouldTryVideo(item, courseID == cid) {
			continue
		}
		seen[courseID] = true
		entry, err := resolveEntry(c, headers, item, courseID)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("aishangke: no playable csslcloud entries found for course %s", cid)
	}
	if title == "" {
		title = "aishangke_" + cid
	}
	return &extractor.MediaInfo{Site: "aishangke", Title: title, Entries: entries}, nil
}

func parseCourseID(raw string) string {
	for _, re := range []*regexp.Regexp{enterIDRe, viewIDRe, publicIDRe} {
		if m := re.FindStringSubmatch(raw); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func baseHeaders(referer string) map[string]string {
	return map[string]string{
		"Accept":           "application/json, text/plain, */*",
		"Origin":           originURL,
		"Referer":          referer,
		"X-Requested-With": "XMLHttpRequest",
	}
}

func checkCookie(c *util.Client, headers map[string]string) error {
	body, err := c.GetString(loginCheckURL, headers)
	if err != nil {
		return fmt.Errorf("aishangke login check: %w", err)
	}
	var resp struct {
		Status int            `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return fmt.Errorf("aishangke login check parse: %w", err)
	}
	if resp.Status != 0 || len(resp.Data) == 0 {
		return fmt.Errorf("aishangke login check failed: status=%d", resp.Status)
	}
	return nil
}

func fetchCourseDetail(c *util.Client, cid string, headers map[string]string) (map[string]any, string, error) {
	body, err := c.GetString(fmt.Sprintf(courseDetailURL, url.QueryEscape(cid)), headers)
	if err != nil {
		return nil, "", fmt.Errorf("aishangke course detail: %w", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, "", fmt.Errorf("aishangke course detail parse: %w", err)
	}
	title := firstString(asMap(resp["data"]), "course_name", "title")
	return resp, cleanTitle(title), nil
}

func fetchSeriesItems(c *util.Client, headers map[string]string, seeds []map[string]any) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	queue := make([]string, 0, len(seeds))
	for _, item := range seeds {
		if id := firstString(item, "id", "course_id"); id != "" && !seen[id] {
			seen[id], queue = true, append(queue, id)
		}
	}
	for len(queue) > 0 && len(out) < 200 {
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

func resolveEntry(c *util.Client, headers map[string]string, item map[string]any, courseID string) (*extractor.MediaInfo, error) {
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
	extra := map[string]any{"course_id": courseID, "cc_info": ccInfo, "source_login_url": csslLoginURL, "source_play_url": csslPlayURL}
	if manifest, err := rewriteManifestIfNeeded(c, play.VideoURL, referer); err != nil {
		return nil, err
	} else if manifest != "" {
		extra["m3u8_manifest"] = manifest
	}
	title := cleanTitle(firstNonEmpty(firstString(item, "course_name", "title", "name"), courseID))
	return &extractor.MediaInfo{
		Site:  "aishangke",
		Title: title,
		Streams: map[string]extractor.Stream{"default": {
			Quality:  "best",
			URLs:     []string{play.VideoURL},
			Format:   pickFormat(play.VideoURL),
			AudioURL: play.AudioURL,
			Headers:  map[string]string{"Referer": referer},
		}},
		Extra: extra,
	}, nil
}

func parseCCInfo(c *util.Client, headers map[string]string, courseID string) (map[string]any, string, error) {
	referer := fmt.Sprintf(enterCourseURL, url.QueryEscape(courseID))
	body, err := c.GetString(referer, headers)
	if err != nil {
		return nil, referer, fmt.Errorf("aishangke enter course: %w", err)
	}
	m := ccInfoBlockRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return nil, referer, fmt.Errorf("aishangke: ccInfo not found for course %s", courseID)
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
		return "", fmt.Errorf("aishangke csslcloud m3u8 fetch: %w", err)
	}
	if !strings.Contains(manifest, "#EXT-X-KEY") {
		return manifest, nil
	}
	return shared.CssLcloudRewriteM3U8Keys(c, manifest, referer)
}

func collectCourseItems(root map[string]any, cid string) []map[string]any {
	var out []map[string]any
	walkAny(root, map[any]bool{}, func(m map[string]any) {
		if firstString(m, "id", "course_id") != "" || hasAny(m, "cc_live_id", "cc_lubo_record_id", "zhibo_url") {
			out = append(out, m)
		}
	})
	return append([]map[string]any{{"id": cid, "course_id": cid}}, out...)
}

func walkAny(v any, seen map[any]bool, visit func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		visit(x)
		for _, child := range x {
			walkAny(child, seen, visit)
		}
	case []any:
		for _, child := range x {
			walkAny(child, seen, visit)
		}
	}
}

func listFromData(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			if m := asMap(item); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	data := asMap(v)
	for _, key := range []string{"course_list", "list", "dataList", "rows", "items"} {
		if arr, ok := data[key].([]any); ok {
			out := make([]map[string]any, 0, len(arr))
			for _, item := range arr {
				if m := asMap(item); len(m) > 0 {
					out = append(out, m)
				}
			}
			return out
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
