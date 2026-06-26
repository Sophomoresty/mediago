// Package minshi implements an extractor for minshiedu.com courses.
package minshi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	origin              = "https://vip.minshiedu.com"
	referer             = "https://vip.minshiedu.com/#/course/courseHome"
	platform_proxy      = "am9pbmVhc3QtYXBw"
	system_id           = "82"
	course_home_route   = "/course/courseHome"
	course_list_api     = "https://vip.minshiedu.com/api/learning/ext/course/my"
	course_valid_api    = "https://vip.minshiedu.com/api/learning/ext/course/valid/expirationDateByCourse/%s"
	course_info_api     = "https://vip.minshiedu.com/api/learning/ext/courseDetails/new/courseTableInfo/%s"
	course_detail_api   = "https://vip.minshiedu.com/api/learning/ext/courseDetails/new/courseTableDetail/%s"
	material_api        = "https://vip.minshiedu.com/api/learning/ext/class/material/list"
	video_encrypted_api = "https://vip.minshiedu.com/api/learning/ext/course/videoEncryptedInfo/%s"
	polyv_secure_url    = "https://player.polyv.net/secure/%s.json"
	polyv_key_url       = "https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}"
)

var patterns = []string{`(?:[\w-]+\.)?minshiedu\.com/`}

func init() {
	extractor.Register(&Minshi{}, extractor.SiteInfo{Name: "Minshi", URL: "minshiedu.com", NeedAuth: true})
}

type Minshi struct{}

func (s *Minshi) Patterns() []string { return patterns }

type lesson struct{ TableID, VideoID, Title string }

type apiResp struct {
	Code any    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

var (
	catalogRe = regexp.MustCompile(`courseCatalog/(\d+)`)
	idKeys    = []string{"courseId", "catalogId", "course_id", "catalog_id", "id"}
)

func (s *Minshi) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("minshi requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := headers(course_home_route)
	cid := parseCID(rawURL)
	courses, _ := requestAPI(c, course_list_api, "POST", map[string]string{"playMethod": ""}, h)
	if cid == "" {
		cid = firstCourseID(courses)
	}
	if cid == "" {
		return nil, fmt.Errorf("minshi: cannot parse courseCatalog/courseId from URL and course list has no id")
	}
	info, err := requestAPI(c, fmt.Sprintf(course_info_api, url.PathEscape(cid)), "GET", nil, headers("/courseCatalog/"+cid))
	if err != nil {
		return nil, fmt.Errorf("minshi courseTableInfo: %w", err)
	}
	_ = fetchValid(c, cid, h)
	title := findFirst(info, "title", "name", "courseName", "catalogueName", "catalogName")
	if title == "" {
		title = firstCourseTitle(courses, cid)
	}
	if title == "" {
		title = "minshi_" + cid
	}
	lessons := collectLessons(info)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("minshi: no courseTableId/videoId lessons in data")
	}
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, le := range lessons {
		if le.TableID != "" && le.VideoID == "" {
			detail, _ := requestAPI(c, fmt.Sprintf(course_detail_api, url.PathEscape(le.TableID)), "GET", nil, headers("/courseCatalog/"+le.TableID))
			le.VideoID = findFirst(detail, "videoId", "vid")
			if le.Title == "" {
				le.Title = findFirst(detail, "title", "name", "tableName")
			}
		}
		play := getPlayToken(c, le, cid)
		vid := first(play.VideoID, le.VideoID)
		if vid == "" || seen[vid] {
			continue
		}
		seen[vid] = true
		streamURL, err := resolvePolyv(c, vid, h)
		if err != nil || streamURL == "" {
			continue
		}
		name := clean(fmt.Sprintf("[%d]--%s", i+1, first(le.Title, vid)))
		entries = append(entries, &extractor.MediaInfo{Site: "minshi", Title: name, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{streamURL}, Format: formatOf(streamURL), Headers: h}}, Extra: map[string]any{"course_table_id": le.TableID, "video_id": vid, "playsafe": play.Token}})
	}
	// Promote source materials / file artifacts to first-class entries.
	fileEntries := collectFileEntries(c, cid, lessons, h)
	entries = append(entries, fileEntries...)

	if len(entries) == 0 {
		return nil, fmt.Errorf("minshi: no playable videos or downloadable files found")
	}
	return &extractor.MediaInfo{Site: "minshi", Title: clean(title), Entries: entries, Extra: map[string]any{"course_id": cid}}, nil
}

type playToken struct{ Token, VideoID string }

func getPlayToken(c *util.Client, le lesson, cid string) playToken {
	for _, targetID := range []string{le.VideoID, le.TableID} {
		if targetID == "" {
			continue
		}
		v, err := requestAPI(c, fmt.Sprintf(video_encrypted_api, url.PathEscape(targetID)), "GET", nil, headers("/courseCatalog/"+targetID))
		if err != nil {
			continue
		}
		pt := playToken{Token: findFirst(v, "playsafe", "playSafe", "token"), VideoID: first(findFirst(v, "videoId", "vid"), le.VideoID)}
		if pt.Token != "" || pt.VideoID != "" {
			_ = cid
			return pt
		}
	}
	return playToken{}
}

func resolvePolyv(c *util.Client, vid string, h map[string]string) (string, error) {
	sec, err := shared.PolyvResolveSecure(c, vid, h)
	if err != nil {
		return "", err
	}
	m3u8, err := shared.PolyvPickBestManifest(sec)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(m3u8, "http") {
		return m3u8, nil
	}
	return m3u8, nil
}

func requestAPI(c *util.Client, api, method string, data map[string]string, h map[string]string) (any, error) {
	var body string
	var err error
	if method == "POST" {
		body, err = c.PostForm(api, data, h)
	} else {
		body, err = c.GetString(api, h)
	}
	if err != nil {
		return nil, err
	}
	var resp apiResp
	if err := json.Unmarshal([]byte(body), &resp); err == nil {
		if resp.Data != nil {
			return resp.Data, nil
		}
	}
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func fetchValid(c *util.Client, cid string, h map[string]string) bool {
	v, err := requestAPI(c, fmt.Sprintf(course_valid_api, url.PathEscape(cid)), "GET", nil, h)
	if err != nil {
		return false
	}
	return !strings.Contains(strings.ToLower(fmt.Sprint(v)), "expired")
}

func collectLessons(v any) []lesson {
	var out []lesson
	walk(v, func(m map[string]any) {
		tid := firstTextMap(m, "courseTableId", "id")
		vid := firstTextMap(m, "videoId", "vid")
		if tid != "" || vid != "" {
			out = append(out, lesson{TableID: tid, VideoID: vid, Title: firstTextMap(m, "title", "name", "courseName", "catalogueName", "catalogName", "chapterName", "tableName")})
		}
	})
	return out
}

// collectFileEntries fetches material lists for each lesson and returns
// downloadable file artifacts as first-class MediaInfo entries.
// Mirrors Minshi_Course._get_material_list + _build_file_info from source.
func collectFileEntries(c *util.Client, cid string, lessons []lesson, h map[string]string) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, le := range lessons {
		if le.TableID == "" {
			continue
		}
		v, err := requestAPI(c, material_api, "POST", map[string]string{"courseTableId": le.TableID}, headers("/courseCatalog/"+le.TableID))
		if err != nil {
			continue
		}
		fileIdx := 0
		walk(v, func(m map[string]any) {
			u := firstTextMap(m, "path", "filePath", "url", "fileUrl", "downloadUrl")
			if u == "" || seen[u] {
				return
			}
			seen[u] = true
			fileIdx++
			fileURL := absURL(u)
			fileName := firstTextMap(m, "fileName", "name", "title")
			if fileName == "" {
				parsed, err := url.Parse(fileURL)
				if err == nil {
					fileName = path.Base(parsed.Path)
				}
			}
			if fileName == "" {
				return
			}
			fileFmt := normalizeFileFmt(firstTextMap(m, "fileType"), fileName, fileURL)
			// Strip extension from display name (source: _build_file_info)
			displayName := fileName
			if dot := strings.LastIndex(displayName, "."); dot >= 0 {
				displayName = displayName[:dot]
			}
			entryTitle := clean(fmt.Sprintf("(%d.%d)--%s", i+1, fileIdx, displayName))
			streamKey := "file"
			if fileFmt != "" {
				streamKey = fileFmt
			}
			out = append(out, &extractor.MediaInfo{
				Site:  "minshi",
				Title: entryTitle,
				Streams: map[string]extractor.Stream{
					streamKey: {
						Quality: "file",
						URLs:    []string{fileURL},
						Format:  fileFmt,
						Headers: h,
					},
				},
				Extra: map[string]any{
					"type":      "file",
					"file_url":  fileURL,
					"file_name": fileName,
					"file_fmt":  fileFmt,
				},
			})
		})
	}
	return out
}

// normalizeFileFmt replicates Minshi_Course._normalize_file_fmt from source.
func normalizeFileFmt(raw, fileName, fileURL string) string {
	raw = strings.TrimSpace(strings.ToLower(strings.Trim(raw, ".")))
	// Handle MIME-style subtypes (e.g. "application/pdf")
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	mimeMap := map[string]string{
		"vnd.openxmlformats-officedocument.wordprocessingml.document":    "docx",
		"msword":    "doc",
		"vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
		"vnd.ms-powerpoint": "ppt",
		"application/pdf":   "pdf",
	}
	if mapped, ok := mimeMap[raw]; ok {
		raw = mapped
	}
	// Source: prefer filename extension over MIME when file_fmt is non-empty and filename has dot
	if raw != "" {
		if dot := strings.LastIndex(fileName, "."); dot >= 0 {
			raw = strings.ToLower(fileName[dot+1:])
		}
	}
	// Fall back to URL path extension
	if raw == "" {
		parsed, err := url.Parse(fileURL)
		if err == nil {
			p := parsed.Path
			if dot := strings.LastIndex(p, "."); dot >= 0 {
				raw = strings.ToLower(p[dot+1:])
			}
		}
	}
	return raw
}

func parseCID(rawURL string) string {
	if m := catalogRe.FindStringSubmatch(rawURL); m != nil {
		return m[1]
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	for _, k := range idKeys {
		if v := u.Query().Get(k); v != "" {
			return v
		}
	}
	if u.Fragment != "" {
		if m := catalogRe.FindStringSubmatch(u.Fragment); m != nil {
			return m[1]
		}
		q := u.Fragment
		if i := strings.Index(q, "?"); i >= 0 {
			if vals, err := url.ParseQuery(q[i+1:]); err == nil {
				for _, k := range idKeys {
					if v := vals.Get(k); v != "" {
						return v
					}
				}
			}
		}
	}
	return ""
}

func headers(route string) map[string]string {
	if route == "" {
		route = course_home_route
	}
	return map[string]string{"Accept": "application/json, text/plain, */*", "Origin": origin, "Referer": origin + "#" + route, "joineast-request-path": route, "joineast-system-id": system_id, "platform-proxy": platform_proxy, "Content-Type": "application/json;charset=UTF-8"}
}

func firstCourseID(v any) string { return findFirst(v, "id", "courseId", "course_id") }
func firstCourseTitle(v any, id string) string {
	_ = id
	return findFirst(v, "title", "name", "courseName")
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
func firstTextMap(m map[string]any, keys ...string) string {
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
func findFirst(v any, keys ...string) string {
	out := ""
	walk(v, func(m map[string]any) {
		if out == "" {
			out = firstTextMap(m, keys...)
		}
	})
	return out
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func clean(s string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s), " .")
}
func absURL(u string) string {
	if strings.HasPrefix(u, "http") {
		return u
	}
	return strings.TrimRight(origin, "/") + "/" + strings.TrimLeft(u, "/")
}
func formatOf(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
