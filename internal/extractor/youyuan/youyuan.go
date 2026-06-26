// Package youyuan implements an extractor for yijiayk.com courses.
package youyuan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL     = "https://h.yijiayk.com/"
	courseInfoAPI  = "https://m.yijiayk.com/course-api/app/course/getByCourseId?courseId=%s"
	chapterListAPI = "https://m.yijiayk.com/course-api/app/courseChapter/listPresentOrPrevious?courseId=%s&annualValue=0"
	videoTokenAPI  = "https://m.yijiayk.com/course-api/app/courseVideo/getToken?chapterId=%s&cacheId=0&clientType=pc"
	bjyAPI         = "https://www.baijiayun.com/vod/video/getPlayUrl?vid=%s&token=%s"
)

var (
	patterns     = []string{`(?:[\w-]+\.)?yijiayk\.com/`}
	cidRe        = regexp.MustCompile(`courseId=(\w+)`)
	titleCleanRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
)

func init() {
	extractor.Register(&Youyuan{}, extractor.SiteInfo{Name: "Youyuan", URL: "yijiayk.com", NeedAuth: true})
}

type Youyuan struct{}

func (s *Youyuan) Patterns() []string { return patterns }

type yyContext struct {
	c       *util.Client
	headers map[string]string
	cid     string
}

type yyLesson struct{ ID, Title string }

func (s *Youyuan) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("youyuan requires login cookies")
	}
	cid := parseCID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("youyuan: cannot parse courseId from URL")
	}
	ctx := &yyContext{c: util.NewClient(), headers: headersFromJar(opts.Cookies), cid: cid}
	ctx.c.SetCookieJar(opts.Cookies)
	info, err := ctx.requestJSON(fmt.Sprintf(courseInfoAPI, url.QueryEscape(cid)))
	if err != nil {
		return nil, fmt.Errorf("youyuan course info: %w", err)
	}
	title := firstNonEmpty(firstString(asMap(info["data"]), "courseName"), "youyuan_"+cid)
	chapters, err := ctx.requestJSON(fmt.Sprintf(chapterListAPI, url.QueryEscape(cid)))
	if err != nil {
		return nil, fmt.Errorf("youyuan chapter list: %w", err)
	}
	lessons := collectLessons(chapters)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("youyuan: no courseLessonList entries found")
	}
	var entries []*extractor.MediaInfo
	for _, lesson := range lessons {
		entry, err := ctx.resolveLesson(lesson)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("youyuan: no baijiayun media resolved")
	}
	return &extractor.MediaInfo{Site: "youyuan", Title: cleanTitle(title), Entries: entries}, nil
}

func parseCID(raw string) string {
	if m := cidRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}

func headersFromJar(jar http.CookieJar) map[string]string {
	h := map[string]string{"User-Agent": util.RandomUA(), "referer": refererURL, "Referer": refererURL, "Accept": "application/json, text/plain, */*"}
	var parts []string
	for _, raw := range []string{refererURL, "https://m.yijiayk.com/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
			if ck.Name == "accessToken" {
				h["accessToken"], h["authorization"] = ck.Value, ck.Value
			}
		}
	}
	if len(parts) > 0 {
		h["cookie"] = strings.Join(parts, "; ")
	}
	return h
}

func (x *yyContext) requestJSON(apiURL string) (map[string]any, error) {
	body, err := x.c.GetString(apiURL, x.headers)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func collectLessons(root map[string]any) []yyLesson {
	var out []yyLesson
	for ci, chapter := range extractItems(root["data"]) {
		chapterName := firstNonEmpty(firstString(chapter, "chapterName"), "默认章节")
		for li, lesson := range extractItems(firstNonNil(chapter["courseLessonList"], chapter["lessonList"], chapter["children"])) {
			id := firstString(lesson, "id", "chapterId")
			if id == "" {
				continue
			}
			name := firstNonEmpty(firstString(lesson, "lessonName", "chapterName", "title"), chapterName)
			out = append(out, yyLesson{ID: id, Title: cleanTitle(fmt.Sprintf("[%d.%d]--%s", ci+1, li+1, name))})
		}
	}
	return out
}

func (x *yyContext) resolveLesson(lesson yyLesson) (*extractor.MediaInfo, error) {
	resp, err := x.requestJSON(fmt.Sprintf(videoTokenAPI, url.QueryEscape(lesson.ID)))
	if err != nil {
		return nil, err
	}
	data := asMap(resp["data"])
	vid := firstString(data, "videoId", "video_id")
	token := firstString(data, "token")
	if vid == "" || token == "" {
		return nil, fmt.Errorf("youyuan: empty videoId/token for %s", lesson.ID)
	}
	playURL, err := shared.BaijiayunResolveVOD(x.c, vid, token, x.headers)
	if err != nil {
		return nil, err
	}
	_ = fmt.Sprintf(bjyAPI, vid, token)
	return &extractor.MediaInfo{Site: "youyuan", Title: lesson.Title, Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{playURL}, Format: pickFormat(playURL), Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"lesson_id": lesson.ID, "video_id": vid}}, nil
}

func extractItems(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	m := asMap(v)
	for _, k := range []string{"data", "list", "records", "items", "courseLessonList", "children"} {
		if out := extractItems(m[k]); len(out) > 0 {
			return out
		}
	}
	return nil
}
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
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
func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
