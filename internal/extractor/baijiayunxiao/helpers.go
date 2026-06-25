package baijiayunxiao

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var (
	coursePathRe    = regexp.MustCompile(`(?i)^https?://([^/#?]+)/(?:.*?/)?course/(\d+)`)
	evURLRe         = regexp.MustCompile(`(?i)^https?://(?:[\w-]+\.)*baijia(?:yun|cloud)\.com/.*?/([\w-]+)\.(ev[12])\S*$`)
	urlLikeRe       = regexp.MustCompile(`https?://[^\s"'<>]+`)
	tokenRe         = regexp.MustCompile(`(?i)token=([\w_-]+)|"token"\s*:\s*"([\w_-]+)"`)
	classIDRe       = regexp.MustCompile(`(?i)(?:classid|room_id)=(\d+)|"(?:video_id|room_id|classid)"\s*:\s*"?(\d+)"?`)
	directParamKeys = []string{"room_id", "classid", "vid", "token"}
)

func parsePlaybackParams(raw string) (playbackParams, bool) {
	decoded := html.UnescapeString(strings.TrimSpace(raw))
	u, err := url.Parse(decoded)
	if err != nil {
		return playbackParams{}, false
	}
	q := u.Query()
	token := q.Get("token")
	roomID := firstNonEmpty(q.Get("room_id"), q.Get("classid"))
	vid := q.Get("vid")
	if token != "" && roomID != "" {
		return playbackParams{roomID: roomID, token: token}, true
	}
	if token != "" && vid != "" {
		return playbackParams{vid: vid, token: token, isVOD: true}, true
	}
	for _, k := range directParamKeys {
		if strings.Contains(decoded, k+"=") {
			return playbackParams{}, false
		}
	}
	return playbackParams{}, false
}

func parseCourseURL(raw string) (courseURL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return courseURL{}, false
	}
	q := u.Query()
	cid := firstNonEmpty(q.Get("course_id"), q.Get("courseId"))
	domain := u.Host
	if cid == "" {
		if m := coursePathRe.FindStringSubmatch(raw); m != nil {
			domain, cid = m[1], m[2]
		}
	}
	if cid == "" || !strings.Contains(domain, "baijiayunxiao") {
		return courseURL{}, false
	}
	return courseURL{domain: domain, cid: cid}, true
}

func findPlaybackURLInText(text string) string {
	var payload any
	if json.Unmarshal([]byte(text), &payload) == nil {
		if u := findPlaybackURLInValue(payload); u != "" {
			return u
		}
	}
	for _, candidate := range urlLikeRe.FindAllString(html.UnescapeString(text), -1) {
		if _, ok := parsePlaybackParams(candidate); ok {
			return candidate
		}
	}
	return ""
}

func findPlaybackURLInValue(v any) string {
	switch x := v.(type) {
	case string:
		if _, ok := parsePlaybackParams(x); ok {
			return x
		}
	case map[string]any:
		for _, vv := range x {
			if u := findPlaybackURLInValue(vv); u != "" {
				return u
			}
		}
	case []any:
		for _, vv := range x {
			if u := findPlaybackURLInValue(vv); u != "" {
				return u
			}
		}
	}
	return ""
}

func collectLessons(nodes []courseNode, prefix []string) []lessonRef {
	var out []lessonRef
	for i, node := range nodes {
		title := firstNonEmpty(node.PeriodsTitle, node.Title, node.Name, fmt.Sprintf("课时%d", i+1))
		id := firstNonEmpty(anyString(node.ID), anyString(node.VideoID), anyString(node.RoomID))
		if id != "" {
			out = append(out, lessonRef{ID: id, Title: strings.Join(append(prefix, title), " - ")})
		}
		children := append([]courseNode{}, node.Child...)
		children = append(children, node.Children...)
		out = append(out, collectLessons(children, append(prefix, title))...)
	}
	return out
}

func mediaInfo(site, title, mediaURL, format string, headers map[string]string) *extractor.MediaInfo {
	return &extractor.MediaInfo{
		Site:  site,
		Title: title,
		Streams: map[string]extractor.Stream{
			"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers},
		},
	}
}

func refererFromRawURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/"
	}
	return urlHome
}

func pickRegex(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

func pickFormat(mediaURL string) string {
	lower := strings.ToLower(mediaURL)
	switch {
	case strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".ev1"):
		return ".ev1"
	case strings.Contains(lower, ".ev2"):
		return ".ev2"
	case strings.Contains(lower, ".mp4"):
		return "mp4"
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

func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return fmt.Sprintf("%.0f", x)
	case int:
		return fmt.Sprint(x)
	case json.Number:
		return x.String()
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// previewVideoResponse models the JSON returned by the CourseWare video preview
// API: https://{domain}/api/app/user/CourseWare/video/preview?video_id={id}
//
// Source: Baijiayunxiao_Course._get_preview_video_url (line 1035-1074).
// Response shape: {"data": [{"url":"...","definition":"...","size":123}, ...]}
type previewVideoVariant struct {
	URL        string  `json:"url"`
	Definition string  `json:"definition"`
	Size       float64 `json:"size"`
}

type previewVideoResponse struct {
	Data json.RawMessage `json:"data"`
}

// fetchPreviewVideoURL calls the CourseWare video preview API and returns the
// best (largest non-audio) variant URL. This mirrors the source's
// _get_preview_video_url method.
func fetchPreviewVideoURL(c *util.Client, domain, videoID string, headers map[string]string) string {
	if domain == "" || videoID == "" {
		return ""
	}
	apiURL := fmt.Sprintf(urlPreviewVideo, domain, url.QueryEscape(videoID))
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return ""
	}
	var resp previewVideoResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return ""
	}

	// data can be an array of variants
	var variants []previewVideoVariant
	if err := json.Unmarshal(resp.Data, &variants); err != nil {
		return ""
	}

	// Filter: skip audio-only entries (definition=="audio" or url contains .mp3)
	var candidates []previewVideoVariant
	for _, v := range variants {
		u := normalizeMediaURL(v.URL)
		if u == "" {
			continue
		}
		if strings.EqualFold(v.Definition, "audio") {
			continue
		}
		if strings.Contains(strings.ToLower(u), ".mp3") {
			continue
		}
		v.URL = u
		candidates = append(candidates, v)
	}
	if len(candidates) == 0 {
		return ""
	}

	// Sort by size descending, pick largest
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Size > candidates[j].Size
	})
	return normalizeMediaURL(candidates[0].URL)
}

// normalizeMediaURL prepends https: to protocol-relative URLs.
// Source: _normalize_media_url
func normalizeMediaURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	return u
}
