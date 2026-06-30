// Package chaoge implements an extractor for chaogejiaoyu.com courses.
package chaoge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
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
	myCoursePageLimit    = 200
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
	htmlCommentRe = regexp.MustCompile(`(?s)<!--[\s\S]*?-->`)
	priceRes      = []*regexp.Regexp{
		regexp.MustCompile(`(?i)<span>\s*RMB\s*</span>\s*<span>\s*([0-9]+(?:\.[0-9]+)?)\s*</span>`),
		regexp.MustCompile(`(?i)price_disc['"]?\s*[:=]\s*['"]?([0-9]+(?:\.[0-9]+)?)`),
		regexp.MustCompile(`(?i)price['"]?\s*[:=]\s*['"]?([0-9]+(?:\.[0-9]+)?)`),
	}
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
	myCourses := fetchMyCourseItems(c, headers, "0")
	detail, title, err := fetchCourseDetail(c, cid, headers)
	if err != nil {
		return nil, err
	}
	courseInfo := selectedCourseInfo(detail, cid)
	items := collectCourseItems(detail, cid)
	items = append(items, findCourseListMatches(myCourses, cid)...)
	items = append(items, fetchMyCourseItems(c, headers, cid)...)
	items = append(items, fetchCourseFiles(c, headers, cid)...)
	items = append(items, fetchSeriesItems(c, headers, items)...)
	if len(items) == 0 {
		items = []map[string]any{{"id": cid, "course_id": cid, "title": title}}
	}

	seenVideo, seenFile := map[string]bool{}, map[string]bool{}
	var entries []*extractor.MediaInfo
	for _, item := range items {
		if fileEntry := resolveFileEntry(item); fileEntry != nil {
			if fileURL := firstEntryURL(fileEntry); fileURL != "" && !seenFile[fileURL] {
				seenFile[fileURL] = true
				entries = append(entries, fileEntry)
			}
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
	extra := map[string]any{"course_id": cid, "purchased": coursePurchased(courseInfo, myCourses, cid)}
	if price, ok := fetchPublicPrice(c, headers, cid, courseInfo); ok {
		extra["price"] = price
	}
	return &extractor.MediaInfo{Site: "chaoge", Title: title, Chapters: chaogeChapters(items), Entries: entries, Extra: extra}, nil
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
	if resp.Status != 0 {
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
	title := courseTitle(resp)
	return resp, cleanTitle(title), nil
}

func fetchMyCourseItems(c *util.Client, headers map[string]string, coursePID string) []map[string]any {
	if strings.TrimSpace(coursePID) == "" {
		coursePID = "0"
	}
	var out []map[string]any
	seenPage, seenCourse := map[string]bool{}, map[string]bool{}
	page, pageLimit := 1, 1
	for page <= pageLimit && page <= myCoursePageLimit {
		u, err := url.Parse(courseListURL)
		if err != nil {
			return out
		}
		q := u.Query()
		q.Set("course_pid", coursePID)
		q.Set("is_follow", "")
		q.Set("page", fmt.Sprint(page))
		q.Set("is_delete", "0")
		q.Set("exam_type", "")
		u.RawQuery = q.Encode()
		body, err := c.GetString(u.String(), headers)
		if err != nil {
			return out
		}
		var resp map[string]any
		if json.Unmarshal([]byte(body), &resp) != nil || intValue(resp["status"], 0) != 0 {
			return out
		}
		items := parseCourseListItems(resp)
		if len(items) == 0 {
			return out
		}
		var pageKey strings.Builder
		for _, it := range items {
			id := firstString(it, "course_id", "id")
			pageKey.WriteString(id)
			pageKey.WriteByte('|')
			if id == "" || seenCourse[id] {
				continue
			}
			seenCourse[id] = true
			out = append(out, it)
		}
		if seenPage[pageKey.String()] {
			break
		}
		seenPage[pageKey.String()] = true
		if data := asMap(resp["data"]); len(data) > 0 {
			pageLimit = minInt(myCoursePageLimit, maxInt(pageLimit, pageLimitFromString(firstNonEmpty(firstString(data, "pageStr"), firstString(data, "page_str")), page)))
		}
		page++
	}
	return out
}

func parseCourseListItems(resp map[string]any) []map[string]any {
	var out []map[string]any
	for _, raw := range listFromData(resp["data"]) {
		if truthy(raw["is_expired"]) {
			continue
		}
		cid := firstString(raw, "course_id", "id")
		title := firstString(raw, "course_name", "title", "name")
		if cid == "" || title == "" {
			continue
		}
		out = append(out, map[string]any{
			"raw":                raw,
			"course_id":          cid,
			"id":                 cid,
			"title":              title,
			"course_name":        title,
			"group_status":       firstString(raw, "group_status"),
			"course_pid":         firstString(raw, "course_pid", "pid", "parent_id"),
			"course_period_disc": firstString(raw, "course_period_disc"),
			"valid_off_time":     firstString(raw, "valid_off_time"),
		})
	}
	return out
}

func findCourseListMatches(items []map[string]any, cid string) []map[string]any {
	var out []map[string]any
	for _, it := range items {
		if firstString(it, "course_id", "id") == cid || firstString(it, "course_pid", "pid", "parent_id") == cid {
			out = append(out, it)
		}
	}
	return out
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
	var out []map[string]any
	if arr := listFromData(resp["data"]); len(arr) > 0 {
		out = append(out, arr...)
	}
	data := asMap(resp["data"])
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
		play, err = legacyCssLcloudResolvePlayInfo(c, payload)
		if err != nil {
			return nil, err
		}
	}
	extra := map[string]any{"course_id": courseID, "cc_info": ccInfo, "source_login_url": csslLoginURL, "source_play_url": csslPlayURL, "source_meta_url": csslMetaURL}
	if manifest, err := rewriteManifestIfNeeded(c, play.VideoURL, referer); err != nil {
		return nil, err
	} else if manifest != "" {
		extra["m3u8_manifest"] = manifest
	}
	title := cleanTitle(firstNonEmpty(firstString(item, "course_name", "title", "name"), courseID))
	return &extractor.MediaInfo{Site: "chaoge", Title: title, Streams: csslStreams(play, referer), Extra: extra}, nil
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

func firstEntryURL(info *extractor.MediaInfo) string {
	if info == nil {
		return ""
	}
	for _, stream := range info.Streams {
		for _, raw := range stream.URLs {
			if u := strings.TrimSpace(raw); u != "" {
				return u
			}
		}
	}
	return ""
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

func legacyCssLcloudResolvePlayInfo(c *util.Client, p shared.CssLcloudPayload) (*shared.CssLcloudPlayInfo, error) {
	accountID := firstNonEmpty(p.AccessID, p.UserID)
	recordID := p.RecordID
	if accountID == "" || recordID == "" {
		return nil, fmt.Errorf("csslcloud legacy: missing accountId/replayId")
	}
	loginPayload := map[string]any{
		"tpl":           20,
		"userName":      firstNonEmpty(p.ViewerName, p.UserID, p.LiveRoomID, accountID),
		"deviceVersion": "3.21.0",
		"deviceType":    "h5-pc",
		"replayId":      recordID,
		"accountId":     accountID,
	}
	if p.ViewerToken != "" {
		loginPayload["userToken"] = p.ViewerToken
	}
	body, _ := json.Marshal(loginPayload)
	headers := map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Content-Type": "application/json;charset=UTF-8",
		"Origin":       csslOrigin,
		"Referer":      csslOrigin + "/",
	}
	resp, err := c.Post(csslLoginURL, bytes.NewReader(body), headers)
	if err != nil {
		return nil, fmt.Errorf("csslcloud legacy login: %w", err)
	}
	defer resp.Body.Close()
	rawLogin, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("csslcloud legacy login read: %w", err)
	}
	var login map[string]any
	if err := json.Unmarshal(rawLogin, &login); err != nil {
		return nil, fmt.Errorf("csslcloud legacy login parse: %w", err)
	}
	token := firstNonEmpty(
		firstString(asMap(asMap(login["data"])["user"]), "token"),
		firstString(asMap(login["data"]), "token"),
		firstString(asMap(login["datas"]), "token", "sessionId"),
	)
	if token == "" {
		return nil, fmt.Errorf("csslcloud legacy login: empty token")
	}
	playHeaders := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Origin":     csslOrigin,
		"Referer":    csslOrigin + "/",
		"X-HD-Token": token,
	}
	u, err := url.Parse(csslPlayURL)
	if err != nil {
		return nil, fmt.Errorf("csslcloud legacy play url parse: %w", err)
	}
	q := u.Query()
	q.Set("tpl", "20")
	q.Set("terminal", "3")
	q.Set("deviceVersion", "3.21.0")
	q.Set("deviceType", "h5-pc")
	q.Set("replayId", recordID)
	q.Set("accountId", accountID)
	u.RawQuery = q.Encode()
	playBody, err := c.GetString(u.String(), playHeaders)
	if err != nil {
		return nil, fmt.Errorf("csslcloud legacy play: %w", err)
	}
	var playRoot map[string]any
	if err := json.Unmarshal([]byte(playBody), &playRoot); err != nil {
		return nil, fmt.Errorf("csslcloud legacy play parse: %w", err)
	}
	videos := legacyCssLcloudVideos(playRoot)
	if len(videos) == 0 {
		return nil, fmt.Errorf("csslcloud legacy play: no video streams")
	}
	best := videos[0]
	for _, v := range videos[1:] {
		if v.URL != "" && v.Definition > best.Definition {
			best = v
		}
	}
	return &shared.CssLcloudPlayInfo{SessionID: token, VideoURL: best.URL, VideoList: videos}, nil
}

func legacyCssLcloudVideos(root map[string]any) []shared.CssLcloudStreamInfo {
	var out []shared.CssLcloudStreamInfo
	walkAny(root, func(m map[string]any) {
		u := firstNonEmpty(firstString(m, "primary", "url", "playUrl", "play_url", "videoUrl", "video_url"), firstString(m, "secondary"))
		if u == "" {
			return
		}
		out = append(out, shared.CssLcloudStreamInfo{URL: normalizeURL(u), Definition: intValue(firstNonEmpty(firstString(m, "definition"), firstString(m, "quality"), firstString(m, "code")), 0)})
	})
	return out
}

func collectCourseItems(root map[string]any, cid string) []map[string]any {
	var out []map[string]any
	walkAny(root, func(m map[string]any) {
		if firstString(m, "id", "course_id") != "" || hasAny(m, "cc_live_id", "cc_lubo_record_id", "zhibo_url", "file_url", "path") {
			out = append(out, m)
		}
		out = append(out, embeddedFileItems(m)...)
	})
	return append([]map[string]any{{"id": cid, "course_id": cid}}, out...)
}

func courseTitle(resp map[string]any) string {
	data := asMap(resp["data"])
	for _, m := range []map[string]any{data, asMap(data["course_info"]), asMap(data["course"]), asMap(data["info"])} {
		if title := firstString(m, "course_name", "title", "name"); title != "" {
			return title
		}
	}
	return ""
}

func selectedCourseInfo(detail map[string]any, cid string) map[string]any {
	data := asMap(detail["data"])
	candidates := []map[string]any{data, asMap(data["course_info"]), asMap(data["course"]), asMap(data["info"])}
	var first map[string]any
	for _, m := range candidates {
		if len(m) == 0 {
			continue
		}
		if len(first) == 0 {
			first = m
		}
		if cid != "" && in(firstString(m, "course_id", "id"), cid) {
			return m
		}
	}
	var matched map[string]any
	walkAny(detail, func(m map[string]any) {
		if len(matched) == 0 && cid != "" && in(firstString(m, "course_id", "id"), cid) {
			matched = m
		}
	})
	if len(matched) > 0 {
		return matched
	}
	return first
}

func coursePurchased(course map[string]any, myCourses []map[string]any, cid string) bool {
	for _, key := range []string{"is_bought", "is_buy", "isBuy", "is_paid", "isPaid", "purchased", "has_buy"} {
		if truthy(course[key]) {
			return true
		}
	}
	for _, it := range myCourses {
		if firstString(it, "course_id", "id") == cid || firstString(it, "course_pid", "pid", "parent_id") == cid {
			return true
		}
	}
	return false
}

func fetchPublicPrice(c *util.Client, headers map[string]string, cid string, course map[string]any) (float64, bool) {
	if price, ok := courseInfoPrice(course); ok {
		return price, true
	}
	if cid == "" {
		return 0, false
	}
	body, err := c.GetString(fmt.Sprintf(publicCourseURL, url.QueryEscape(cid)), headers)
	if err != nil {
		return 0, false
	}
	body = htmlCommentRe.ReplaceAllString(body, "")
	for _, re := range priceRes {
		m := re.FindStringSubmatch(body)
		if len(m) < 2 {
			continue
		}
		price := parsePrice(m[1])
		if price > 0 {
			return price, true
		}
	}
	return 0, false
}

func courseInfoPrice(course map[string]any) (float64, bool) {
	if len(course) == 0 {
		return 0, false
	}
	for _, key := range []string{"price_disc", "pre_discount_price", "time_limit_discount_money"} {
		if _, ok := course[key]; !ok {
			continue
		}
		if price := parsePrice(course[key]); price > 0 {
			return price, true
		}
	}
	for _, key := range []string{"price", "ori_price"} {
		if _, ok := course[key]; !ok {
			continue
		}
		price := parsePrice(course[key])
		if price > 0 {
			return price / 100, true
		}
		return 0, true
	}
	return 0, false
}

func embeddedFileItems(course map[string]any) []map[string]any {
	seen := map[string]bool{}
	var out []map[string]any
	for _, key := range []string{"file", "file_json"} {
		for _, item := range decodeFileItems(course[key]) {
			fileURL := normalizeURL(firstString(item, "path", "url", "file_url", "file"))
			name := firstString(item, "name", "title", "file_name")
			dedupe := fileURL + "|" + name
			if fileURL == "" || seen[dedupe] {
				continue
			}
			seen[dedupe] = true
			out = append(out, item)
		}
	}
	return out
}

func decodeFileItems(v any) []map[string]any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		var parsed any
		if json.Unmarshal([]byte(x), &parsed) != nil {
			return nil
		}
		return decodeFileItems(parsed)
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, it := range x {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{x}
	default:
		return nil
	}
}

func chaogeChapters(items []map[string]any) []extractor.Chapter {
	var chapters []extractor.Chapter
	seen := map[string]bool{}
	for _, item := range items {
		title := cleanTitle(firstString(item, "course_name", "title", "name"))
		id := firstString(item, "course_id", "id")
		if title == "" || id == "" || seen[id] || !looksFolder(item) {
			continue
		}
		seen[id] = true
		chapters = append(chapters, extractor.Chapter{Title: title, URL: fmt.Sprintf(enterCourseURL, url.QueryEscape(id)), Index: len(chapters) + 1})
	}
	return chapters
}

func csslStreams(play *shared.CssLcloudPlayInfo, referer string) map[string]extractor.Stream {
	streams := map[string]extractor.Stream{}
	list := play.VideoList
	if len(list) == 0 && play.VideoURL != "" {
		list = []shared.CssLcloudStreamInfo{{URL: play.VideoURL}}
	}
	for i, v := range list {
		if v.URL == "" {
			continue
		}
		key := fmt.Sprintf("definition_%d", v.Definition)
		if v.Definition == 0 {
			key = fmt.Sprintf("video_%d", i+1)
		}
		streams[key] = extractor.Stream{Quality: key, URLs: []string{v.URL}, Format: pickFormat(v.URL), AudioURL: play.AudioURL, Headers: map[string]string{"Referer": referer}}
	}
	if len(streams) == 0 && play.VideoURL != "" {
		streams["default"] = extractor.Stream{Quality: "best", URLs: []string{play.VideoURL}, Format: pickFormat(play.VideoURL), AudioURL: play.AudioURL, Headers: map[string]string{"Referer": referer}}
	}
	return streams
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
	for _, k := range []string{"course_list", "courseList", "file_seg_list", "file_list", "list", "dataList", "rows", "items"} {
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
func parsePrice(v any) float64 {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
func in(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}
func truthy(v any) bool {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	return s == "1" || s == "true" || s == "yes"
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

func pageLimitFromString(pageStr string, def int) int {
	maxPage := def
	for _, m := range regexp.MustCompile(`page=(\d+)`).FindAllStringSubmatch(pageStr, -1) {
		if v := intValue(m[1], def); v > maxPage {
			maxPage = v
		}
	}
	return maxPage
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
