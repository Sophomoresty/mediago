// Package xuelang implements an extractor for iyincaishijiao.com courses.
package xuelang

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL = "https://student-api.iyincaishijiao.com"
	orderURL   = "https://student-api.iyincaishijiao.com/ep/trade/v2/order/list?anchor=%s&count=50"
	profileURL = "https://student-api.iyincaishijiao.com/ep/user/profile/"
	courseURL  = "https://student-api.iyincaishijiao.com/ep/student/learn_data_v2/?course_count=999"
	infoURL    = "https://student-api.iyincaishijiao.com/ep/study_pc/course/lessons/?cursor=%s&course_id=%s&count=99&version_code=1.9.2.0&aid=4783&msToken=%s"
	liveURL    = "https://classroom.iyincaishijiao.com/classroom/playback/v1/enter_playback/?aid=2989"
	m3u8URL    = "https://vod.bytedanceapi.com/?"
	keyURL     = "https://student-api.iyincaishijiao.com/video/drm/v1/play_licenses"
	sourceURL  = "https://student-api.iyincaishijiao.com/ep/student/course_resource/?course_id=%s&token=%s&count=999"
	fileURL    = "https://student-api.iyincaishijiao.com/ep/student/preview_course_resource/?token=%s&course_id=%s"
	tokenURL   = "https://api.juejin.cn/user_api/v1/video/key_token"
	v3KeyURL   = "https://kds.bytedance.com/kds/api/v3/keys?source=jarvis&ak=%s&token=%s"
	msTokenURL = "https://mssdk.bytedance.com/web/common?msToken="
	sampleURL  = "https://ke.qq.com/webcourse/287404/100471025#taid=3323810766152364&vid=5285890790569679069"
	defaultUA  = "Mozilla/5.0 (Windows NT 10.0.19044; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/78.0.3904.108 Safari/537.36 Cef/3904 ep_pc_student/1.9.2.0"
)

var patterns = []string{`(?:[\w-]+\.)?(?:iyincaishijiao\.com|xuelangapp\.com|ke\.qq\.com)/`}
var idRe = regexp.MustCompile(`(?i)(?:course_id|courseId|cid)=([0-9]+)|/course/(?:detail/)?([0-9]+)|course_id[=:]([0-9]+)`)
var profileOKRe = regexp.MustCompile(`"status_code"\s*:\s*0`)
var dataStringRe = regexp.MustCompile(`"data"\s*:\s*"(.*?)"`)

type Xuelang struct{}
type course struct{ id, title string }
type lesson struct{ title, roomID, playAuth string }
type playMedia struct {
	videoURL, audioURL, videoID, keyID string
	size                               int64
}

func init() {
	extractor.Register(&Xuelang{}, extractor.SiteInfo{Name: "Xuelang", URL: "iyincaishijiao.com", NeedAuth: true})
}
func (s *Xuelang) Patterns() []string { return patterns }

func (s *Xuelang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xuelang requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	cookie := cookieHeader(opts.Cookies)
	h := headers(cookie)
	body, err := c.GetString(profileURL, h)
	if err != nil {
		return nil, fmt.Errorf("xuelang profile check: %w", err)
	}
	if profileOKRe.FindStringSubmatch(body) == nil {
		return nil, fmt.Errorf("xuelang profile check rejected cookie")
	}
	wantCID := firstMatch(idRe, rawURL)
	courses, err := fetchCourses(c, h)
	if err != nil {
		return nil, err
	}
	co := selectCourse(courses, wantCID)
	if co.id == "" {
		return nil, fmt.Errorf("xuelang course %q not found in learn_data_v2", wantCID)
	}
	lessons, err := fetchLessons(c, h, co.id)
	if err != nil {
		return nil, err
	}
	entries, seen := []*extractor.MediaInfo{}, map[string]bool{}
	for _, l := range lessons {
		for _, pm := range resolveLesson(c, h, l) {
			if pm.videoURL == "" || seen[pm.videoURL] {
				continue
			}
			seen[pm.videoURL] = true
			entries = append(entries, media(firstNonEmpty(l.title, "lesson"), pm, co, l))
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("xuelang: no playable m3u8 URL resolved")
	}
	return &extractor.MediaInfo{Site: "xuelang", Title: firstNonEmpty(co.title, "xuelang_"+co.id), Entries: entries}, nil
}

func fetchCourses(c *util.Client, h map[string]string) ([]course, error) {
	body, err := c.GetString(courseURL, h)
	if err != nil {
		return nil, err
	}
	root, err := parseJSON(body)
	if err != nil {
		return nil, err
	}
	out := []course{}
	for _, m := range listUnder(mapAt(mapAt(root, "data"), "student_course"), "data") {
		info := mapAt(m, "course_info")
		id := val(info, "course_id")
		if id != "" {
			out = append(out, course{id: id, title: val(info, "title")})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xuelang learn_data_v2 course list is empty")
	}
	return out, nil
}

func fetchLessons(c *util.Client, h map[string]string, cid string) ([]lesson, error) {
	out, cursor := []lesson{}, "0"
	for i := 0; i < 99; i++ {
		ms := getMSToken(c, h)
		u := fmt.Sprintf(infoURL, url.QueryEscape(cursor), url.QueryEscape(cid), url.QueryEscape(ms))
		if ms != "" {
			u += "&a_bogus="
		}
		body, err := c.GetString(u, h)
		if err != nil {
			return nil, err
		}
		root, err := parseJSON(body)
		if err != nil {
			return nil, err
		}
		items := listFrom(mapAt(root, "data")["data"])
		for n, it := range items {
			li := mapAt(it, "lesson_info")
			if len(li) == 0 {
				continue
			}
			out = append(out, lesson{roomID: val(li, "related_room_id_str"), playAuth: val(mapAt(li, "video"), "play_auth_token"), title: fmt.Sprintf("[%d]-%s", len(out)+n+1, val(li, "title"))})
		}
		fc := mapAt(mapAt(root, "data"), "forward_cursor")
		if !truthy(fc["has_more"]) {
			break
		}
		cursor = firstNonEmpty(val(fc, "cursor"), cursor)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xuelang lesson list is empty")
	}
	return out, nil
}

func getMSToken(c *util.Client, h map[string]string) string {
	payload := fmt.Sprintf(`{"magic":538969122,"version":1,"dataType":8,"strData":"","tspFromClient":%d}`, time.Now().UnixMilli())
	resp, err := c.Post(msTokenURL, bytes.NewReader([]byte(payload)), h)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	return resp.Header.Get("X-Ms-Token")
}

func resolveLesson(c *util.Client, h map[string]string, l lesson) []playMedia {
	out := []playMedia{}
	if l.playAuth != "" {
		if tok := decodePlayToken(l.playAuth); tok != "" {
			if pm := getM3U8Info(c, h, tok); pm.videoURL != "" {
				out = append(out, pm)
			}
		}
	}
	if len(out) == 0 && l.roomID != "" {
		for _, tok := range getLiveTokens(c, h, l.roomID) {
			if pm := getM3U8Info(c, h, tok); pm.videoURL != "" {
				out = append(out, pm)
			}
		}
	}
	return out
}

func getLiveTokens(c *util.Client, h map[string]string, roomID string) []string {
	root, err := postJSON(c, liveURL, map[string]any{"room_id": roomID}, h)
	if err != nil {
		return nil
	}
	out := []string{val(mapAt(root, "teacher_video_info"), "play_auth_token")}
	infos := listFrom(root["external_video_infos"])
	if len(infos) > 0 {
		out = append(out, val(mapAt(infos[0], "video"), "play_auth_token"))
	}
	return unique(out)
}

func getM3U8Info(c *util.Client, h map[string]string, playInfo string) playMedia {
	body, err := c.GetString(m3u8URL+playInfo, h)
	if err != nil {
		return playMedia{}
	}
	root, err := parseJSON(body)
	if err != nil {
		return playMedia{}
	}
	data := mapAt(mapAt(root, "Result"), "Data")
	list := listFrom(data["PlayInfoList"])
	pm := playMedia{videoID: val(data, "VideoID")}
	var best map[string]any
	var bestSize float64 = -1
	for _, m := range list {
		if val(m, "MediaType") == "audio" {
			pm.audioURL = firstNonEmpty(val(m, "MainPlayUrl"), val(m, "BackupPlayUrl"))
			continue
		}
		sz := num(m["Size"])
		if best == nil || sz > bestSize {
			best, bestSize = m, sz
		}
	}
	if best != nil {
		pm.videoURL = firstNonEmpty(val(best, "MainPlayUrl"), val(best, "BackupPlayUrl"))
		pm.keyID = val(best, "PlayAuthID")
		pm.size = int64(bestSize)
	}
	if pm.videoURL == "" {
		pm.videoURL = firstMediaURL(root)
	}
	if pm.keyID != "" && pm.videoID != "" {
		_ = decryptM3U8Key(c, h, pm.videoID, pm.keyID)
	}
	return pm
}

func decryptM3U8Key(c *util.Client, h map[string]string, vid, kid string) string {
	body, err := c.GetString(tokenURL, h)
	if err != nil {
		return ""
	}
	m := dataStringRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	body, err = c.GetString(fmt.Sprintf(v3KeyURL, url.QueryEscape(kid), url.QueryEscape(m[1])), h)
	if err != nil {
		return ""
	}
	m = dataStringRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	parts := strings.SplitN(m[1], ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func decodePlayToken(s string) string {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				return firstNonEmpty(val(m, "GetPlayInfoToken"), val(m, "play_info"))
			}
		}
	}
	return ""
}
