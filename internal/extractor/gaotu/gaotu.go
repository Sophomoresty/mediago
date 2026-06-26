// Package gaotu implements the Gaotu / Gaotu100 study-platform extractor.
package gaotu

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	courseURLFormat = "https://%s/studyPlatform/v1/unit/clazz/list?isDebounce=true&os=h5-pc&p_client=%s"
	infoURLFormat   = "https://%s/live/api/studyCenter/v1/user/pc/clazz/detail"
	videoURLFormat  = "https://%s/live/zplan/login/videoLive"
	liveURLFormat   = "https://%s/live/api/live/zplan/playbackWeb"
	sourceURLFormat = "https://%s/live/api/pan/listDir"
	fileURLFormat   = "https://%s/live/api/pan/file"
	priceURLFormat  = "https://%s/cs/api/product/course/detailButton?productSpuNumber=%%s"
	video_play_url  = "https://api.wenzaizhibo.com/web/video/getPlayUrl?vid=%s&partner_id=%s&user_number=%s&expires_in=%s&user_role=%s&timestamp=%s&is_encrypted=%s&sign=%s"
	live_play_url   = "https://api.wenzaizhibo.com/web/playback/getPlaybackInfoV4?room_id=%s&partner_id=%s&user_number=%s&expires_in=%s&user_role=%s&timestamp=%s&is_encrypted=%s&sign=%s&playlist=%s"
)

var patterns = []string{`(?:[\w-]+\.)?(?:gaotu\.cn|gaotu100\.com|gtgz\.cn|naiyouxuexi\.com|wenzaizhibo\.com)/`}

func init() {
	extractor.Register(&Gaotu{}, extractor.SiteInfo{Name: "Gaotu", URL: "gaotu.cn", NeedAuth: true})
}

type Gaotu struct{}

func (g *Gaotu) Patterns() []string { return patterns }

type gaotuEndpoints struct {
	referer         string
	apiHost         string
	interactiveHost string
	pClient         string
}

func (e gaotuEndpoints) courseURL() string { return fmt.Sprintf(courseURLFormat, e.apiHost, e.pClient) }
func (e gaotuEndpoints) infoURL() string   { return fmt.Sprintf(infoURLFormat, e.interactiveHost) }
func (e gaotuEndpoints) videoURL() string  { return fmt.Sprintf(videoURLFormat, e.apiHost) }
func (e gaotuEndpoints) liveURL() string   { return fmt.Sprintf(liveURLFormat, e.interactiveHost) }
func (e gaotuEndpoints) sourceURL() string { return fmt.Sprintf(sourceURLFormat, e.interactiveHost) }
func (e gaotuEndpoints) fileURL() string   { return fmt.Sprintf(fileURLFormat, e.interactiveHost) }
func (e gaotuEndpoints) priceURL() string  { return fmt.Sprintf(priceURLFormat, e.apiHost) }

var (
	clazzRe = regexp.MustCompile(`(?i)(?:clazzNumber|clazzId|courseId|productSpuNumber|cid)=([A-Za-z0-9_-]+)`)
	liveRe  = regexp.MustCompile(`(?i)(?:clazzLessonNumber|liveId|lessonId|videoId|vid)=([A-Za-z0-9_-]+)`)
	roomRe  = regexp.MustCompile(`(?i)(?:room_id|roomId)=([A-Za-z0-9_-]+)`)
)

type ids struct {
	Clazz string
	Live  string
	Room  string
	SID   string
	Role  string
}

type lessonNode struct {
	ID    string
	Title string
	Kind  string
}

func (g *Gaotu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("gaotu requires login cookies")
	}
	id := parseIDs(rawURL)
	if id.Clazz == "" && id.Live == "" && id.Room == "" {
		return nil, fmt.Errorf("cannot parse gaotu clazz/live id from URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	endpoints := endpointsFor(rawURL)
	headers := map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Referer":      endpoints.referer,
		"Origin":       strings.TrimRight(endpoints.referer, "/"),
		"Content-Type": "application/json;charset=UTF-8",
	}

	if id.Live != "" || id.Room != "" {
		entry, err := resolveLesson(c, headers, endpoints, id, "gaotu_"+firstNonEmpty(id.Live, id.Room))
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	entries, title, extra, err := resolveCourse(c, headers, endpoints, id)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("gaotu: no playable resource found for clazz %s", id.Clazz)
	}
	return &extractor.MediaInfo{Site: "gaotu", Title: util.SanitizeFilename(firstNonEmpty(title, "gaotu_"+id.Clazz)), Entries: entries, Extra: extra}, nil
}

func resolveCourse(c *util.Client, headers map[string]string, endpoints gaotuEndpoints, id ids) ([]*extractor.MediaInfo, string, map[string]any, error) {
	payload, err := postJSON(c, endpoints.infoURL(), map[string]any{"platformType": 3, "clazzNumber": id.Clazz}, headers)
	if err != nil {
		return nil, "", nil, fmt.Errorf("fetch gaotu clazz detail: %w", err)
	}
	title := firstNonEmpty(pickTitle(payload), id.Clazz)
	extra := map[string]any{"clazz_number": id.Clazz}
	if root := firstFieldString(payload, "subclazzNumber", "rootNumber"); root != "" {
		extra["subclazz_number"] = root
	}
	if price, ok := fetchGaotuPrice(c, headers, endpoints, id.Clazz); ok {
		extra["price"] = price
	}
	entries := make([]*extractor.MediaInfo, 0)
	if media := findMediaURL(payload); media != "" {
		entries = append(entries, mediaInfo(title, media, headers))
	}

	nodes := collectLessons(payload)
	if len(nodes) == 0 {
		// Source also opens course_url while selecting purchased classes; keep that API path covered.
		if listPayload, err := postJSON(c, endpoints.courseURL(), map[string]any{"searchTypeList": []any{}, "modulePage": map[string]any{"pageNum": 1}}, headers); err == nil {
			if title == id.Clazz {
				title = firstNonEmpty(pickTitle(listPayload), title)
			}
			nodes = append(nodes, collectLessons(listPayload)...)
		}
	}

	seen := map[string]bool{}
	for _, node := range nodes {
		if node.ID == "" || seen[node.ID] {
			continue
		}
		seen[node.ID] = true
		lessonID := id
		lessonID.Live = node.ID
		entry, err := resolveLesson(c, headers, endpoints, lessonID, node.Title)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	if root, _ := extra["subclazz_number"].(string); root != "" {
		entries = append(entries, resolveGaotuFiles(c, headers, endpoints, root)...)
	}
	return entries, title, compactExtra(extra), nil
}

func resolveLesson(c *util.Client, headers map[string]string, endpoints gaotuEndpoints, id ids, fallbackTitle string) (*extractor.MediaInfo, error) {
	if id.Role == "" {
		id.Role = "3"
	}
	payloads := make([]any, 0, 2)
	if id.Live != "" {
		if p, err := postJSON(c, endpoints.videoURL(), map[string]any{"liveId": id.Live, "sid": id.SID, "roleType": id.Role}, headers); err == nil {
			payloads = append(payloads, p)
		}
		if p, err := postJSON(c, endpoints.liveURL(), map[string]any{"liveId": id.Live, "roleType": id.Role}, headers); err == nil {
			payloads = append(payloads, p)
		}
	}
	if id.Room != "" {
		payloads = append(payloads, map[string]any{"data": map[string]any{"pcUrl": rawPlaybackURL(id)}})
	}
	for _, payload := range payloads {
		if media := mediaFromPayload(c, headers, payload); media != "" {
			title := util.SanitizeFilename(firstNonEmpty(pickTitle(payload), fallbackTitle, "gaotu_"+firstNonEmpty(id.Live, id.Room)))
			return mediaInfo(title, media, headers), nil
		}
	}
	return nil, fmt.Errorf("gaotu: no cdn_list url for live %s", firstNonEmpty(id.Live, id.Room))
}

func mediaFromPayload(c *util.Client, headers map[string]string, payload any) string {
	if media := findMediaURL(payload); media != "" {
		return media
	}
	for _, pc := range collectStrings(payload, "pcUrl") {
		if media := findMediaURL(pc); media != "" {
			return media
		}
		if media := decodePcURL(c, headers, pc); media != "" {
			return media
		}
	}
	return ""
}

func decodePcURL(c *util.Client, headers map[string]string, pc string) string {
	values := queryValues(pc)
	if values.Get("vid") != "" {
		api := fmt.Sprintf(video_play_url, q(values.Get("vid")), q(values.Get("partner_id")), q(values.Get("user_number")), q(values.Get("expires_in")), q(values.Get("user_role")), q(values.Get("timestamp")), q(values.Get("is_encrypted")), q(values.Get("sign")))
		return getMediaJSON(c, headers, api)
	}
	if values.Get("room_id") != "" {
		api := fmt.Sprintf(live_play_url, q(values.Get("room_id")), q(values.Get("partner_id")), q(values.Get("user_number")), q(values.Get("expires_in")), q(values.Get("user_role")), q(values.Get("timestamp")), q(values.Get("is_encrypted")), q(values.Get("sign")), q(values.Get("playlist")))
		return getMediaJSON(c, headers, api)
	}
	return ""
}

func getMediaJSON(c *util.Client, headers map[string]string, api string) string {
	body, err := c.GetString(api, headers)
	if err != nil {
		return ""
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		return ""
	}
	return findMediaURL(payload)
}

func postJSON(c *util.Client, api string, payload map[string]any, headers map[string]string) (any, error) {
	buf, _ := json.Marshal(payload)
	h := cloneHeaders(headers)
	h["Content-Type"] = "application/json;charset=UTF-8"
	resp, err := c.Post(api, strings.NewReader(string(buf)), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func collectLessons(v any) []lessonNode {
	var out []lessonNode
	var walk func(any, string)
	walk = func(x any, prefix string) {
		switch vv := x.(type) {
		case map[string]any:
			node := vv
			if inner, ok := vv["userClazzLessonBaseVO"].(map[string]any); ok {
				node = inner
			}
			id := valueString(node, "clazzLessonNumber", "liveId", "lessonId", "videoId", "id")
			title := firstNonEmpty(valueString(node, "clazzLessonName", "lessonName", "title", "name"), prefix)
			kind := valueString(node, "bindType", "type")
			if id != "" && (hasAny(vv, "userClazzLessonBaseVO") || hasAny(node, "clazzLessonName", "bindType", "liveId", "videoId")) {
				out = append(out, lessonNode{ID: id, Title: title, Kind: kind})
			}
			next := firstNonEmpty(title, valueString(vv, "chapterName", "cardTitle", "moduleTitle"), prefix)
			for _, k := range []string{"chapterItemVOList", "lessonCardList", "moduleList", "moduleCardList", "data", "list", "children"} {
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
