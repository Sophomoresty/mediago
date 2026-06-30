// Package aishangke implements an extractor for loveshangke.com courses.
package aishangke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
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
	viewMyCourseURL      = "https://loveshangke.com/course/index/viewMyCourse?id=%s"
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
	viewCourseRe  = regexp.MustCompile(`/course/index/viewMyCourse\?[^#]*[?&]?id=(\d+)`)
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
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := baseHeaders(refererURL)
	if err := checkCookie(c, headers); err != nil {
		return nil, err
	}

	cid := parseCourseID(rawURL)
	enteredCourseID, parentCourseID := resolveEnteredCourse(c, headers, rawURL, cid)
	if parentCourseID != "" {
		cid = parentCourseID
	}
	var selected map[string]any
	if cid == "" {
		courses := fetchCourseList(c, headers)
		if len(courses) == 0 {
			return nil, fmt.Errorf("aishangke: cannot parse course id from URL %q and course list is empty", rawURL)
		}
		selected = courses[0]
		cid = firstString(selected, "course_id", "id")
	}
	if cid == "" {
		return nil, fmt.Errorf("aishangke: selected course has empty id")
	}

	detail, title, err := fetchCourseDetail(c, cid, headers)
	if err != nil {
		return nil, err
	}
	items := collectCourseItems(detail, cid)
	if selected != nil {
		items = append([]map[string]any{selected}, items...)
	}
	items = append(items, fetchSeriesItems(c, headers, items)...)
	if enteredCourseID != "" && enteredCourseID != cid {
		items = filterOrAppendEnteredCourse(c, headers, items, enteredCourseID)
	}
	if len(items) == 0 {
		items = []map[string]any{{"id": cid, "course_id": cid, "title": title}}
	}

	seen := map[string]bool{}
	fileSeen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for idx, item := range items {
		entries = append(entries, fileEntriesFromCourseItem(item, idx+1, fileSeen)...)
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
	for _, re := range []*regexp.Regexp{viewCourseRe, enterIDRe, viewIDRe, publicIDRe} {
		if m := re.FindStringSubmatch(raw); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func resolveEnteredCourse(c *util.Client, headers map[string]string, rawURL, parsedID string) (string, string) {
	enteredID := ""
	if enterIDRe.MatchString(rawURL) {
		enteredID = parsedID
	}
	if enteredID == "" {
		return "", ""
	}
	body, err := c.GetString(fmt.Sprintf(enterCourseURL, url.QueryEscape(enteredID)), headers)
	if err != nil {
		return enteredID, ""
	}
	parentID := firstNonEmpty(
		matchFirst(body, `/course/index/viewMyCourse\?id=(\d+)`),
		matchFirst(body, `course_pid=(\d+)`),
		matchFirst(body, `[?&]pid=(\d+)`),
	)
	if parentID == enteredID {
		parentID = ""
	}
	return enteredID, parentID
}

func filterOrAppendEnteredCourse(c *util.Client, headers map[string]string, items []map[string]any, enteredID string) []map[string]any {
	if enteredID == "" {
		return items
	}
	matches := make([]map[string]any, 0, 1)
	for _, item := range items {
		if firstString(item, "id", "course_id") == enteredID {
			matches = append(matches, item)
		}
	}
	if len(matches) > 0 {
		return matches
	}
	detail, title, err := fetchCourseDetail(c, enteredID, headers)
	if err == nil && len(detail) > 0 {
		out := collectCourseItems(detail, enteredID)
		if len(out) > 0 {
			return out
		}
		if title != "" {
			return []map[string]any{{"id": enteredID, "course_id": enteredID, "title": title}}
		}
	}
	return []map[string]any{{"id": enteredID, "course_id": enteredID}}
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
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return fmt.Errorf("aishangke login check parse: %w", err)
	}
	var resp struct {
		Status  int            `json:"status"`
		Code    int            `json:"code"`
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return fmt.Errorf("aishangke login check typed parse: %w", err)
	}
	_, hasStatus := raw["status"]
	_, hasCode := raw["code"]
	ok := resp.Success || (hasStatus && resp.Status == 0) || (hasCode && (resp.Code == 0 || resp.Code == 1))
	if !ok {
		return fmt.Errorf("aishangke login check failed: status=%d code=%d", resp.Status, resp.Code)
	}
	return nil
}

func fetchCourseList(c *util.Client, headers map[string]string) []map[string]any {
	body, err := c.GetString(courseListURL, headers)
	if err != nil {
		return nil
	}
	var resp map[string]any
	if json.Unmarshal([]byte(body), &resp) != nil {
		return nil
	}
	items := listFromData(resp["data"])
	if len(items) == 0 {
		walkAny(resp, func(m map[string]any) {
			if firstString(m, "course_id", "id") != "" && firstString(m, "course_name", "title", "name") != "" {
				items = append(items, m)
			}
		})
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		id := firstString(item, "course_id", "id")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if item["course_id"] == nil {
			item["course_id"] = id
		}
		if item["title"] == nil {
			item["title"] = firstString(item, "course_name", "name")
		}
		out = append(out, item)
	}
	return out
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
	if direct := normalizeMediaURL(firstString(item, "play_url", "playUrl", "video_url", "videoUrl", "primary", "secondary", "url")); direct != "" {
		title := cleanTitle(firstNonEmpty(firstString(item, "course_name", "title", "name"), courseID))
		return &extractor.MediaInfo{
			Site:  "aishangke",
			Title: title,
			Streams: map[string]extractor.Stream{"default": {
				Quality: "source",
				URLs:    []string{direct},
				Format:  pickFormat(direct),
				Headers: map[string]string{"Referer": refererURL},
			}},
			Extra: map[string]any{"course_id": courseID, "source_type": "direct_url"},
		}, nil
	}
	ccInfo, referer, err := parseCCInfo(c, headers, courseID)
	if err != nil {
		return nil, err
	}
	if src, err := resolveCSSLReplay(c, ccInfo, item, courseID); err == nil && src.URL != "" {
		extra := map[string]any{"course_id": courseID, "cc_info": ccInfo, "source_login_url": csslLoginURL, "source_play_url": csslPlayURL, "source_type": "csslcloud_replay"}
		if manifest, err := rewriteManifestIfNeeded(c, src.URL, src.Referer); err != nil {
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
				URLs:     []string{src.URL},
				Format:   pickFormat(src.URL),
				AudioURL: src.AudioURL,
				Headers:  map[string]string{"Referer": src.Referer},
			}},
			Extra: extra,
		}, nil
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

type csslReplaySource struct {
	URL, AudioURL, Referer string
}

func resolveCSSLReplay(c *util.Client, ccInfo, item map[string]any, courseID string) (csslReplaySource, error) {
	accountID := firstString(ccInfo, "userId", "userid", "accountId", "accountid", "accessid")
	replayID := firstNonEmpty(firstString(ccInfo, "recordId", "recordid", "replayId"), firstString(item, "cc_lubo_record_id"))
	userName := firstNonEmpty(firstString(ccInfo, "viewername", "viewerName", "userName", "username"), courseID)
	if accountID == "" || replayID == "" || userName == "" {
		return csslReplaySource{}, fmt.Errorf("aishangke csslcloud: missing accountId/replayId/userName")
	}
	loginHeaders := map[string]string{
		"Referer":      csslOrigin + "/",
		"Origin":       csslOrigin,
		"Content-Type": "application/json;charset=UTF-8",
		"Accept":       "application/json, text/plain, */*",
	}
	loginPayload := map[string]any{
		"tpl":           20,
		"userName":      userName,
		"deviceVersion": "3.21.0",
		"deviceType":    "h5-pc",
		"replayId":      replayID,
		"accountId":     accountID,
	}
	if token := firstString(ccInfo, "viewertoken", "viewerToken", "userToken", "token"); token != "" {
		loginPayload["userToken"] = token
	}
	loginResp, err := postJSONMap(c, csslLoginURL, loginPayload, loginHeaders)
	if err != nil {
		return csslReplaySource{}, err
	}
	hdToken := findFirstString(loginResp, "token", "sessionid", "sessionId", "session_id")
	if hdToken == "" {
		return csslReplaySource{}, fmt.Errorf("aishangke csslcloud login: empty token")
	}
	playHeaders := map[string]string{
		"Referer":    csslOrigin + "/",
		"Origin":     csslOrigin,
		"Accept":     "application/json, text/plain, */*",
		"X-HD-Token": hdToken,
	}
	playURL := csslPlayURL + "?" + url.Values{
		"tpl":           {"20"},
		"terminal":      {"3"},
		"deviceVersion": {"3.21.0"},
		"deviceType":    {"h5-pc"},
		"replayId":      {replayID},
		"accountId":     {accountID},
	}.Encode()
	body, err := c.GetString(playURL, playHeaders)
	if err != nil {
		return csslReplaySource{}, fmt.Errorf("aishangke csslcloud play: %w", err)
	}
	var playResp any
	if err := json.Unmarshal([]byte(body), &playResp); err != nil {
		return csslReplaySource{}, fmt.Errorf("aishangke csslcloud play parse: %w", err)
	}
	mediaURL := pickCSSLVideoURL(playResp)
	if mediaURL == "" {
		return csslReplaySource{}, fmt.Errorf("aishangke csslcloud play: no media URL")
	}
	return csslReplaySource{URL: normalizeMediaURL(mediaURL), AudioURL: pickCSSLAudioURL(playResp), Referer: csslOrigin + "/"}, nil
}

func postJSONMap(c *util.Client, api string, payload map[string]any, headers map[string]string) (map[string]any, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Post(api, bytes.NewReader(b), headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
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
	walkAny(root, func(m map[string]any) {
		if firstString(m, "id", "course_id") != "" || hasAny(m, "cc_live_id", "cc_lubo_record_id", "zhibo_url") {
			out = append(out, m)
		}
	})
	return append([]map[string]any{{"id": cid, "course_id": cid}}, out...)
}

func walkAny(v any, visit func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		visit(x)
		for _, child := range x {
			walkAny(child, visit)
		}
	case []any:
		for _, child := range x {
			walkAny(child, visit)
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
	if strings.Contains(strings.ToLower(u), ".mp3") || strings.Contains(strings.ToLower(u), ".m4a") || strings.Contains(strings.ToLower(u), ".aac") || strings.Contains(strings.ToLower(u), ".wav") {
		return "mp3"
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

func matchFirst(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func fileEntriesFromCourseItem(item map[string]any, index int, seen map[string]bool) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for i, file := range iterFileItems(item) {
		fileURL := normalizeAnyURL(firstNonEmpty(firstString(file, "path", "url", "file_url", "file"), firstString(file, "fileUrl", "downloadUrl", "filePath")))
		if fileURL == "" {
			continue
		}
		name := firstNonEmpty(firstString(file, "name", "title", "file_name", "fileName"), fileNameFromURL(fileURL), "资料")
		ext := firstNonEmpty(strings.TrimPrefix(strings.ToLower(firstString(file, "ext", "suffix", "file_fmt", "format")), "."), extFromURL(fileURL, "pdf"))
		key := fileURL + "\x00" + name + "\x00" + ext
		if seen[key] {
			continue
		}
		seen[key] = true
		title := cleanTitle(fmt.Sprintf("(%d.%d)--%s", index, i+1, stripExt(name, ext)))
		out = append(out, &extractor.MediaInfo{
			Site:  "aishangke",
			Title: title,
			Streams: map[string]extractor.Stream{"file": {
				Quality: "file",
				URLs:    []string{fileURL},
				Format:  ext,
				Headers: map[string]string{"Referer": refererURL},
			}},
			Extra: map[string]any{"type": "file", "course_id": firstString(item, "course_id", "id")},
		})
	}
	return out
}

func iterFileItems(course map[string]any) []map[string]any {
	var raws []any
	for _, k := range []string{"file", "file_json"} {
		v := course[k]
		if s, ok := v.(string); ok {
			var decoded any
			if json.Unmarshal([]byte(s), &decoded) == nil {
				v = decoded
			}
		}
		switch x := v.(type) {
		case []any:
			raws = append(raws, x...)
		case map[string]any:
			raws = append(raws, x)
		}
	}
	out := make([]map[string]any, 0, len(raws))
	seen := map[string]bool{}
	for _, raw := range raws {
		m := asMap(raw)
		if len(m) == 0 {
			continue
		}
		u := firstNonEmpty(firstString(m, "path", "url", "file_url", "file"), firstString(m, "fileUrl", "downloadUrl", "filePath"))
		n := firstString(m, "name", "title", "file_name", "fileName")
		key := u + "\x00" + n
		if key == "\x00" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}

func normalizeMediaURL(raw string) string {
	u := normalizeAnyURL(raw)
	if u == "" {
		return ""
	}
	lu := strings.ToLower(u)
	for _, ext := range []string{".m3u8", ".mp4", ".m4v", ".mov", ".flv", ".mp3", ".m4a", ".aac", ".wav"} {
		if strings.Contains(lu, ext) {
			return u
		}
	}
	return ""
}

func normalizeAnyURL(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, `"'`))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		base, err := url.Parse(refererURL)
		if err != nil {
			return ""
		}
		ref, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return base.ResolveReference(ref).String()
	}
	return raw
}

func findFirstString(v any, keys ...string) string {
	var found string
	walkAny(v, func(m map[string]any) {
		if found == "" {
			found = firstString(m, keys...)
		}
	})
	return found
}

func pickCSSLVideoURL(v any) string {
	var candidates []map[string]any
	walkAny(v, func(m map[string]any) {
		u := firstNonEmpty(firstString(m, "primary", "secondary"), firstString(m, "url", "play_url", "playUrl", "video_url", "videoUrl"))
		if normalizeMediaURL(u) != "" {
			cp := map[string]any{}
			for k, val := range m {
				cp[k] = val
			}
			cp["_url"] = u
			candidates = append(candidates, cp)
		}
	})
	if len(candidates) == 0 {
		return ""
	}
	sortCSSL(candidates)
	return firstString(candidates[0], "_url")
}

func pickCSSLAudioURL(v any) string {
	var out string
	walkAny(v, func(m map[string]any) {
		if out != "" {
			return
		}
		for _, k := range []string{"audio_url", "audioUrl", "audio", "mp3"} {
			if u := normalizeMediaURL(firstString(m, k)); u != "" {
				out = u
				return
			}
		}
	})
	return out
}

func sortCSSL(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return qualityKey(items[i]) > qualityKey(items[j])
	})
}

func qualityKey(m map[string]any) int {
	text := strings.ToUpper(firstString(m, "desc", "qualityDesc", "code", "quality", "definition"))
	switch {
	case strings.Contains(text, "4K") || strings.Contains(text, "1080") || strings.Contains(text, "FHD") || strings.Contains(text, "原画") || strings.Contains(text, "蓝光"):
		return 400
	case strings.Contains(text, "超清"):
		return 320
	case strings.Contains(text, "720") || strings.Contains(text, "HD") || strings.Contains(text, "高清"):
		return 240
	case strings.Contains(text, "480") || strings.Contains(text, "360") || strings.Contains(text, "SD") || strings.Contains(text, "标清") || strings.Contains(text, "流畅"):
		return 160
	default:
		return intValue(firstString(m, "code", "quality"), 0)
	}
}

func fileNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	name := u.Path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if dec, err := url.QueryUnescape(name); err == nil {
		name = dec
	}
	return strings.TrimSpace(name)
}

func extFromURL(rawURL, def string) string {
	pathPart := strings.Split(strings.Split(rawURL, "?")[0], "#")[0]
	if idx := strings.LastIndex(pathPart, "."); idx >= 0 && idx+1 < len(pathPart) {
		ext := strings.ToLower(pathPart[idx+1:])
		if matched, _ := regexp.MatchString(`^[a-z0-9]{1,8}$`, ext); matched {
			return ext
		}
	}
	return def
}

func stripExt(name, ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext != "" && strings.HasSuffix(strings.ToLower(name), "."+ext) {
		return name[:len(name)-len(ext)-1]
	}
	return name
}
