// Package minshi implements an extractor for minshiedu.com courses.
package minshi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
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
	polyvIVHex          = "01020305070B0D1113171D0705030201"
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
	h := headersFromJar(opts.Cookies, course_home_route)
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
	catalogGroups := buildCatalogGroups(info)
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, le := range lessons {
		if le.TableID != "" && le.VideoID == "" {
			detail, _ := requestAPI(c, fmt.Sprintf(course_detail_api, url.PathEscape(le.TableID)), "GET", nil, headersFromJar(opts.Cookies, "/courseCatalog/"+le.TableID))
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
		streamURL, manifest, err := resolvePolyv(c, vid, play.Token, h)
		if err != nil || streamURL == "" {
			continue
		}
		name := clean(fmt.Sprintf("[%d]--%s", i+1, first(le.Title, vid)))
		extra := map[string]any{"course_table_id": le.TableID, "video_id": vid, "playsafe": play.Token}
		if manifest != "" {
			extra["m3u8_manifest"] = manifest
			if strings.HasPrefix(strings.TrimSpace(manifest), "#EXTM3U") {
				streamURL = minshiM3U8DataURL(manifest)
				extra["m3u8_text"] = manifest
				extra["source_type"] = "m3u8_text"
			}
		}
		format := formatOf(streamURL)
		entries = append(entries, &extractor.MediaInfo{Site: "minshi", Title: name, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{streamURL}, Format: format, NeedMerge: format == "m3u8", Headers: h}}, Extra: extra})
	}
	// Promote source materials / file artifacts to first-class entries.
	fileEntries := collectFileEntries(c, cid, lessons, h)
	entries = append(entries, fileEntries...)

	if len(entries) == 0 {
		return nil, fmt.Errorf("minshi: no playable videos or downloadable files found")
	}
	extra := map[string]any{
		"course_id": cid,
		"price":     extractPrice(courses, info),
		"purchased": true,
		"valid":     fetchValid(c, cid, h),
	}
	if len(catalogGroups) > 0 {
		extra["catalog"] = catalogGroups
	}
	return &extractor.MediaInfo{Site: "minshi", Title: clean(title), Entries: entries, Extra: extra}, nil
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
		pt := playToken{Token: pickPlayToken(v), VideoID: first(findFirst(v, "videoId", "vid"), le.VideoID)}
		if pt.Token != "" || pt.VideoID != "" {
			_ = cid
			return pt
		}
	}
	return playToken{}
}

func resolvePolyv(c *util.Client, vid string, token string, h map[string]string) (string, string, error) {
	if token != "" {
		if manifest, sourceURL, err := getPolyvM3U8(c, vid, token, h); err == nil && sourceURL != "" {
			return sourceURL, manifest, nil
		}
	}
	sec, err := shared.PolyvResolveSecure(c, vid, h)
	if err != nil {
		return "", "", err
	}
	m3u8, err := shared.PolyvPickBestManifest(sec)
	if err != nil {
		return "", "", err
	}
	if strings.HasPrefix(m3u8, "http") {
		return m3u8, "", nil
	}
	if strings.HasPrefix(strings.TrimSpace(m3u8), "#EXTM3U") {
		return minshiM3U8DataURL(m3u8), m3u8, nil
	}
	return m3u8, "", nil
}

func requestAPI(c *util.Client, api, method string, data map[string]string, h map[string]string) (any, error) {
	var body string
	var err error
	if method == "POST" {
		payload, marshalErr := json.Marshal(data)
		if marshalErr != nil {
			return nil, marshalErr
		}
		resp, postErr := c.Post(api, bytes.NewReader(payload), h)
		if postErr != nil {
			return nil, postErr
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, api)
		}
		raw, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		body = string(raw)
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
	if m, ok := v.(map[string]any); ok {
		for _, key := range []string{"valid", "isValid"} {
			if value, exists := m[key]; exists {
				return truthy(value)
			}
		}
		msg := strings.ToLower(firstTextMap(m, "msg", "message"))
		if msg != "" {
			return !strings.Contains(msg, "expire") && !strings.Contains(msg, "过期") && !strings.Contains(msg, "失效")
		}
	}
	low := strings.ToLower(fmt.Sprint(v))
	return !strings.Contains(low, "expired") && !strings.Contains(low, "expire") && !strings.Contains(low, "过期") && !strings.Contains(low, "失效")
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
			fileFmt := normalizeFileFmt(firstTextMap(m, "fileType", "fileFmt", "suffix", "ext"), fileName, fileURL)
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
		"vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
		"msword": "doc",
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

func headersFromJar(jar http.CookieJar, route string) map[string]string {
	h := headers(route)
	payload := minshiCookiePayload(jar)
	if payload != "" {
		h["cookie"] = payload
		h["Cookie"] = payload
		if token := pickTokenFromRaw(payload); token != "" {
			h["authorization"] = token
		}
	}
	return h
}

func minshiCookiePayload(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	seen := map[string]bool{}
	for _, raw := range []string{origin, "https://www.minshiedu.com/"} {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		for _, ck := range jar.Cookies(u) {
			value := strings.TrimSpace(ck.Value)
			if value == "" || seen[ck.Name] {
				continue
			}
			seen[ck.Name] = true
			if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") || strings.Contains(value, "token") || strings.Contains(value, "Authorization") {
				return value
			}
		}
	}
	var parts []string
	for _, raw := range []string{origin, "https://www.minshiedu.com/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			key := ck.Name + "=" + ck.Value
			if key != "=" {
				parts = append(parts, key)
			}
		}
	}
	return strings.Join(parts, "; ")
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

func walkValues(v any, fn func(any)) {
	fn(v)
	switch t := v.(type) {
	case map[string]any:
		for _, x := range t {
			walkValues(x, fn)
		}
	case []any:
		for _, x := range t {
			walkValues(x, fn)
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

func pickPlayToken(v any) string {
	if token := findFirst(v, "playsafe", "playSafe", "play_safe", "playSafeToken", "playToken", "play_token"); token != "" {
		return pickTokenFromRaw(token)
	}
	out := ""
	walkValues(v, func(x any) {
		if out != "" {
			return
		}
		switch t := x.(type) {
		case map[string]any:
			for _, key := range []string{"token"} {
				if s := firstTextMap(t, key); s != "" {
					out = s
					return
				}
			}
		case string:
			out = pickTokenFromRaw(t)
		}
	})
	return out
}

func pickTokenFromRaw(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), `'"`)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		var decoded any
		if json.Unmarshal([]byte(raw), &decoded) == nil {
			if token := pickPlayToken(decoded); token != "" {
				return token
			}
		}
	}
	for _, key := range []string{"Authorization", "authorization", "access_token", "accessToken", "token"} {
		re := regexp.MustCompile(`(?:^|[?&;,\s])` + regexp.QuoteMeta(key) + `\s*[:=]\s*"?([^";,\s]+)`)
		if m := re.FindStringSubmatch(raw); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	if !strings.Contains(raw, "=") && !strings.HasPrefix(raw, "http") {
		return raw
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

func buildCatalogGroups(info any) []map[string]any {
	var groups []map[string]any
	switch root := info.(type) {
	case map[string]any:
		for _, key := range []string{"courseList", "children", "childList", "list"} {
			if rows, ok := root[key].([]any); ok {
				for _, row := range rows {
					if m, ok := row.(map[string]any); ok {
						lessons := collectLessons(m)
						if len(lessons) == 0 {
							continue
						}
						groups = append(groups, map[string]any{
							"title":   first(firstTextMap(m, "title", "name", "chapterName", "catalogName"), "默认章节"),
							"lessons": lessonSummaries(lessons),
						})
					}
				}
			}
		}
	}
	if len(groups) == 0 {
		lessons := collectLessons(info)
		if len(lessons) > 0 {
			groups = append(groups, map[string]any{"title": "默认章节", "lessons": lessonSummaries(lessons)})
		}
	}
	return groups
}

func lessonSummaries(lessons []lesson) []map[string]string {
	out := make([]map[string]string, 0, len(lessons))
	for _, le := range lessons {
		out = append(out, map[string]string{"course_table_id": le.TableID, "video_id": le.VideoID, "title": le.Title})
	}
	return out
}

func extractPrice(payloads ...any) float64 {
	for _, payload := range payloads {
		var price float64
		walk(payload, func(m map[string]any) {
			if price > 0 {
				return
			}
			for _, key := range []string{"showPrice", "salePrice", "discountPrice", "price", "goodsPrice", "payPrice", "marketPrice", "originalPrice", "activityPrice", "coursePrice", "sellingPrice", "realPrice", "amount"} {
				if p := normalizePrice(m[key]); p > 0 {
					price = p
					return
				}
			}
		})
		if price > 0 {
			return price
		}
	}
	return 0
}

func normalizePrice(value any) float64 {
	var f float64
	switch x := value.(type) {
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case float64:
		f = x
	case string:
		m := regexp.MustCompile(`\d+(?:\.\d+)?`).FindString(x)
		if m == "" {
			return 0
		}
		parsed, err := strconv.ParseFloat(m, 64)
		if err != nil {
			return 0
		}
		f = parsed
	default:
		s := strings.TrimSpace(fmt.Sprint(value))
		if s == "" || s == "<nil>" {
			return 0
		}
		return normalizePrice(s)
	}
	if f > 5000 && f == float64(int64(f)) {
		f /= 100
	}
	return f
}

func truthy(value any) bool {
	switch x := value.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	}
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "", "0", "false", "no", "null", "<nil>", "过期", "失效", "expired":
		return false
	default:
		return true
	}
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
	low := strings.ToLower(strings.TrimSpace(u))
	if strings.Contains(low, ".m3u8") || strings.HasPrefix(low, "data:application/vnd.apple.mpegurl") || strings.HasPrefix(low, "#extm3u") {
		return "m3u8"
	}
	return "mp4"
}

func minshiM3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}
