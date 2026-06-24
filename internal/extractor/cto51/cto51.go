// Package cto51 implements 51CTO course and training extraction.
//
// Source alignment:
//
//	Mooc/Courses/Cto51/Cto51_Base.pyc.1shot.cdc.py
//	Mooc/Courses/Cto51/Cto51_Course.pyc.1shot.cdc.py
package cto51

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlStudyCourse       = "https://edu.51cto.com/center/course/user/get-study-course"
	urlWejobIndex        = "https://edu.51cto.com/center/wejob/user/index?train_id=%s"
	urlWejobCourse       = "https://edu.51cto.com/center/wejob/user/course?train_id=%s"
	urlEStudy            = "https://e.51cto.com/study"
	urlCourse            = "https://edu.51cto.com/course/%s.html"
	urlLesson            = "https://edu.51cto.com/lesson/%s.html"
	urlCourseTypeAPI     = "https://edu.51cto.com/center/course/user-june/search-list"
	urlCourseListAPI     = "https://edu.51cto.com/center/course/user-june/list"
	urlLessonListAPI     = "https://edu.51cto.com/center/course/user/get-lesson-list"
	urlLessonFileListAPI = "https://edu.51cto.com/center/course/index/lesson-file-list"
	urlMaterialListAPI   = "https://edu.51cto.com/center/course/user-june/file-list"
	urlCourseIndexAPI    = "https://edu.51cto.com/center/course/index/index-api"
	urlCourseFileListAPI = "https://edu.51cto.com/center/course/index/file-list"
	urlVodPlayAuthAPI    = "https://edu.51cto.com/center/player/play/vod-play-auth"
	urlQCloudPlayAPI     = "https://playvideo.qcloud.com/getplayinfo/v4/%s/%s"
	urlTrainingAPI       = "https://apie.51cto.com/api"
	urlTrainStageAPI     = "https://edu.51cto.com/center/wejob/user/train-course-stage-ajax"
	urlTrainCourseAPI    = "https://edu.51cto.com/center/wejob/user/train-course-ajax"
	urlTrainInfoAPI      = "https://edu.51cto.com/center/wejob/user/course-info-ajax"
	urlTrainLiveAPI      = "https://edu.51cto.com/center/wejob/user/train-live-ajax"
	urlTrainFileAPI      = "https://edu.51cto.com/center/wejob/user/train-file-list-ajax"
	urlTrainNextAPI      = "https://edu.51cto.com/center/wejob/center/next-info"
	urlOrderListAPI      = "https://edu.51cto.com/center/orders/order/ajax-get-order-list"
)

var patterns = []string{`(?:[\w-]+\.)?51cto\.com/`}

func init() {
	extractor.Register(&Cto51{}, extractor.SiteInfo{Name: "Cto51", URL: "51cto.com", NeedAuth: true})
}

type Cto51 struct{}

func (c *Cto51) Patterns() []string { return patterns }

type route struct{ CID, LID, TrainID, TrainCourseID string }
type media struct{ URL, Title, Format string }

var (
	courseRe      = regexp.MustCompile(`(?i)/course/(\d+)\.html|(?:course_id|courseId|cid)=([0-9]+)`)
	lessonRe      = regexp.MustCompile(`(?i)/lesson/(\d+)\.html|(?:lesson_id|lessonId|lid|id)=([0-9]+)`) // line 75 + train lesson line 78
	trainRe       = regexp.MustCompile(`(?i)(?:train_id|trainId)=([0-9]+)|/(?:px/train/(\d+)\.html|training_(\d+)\.html)`)
	trainCourseRe = regexp.MustCompile(`(?i)(?:train_course_id|trainCourseId)=([0-9]+)|(\d+)_\d+`)
	lessonLinkRe  = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']*/lesson/(\d+)\.html[^"']*)["'][^>]*>(.*?)</a>`)
	titleRe       = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>|<title[^>]*>(.*?)</title>`)
	aliParamRe    = regexp.MustCompile(`(?is)var\s+aliplayparam\s*=\s*\{(?P<body>[\s\S]*?)\}\s*;`)
	jsPairRe      = regexp.MustCompile(`(?is)["']?([A-Za-z0-9_]+)["']?\s*:\s*["']([^"']+)["']`)
	mediaURLRe    = regexp.MustCompile(`(?i)https?:\\?/\\?/[^"'<>\s]+?\.(?:m3u8|mp4)(?:\?[^"'<>\s]*)?`)
)

func (x *Cto51) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("51cto requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := headers(rawURL)
	r := parseRoute(rawURL)

	if r.LID != "" {
		return resolveLesson(c, r.LID, h)
	}
	if r.TrainID != "" {
		return resolveTraining(c, r, h)
	}
	if r.CID != "" {
		return resolveCourse(c, r.CID, h)
	}
	return resolveMyCourses(c, h)
}

func resolveCourse(c *util.Client, cid string, h map[string]string) (*extractor.MediaInfo, error) {
	payloads := fetchJSONPayloads(c, h, []apiReq{
		{urlCourseIndexAPI, map[string]string{"course_id": cid, "course_id_str": cid}},
		{urlLessonListAPI, map[string]string{"id": cid, "page": "1"}},
		{urlLessonFileListAPI, map[string]string{"course_id": cid, "page": "1", "size": "100"}},
		{urlCourseFileListAPI, map[string]string{"course_id": cid}},
	})
	entries := entriesFromPayloads(c, payloads, h)
	if len(entries) == 0 {
		page := fmt.Sprintf(urlCourse, cid)
		body, err := c.GetString(page, h)
		if err != nil {
			return nil, fmt.Errorf("51cto fetch course page: %w", err)
		}
		for _, item := range parseLessonLinks(body) {
			entry, err := resolveLesson(c, item.ID, h)
			if err == nil {
				entry.Title = util.SanitizeFilename(firstNonEmpty(item.Title, entry.Title))
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			entries = entriesFromPayloads(c, []any{body}, h)
		}
		if len(entries) > 0 {
			return &extractor.MediaInfo{Site: "cto51", Title: util.SanitizeFilename(firstNonEmpty(extractTitle(body), "51cto_"+cid)), Entries: entries}, nil
		}
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	if len(entries) > 1 {
		return &extractor.MediaInfo{Site: "cto51", Title: "51cto_" + cid, Entries: entries}, nil
	}
	return nil, fmt.Errorf("51cto course %s: no playable lesson media", cid)
}

func resolveLesson(c *util.Client, lid string, h map[string]string) (*extractor.MediaInfo, error) {
	pageURL := fmt.Sprintf(urlLesson, lid)
	body, err := c.GetString(pageURL, h)
	if err != nil {
		return nil, fmt.Errorf("51cto fetch lesson page: %w", err)
	}
	if m := mediaFromText(body); m.URL != "" {
		return mediaInfo(firstNonEmpty(extractTitle(body), "lesson_"+lid), m.URL, m.Format, h), nil
	}
	if auth := parseAliPlayParam(body); len(auth) > 0 {
		if m, err := resolveAuth(c, auth, h); err == nil && m.URL != "" {
			return mediaInfo(firstNonEmpty(extractTitle(body), "lesson_"+lid), m.URL, m.Format, h), nil
		}
	}
	payloads := fetchJSONPayloads(c, h, []apiReq{{urlVodPlayAuthAPI, map[string]string{"lesson_id": lid, "id": lid}}})
	entries := entriesFromPayloads(c, payloads, h)
	if len(entries) > 0 {
		return entries[0], nil
	}
	return nil, fmt.Errorf("51cto lesson %s: no media URL/playAuth found", lid)
}

func resolveTraining(c *util.Client, r route, h map[string]string) (*extractor.MediaInfo, error) {
	if r.TrainCourseID != "" && r.LID != "" {
		return resolveLesson(c, r.LID, h)
	}
	payloads := fetchJSONPayloads(c, h, []apiReq{
		{urlTrainStageAPI, map[string]string{"train_id": r.TrainID}},
		{urlTrainCourseAPI, map[string]string{"train_id": r.TrainID}},
		{urlTrainInfoAPI, map[string]string{"train_id": r.TrainID, "train_course_id": r.TrainCourseID}},
		{urlTrainLiveAPI, map[string]string{"train_id": r.TrainID}},
		{urlTrainFileAPI, map[string]string{"train_id": r.TrainID}},
		{urlTrainNextAPI, map[string]string{"train_id": r.TrainID}},
	})
	entries := entriesFromPayloads(c, payloads, h)
	if len(entries) == 1 {
		return entries[0], nil
	}
	if len(entries) > 1 {
		return &extractor.MediaInfo{Site: "cto51", Title: "train_" + r.TrainID, Entries: entries}, nil
	}
	return nil, fmt.Errorf("51cto train %s: no playable media", r.TrainID)
}

func resolveMyCourses(c *util.Client, h map[string]string) (*extractor.MediaInfo, error) {
	payloads := fetchJSONPayloads(c, h, []apiReq{
		{urlStudyCourse, nil}, {urlCourseTypeAPI, nil}, {urlCourseListAPI, nil}, {urlTrainingAPI, map[string]string{"type": "1"}}, {urlOrderListAPI, map[string]string{"page": "1"}},
	})
	entries := entriesFromPayloads(c, payloads, h)
	if len(entries) == 0 {
		return nil, fmt.Errorf("51cto: no playable media in account course APIs")
	}
	return &extractor.MediaInfo{Site: "cto51", Title: "51cto", Entries: entries}, nil
}

type apiReq struct {
	URL    string
	Params map[string]string
}

func fetchJSONPayloads(c *util.Client, h map[string]string, reqs []apiReq) []any {
	var out []any
	for _, req := range reqs {
		body, err := c.GetString(addQuery(req.URL, req.Params), h)
		if err != nil {
			continue
		}
		var payload any
		if json.Unmarshal([]byte(body), &payload) == nil {
			out = append(out, payload)
		} else {
			out = append(out, body)
		}
	}
	return out
}

func entriesFromPayloads(c *util.Client, payloads []any, h map[string]string) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for _, p := range payloads {
		for _, m := range collectMedia(c, p, h) {
			if m.URL == "" || seen[m.URL] {
				continue
			}
			seen[m.URL] = true
			entries = append(entries, mediaInfo(firstNonEmpty(m.Title, "51cto"), m.URL, m.Format, h))
		}
	}
	return entries
}

func collectMedia(c *util.Client, v any, h map[string]string) []media {
	var out []media
	switch x := v.(type) {
	case string:
		if m := mediaFromText(x); m.URL != "" {
			out = append(out, m)
		}
		if auth := parseAliPlayParam(x); len(auth) > 0 {
			if m, err := resolveAuth(c, auth, h); err == nil && m.URL != "" {
				out = append(out, m)
			}
		}
	case map[string]any:
		title := textValue(x, "lesson_name", "lessonName", "course_name", "courseName", "title", "name", "file_name", "fileName")
		if auth := authFromMap(x); len(auth) > 0 {
			if m, err := resolveAuth(c, auth, h); err == nil && m.URL != "" {
				m.Title = title
				out = append(out, m)
			}
		}
		for _, key := range []string{"play_url", "playUrl", "url", "lesson_url", "lessonUrl", "video_url", "videoUrl", "m3u8", "m3u8_url", "file_url", "fileUrl", "path"} {
			if m := mediaFromText(textValue(x, key)); m.URL != "" {
				m.Title = title
				out = append(out, m)
			}
		}
		for _, vv := range x {
			out = append(out, collectMedia(c, vv, h)...)
		}
	case []any:
		for _, vv := range x {
			out = append(out, collectMedia(c, vv, h)...)
		}
	}
	return out
}

func resolveAuth(c *util.Client, auth map[string]string, h map[string]string) (media, error) {
	if auth["app_id"] != "" && auth["file_id"] != "" && auth["psign"] != "" {
		api := fmt.Sprintf(urlQCloudPlayAPI, url.PathEscape(auth["app_id"]), url.PathEscape(auth["file_id"]))
		body, err := c.GetString(addQuery(api, map[string]string{"keyId": "1", "psign": auth["psign"]}), h)
		if err != nil {
			return media{}, err
		}
		var payload any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			return media{}, err
		}
		if m := firstMedia(collectMedia(c, payload, h)); m.URL != "" {
			return m, nil
		}
	}

	// Aliyun VOD flow: if playAuth decoded to AccessKeyId/Secret/Token, sign
	// a GetPlayInfo request via shared.AliyunResolvePlayInfo (source _request_aliyun_play_info_by_rand).
	if auth["vod_video_id"] != "" && auth["access_key_id"] != "" {
		payload := shared.AliyunPlayPayload{
			AccessKeyID:     auth["access_key_id"],
			AccessKeySecret: auth["access_key_secret"],
			SecurityToken:   auth["sts_token"],
			Region:          firstNonEmpty(auth["region"], "cn-shanghai"),
			AuthInfo:        auth["auth_info"],
		}
		if info, err := shared.AliyunResolvePlayInfo(c, payload, auth["vod_video_id"], shared.AliyunPlayOptions{Headers: h}); err == nil && info.URL != "" {
			return media{URL: info.URL}, nil
		}
	}

	if auth["play_url"] != "" {
		return mediaFromText(auth["play_url"]), nil
	}
	return media{}, fmt.Errorf("51cto auth has no supported media")
}

func authFromMap(m map[string]any) map[string]string {
	out := map[string]string{
		"app_id":   textValue(m, "app_id", "appId", "appID", "appid"),
		"file_id":  textValue(m, "file_id", "fileId", "fileID", "fileid", "vid", "video_id", "videoId"),
		"psign":    textValue(m, "psign", "pSign", "p_sign", "playAuth", "play_auth", "sign", "token"),
		"play_url": textValue(m, "play_url", "playUrl", "url"),
	}
	if decoded := decodePlayAuth(out["psign"]); len(decoded) > 0 {
		for k, v := range decoded {
			if out[k] == "" {
				out[k] = v
			}
		}
	}
	return out
}

func parseAliPlayParam(text string) map[string]string {
	m := aliParamRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	out := map[string]string{}
	for _, pair := range jsPairRe.FindAllStringSubmatch(m[1], -1) {
		out[strings.ToLower(pair[1])] = pair[2]
	}
	return map[string]string{"app_id": firstNonEmpty(out["appid"], out["app_id"]), "file_id": firstNonEmpty(out["fileid"], out["file_id"], out["vid"]), "psign": firstNonEmpty(out["psign"], out["playsign"], out["playauth"], out["token"]), "play_url": out["url"]}
}

func decodePlayAuth(s string) map[string]string {
	if s == "" || strings.HasPrefix(s, "http") {
		return nil
	}
	padded := s + strings.Repeat("=", (4-len(s)%4)%4)
	b, err := base64.StdEncoding.DecodeString(padded)
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(padded)
	}
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return authFromMap(m)
}

func mediaFromText(text string) media {
	text = normalizeText(text)
	if m := mediaURLRe.FindStringSubmatch(text); m != nil {
		text = normalizeText(m[0])
	}
	lower := strings.ToLower(text)
	if !strings.HasPrefix(lower, "http") || !(strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4")) {
		return media{}
	}
	format := "mp4"
	if strings.Contains(lower, ".m3u8") {
		format = "m3u8"
	}
	return media{URL: text, Format: format}
}
