// Package classin implements ClassIn m3u8 / record-class extraction.
//
// Source alignment:
//
//	Mooc/Courses/Classin/Classin_Base.pyc.1shot.cdc.py
//	Mooc/Courses/Classin/Classin_Course.pyc.1shot.cdc.py
//	Mooc/Courses/Classin/Classin_Video.pyc.1shot.cdc.py
package classin

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlM3u8Token   = "https://w0d-cdn.eeo.cn/cloudspace/api/tencent/getM3u8Token"
	urlLessonInfo  = "https://w0d-cdn.eeo.cn/api/classin.api.php?action=getLessonRecordInfo"
	urlUserRecords = "https://a0d-cdn.eeo.cn/uc/classin_uc.php?action=getuserRecordclasses"
	urlRecordGet   = "https://w0d-cdn.eeo.cn/lms/app/activity/recordClass/get"
	urlW0sCDN      = "https://w0s-cdn.eeo.cn/files/pm3u8/"

	// Course-tree APIs (host t0d-cdn.eeo.cn), aligned with Classin_Config.pyc.
	urlCourseList   = "https://t0d-cdn.eeo.cn/course/app/member/course_list"
	urlCategoryList = "https://t0d-cdn.eeo.cn/lms/app/category/list"
	urlUnitList     = "https://t0d-cdn.eeo.cn/lms/app/course/studentUnitList"
	urlUnitActivity = "https://t0d-cdn.eeo.cn/lms/app/course/studentUnitActivityList"
	urlHomeworkGet  = "https://t0d-cdn.eeo.cn/lms/app/activity/homework/get"
	urlFileDownInfo = "https://t0d-cdn.eeo.cn/lms/app/file/getDownInfo"
	classinCDNBase  = "https://w0s-cdn.eeo.cn"

	classinUID = "70755184"
	classinKey = "EJAeISv47899WRMjdYK1769177711067"
	classinUA  = "Windows/11 (24H2) ClassIn/6.0.3.2611 QNAM/5.15.1"
)

var patterns = []string{`(?:[\w-]+\.)?eeo\.cn/|files/pm3u8/`}

func init() {
	extractor.Register(&Classin{}, extractor.SiteInfo{Name: "Classin", URL: "eeo.cn", NeedAuth: true})
}

type Classin struct{}

func (c *Classin) Patterns() []string { return patterns }

type ids struct {
	SID        string
	CourseID   string
	ActivityID string
	ClassID    string
	RecordID   string
}

type tokenResponse struct {
	ErrorInfo struct {
		Errno int    `json:"errno"`
		Msg   string `json:"msg"`
	} `json:"error_info"`
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
}

type playable struct {
	Title  string
	URL    string
	Format string
}

var (
	pm3u8Re = regexp.MustCompile(`(?i)(files/pm3u8/[^\s?#"'<>]+\.m3u8)`)
	idRe    = regexp.MustCompile(`(?i)(?:SID|schoolUid|sid)=([^&#]+)|(?:courseId|clientCourseId)=([^&#]+)|(?:activityId|recordId|lessonId)=([^&#]+)|(?:clientClassId|classId)=([^&#]+)`)
)

func (ci *Classin) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		c.SetCookieJar(opts.Cookies)
	}
	headers := map[string]string{"Referer": referer(rawURL), "User-Agent": classinUA}

	if pm3u8 := extractPM3U8Path(rawURL); pm3u8 != "" {
		mediaURL, err := resolveM3U8Token(c, pm3u8)
		if err != nil {
			return nil, err
		}
		title := "ClassIn-" + strings.TrimSuffix(lastPath(pm3u8), ".m3u8")
		return mediaInfo(title, mediaURL, "m3u8", headers), nil
	}

	parsed := parseIDs(rawURL)
	payloads := requestRecordPayloads(c, parsed)
	var plays []playable
	for _, payload := range payloads {
		plays = append(plays, collectPlayables(c, payload)...)
	}
	plays = dedupePlayables(plays)
	if len(plays) == 1 {
		return mediaInfo(firstNonEmpty(plays[0].Title, "classin"), plays[0].URL, plays[0].Format, headers), nil
	}
	if len(plays) > 1 {
		entries := make([]*extractor.MediaInfo, 0, len(plays))
		for i, p := range plays {
			entries = append(entries, mediaInfo(firstNonEmpty(p.Title, fmt.Sprintf("ClassIn-%02d", i+1)), p.URL, p.Format, headers))
		}
		return &extractor.MediaInfo{Site: "classin", Title: "classin", Entries: entries}, nil
	}

	// No direct media resolved from the URL. Fall back to the bulk course tree
	// (course_list -> studentUnitList -> studentUnitActivityList -> homework/get
	// -> file/getDownInfo), aligned with Classin_Course.pyc.
	if course, err := ci.extractCourseTree(c, parsed, headers); err == nil && course != nil {
		return course, nil
	}
	return nil, fmt.Errorf("classin: no pm3u8/mp4 media found in record APIs")
}

func requestRecordPayloads(c *util.Client, in ids) []any {
	var out []any
	forms := []struct {
		api  string
		data map[string]string
	}{
		{urlRecordGet, map[string]string{"getStuStatistic": "1", "activityId": in.ActivityID, "courseId": in.CourseID, "classRole": "1", "clusterRole": "0", "SID": in.SID}},
		{urlLessonInfo, map[string]string{"flag": "1", "memberUid": classinUID, "clientClassId": firstNonEmpty(in.ClassID, in.RecordID, in.ActivityID), "clientCourseId": in.CourseID, "SID": in.SID}},
		{urlUserRecords, map[string]string{"clientCourseId": in.CourseID, "UID": classinUID, "schoolUid": in.SID, "clientClassId": firstNonEmpty(in.ClassID, in.RecordID)}},
	}
	for _, form := range forms {
		if !hasUsefulValue(form.data) {
			continue
		}
		payload, err := postFormJSON(c, form.api, form.data)
		if err == nil {
			out = append(out, payload)
		}
	}
	return out
}

func postFormJSON(c *util.Client, api string, data map[string]string) (any, error) {
	headers := classinSignHeaders(data, "application/x-www-form-urlencoded")
	body, err := c.PostForm(api, data, headers)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// postFormMap is postFormJSON typed to the standard ClassIn envelope so callers
// can read error_info/data directly without re-walking an any tree.
func postFormMap(c *util.Client, api string, data map[string]string) (classinEnvelope, error) {
	var env classinEnvelope
	headers := classinSignHeaders(data, "application/x-www-form-urlencoded")
	body, err := c.PostForm(api, data, headers)
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return env, err
	}
	return env, nil
}

// postJSONMap signs the JSON body keys (course_list is the only JSON endpoint)
// and returns the parsed envelope. The signature still hashes the flat key=value
// pairs, matching _create_sign_headers in the Python source.
func postJSONMap(c *util.Client, api string, data map[string]string) (classinEnvelope, error) {
	var env classinEnvelope
	headers := classinSignHeaders(data, "application/json")
	payload, err := json.Marshal(jsonBody(data))
	if err != nil {
		return env, err
	}
	resp, err := c.Post(api, bytes.NewReader(payload), headers)
	if err != nil {
		return env, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return env, fmt.Errorf("classin %s: HTTP %d", api, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return env, err
	}
	return env, nil
}

func resolveM3U8Token(c *util.Client, pm3u8Path string) (string, error) {
	pub, err := generatePublicKeyPEM()
	if err != nil {
		return "", err
	}
	data := map[string]string{"publicKey": pub, "fileUrl": pm3u8Path}
	body, err := c.PostForm(urlM3u8Token, data, classinSignHeaders(data, "application/x-www-form-urlencoded"))
	if err != nil {
		return "", fmt.Errorf("classin getM3u8Token: %w", err)
	}
	var resp tokenResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", fmt.Errorf("classin parse getM3u8Token: %w", err)
	}
	if resp.ErrorInfo.Errno != 1 || resp.Data.Token == "" {
		return "", fmt.Errorf("classin getM3u8Token errno=%d msg=%q", resp.ErrorInfo.Errno, resp.ErrorInfo.Msg)
	}
	parts := strings.Split(resp.Data.Token, "&")
	manifest := "https://w0s-cdn.eeo.cn/" + strings.TrimLeft(pm3u8Path, "/") + "?expires=43200&ci-process=getplaylist&tokenType=JwtToken&token=" + parts[0]
	if len(parts) > 1 {
		manifest += "&" + strings.Join(parts[1:], "&")
	}
	return manifest, nil
}

func collectPlayables(c *util.Client, payload any) []playable {
	var out []playable
	for _, node := range walkMaps(payload) {
		title := textValue(node, "lessonName", "name", "title", "fileName")
		if video := textValue(node, "video"); strings.HasPrefix(strings.TrimSpace(video), "[") || strings.HasPrefix(strings.TrimSpace(video), "{") {
			var nested any
			if json.Unmarshal([]byte(video), &nested) == nil {
				out = append(out, collectPlayables(c, nested)...)
			}
		}
		for _, key := range []string{"pm3u8_path", "pm3u8", "Pm3u8", "m3u8", "M3u8", "Url", "url", "mp4_url", "path"} {
			if p := playableFromString(c, textValue(node, key), title); p.URL != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func playableFromString(c *util.Client, raw string, title string) playable {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return playable{}
	}
	if path := extractPM3U8Path(raw); path != "" {
		mediaURL, err := resolveM3U8Token(c, path)
		if err == nil {
			return playable{Title: title, URL: mediaURL, Format: "m3u8"}
		}
	}
	if strings.HasPrefix(raw, "http") && (strings.Contains(strings.ToLower(raw), ".mp4") || strings.Contains(strings.ToLower(raw), ".m3u8")) {
		return playable{Title: title, URL: raw, Format: pickFormat(raw)}
	}
	return playable{}
}

func classinSignHeaders(data map[string]string, contentType string) map[string]string {
	ts := fmt.Sprint(time.Now().Unix())
	pairs := make([]string, 0, len(data)+1)
	for k, v := range data {
		pairs = append(pairs, k+"="+v)
	}
	pairs = append(pairs, "timeStamp="+ts)
	sort.Strings(pairs)
	sign := util.MD5(strings.Join(pairs, "&") + "&key=" + classinKey)
	return map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Content-Type": contentType,
		"User-Agent":   classinUA,
		"x-eeo-uid":    classinUID,
		"x-eeo-sign":   sign,
		"x-eeo-ts":     ts,
	}
}

func parseIDs(raw string) ids {
	var out ids
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		out.SID = firstNonEmpty(q.Get("SID"), q.Get("sid"), q.Get("schoolUid"))
		out.CourseID = firstNonEmpty(q.Get("courseId"), q.Get("clientCourseId"))
		out.ActivityID = firstNonEmpty(q.Get("activityId"), q.Get("recordId"), q.Get("lessonId"))
		out.ClassID = firstNonEmpty(q.Get("clientClassId"), q.Get("classId"))
	}
	for _, m := range idRe.FindAllStringSubmatch(raw, -1) {
		out.SID = firstNonEmpty(out.SID, m[1])
		out.CourseID = firstNonEmpty(out.CourseID, m[2])
		out.ActivityID = firstNonEmpty(out.ActivityID, m[3])
		out.ClassID = firstNonEmpty(out.ClassID, m[4])
	}
	return out
}

func extractPM3U8Path(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if m := pm3u8Re.FindStringSubmatch(raw); m != nil {
		return strings.TrimLeft(m[1], "/")
	}
	if strings.HasPrefix(raw, "http") {
		if u, err := url.Parse(raw); err == nil && strings.Contains(u.Path, "pm3u8") {
			return strings.TrimLeft(u.Path, "/")
		}
	}
	if strings.HasPrefix(raw, "files/pm3u8/") {
		return raw
	}
	if strings.HasPrefix(raw, strings.TrimPrefix(urlW0sCDN, "https://w0s-cdn.eeo.cn/")) {
		return raw
	}
	return ""
}

func generatePublicKeyPEM() (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	switch x := v.(type) {
	case map[string]any:
		out = append(out, x)
		for _, vv := range x {
			out = append(out, walkMaps(vv)...)
		}
	case []any:
		for _, vv := range x {
			out = append(out, walkMaps(vv)...)
		}
	}
	return out
}

func mediaInfo(title, mediaURL, format string, headers map[string]string) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "classin", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{
		"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers},
	}}
}

func dedupePlayables(in []playable) []playable {
	seen := map[string]bool{}
	var out []playable
	for _, p := range in {
		if p.URL == "" || seen[p.URL] {
			continue
		}
		seen[p.URL] = true
		out = append(out, p)
	}
	return out
}

func hasUsefulValue(m map[string]string) bool {
	for k, v := range m {
		if k != "UID" && k != "memberUid" && k != "classRole" && k != "clusterRole" && k != "flag" && k != "getStuStatistic" && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func textValue(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func referer(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/"
	}
	return "https://www.eeo.cn"
}

func lastPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return "classin"
	}
	return parts[len(parts)-1]
}

func pickFormat(mediaURL string) string {
	if strings.Contains(strings.ToLower(mediaURL), ".m3u8") {
		return "m3u8"
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
