// Package wendao implements an extractor for wendao101.com courses.
package wendao

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	pcReferer            = "https://pc.wendao101.com/"
	pcOrigin             = "https://pc.wendao101.com"
	wapReferer           = "https://wap.wendao101.com/"
	wapOrigin            = "https://wap.wendao101.com"
	loginURL             = "https://wap.wendao101.com/#/pages_mine/myCourse/myCourse"
	apiHost              = "https://pc.wendao101.com/prod-api"
	wapAPIHost           = "https://wap.wendao101.com"
	appNameType          = 2
	defaultOrderPlatform = 0
	wapOrderPlatform     = 5
	userAgent            = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var patterns = []string{`(?:[\w-]+\.)?wendao101\.com/`}

func init() {
	extractor.Register(&Wendao{}, extractor.SiteInfo{Name: "Wendao", URL: "wendao101.com", NeedAuth: true})
}

type Wendao struct{}

func (s *Wendao) Patterns() []string { return patterns }

type wdSession struct{ token, openID, cookie string }
type wdCourse struct {
	id, title string
	price     float64
	purchased bool
	orderPlat int
	raw       map[string]any
}
type wdLesson struct {
	title, id, url string
	typ            int
}

var (
	cidRe      = regexp.MustCompile(`(?i)(?:[?&#]|^)(?:id|courseId|course_id)=(\d+)|/(?:course|detail)/(\d+)`)
	bareHostRe = regexp.MustCompile(`(?i)^[\w.-]+\.[a-z]{2,}(?:/|$)`)
)

func (s *Wendao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wendao requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess := wdSession{token: tokenFromJar(opts.Cookies), openID: openIDFromJar(opts.Cookies), cookie: cookieString(opts.Cookies)}
	if sess.token == "" {
		return nil, fmt.Errorf("wendao requires token/Admin-Token cookie or localStorage token")
	}
	if sess.openID == "" {
		sess.openID = sess.token
	}
	courseID := firstGroup(cidRe, rawURL)
	selected := wdCourse{purchased: true, orderPlat: wapOrderPlatform}
	if courseID == "" {
		course, err := firstCourse(c, sess)
		if err != nil {
			return nil, err
		}
		selected = course
		courseID = course.id
	} else if course, err := findCourse(c, sess, courseID); err == nil && course.id != "" {
		selected = course
	}
	detail, err := loadDetail(c, sess, courseID)
	if err != nil {
		return nil, err
	}
	lessons := lessonsFromDetail(detail)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("wendao: course detail has no downloadable lesson URLs")
	}
	onlyPDF := onlyPDFMode(opts.Quality)
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}
	for i, les := range lessons {
		u := normalizeURL(les.url)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		format := lessonFormat(les, u)
		if format == "" {
			continue
		}
		if onlyPDF && isLessonMedia(les, format) {
			continue
		}
		streamURL := u
		if strings.HasPrefix(strings.TrimSpace(u), "#EXTM3U") {
			streamURL = dataURL("application/vnd.apple.mpegurl", u)
		}
		stream := extractor.Stream{Quality: streamQuality(les.typ), URLs: []string{streamURL}, Format: format, Headers: downloadHeaders(sess), Extra: map[string]any{"lesson_id": les.id, "type": les.typ}}
		if format == "m3u8" {
			stream.NeedMerge = true
		}
		title := firstNonEmpty(les.title, "lesson_"+les.id, fmt.Sprintf("lesson_%d", i+1))
		entries = append(entries, &extractor.MediaInfo{Site: "wendao", Title: sanitizeTitle(title), Streams: map[string]extractor.Stream{"default": stream}, Extra: map[string]any{"lesson_id": les.id, "type": les.typ, "source_url": les.url}})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wendao: no downloadable lesson URL resolved")
	}
	price := firstPrice(coursePrice(detail), selected.price)
	purchased := selected.purchased
	if v, ok := purchaseFlag(detail); ok {
		purchased = v
	}
	return &extractor.MediaInfo{Site: "wendao", Title: sanitizeTitle(firstNonEmpty(detailTitle(detail, courseID), selected.title)), Entries: entries, Extra: map[string]any{"course_id": courseID, "price": price, "purchased": purchased, "order_platform": selected.orderPlat}}, nil
}

func firstCourse(c *util.Client, sess wdSession) (wdCourse, error) {
	courses, err := courseList(c, sess)
	if err != nil {
		return wdCourse{}, err
	}
	if len(courses) > 0 {
		return courses[0], nil
	}
	return wdCourse{}, fmt.Errorf("wendao: purchased course list is empty")
}

func findCourse(c *util.Client, sess wdSession, cid string) (wdCourse, error) {
	cid = strings.TrimSpace(cid)
	if cid == "" {
		return wdCourse{}, fmt.Errorf("wendao: empty course id")
	}
	courses, err := courseList(c, sess)
	if err != nil {
		return wdCourse{}, err
	}
	for _, course := range courses {
		if course.id == cid {
			return course, nil
		}
	}
	return wdCourse{}, nil
}

func courseList(c *util.Client, sess wdSession) ([]wdCourse, error) {
	var out []wdCourse
	seen := map[string]bool{}
	for _, source := range []struct {
		host, path string
		wap        bool
		platform   int
	}{
		{wapAPIHost, "/wap/home_page/course/purchased", true, wapOrderPlatform},
		{apiHost, "/home_page/course/purchased", false, defaultOrderPlatform},
	} {
		for page := 1; page <= 20; page++ {
			body := map[string]any{"appNameType": appNameType, "pageSize": 100, "pageNum": page, "orderPlatform": source.platform, "openId": sess.openID}
			data, err := requestData(c, sess, source.host, source.path, body, source.wap)
			if err != nil || data == nil {
				if page == 1 {
					break
				}
				return out, nil
			}
			items := courseItems(data)
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				course := normalizeCourse(item, source.platform)
				if course.id == "" || seen[course.id] {
					continue
				}
				seen[course.id] = true
				out = append(out, course)
			}
			if len(items) < 100 || !hasNextPage(data, page, len(out)) {
				break
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return out, nil
}

func loadDetail(c *util.Client, sess wdSession, courseID string) (map[string]any, error) {
	body := map[string]any{"needReferer": 1, "dataId": "", "platform": wapOrderPlatform, "appNameType": appNameType, "tempSeeSecret": "", "openId": sess.openID, "courseId": courseID}
	data, err := requestData(c, sess, wapAPIHost, "/wap/course/detail", body, true)
	if err != nil || len(mapsUnder(data)) == 0 {
		body["platform"] = defaultOrderPlatform
		data, err = requestData(c, sess, apiHost, "/course_detail/detail", body, false)
	}
	if err != nil {
		return nil, err
	}
	if m, ok := data.(map[string]any); ok {
		return m, nil
	}
	return nil, fmt.Errorf("wendao: detail response is not object")
}

func requestData(c *util.Client, sess wdSession, host, path string, body map[string]any, wap bool) (any, error) {
	root, err := requestJSON(c, sess, host, path, body, wap)
	if err != nil {
		return nil, err
	}
	code := fmt.Sprint(root["code"])
	if code != "0" && code != "200" && code != "<nil>" && code != "" {
		return nil, fmt.Errorf("wendao API code=%s", code)
	}
	if d, ok := root["data"]; ok {
		return d, nil
	}
	return root, nil
}
func requestJSON(c *util.Client, sess wdSession, host, path string, body map[string]any, wap bool) (map[string]any, error) {
	payload, _ := json.Marshal(body)
	apiURL := strings.TrimRight(host, "/") + "/" + strings.TrimLeft(path, "/")
	resp, err := c.Post(apiURL, bytes.NewReader(payload), headers(sess, wap))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("wendao parse JSON: %w", err)
	}
	return root, nil
}

func lessonsFromDetail(detail map[string]any) []wdLesson {
	lessons := []wdLesson{}
	seen := map[string]bool{}
	for _, m := range mapsUnder(detail) {
		u := firstNonEmpty(
			val(m, "courseDirectoryUrl"),
			val(m, "studyFileUrl"),
			val(m, "videoUrl"),
			val(m, "audioUrl"),
			val(m, "fileUrl"),
			val(m, "materialUrl"),
			val(m, "coursewareUrl"),
			val(m, "attachmentUrl"),
			val(m, "downloadUrl"),
			val(m, "resourceUrl"),
			val(m, "url"),
		)
		if u == "" {
			continue
		}
		id := firstNonEmpty(val(m, "id"), val(m, "courseDirectoryId"), val(m, "directoryId"))
		key := id + "|" + u
		if seen[key] {
			continue
		}
		seen[key] = true
		lessons = append(lessons, wdLesson{title: firstNonEmpty(val(m, "directoryName"), val(m, "studyFileName"), val(m, "fileName"), val(m, "name"), val(m, "title")), id: id, url: u, typ: toInt(m["directoryType"])})
	}
	return lessons
}
func detailTitle(detail map[string]any, cid string) string {
	for _, m := range mapsUnder(detail) {
		if t := firstNonEmpty(val(m, "title"), val(m, "courseTitle"), val(m, "courseUploadTitle"), val(m, "courseName")); t != "" {
			return t
		}
	}
	return "wendao_course_" + cid
}
func headers(sess wdSession, wap bool) map[string]string {
	h := map[string]string{"Content-Type": "application/json;charset=UTF-8", "Accept": "application/json, text/plain, */*", "User-Agent": userAgent}
	if sess.cookie != "" {
		h["cookie"] = sess.cookie
		h["Cookie"] = sess.cookie
	}
	if wap {
		h["Origin"], h["Referer"], h["token"] = wapOrigin, wapReferer, sess.openID
	} else {
		h["Origin"], h["Referer"], h["Authorization"], h["token"] = pcOrigin, pcReferer, "Bearer "+sess.token, sess.token
	}
	return h
}

func downloadHeaders(sess wdSession) map[string]string {
	h := headers(sess, true)
	h["Accept"] = "*/*"
	return h
}

func tokenFromJar(jar http.CookieJar) string {
	return stripBearer(cookieValue(jar, []string{"token", "Admin-Token", "adminToken", "Authorization", "authorization", "accessToken", "access_token", "Access-Token"}))
}
func openIDFromJar(jar http.CookieJar) string {
	return cookieValue(jar, []string{"openId", "openid", "OpenId"})
}
func cookieString(jar http.CookieJar) string {
	seen := map[string]bool{}
	parts := []string{}
	for _, raw := range []string{wapOrigin, pcOrigin, loginURL} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				if c.Name == "" || c.Value == "" || seen[strings.ToLower(c.Name)] {
					continue
				}
				seen[strings.ToLower(c.Name)] = true
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func cookieValue(jar http.CookieJar, names []string) string {
	for _, raw := range []string{wapOrigin, pcOrigin} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				for _, n := range names {
					if strings.EqualFold(c.Name, n) && c.Value != "" {
						return c.Value
					}
				}
			}
		}
	}
	return ""
}
func stripBearer(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return value
}
func mapsUnder(v any) []map[string]any {
	out := []map[string]any{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(v)
	return out
}
func val(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}
func firstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) == 0 {
		return ""
	}
	for _, v := range m[1:] {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "#EXTM3U") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if bareHostRe.MatchString(raw) {
		return "https://" + raw
	}
	return wapAPIHost + "/" + strings.TrimLeft(raw, "/")
}
func isMediaURL(u string) bool {
	return mediaFormat(u) != ""
}
func mediaFormat(u string) string {
	if strings.HasPrefix(strings.TrimSpace(u), "#EXTM3U") {
		return "m3u8"
	}
	parsed, err := url.Parse(u)
	target := u
	if err == nil {
		target = parsed.Path
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(target)), ".")
	switch ext {
	case "m3u8", "mp4", "flv", "mp3", "m4a", "aac", "wav", "pdf", "ppt", "pptx", "doc", "docx", "xls", "xlsx", "zip", "rar", "7z", "txt", "md":
		return ext
	default:
		l := strings.ToLower(u)
		for _, fallback := range []string{"m3u8", "mp4", "flv", "mp3", "m4a", "aac", "wav", "pdf"} {
			if strings.Contains(l, "."+fallback) {
				return fallback
			}
		}
		return ""
	}
}

func lessonFormat(lesson wdLesson, u string) string {
	if f := mediaFormat(u); f != "" {
		return f
	}
	switch lesson.typ {
	case 1:
		return "mp4"
	case 2:
		return "mp3"
	case 3:
		return "jpg"
	case 4:
		return "file"
	default:
		return ""
	}
}

func onlyPDFMode(quality string) bool {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "2", "pdf", "only_pdf", "only-pdf", "file", "files":
		return true
	default:
		return false
	}
}

func isLessonMedia(lesson wdLesson, format string) bool {
	switch lesson.typ {
	case 1, 2:
		return true
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "m3u8", "mp4", "m4v", "mov", "flv", "mp3", "m4a", "aac", "wav":
		return true
	default:
		return false
	}
}

func streamQuality(typ int) string {
	switch typ {
	case 1:
		return "video"
	case 2:
		return "audio"
	case 3, 4:
		return "file"
	default:
		return "source"
	}
}
func toInt(v any) int {
	var n int
	fmt.Sscanf(fmt.Sprint(v), "%d", &n)
	return n
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func courseItems(data any) []map[string]any {
	switch x := data.(type) {
	case []any:
		return mapsFromList(x)
	case map[string]any:
		for _, key := range []string{"list", "rows", "records", "data"} {
			switch v := x[key].(type) {
			case []any:
				return mapsFromList(v)
			case map[string]any:
				if items := courseItems(v); len(items) > 0 {
					return items
				}
			}
		}
	}
	return nil
}

func mapsFromList(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func normalizeCourse(item map[string]any, orderPlatform int) wdCourse {
	id := firstNonEmpty(val(item, "courseId"), val(item, "course_id"), val(item, "id"))
	title := firstNonEmpty(val(item, "courseTitle"), val(item, "courseName"), val(item, "title"), val(item, "courseUploadTitle"), val(item, "name"))
	purchased, ok := purchaseFlag(item)
	if !ok {
		purchased = true
	}
	if plat := toInt(item["orderPlatform"]); plat != 0 {
		orderPlatform = plat
	}
	return wdCourse{id: id, title: title, price: coursePrice(item), purchased: purchased, orderPlat: orderPlatform, raw: item}
}

func coursePrice(value any) float64 {
	for _, m := range mapsUnder(value) {
		for _, key := range []string{"coursePrice", "payPrice", "price", "originalPrice"} {
			if p := normalizePrice(m[key]); p > 0 {
				return p
			}
		}
	}
	return 0
}

func normalizePrice(value any) float64 {
	if value == nil {
		return 0
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return 0
	}
	text = strings.NewReplacer(",", "", "¥", "", "￥", "", "元", "").Replace(text)
	price, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || price < 0 {
		return 0
	}
	return math.Round(price*100) / 100
}

func firstPrice(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func purchaseFlag(value any) (bool, bool) {
	for _, m := range mapsUnder(value) {
		for _, key := range []string{"purchased", "isBuy", "is_buy", "isPurchased", "currentUserBuy", "current_user_buy", "buyStatus", "orderStatus"} {
			if raw, ok := m[key]; ok && raw != nil {
				text := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
				switch text {
				case "1", "true", "yes", "y":
					return true, true
				case "0", "false", "no", "n":
					return false, true
				default:
					return text != "", true
				}
			}
		}
	}
	return true, false
}

func hasNextPage(data any, page int, loaded int) bool {
	for _, m := range mapsUnder(data) {
		if v, ok := m["nextPage"]; ok {
			text := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
			return text != "" && text != "0" && text != "false" && text != "-1"
		}
		for _, key := range []string{"total", "totalCount", "count"} {
			if n := toInt(m[key]); n > 0 {
				return loaded < n
			}
		}
	}
	return page == 1
}

func sanitizeTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "wendao"
	}
	return regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`).ReplaceAllString(title, "_")
}

func dataURL(mime, content string) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(content))
}
