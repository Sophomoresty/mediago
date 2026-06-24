// Package meeting implements an extractor for meeting.tencent.com (腾讯会议) replays.
package meeting

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	referer             = "https://meeting.tencent.com"
	recordInfoURL       = "https://meeting.tencent.com/wemeet-tapi/v2/meetlog/public/detail/record-info?c_instance_id=5"
	commonRecordInfoURL = "https://meeting.tencent.com/wemeet-tapi/v2/meetlog/public/detail/common-record-info?c_instance_id=5"
	shareSignURL        = "https://meeting.tencent.com/wemeet-cloudrecording-webapi/v1/sign?id=%s&sharing_id=%s&source=shares&pwd=%s&need_multi_stream=1"
	shareSignPostURL    = "https://meeting.tencent.com/wemeet-tapi/v2/wemeet-cloudrecording-webapi/v1/sign?c_instance_id=5"
	liveStreamURL       = "https://meeting.tencent.com/wemeet-tapi/liveportal/v2/query_live_stream"
	liveReplayURL       = "https://meeting.tencent.com/wemeet-tapi/liveportal/v2/query_meeting_room_live_replay_info"
	shortShareURL       = "https://meeting.tencent.com/%s/%s"
)

var patterns = []string{`meeting\.tencent\.com/`}

func init() {
	extractor.Register(&Meeting{}, extractor.SiteInfo{Name: "Meeting", URL: "meeting.tencent.com", NeedAuth: true})
}

type Meeting struct{}

func (m *Meeting) Patterns() []string { return patterns }

type mediaItem struct{ URL, Title, Kind, CourseID string }

var (
	meetlogRe = regexp.MustCompile(`https?://meeting\.tencent\.com/meetlog/detail/index\.html\?[^\s#]*?s=([\w-]+)`) // source _fallback_course_id
	liveRe    = regexp.MustCompile(`https?://meeting\.tencent\.com/live/(\d+)`)
	shareRe   = regexp.MustCompile(`https?://meeting\.tencent\.com/(?:(cw)|(cr?m)|(ctm?))/([\w-]+)|https?://meeting\.tencent\.com/.*?/share.*?id=([\w-]+)`)
	jsonURLRe = regexp.MustCompile(`(?i)"(?:origin_video_url|video_url|replay_url_long|download_url|url)"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)
	titleRe   = regexp.MustCompile(`(?is)<title>(.*?)</title>|"(?:title|filename|file_name)"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)
)

func (m *Meeting) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("meeting.tencent requires login cookies")
	}
	id, typ := parseID(rawURL)
	if id == "" {
		return nil, fmt.Errorf("cannot parse meeting.tencent id from URL")
	}
	pwd := passwordFromURL(rawURL)
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": referer, "Origin": referer, "Accept": "application/json, text/plain, */*"}

	items, pageTitle := resolveMeeting(c, rawURL, id, typ, pwd, h)
	if len(items) == 0 {
		return nil, fmt.Errorf("meeting.tencent: no playable origin_video_url/video_url/replay_url_long found")
	}
	entries := make([]*extractor.MediaInfo, 0, len(items))
	seen := map[string]bool{}
	for i, it := range items {
		u := strings.TrimSpace(it.URL)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		title := clean(first(it.Title, pageTitle, "腾讯会议录制_"+strconv.Itoa(i+1)))
		entries = append(entries, &extractor.MediaInfo{Site: "meeting", Title: title, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: formatOf(u), Headers: h}}, Extra: map[string]any{"course_id": first(it.CourseID, id), "kind": it.Kind}})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("meeting.tencent: playable fields parsed but empty after normalization")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "meeting", Title: clean(first(pageTitle, "腾讯会议批量")), Entries: entries}, nil
}

func resolveMeeting(c *util.Client, rawURL, id, typ, pwd string, h map[string]string) ([]mediaItem, string) {
	var out []mediaItem
	var title string
	if body, err := c.GetString(rawURL, h); err == nil {
		out = append(out, parseMediaFromText(body, id)...)
		title = parseTitle(body)
	}
	if len(out) > 0 {
		return out, title
	}
	if typ == "live" {
		body, _ := c.PostForm(liveStreamURL, map[string]string{"meeting_id": id, "password": pwd}, h)
		out = append(out, parseMediaFromJSON(body, id)...)
		roomID := firstJSONText(body, "data.room_id", "data.media_room_id", "room_id")
		if roomID != "" {
			body, _ = c.PostForm(liveReplayURL, map[string]string{"media_room_id": roomID, "live_password": pwd}, h)
			out = append(out, parseMediaFromJSON(body, id)...)
		}
		return out, first(title, firstJSONText(body, "data.title", "title"))
	}
	if typ == "meetlog" {
		body, _ := c.PostForm(recordInfoURL, map[string]string{"record_id": id, "activity_uid": id, "password": base64.StdEncoding.EncodeToString([]byte(pwd)), "cover_image_style": "meetlog_detail_webp_1000"}, h)
		out = append(out, parseMediaFromJSON(body, id)...)
		return out, first(title, firstJSONText(body, "data.title", "title"))
	}
	body, _ := c.PostForm(commonRecordInfoURL, map[string]string{"code": id, "short_url_code": id, "sharing_id": id, "pwd": pwd, "source": "share", "enter_from": "share", "forward_cgi_path": fmt.Sprintf(shortShareURL, first(typ, "cw"), id)}, h)
	out = append(out, parseMediaFromJSON(body, id)...)
	sharingID := firstJSONText(body, "data.sharing_id", "sharing_id")
	if sharingID == "" {
		sharingID = id
	}
	for _, rid := range recordingIDs(body) {
		signURL := fmt.Sprintf(shareSignURL, url.QueryEscape(rid), url.QueryEscape(sharingID), url.QueryEscape(pwd))
		if signBody, err := c.GetString(signURL, h); err == nil {
			out = append(out, parseMediaFromJSON(signBody, rid)...)
		}
		postBody, _ := c.PostForm(shareSignPostURL, map[string]string{"id": rid, "sharing_id": sharingID, "source": "shares", "pwd": pwd, "need_multi_stream": "1"}, h)
		out = append(out, parseMediaFromJSON(postBody, rid)...)
	}
	return out, first(title, firstJSONText(body, "data.title", "title"))
}

func parseID(rawURL string) (string, string) {
	if m := liveRe.FindStringSubmatch(rawURL); m != nil {
		return m[1], "live"
	}
	if m := meetlogRe.FindStringSubmatch(rawURL); m != nil {
		return m[1], "meetlog"
	}
	if m := shareRe.FindStringSubmatch(rawURL); m != nil {
		if m[4] != "" {
			return m[4], first(m[1], m[2], m[3], "cw")
		}
		return m[5], "share"
	}
	return "", ""
}

func passwordFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	return first(q.Get("pwd"), q.Get("password"), q.Get("pw"))
}

func parseMediaFromText(text, id string) []mediaItem {
	items := parseMediaFromJSON(text, id)
	for _, m := range jsonURLRe.FindAllStringSubmatch(text, -1) {
		if u := unescape(m[1]); strings.HasPrefix(u, "http") && looksMedia(u) {
			items = append(items, mediaItem{URL: u, Title: parseTitle(text), Kind: "record", CourseID: id})
		}
	}
	return items
}

func parseMediaFromJSON(text, id string) []mediaItem {
	var v any
	if json.Unmarshal([]byte(stripJSONP(text)), &v) != nil {
		return nil
	}
	var out []mediaItem
	walk(v, func(m map[string]any) {
		u := firstText(m, "origin_video_url", "video_url", "replay_url_long", "download_url", "play_url", "url")
		if u != "" && strings.HasPrefix(u, "http") && looksMedia(u) {
			out = append(out, mediaItem{URL: u, Title: firstText(m, "title", "filename", "file_name", "name"), Kind: firstText(m, "kind", "stream_type"), CourseID: firstText(m, "course_id", "recording_id", "id")})
		}
	})
	return out
}

func recordingIDs(text string) []string {
	ids := map[string]bool{}
	var out []string
	var v any
	if json.Unmarshal([]byte(stripJSONP(text)), &v) != nil {
		return nil
	}
	walk(v, func(m map[string]any) {
		if id := firstText(m, "recording_id", "id"); id != "" && !ids[id] {
			ids[id] = true
			out = append(out, id)
		}
	})
	return out
}

func walk(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, x := range t {
			walk(x, fn)
		}
	case []any:
		for _, x := range t {
			walk(x, fn)
		}
	}
}
func firstJSONText(text string, paths ...string) string {
	var v any
	if json.Unmarshal([]byte(stripJSONP(text)), &v) != nil {
		return ""
	}
	for _, p := range paths {
		if s := jsonPath(v, strings.Split(p, ".")); s != "" {
			return s
		}
	}
	return ""
}
func jsonPath(v any, parts []string) string {
	if len(parts) == 0 {
		return fmt.Sprint(v)
	}
	if m, ok := v.(map[string]any); ok {
		return jsonPath(m[parts[0]], parts[1:])
	}
	return ""
}
func firstText(m map[string]any, keys ...string) string {
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
func stripJSONP(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "("); i >= 0 && strings.HasSuffix(strings.TrimSuffix(s, ";"), ")") {
		e := strings.LastIndex(s, ")")
		if e > i {
			return s[i+1 : e]
		}
	}
	return s
}
func parseTitle(text string) string {
	if m := titleRe.FindStringSubmatch(text); m != nil {
		return clean(first(m[1], unescape(m[2])))
	}
	return ""
}
func unescape(s string) string {
	if u, err := strconv.Unquote(`"` + strings.ReplaceAll(s, `"`, `\"`) + `"`); err == nil {
		return u
	}
	return s
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func looksMedia(u string) bool {
	l := strings.ToLower(u)
	return strings.Contains(l, ".mp4") || strings.Contains(l, ".m3u8") || strings.Contains(l, "video")
}
func formatOf(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
func clean(s string) string {
	s = strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, ""))
	if s == "" {
		return "腾讯会议录制"
	}
	return s
}
