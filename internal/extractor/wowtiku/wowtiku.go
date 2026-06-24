// Package wowtiku implements an extractor for wowtiku.com.
package wowtiku

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	refererURL       = "https://www.wowtiku.com/"
	originURL        = "https://www.wowtiku.com"
	apiHost          = "https://new.wowtiku.net"
	wwwAPIHost       = "https://www.wowtiku.net"
	buyListsAPI      = "/goods/buy_lists"
	detailAPI        = "/goods/sg_detail"
	subsetAPI        = "/goods/subset"
	documentAPI      = "/goods/class_document_lists"
	platformListsAPI = "/config/platform_lists"
	stsAPI           = "/alibaba/get_sts"
	playTokenAPI     = "/alibaba/get_play_token"
	vodRegion        = "cn-shanghai"
)

var patterns = []string{`(?:[\w-]+\.)?wowtiku\.com/|(?:[\w-]+\.)?wowtiku\.net/`}

func init() {
	extractor.Register(&Wowtiku{}, extractor.SiteInfo{Name: "Wowtiku", URL: "wowtiku.com", NeedAuth: true})
}

type Wowtiku struct{}

func (s *Wowtiku) Patterns() []string { return patterns }

type wtSession struct{ token string }
type wtVideo struct{ title, vid, directURL string }

func (s *Wowtiku) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wowtiku requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess := wtSession{token: tokenFromJar(opts.Cookies)}
	if sess.token == "" {
		return nil, fmt.Errorf("wowtiku requires token/Authorization cookie")
	}
	if _, err := requestData(c, sess, "/question_bank/user/user_info", nil, nil, "GET", "www", "v2"); err != nil {
		return nil, fmt.Errorf("wowtiku user_info: %w", err)
	}
	cid := courseID(rawURL)
	if cid == "" {
		var err error
		cid, err = firstCourseID(c, sess)
		if err != nil {
			return nil, err
		}
	}
	detail, err := requestData(c, sess, detailAPI, map[string]string{"id": cid}, nil, "GET", "", "v1")
	if err != nil {
		return nil, err
	}
	videos := collectVideos(detail)
	for _, classID := range classIDs(detail) {
		if subset, err := requestData(c, sess, subsetAPI, map[string]string{"stage_goods_id": cid, "class_id": classID}, nil, "GET", "", "v1"); err == nil {
			videos = append(videos, collectVideos(subset)...)
		}
	}
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}
	for _, v := range videos {
		entry, err := resolveVideo(c, sess, v)
		if err != nil || entry == nil || len(entry.Streams) == 0 {
			continue
		}
		u := entry.Streams["default"].URLs[0]
		if u != "" && !seen[u] {
			seen[u] = true
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wowtiku: no playable Aliyun/direct video resolved")
	}
	return &extractor.MediaInfo{Site: "wowtiku", Title: detailTitle(detail, cid), Entries: entries}, nil
}

func firstCourseID(c *util.Client, sess wtSession) (string, error) {
	platforms := []string{"3"}
	if data, err := requestData(c, sess, platformListsAPI, nil, nil, "GET", "", "v1"); err == nil {
		for _, m := range mapsUnder(data) {
			if id := firstNonEmpty(val(m, "id"), val(m, "platform_id")); id != "" {
				platforms = append(platforms, id)
			}
		}
	}
	seenPlatform := map[string]bool{}
	for _, pid := range platforms {
		if pid == "" || seenPlatform[pid] {
			continue
		}
		seenPlatform[pid] = true
		data, err := requestData(c, sess, buyListsAPI, map[string]string{"platform_id": pid}, nil, "GET", "", "v1")
		if err != nil {
			continue
		}
		for _, m := range mapsUnder(data) {
			if id := firstNonEmpty(val(m, "id"), val(m, "stage_goods_id")); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("wowtiku: purchased course list is empty")
}

func requestData(c *util.Client, sess wtSession, path string, params map[string]string, data map[string]string, method, host, version string) (any, error) {
	root, err := requestJSON(c, sess, path, params, data, method, host, version)
	if err != nil {
		return nil, err
	}
	code := fmt.Sprint(root["code"])
	if code != "1" && code != "0" && code != "200" && code != "<nil>" && code != "" {
		return nil, fmt.Errorf("wowtiku API code=%s", code)
	}
	if d, ok := root["data"]; ok {
		return d, nil
	}
	return root, nil
}
func requestJSON(c *util.Client, sess wtSession, path string, params map[string]string, data map[string]string, method, host, version string) (map[string]any, error) {
	version = firstNonEmpty(version, "v1")
	base := apiHost
	if host == "www" {
		base = wwwAPIHost
	}
	apiURL := path
	if !strings.HasPrefix(apiURL, "http") {
		apiURL = strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	}
	headers := wtHeaders(sess, version, host)
	var body string
	var err error
	if strings.EqualFold(method, "POST") {
		body, err = c.PostForm(apiURL, data, headers)
	} else {
		if len(params) > 0 {
			q := url.Values{}
			for k, v := range params {
				q.Set(k, v)
			}
			apiURL += "?" + q.Encode()
		}
		body, err = c.GetString(apiURL, headers)
	}
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("wowtiku parse JSON: %w", err)
	}
	return root, nil
}

func collectVideos(data any) []wtVideo {
	out := []wtVideo{}
	for _, m := range mapsUnder(data) {
		direct := firstMediaURL(m)
		vid := firstNonEmpty(val(m, "vid"), val(m, "video_id"), val(m, "videoId"))
		if direct == "" && vid == "" {
			continue
		}
		out = append(out, wtVideo{title: firstNonEmpty(val(m, "name"), val(m, "title"), val(m, "subject_name")), vid: vid, directURL: direct})
	}
	return out
}
func classIDs(data any) []string {
	out, seen := []string{}, map[string]bool{}
	for _, m := range mapsUnder(data) {
		id := firstNonEmpty(val(m, "class_id"), val(m, "classId"), val(m, "id"))
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
func resolveVideo(c *util.Client, sess wtSession, v wtVideo) (*extractor.MediaInfo, error) {
	if v.directURL != "" {
		return media(v.title, normalizeURL(v.directURL), mediaFormat(v.directURL), map[string]any{"vid": v.vid}), nil
	}
	if v.vid == "" {
		return nil, fmt.Errorf("wowtiku: empty vid")
	}
	sts, err := requestData(c, sess, stsAPI, nil, nil, "POST", "www", "v1")
	if err != nil {
		return nil, err
	}
	u, err := aliyunPlayURL(c, sts, v.vid)
	if err != nil {
		return nil, err
	}
	return media(v.title, u, mediaFormat(u), map[string]any{"vid": v.vid, "vod_region": vodRegion}), nil
}
func aliyunPlayURL(c *util.Client, sts any, vid string) (string, error) {
	m := firstMap(sts)
	ak, sec, tk := firstNonEmpty(val(m, "ky"), val(m, "AccessKeyId")), firstNonEmpty(val(m, "sc"), val(m, "AccessKeySecret")), firstNonEmpty(val(m, "tk"), val(m, "SecurityToken"))
	if ak == "" || sec == "" {
		return "", fmt.Errorf("wowtiku: empty Aliyun STS credentials")
	}
	params := map[string]string{"Action": "GetPlayInfo", "VideoId": vid, "Format": "JSON", "Version": "2017-03-21", "AccessKeyId": ak, "SecurityToken": tk, "SignatureMethod": "HMAC-SHA1", "SignatureNonce": fmt.Sprintf("%d", time.Now().UnixNano()), "SignatureVersion": "1.0", "Timestamp": time.Now().UTC().Format("2006-01-02T15:04:05Z"), "StreamType": "video", "Formats": "m3u8", "ResultType": "Multiple", "Channel": "HTML5", "PlayerVersion": "2.32.0"}
	params["Signature"] = aliyunSignature(params, sec, "GET")
	q := url.Values{}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	body, err := c.GetString("https://vod."+vodRegion+".aliyuncs.com/?"+q.Encode(), map[string]string{"Origin": originURL, "Referer": refererURL, "Accept": "application/json, text/plain, */*"})
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return "", err
	}
	if u := firstURLByKeys(root, "PlayURL", "PlayUrl", "playUrl", "url"); u != "" {
		return normalizeURL(u), nil
	}
	return "", fmt.Errorf("wowtiku: Aliyun play response has no PlayURL")
}
func aliyunSignature(params map[string]string, secret, method string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k != "Signature" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, aliyunEscape(k)+"="+aliyunEscape(params[k]))
	}
	toSign := strings.ToUpper(method) + "&" + aliyunEscape("/") + "&" + aliyunEscape(strings.Join(parts, "&"))
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(toSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func aliyunEscape(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

func wtHeaders(sess wtSession, version, host string) map[string]string {
	h := map[string]string{"From-type-v": "2.2.6", "Content-Type": "application/x-www-form-urlencoded", "Accept": "application/vnd.wowtiku." + version + "+json", "Origin": originURL, "Referer": refererURL, "Authorization": "Bearer " + sess.token}
	if host == "www" {
		h["From-type"] = "3"
	}
	return h
}
func tokenFromJar(jar http.CookieJar) string {
	for _, raw := range []string{originURL, apiHost, wwwAPIHost} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				if strings.EqualFold(c.Name, "token") || strings.EqualFold(c.Name, "Authorization") || strings.EqualFold(c.Name, "access_token") || strings.EqualFold(c.Name, "accessToken") {
					return strings.TrimPrefix(c.Value, "Bearer ")
				}
			}
		}
	}
	return ""
}
func courseID(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		if q.Get("id") != "" || q.Get("stage_goods_id") != "" || q.Get("course_id") != "" {
			return firstNonEmpty(q.Get("id"), q.Get("stage_goods_id"), q.Get("course_id"))
		}
		if strings.Contains(u.Fragment, "?") {
			fq, _ := url.ParseQuery(strings.SplitN(u.Fragment, "?", 2)[1])
			return firstNonEmpty(fq.Get("id"), fq.Get("stage_goods_id"), fq.Get("course_id"))
		}
	}
	return ""
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
func firstMap(v any) map[string]any {
	for _, m := range mapsUnder(v) {
		return m
	}
	return nil
}
func firstMediaURL(m map[string]any) string {
	return firstURLByKeys(m, "video_url", "play_url", "playUrl", "m3u8_url", "url", "path", "src", "file_url")
}
func firstURLByKeys(v any, keys ...string) string {
	for _, m := range mapsUnder(v) {
		for _, k := range keys {
			if u := val(m, k); strings.HasPrefix(u, "http") || strings.HasPrefix(u, "//") {
				return u
			}
		}
	}
	return ""
}
func detailTitle(data any, cid string) string {
	for _, m := range mapsUnder(data) {
		if t := firstNonEmpty(val(m, "name"), val(m, "title")); t != "" {
			return t
		}
	}
	return "wowtiku_" + cid
}
func val(m map[string]any, k string) string {
	if v, ok := m[k]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, "/"))
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}
func media(title, u, fmtName string, extra map[string]any) *extractor.MediaInfo {
	if title == "" {
		title = "video"
	}
	return &extractor.MediaInfo{Site: "wowtiku", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{u}, Format: fmtName, Headers: map[string]string{"Referer": refererURL}}}, Extra: extra}
}
func mediaFormat(u string) string {
	l := strings.ToLower(u)
	if strings.Contains(l, ".m3u8") {
		return "m3u8"
	}
	if strings.Contains(l, ".mp3") {
		return "mp3"
	}
	return "mp4"
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
