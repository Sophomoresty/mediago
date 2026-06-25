// Package yizhiknow implements an extractor for yizhiknow.com courses.
package yizhiknow

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	refererURL         = "https://user.yizhiknow.com"
	originURL          = "https://user.yizhiknow.com"
	apiHost            = "https://curriculum-api.yizhiknow.com"
	apiSecret          = "dcwsnmsb"
	listPath           = "/curriculum/user/getMultiPlatformMyCurricums"
	liveListPath       = "/curriculum/user/getMyselfLiveCurricumX"
	detailPath         = "/curriculum/newDetailX"
	statusPath         = "/curriculum/user/getCurriculumStatusV2"
	lessonResourcePath = "/curriculum/getLessonResourceV2"
	liveResourcePath   = "/curriculum/getPlayLiveSteamX"
	platformName       = "yizhiknow"
	platformType       = "wxkt"
)

var (
	patterns     = []string{`(?:[\w-]+\.)?yizhiknow\.com/`}
	cidRe        = regexp.MustCompile(`(?:/course/video/(\d+)|[?&](?:curriculum_id|curriculumId|course_id|id)=(\d+))`)
	titleCleanRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
)

func init() {
	extractor.Register(&Yizhiknow{}, extractor.SiteInfo{Name: "Yizhiknow", URL: "yizhiknow.com", NeedAuth: true})
}

type Yizhiknow struct{}

func (s *Yizhiknow) Patterns() []string { return patterns }

type yzContext struct {
	c       *util.Client
	token   string
	cid     string
	headers map[string]string
}

type yzLesson struct {
	ID       string
	Title    string
	Type     string
	Material any
	Raw      map[string]any
}

func (s *Yizhiknow) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("yizhiknow requires login cookies")
	}
	cid := parseCID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("yizhiknow: cannot parse curriculum id from URL")
	}
	ctx := &yzContext{c: util.NewClient(), token: tokenFromJar(opts.Cookies), cid: cid}
	ctx.c.SetCookieJar(opts.Cookies)
	ctx.headers = ctx.baseHeaders()
	if ctx.token == "" {
		return nil, fmt.Errorf("yizhiknow: Access-Token/token cookie is required")
	}
	if err := ctx.checkCookie(); err != nil {
		return nil, err
	}
	detail, err := ctx.detail()
	if err != nil {
		return nil, err
	}
	_, _ = ctx.requestData(statusPath, map[string]string{
		"curriculum_id": cid,
		"platform":      platformType,
	}, nil, "GET")
	title := firstNonEmpty(firstString(asMap(detail["curriculum_detail"]), "title"), firstString(detail, "title"), "yizhiknow_"+cid)
	lessons := collectLessons(detail)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("yizhiknow: no lessons found")
	}
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for _, lesson := range lessons {
		if lesson.ID == "" || seen[lesson.ID] {
			continue
		}
		seen[lesson.ID] = true
		resolved := ctx.resolveLesson(lesson)
		entries = append(entries, resolved...)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("yizhiknow: no media URL resolved")
	}
	return &extractor.MediaInfo{Site: "yizhiknow", Title: cleanTitle(title), Entries: entries}, nil
}

func parseCID(raw string) string {
	m := cidRe.FindStringSubmatch(raw)
	if len(m) == 0 {
		return ""
	}
	for _, v := range m[1:] {
		if v != "" {
			return v
		}
	}
	return ""
}

func tokenFromJar(jar http.CookieJar) string {
	for _, raw := range []string{refererURL, originURL + "/", apiHost + "/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			if in(ck.Name, "token", "Token", "Access-Token", "access_token", "accessToken") {
				return ck.Value
			}
		}
	}
	return ""
}

func (x *yzContext) baseHeaders() map[string]string {
	return map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Content-Type": "application/json;charset=UTF-8",
		"Origin":       originURL,
		"Referer":      refererURL,
	}
}

func (x *yzContext) checkCookie() error {
	_, err := x.requestData(listPath, map[string]string{
		"page":      "1",
		"page_size": "1",
		"platform":  platformType,
	}, nil, "GET")
	if err != nil {
		return fmt.Errorf("yizhiknow check cookie: %w", err)
	}
	return nil
}

func (x *yzContext) detail() (map[string]any, error) {
	data, err := x.requestData(detailPath, map[string]string{"curriculum_id": x.cid}, nil, "GET")
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (x *yzContext) requestData(path string, params map[string]string, data any, method string) (map[string]any, error) {
	resp, err := x.requestJSON(method, path, params, data)
	if err != nil {
		return nil, err
	}
	code := firstString(resp, "code")
	if code != "" && code != "0" && code != "200" {
		return nil, fmt.Errorf("yizhiknow api code=%s msg=%s", code, firstString(resp, "msg", "message"))
	}
	if d := asMap(resp["data"]); len(d) > 0 {
		return d, nil
	}
	return resp, nil
}

// requestJSON sends an authenticated API request. Sign params go into HTTP
// headers (token, sign, time-stamp, nonce-str). Original params go as query
// string; data goes as JSON body for POST. This matches the source
// _request_json which merges sign output into headers, not params/body.
func (x *yzContext) requestJSON(method, path string, params map[string]string, data any) (map[string]any, error) {
	if method == "" {
		method = "GET"
	}
	apiURL := apiHost + path

	// Build combined payload for sign computation (params + data merged).
	payload := map[string]any{}
	for k, v := range params {
		payload[k] = v
	}
	if m, ok := data.(map[string]string); ok {
		for k, v := range m {
			payload[k] = v
		}
	}

	// Compute sign; result goes into HTTP headers.
	signHdrs := signParams(payload, x.token)

	// Build request headers: base headers + sign headers.
	h := map[string]string{}
	for k, v := range x.headers {
		h[k] = v
	}
	delete(h, "Access-Token") // source pops Access-Token before request
	for k, v := range signHdrs {
		h[k] = v
	}

	// Always encode params as query string (requests.request sends params
	// as query string regardless of HTTP method).
	u, _ := url.Parse(apiURL)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	if strings.EqualFold(method, "GET") {
		body, err := x.c.GetString(u.String(), h)
		if err != nil {
			return nil, err
		}
		return parseJSON(body)
	}

	// POST: send data as JSON body.
	var bodyData any = data
	if bodyData == nil {
		bodyData = map[string]any{}
	}
	b, _ := json.Marshal(bodyData)
	resp, err := x.c.Post(u.String(), strings.NewReader(string(b)), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseJSON(string(respBody))
}

// signParams computes the authentication signature. The sign value is
// md5(apiSecret + base64(serialized_payload)), then conditionally
// concatenated with the token based on timestamp % 5 % 2. The result is
// returned as HTTP header key-values (token, sign, time-stamp, nonce-str).
func signParams(payload map[string]any, token string) map[string]string {
	ts := fmt.Sprint(time.Now().Unix())
	ns := nonce(32)

	combined := map[string]any{
		"token":      token,
		"time_stamp": ts,
		"nonce_str":  ns,
	}
	for k, v := range payload {
		combined[k] = v
	}

	serialized := serializeSignValue(combined, false)
	b64 := base64.StdEncoding.EncodeToString([]byte(serialized))
	sum := md5.Sum([]byte(apiSecret + b64))
	md5hex := hex.EncodeToString(sum[:])

	tsInt, _ := strconv.ParseInt(ts, 10, 64)
	var sign string
	if tsInt%5%2 != 0 {
		sign = md5hex + token
	} else {
		sign = token + md5hex
	}

	return map[string]string{
		"token":      token,
		"sign":       sign,
		"time-stamp": ts,
		"nonce-str":  ns,
	}
}

// serializeSignValue recursively serializes a value for signing, matching
// the source _serialize_sign_value. Dicts become sorted key=value pairs
// joined by &, wrapped in {} when nested. Lists become comma-joined values
// wrapped in [] when nested. None/nil values are skipped.
func serializeSignValue(value any, nested bool) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			val := v[k]
			if val == nil {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%s", k, serializeSignValue(val, true)))
		}
		s := strings.Join(parts, "&")
		if nested {
			return "{" + s + "}"
		}
		return s
	case []any:
		var parts []string
		for _, item := range v {
			if item == nil {
				continue
			}
			parts = append(parts, serializeSignValue(item, true))
		}
		s := strings.Join(parts, ",")
		if nested {
			return "[" + s + "]"
		}
		return s
	default:
		return fmt.Sprint(value)
	}
}

func nonce(n int) string {
	chars := "ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678"
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(chars[rand.Intn(len(chars))])
	}
	return b.String()
}

func collectLessons(detail map[string]any) []yzLesson {
	lessonList := firstNonNil(detail["lesson_list"], detail["lessonList"], detail["lessons"])
	var out []yzLesson
	var walk func(any, []int)
	walk = func(v any, idx []int) {
		if arr, ok := v.([]any); ok {
			for i, it := range arr {
				walk(it, append(idx, i+1))
			}
			return
		}
		m := asMap(v)
		if len(m) == 0 {
			return
		}
		if child := firstNonNil(m["lesson"], m["lessons"], m["children"], m["list"]); len(extractItems(child)) > 0 {
			walk(child, idx)
			return
		}
		id := firstString(m, "lesson_id", "curriculum_lesson_id", "id")
		if id == "" {
			return
		}
		title := cleanTitle(fmt.Sprintf("[%s]--%s", joinIdx(idx), firstNonEmpty(firstString(m, "lesson_title", "title", "name"), id)))
		out = append(out, yzLesson{ID: id, Title: title, Type: firstString(m, "type"), Material: firstNonNil(m["study_material"], m["material"]), Raw: m})
	}
	walk(lessonList, nil)
	return out
}

// resolveLesson resolves media and material entries for a single lesson.
// Returns a slice because each lesson can produce both a media entry and
// zero or more material (courseware) entries.
func (x *yzContext) resolveLesson(lesson yzLesson) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo

	// Determine numeric lesson type.
	lessonType := 0
	if t, err := strconv.Atoi(lesson.Type); err == nil {
		lessonType = t
	}

	// 1. Resolve media (video/audio).
	var resource map[string]any
	switch {
	case lessonType == 1 || lessonType == 2:
		// Video-type: request lesson resource API.
		resourceResp, err := x.requestJSON("GET", lessonResourcePath, map[string]string{
			"curriculum_id": x.cid,
			"lesson_id":     lesson.ID,
			"source":        "web",
			"platform":      platformType,
		}, nil)
		if err == nil {
			resource = asMap(resourceResp["data"])
		}
		if len(resource) == 0 {
			resource = lesson.Raw
		}
	case lessonType == 8:
		// Live-type: get vid from stream_vod, call live resource API.
		streamVod := asMap(lesson.Raw["stream_vod"])
		vid := firstString(streamVod, "vid_x", "vid")
		if vid != "" {
			if live, err := x.requestData(liveResourcePath, map[string]string{"vid_x": vid}, nil, "GET"); err == nil {
				resource = live
			}
		}
		if len(resource) == 0 {
			resource = lesson.Raw
		}
	default:
		// Other types: use raw lesson data directly.
		resource = lesson.Raw
	}

	urls := collectMediaCandidates(resource, lesson.Type)

	// Fallback: if no URLs found and not already tried live, check for vid.
	if len(urls) == 0 && lessonType != 8 {
		streamVod := asMap(lesson.Raw["stream_vod"])
		if vid := firstString(streamVod, "vid_x", "vid"); vid != "" {
			if live, err := x.requestData(liveResourcePath, map[string]string{"vid_x": vid}, nil, "GET"); err == nil {
				urls = collectMediaCandidates(live, lesson.Type)
			}
		}
		// Also try vid keys at top level of resource.
		if len(urls) == 0 {
			if vid := firstString(resource, "vid_x", "vid", "video_id"); vid != "" {
				if live, err := x.requestData(liveResourcePath, map[string]string{"vid_x": vid}, nil, "GET"); err == nil {
					urls = collectMediaCandidates(live, lesson.Type)
				}
			}
		}
	}

	// Emit first working media URL (source returns on first success).
	if len(urls) > 0 {
		u := urls[0]
		format := pickFormat(u)
		entries = append(entries, &extractor.MediaInfo{
			Site:  "yizhiknow",
			Title: lesson.Title,
			Streams: map[string]extractor.Stream{
				"default": {
					Quality: "best",
					URLs:    []string{u},
					Format:  format,
					Headers: map[string]string{"Referer": refererURL},
				},
			},
			Extra: map[string]any{"lesson_id": lesson.ID},
		})
	}

	// 2. Resolve materials (courseware/attachments).
	defaultMatName := lesson.Title + "_material"
	matItems := collectMaterialItems(lesson.Material, defaultMatName)
	for _, mat := range matItems {
		if mat.URL == "" {
			continue
		}
		format := materialFormat(mat.URL)
		entries = append(entries, &extractor.MediaInfo{
			Site:  "yizhiknow",
			Title: cleanTitle(mat.Name),
			Streams: map[string]extractor.Stream{
				"default": {
					Quality: "default",
					URLs:    []string{mat.URL},
					Format:  format,
					Headers: map[string]string{"Referer": refererURL},
				},
			},
			Extra: map[string]any{"lesson_id": lesson.ID, "type": "material"},
		})
	}

	return entries
}

func collectMediaCandidates(v any, lessonType string) []string {
	keys := []string{"mp4_url", "mp4Url", "media_url", "mediaUrl", "url", "play_url", "playUrl"}
	if lessonType != "1" && lessonType != "2" {
		keys = []string{"media_url", "mediaUrl", "url", "play_url", "playUrl", "mp4_url", "mp4Url"}
	}
	keySet, seen := map[string]bool{}, map[string]bool{}
	for _, k := range keys {
		keySet[k] = true
	}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			if u := normalizeMediaURL(t); u != "" && !seen[strings.ToLower(u)] {
				seen[strings.ToLower(u)] = true
				out = append(out, u)
			}
		case []any:
			for _, it := range t {
				walk(it)
			}
		case map[string]any:
			for k, v := range t {
				if keySet[k] {
					walk(v)
				}
			}
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(v)
	return out
}
