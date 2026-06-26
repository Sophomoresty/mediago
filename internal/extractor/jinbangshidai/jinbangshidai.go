// Package jinbangshidai implements source-aligned Jinbangshidai extraction.
package jinbangshidai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	platform                = "jinbangshidai"
	site                    = "金榜时代"
	referer                 = "https://pc.vkbrother.com"
	api_base                = "https://app.vkbrother.com"
	pc_origin               = "https://pc.vkbrother.com"
	student_info_url        = api_base + "/app/student/info"
	course_list_url         = api_base + "/app/course/me/courseList"
	room_course_list_url    = api_base + "/app/room/jbCourseList"
	course_info_url         = api_base + "/app/course/v2/info"
	course_play_url         = api_base + "/app/course/v2/coursePlay"
	video_token_url         = api_base + "/app/bjvod/videoPlayerToken"
	room_playback_token_url = api_base + "/app/bjvod/getPlaybackToken"
	video_play_url          = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id:}&token={token:}&use_encrypt=0&render=jsonp"
	live_play_url           = "https://www.baijiayun.com/vod/video/getPlayUrl?vid={live_id:}&render=jsonp&token={token:}&use_encrypt=0"
)

var patterns = []string{`\s*((?P<jinbang_name>jinbangshidai|金榜时代|金榜)|(https?://(?:pc|app)\.vkbrother\.com/.*?(?:courseId|id)=(?P<cid1>[^\s&#]+))|(https?://(?:pc|app)\.vkbrother\.com(?:[/?#].*)?)|https?://(?:[\w-]+\.)*vkbrother\.com(?:[/?#].*)?)`}

func init() {
	extractor.Register(&Jinbangshidai{}, extractor.SiteInfo{Name: "Jinbangshidai", URL: "vkbrother.com", NeedAuth: true})
}

type Jinbangshidai struct{}

func (s *Jinbangshidai) Patterns() []string { return patterns }

type jbCtx struct {
	c        *util.Client
	headers  map[string]string
	token    string
	deviceID string
	cid      string
	title    string
	price    float64
}

type courseInfo struct {
	CourseID string
	Title    string
	Price    float64
	Raw      map[string]any
}

type resourceInfo struct {
	Name         string
	VideoID      string
	ResourceType int
	Prefix       string
	Source       map[string]any
}

type fileInfo struct{ Name, URL, Fmt string }

func (s *Jinbangshidai) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("jinbangshidai requires login cookies")
	}
	x, err := newCtx(opts.Cookies)
	if err != nil {
		return nil, err
	}
	if err := x.checkCookie(); err != nil {
		return nil, err
	}
	x.cid = parseCID(rawURL)
	if x.cid == "" {
		if first := x.firstCourse(); first.CourseID != "" {
			x.cid, x.title, x.price = first.CourseID, first.Title, first.Price
		}
	}
	if x.cid == "" {
		return nil, fmt.Errorf("jinbangshidai: cannot parse course id from URL or course list")
	}
	if x.title == "" {
		x.applyCourseListMetadata()
	}
	resources, files, err := x.loadInfos()
	if err != nil {
		return nil, err
	}
	return x.mediaFromResources(resources, files)
}

func newCtx(jar http.CookieJar) (*jbCtx, error) {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{referer + "/", api_base + "/"})
	token := extractCookieToken(cookie)
	if token == "" {
		return nil, fmt.Errorf("jinbangshidai: missing ToKen/token cookie")
	}
	payload := decodeJWTPayload(token)
	deviceID := str(payload["deviceId"])
	headers := map[string]string{
		"token":        token,
		"cookie":       "ToKen=" + token,
		"Origin":       pc_origin,
		"Referer":      referer + "/",
		"Content-Type": "application/json",
		"Accept":       "application/json, text/plain, */*",
	}
	return &jbCtx{c: c, headers: headers, token: token, deviceID: deviceID}, nil
}

func (x *jbCtx) checkCookie() error {
	root, err := x.postJSON(student_info_url, map[string]any{})
	if err != nil {
		return err
	}
	if intVal(root["code"]) == 0 {
		return nil
	}
	return fmt.Errorf("jinbangshidai cookie check failed: code=%v", root["code"])
}

func (x *jbCtx) postJSON(endpoint string, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := x.c.Post(endpoint, bytes.NewReader(body), x.headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	return root, nil
}

func (x *jbCtx) getCourseList() []courseInfo {
	seen := map[string]int{}
	var out []courseInfo
	for _, spec := range []struct {
		url string
		key string
	}{
		{course_list_url, "courseList"},
		{room_course_list_url, "jbCourseSelfVOS"},
	} {
		root, err := x.postJSON(spec.url, map[string]any{})
		if err != nil {
			continue
		}
		for _, item := range listAt(root, spec.key) {
			course := normalizeCourseInfo(item)
			if course.CourseID == "" {
				continue
			}
			if idx, ok := seen[course.CourseID]; ok {
				if course.Price != 0 {
					out[idx].Price = course.Price
				}
				continue
			}
			seen[course.CourseID] = len(out)
			out = append(out, course)
		}
	}
	return out
}

func (x *jbCtx) firstCourse() courseInfo {
	for _, c := range x.getCourseList() {
		return c
	}
	return courseInfo{}
}

func (x *jbCtx) applyCourseListMetadata() {
	for _, c := range x.getCourseList() {
		if c.CourseID == x.cid {
			x.title, x.price = c.Title, c.Price
			return
		}
	}
}

func (x *jbCtx) getCourseDetail() map[string]any {
	root, err := x.postJSON(course_info_url, map[string]any{"courseId": x.cid})
	if err != nil {
		return map[string]any{}
	}
	if intVal(root["code"]) == 0 {
		info := mapAt(root, "info")
		if title := pickText(info["name"], info["title"]); title != "" {
			x.title = cleanTitle(title)
		}
		if p := pickNumber(info["price"], info["oldPrice"], info["realPrice"]); p != 0 {
			x.price = p
		}
	}
	return root
}

func (x *jbCtx) getCoursePlay() map[string]any {
	root, err := x.postJSON(course_play_url, map[string]any{
		"deviceType": "PC",
		"deviceId":   x.deviceID,
		"courseId":   x.cid,
	})
	if err != nil {
		return map[string]any{}
	}
	if intVal(root["code"]) == 0 {
		info := mapAt(root, "info")
		if title := pickText(info["name"], info["title"]); title != "" {
			x.title = cleanTitle(title)
		}
	}
	return root
}

func (x *jbCtx) loadInfos() ([]resourceInfo, []fileInfo, error) {
	detail := x.getCourseDetail()
	play := x.getCoursePlay()
	syllabus := listAt(play, "syllabusList")
	if len(syllabus) == 0 {
		syllabus = listAt(detail, "syllabusList")
	}
	if len(syllabus) == 0 {
		if info := mapAt(play, "info"); len(info) > 0 {
			syllabus = listAt(info, "syllabusList")
		}
	}
	var resources []resourceInfo
	var files []fileInfo
	for i, node := range syllabus {
		children := listAt(node, "list")
		name := pickText(node["name"], node["title"], "课程")
		if len(children) > 0 {
			resources = append(resources, collectSyllabusResources(children, []int{i + 1})...)
			continue
		}
		if typeIn(node["type"], 3, 4, 5) {
			if name != "" {
				node["name"] = name
			}
			resources = append(resources, makeResourceInfo(node, []int{i + 1}))
		}
	}
	for _, r := range resources {
		files = append(files, materialFiles(r)...)
	}
	if len(resources) == 0 && len(files) == 0 {
		return nil, nil, fmt.Errorf("jinbangshidai: empty syllabus resources")
	}
	return dedupeResources(resources), dedupeFiles(files), nil
}

func (x *jbCtx) mediaFromResources(resources []resourceInfo, files []fileInfo) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	var lastErr error
	for _, r := range resources {
		if r.ResourceType == 4 {
			continue
		}
		entry, err := x.resolveVideo(r)
		if err != nil {
			lastErr = err
			continue
		}
		entries = append(entries, entry)
	}
	for _, f := range files {
		if f.URL == "" {
			continue
		}
		entries = append(entries, &extractor.MediaInfo{Site: platform, Title: f.Name, Streams: map[string]extractor.Stream{f.Fmt: {Quality: f.Fmt, URLs: []string{f.URL}, Format: f.Fmt, Headers: cloneHeaders(x.headers)}}, Extra: map[string]any{"kind": "file"}})
	}
	if len(entries) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("jinbangshidai: no playable entries")
	}
	if len(entries) == 1 {
		if x.title != "" {
			entries[0].Extra["course_title"] = x.title
		}
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: platform, Title: firstNonEmpty(x.title, x.cid, site), Entries: entries, Extra: map[string]any{"course_id": x.cid, "price": x.price}}, nil
}

func (x *jbCtx) resolveVideo(r resourceInfo) (*extractor.MediaInfo, error) {
	var mediaURL, token string
	var err error
	headers := map[string]string{"Referer": referer + "/", "Origin": pc_origin, "token": x.token, "cookie": "ToKen=" + x.token}
	if r.ResourceType == 5 {
		roomID := pickText(r.Source["roomString"], r.VideoID)
		if status := str(r.Source["status"]); status != "" && status != "3" {
			return nil, fmt.Errorf("jinbangshidai room %s is not replayable: status=%s", roomID, status)
		}
		token, err = x.requestRoomPlaybackToken(roomID)
		if err == nil {
			mediaURL, err = shared.BaijiayunResolvePlayback(x.c, roomID, token, headers)
		}
	} else {
		vid := pickText(r.VideoID, r.Source["url"], r.Source["videoId"])
		token, err = x.requestVideoToken(vid, intVal(r.Source["old"]))
		if err == nil {
			mediaURL, err = shared.BaijiayunResolveVOD(x.c, vid, token, headers)
		}
	}
	if err != nil {
		return nil, err
	}
	format := extFormat(mediaURL)
	if format == "" {
		format = "mp4"
	}
	return &extractor.MediaInfo{
		Site:  platform,
		Title: firstNonEmpty(r.Name, r.VideoID),
		Streams: map[string]extractor.Stream{"best": {
			Quality:   "best",
			URLs:      []string{mediaURL},
			Format:    format,
			NeedMerge: format == "m3u8",
			Headers:   headers,
		}},
		Extra: map[string]any{"resource_type": r.ResourceType, "video_id": r.VideoID, "token": token},
	}, nil
}

func (x *jbCtx) requestVideoToken(videoID string, old int) (string, error) {
	root, err := x.postJSON(video_token_url, map[string]any{"deviceType": "PC", "deviceId": x.deviceID, "old": old, "courseId": x.cid, "video_id": videoID})
	if err != nil {
		return "", err
	}
	token := strAt(root, "data", "token")
	if token == "" {
		return "", fmt.Errorf("jinbangshidai: empty video token for %s", videoID)
	}
	return token, nil
}

func (x *jbCtx) requestRoomPlaybackToken(roomID string) (string, error) {
	root, err := x.postJSON(room_playback_token_url, map[string]any{"deviceType": "PC", "deviceId": x.deviceID, "roomId": roomID})
	if err != nil {
		return "", err
	}
	token := strAt(root, "data", "token")
	if token == "" {
		return "", fmt.Errorf("jinbangshidai: empty room playback token for %s", roomID)
	}
	return token, nil
}
