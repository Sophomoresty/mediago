// Package xiaoeapp implements an extractor for xiaoeknow.com app shops.
package xiaoeapp

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	appAPIBase         = "https://xiaoeapp-server.xiaoeknow.com"
	signSaltKey        = "xiaoeapp2024"
	courseListAPI      = "/app/my.all.course.lists.get/2.0.0"
	videoInfoAPI       = "/app/goods/xe.goods.detail.get/1.0.3"
	lookbackURLAPI     = "/app/alive/xe.alive.lookbackurl.get/1.0.0"
	courseListPageSize = 200
	appUA              = "okhttp/3.12.0;xet-android-app 6.1.1"
)

var patterns = []string{`(?:[\w-]+\.)?xiaoeknow\.com/`}
var idRe = regexp.MustCompile(`(?i)(?:/(?:p/course/(?:camp|alive|ebook|text|audio|video|ecourse|member|big_column|column)|v3/course/alive)/|[?&](?:activity_id|resource_id|goods_id|course_id)=)([A-Za-z0-9_\-]+)`) // source _get_h5_course_url url forms

func init() {
	extractor.Register(&Xiaoeapp{}, extractor.SiteInfo{Name: "Xiaoeapp", URL: "xiaoeknow.com", NeedAuth: true})
}

type Xiaoeapp struct{}

func (x *Xiaoeapp) Patterns() []string { return patterns }

type xeSession struct{ token, bUserID, appUserID, unionID, appID, cUserID string }
type xeItem struct {
	id, title, typ, appID, cUserID, productID string
	raw                                       map[string]any
}

func (x *Xiaoeapp) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xiaoeapp requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess := sessionFromCookies(opts.Cookies, rawURL)
	if sess.token == "" {
		return nil, fmt.Errorf("xiaoeapp requires token cookie")
	}
	if _, err := postAppAPI(c, sess, "/app/xe.user.info/1.0.0", map[string]any{}); err != nil {
		return nil, fmt.Errorf("xiaoeapp user.info: %w", err)
	}
	wantID := firstMatch(idRe, rawURL)
	items := []xeItem{}
	if wantID != "" {
		items = append(items, xeItem{id: wantID, title: wantID, typ: typeFromURL(rawURL), appID: sess.appID, cUserID: sess.cUserID, raw: map[string]any{"resource_id": wantID, "app_id": sess.appID, "resource_type": typeFromURL(rawURL)}})
	}
	listed, err := fetchCourseList(c, sess)
	if err == nil {
		items = append(items, listed...)
	}
	entries := []*extractor.MediaInfo{}
	blockedReasons := []string{}
	seen := map[string]bool{}
	seenItem := map[string]bool{}
	for _, it := range items {
		if it.id == "" || seenItem[it.id] || (wantID != "" && it.id != wantID) {
			continue
		}
		seenItem[it.id] = true
		u, extra := resolveItemURL(c, sess, it)
		if reason := val(extra, "blocked_reason"); reason != "" {
			blockedReasons = append(blockedReasons, reason)
			continue
		}
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		entries = append(entries, media(firstNonEmpty(it.title, it.id), u, extra))
	}
	if len(entries) == 0 {
		if len(blockedReasons) > 0 {
			return nil, fmt.Errorf("blocked: %s", blockedReasons[0])
		}
		return nil, fmt.Errorf("xiaoeapp: no playable URL resolved from course list/detail")
	}
	title := "xiaoeapp"
	if wantID != "" {
		title += "_" + wantID
	}
	return &extractor.MediaInfo{Site: "xiaoeapp", Title: title, Entries: entries}, nil
}

func fetchCourseList(c *util.Client, sess xeSession) ([]xeItem, error) {
	out := []xeItem{}
	seen := map[string]bool{}
	for _, rt := range []string{"0", "4", "10", "12", "8", "50", "51", "64"} {
		for page := 1; page <= 20; page++ {
			root, err := postAppAPI(c, sess, courseListAPI, map[string]any{"data": map[string]any{"page_size": courseListPageSize, "page": page}, "union_id": firstNonEmpty(sess.appUserID, sess.unionID), "state": 1, "resource_type": toInt(rt), "is_recent_update": 0})
			if err != nil {
				if len(out) > 0 {
					return out, nil
				}
				return nil, err
			}
			if code(root) != "0" {
				break
			}
			list := listUnder(root["data"], "list")
			if len(list) == 0 {
				break
			}
			for _, m := range list {
				it := itemFromMap(m, sess)
				if it.id == "" || it.title == "" || seen[it.id] || !itemAvailable(m) {
					continue
				}
				seen[it.id] = true
				out = append(out, it)
			}
			if len(list) < courseListPageSize || (toInt(firstNonEmpty(val(root["data"], "total"))) > 0 && len(out) >= toInt(firstNonEmpty(val(root["data"], "total")))) {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xiaoeapp course list is empty")
	}
	return out, nil
}

func resolveItemURL(c *util.Client, sess xeSession, it xeItem) (string, map[string]any) {
	if isLiveType(it.typ) {
		if containsPrivateXiaoeFlow(it.raw) {
			return "", map[string]any{"blocked_reason": "blocked: needs xiaoe private lookback m3u8 decrypt", "resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
		}
		if u := pickURL(it.raw); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
		}
		body := map[string]any{"type": "1", "app_id": firstNonEmpty(it.appID, sess.appID), "resource_id": it.id}
		if it.productID != "" {
			body["product_id"] = it.productID
		}
		if root, err := postAppAPI(c, sess, lookbackURLAPI, body); err == nil && code(root) == "0" {
			if containsPrivateXiaoeFlow(root["data"]) {
				return "", map[string]any{"blocked_reason": "blocked: needs xiaoe private lookback m3u8 decrypt", "resource_id": it.id, "resource_type": it.typ, "app_id": it.appID, "api": lookbackURLAPI}
			}
			if u := pickURL(root["data"]); u != "" {
				return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": it.appID, "api": lookbackURLAPI}
			}
		}
	}
	if containsPrivateXiaoeFlow(it.raw) {
		return "", map[string]any{"blocked_reason": "blocked: needs xiaoe private lookback m3u8 decrypt", "resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
	}
	if u := pickURL(it.raw); u != "" {
		return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
	}
	goodsType := goodsType(it.typ)
	body := map[string]any{"data": map[string]any{"time": 205, "is_show_resourcecount": 1, "hide_view_count": 0, "goods_type": goodsType, "goods_id": it.id}, "content_app_id": "", "app_id": firstNonEmpty(it.appID, sess.appID)}
	if it.cUserID != "" {
		body["c_user_id"] = it.cUserID
	}
	if sess.token != "" {
		body["token"] = sess.token
	}
	root, err := postAppAPI(c, sess, videoInfoAPI, body)
	if err != nil || code(root) != "0" {
		return "", nil
	}
	data := root["data"]
	if containsPrivateXiaoeFlow(data) {
		return "", map[string]any{"blocked_reason": "blocked: needs xiaoe private lookback m3u8 decrypt", "resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI}
	}
	if u := pickURL(data); u != "" {
		return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI}
	}
	for _, m := range mapsUnder(data) {
		if u := pickURL(m); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI}
		}
	}
	return "", nil
}

func postAppAPI(c *util.Client, sess xeSession, path string, body map[string]any) (map[string]any, error) {
	payload := baseParams(sess)
	for k, v := range body {
		payload[k] = v
	}
	if sess.token != "" {
		payload["api_token"] = sess.token
	}
	bodyJSON, err := marshalPythonJSON(payload)
	if err != nil {
		return nil, err
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sig := signature(bodyJSON, timestamp)
	h := map[string]string{"Content-Type": "application/json; charset=utf-8", "User-Agent": appUA, "app-type": "merchant_assistant_app", "timestamp": timestamp, "App-Signature": sig, "XE-Require-Sign": "true"}
	resp, err := c.Post(appAPIBase+path, bytes.NewReader([]byte(bodyJSON)), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xiaoeapp read body: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("xiaoeapp parse JSON: %w", err)
	}
	return root, nil
}

func baseParams(s xeSession) map[string]any {
	now := time.Now().Format("2006-01-02 15:04:05")
	p := map[string]any{"client_info": map[string]any{"appVersion": "6.1.1", "device": "SM-N976N", "deviceName": "shamu", "apiLevel": 22, "systemVersion": "Android 5.1.1", "phoneModel": "SM-N976N", "phoneBrand": "Android"}, "app_install_time": "2023-09-16 16:56:25", "app_boot_time": now, "system_boot_time": now, "is_mock_user": false, "terminal_type": 3, "platform": "android", "client": 6, "agent_type": 14, "build_version": 1182, "app_version": "6.1.1", "login_version": 2, "check_login_version": true, "channel_no": "yingyongbao"}
	if s.bUserID != "" {
		p["b_user_id"] = s.bUserID
	}
	if s.appUserID != "" {
		p["app_user_id"] = s.appUserID
	}
	if s.unionID != "" {
		p["union_id"] = s.unionID
	}
	return p
}

func marshalPythonJSON(v any) (string, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return "", err
	}
	return strings.ReplaceAll(strings.TrimSpace(b.String()), "/", `\/`), nil
}
func signature(bodyJSON, timestamp string) string {
	salt := fmt.Sprintf("%x", md5.Sum([]byte(signSaltKey)))
	s := fmt.Sprintf("%x", sha1.Sum([]byte(bodyJSON+timestamp+salt)))
	return fmt.Sprintf("%x", md5.Sum([]byte(strings.ToLower(s))))
}

func sessionFromCookies(jar http.CookieJar, rawURL string) xeSession {
	host := ""
	domains := []string{}
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Host
		if strings.HasPrefix(host, "app") {
			host = strings.Split(host, ".")[0]
		}
		if u.Scheme != "" && u.Host != "" {
			domains = append(domains, u.Scheme+"://"+u.Host)
		}
	}
	domains = append(domains, "https://xiaoeapp-server.xiaoeknow.com", "https://www.xiaoeknow.com", "https://h5.xiaoeknow.com")
	v := func(names ...string) string { return cookieValue(jar, domains, names...) }
	return xeSession{token: v("token", "api_token", "ko_token"), bUserID: v("b_user_id", "bUserId"), appUserID: firstNonEmpty(v("app_user_id"), v("b_user_id")), unionID: v("union_id"), appID: firstNonEmpty(v("app_id"), host), cUserID: firstNonEmpty(v("c_user_id"), v("user_id"), v("app_user_id"))}
}
func cookieValue(jar http.CookieJar, domains []string, names ...string) string {
	for _, d := range domains {
		if u, err := url.Parse(d); err == nil {
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

func itemFromMap(m map[string]any, sess xeSession) xeItem {
	typ := firstVal(m, "resource_type", "goods_type")
	return xeItem{id: firstVal(m, "resource_id", "goods_id", "course_id", "id", "product_id"), title: firstVal(m, "title", "resource_title", "goods_title", "name", "goods_name"), typ: typeMap(typ), appID: firstVal(m, "app_id", "content_app_id"), cUserID: firstVal(m, "user_id", "c_user_id"), productID: firstVal(m, "product_id", "term_id"), raw: m}
}
func typeMap(t string) string {
	if v := map[string]string{"1": "text", "2": "audio", "3": "video", "4": "live", "5": "member", "6": "column", "7": "column", "8": "bigcolumn", "10": "live", "12": "live", "16": "clock", "18": "column", "20": "book", "25": "train", "50": "ecourse", "51": "document", "64": "ecourse"}[t]; v != "" {
		return v
	}
	return t
}
func typeFromURL(u string) string {
	for k, v := range map[string]string{"/video/": "video", "/audio/": "audio", "/alive/": "live", "/ebook/": "book", "/text/": "text", "/camp/": "train", "/ecourse/": "ecourse", "/member/": "member", "/big_column/": "bigcolumn", "/column/": "column", "clockIntroduce": "clock"} {
		if strings.Contains(u, k) {
			return v
		}
	}
	return "video"
}
func goodsType(t string) int {
	if v, ok := map[string]int{"text": 1, "audio": 2, "video": 3, "book": 20, "live": 4, "ecourse": 50, "train": 25, "document": 51, "bigcolumn": 8}[t]; ok {
		return v
	}
	if n := toInt(t); n > 0 {
		return n
	}
	return 3
}
func isLiveType(t string) bool { return t == "live" || t == "4" || t == "10" || t == "12" }
func itemAvailable(m map[string]any) bool {
	for _, k := range []string{"is_available", "is_valid", "subscribe_status", "buy_status"} {
		v := strings.ToLower(val(m, k))
		if v == "0" || v == "false" {
			return false
		}
	}
	return true
}
func pickURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"video_m3u8_url", "video_hls", "video_url", "audio_m3u8_url", "audio_url", "aliveVideoUrl", "aliveVideoMp4Url", "miniAliveVideoUrl", "aliveReviewUrl", "play_url", "playUrl", "url", "m3u8_url", "file_url", "download_url"} {
			if u := normalizeURL(val(m, k)); strings.HasPrefix(u, "http") || strings.HasPrefix(u, "//") {
				return normalizeURL(u)
			}
		}
	}
	return ""
}
func media(title, u string, extra map[string]any) *extractor.MediaInfo {
	if title == "" {
		title = "xiaoeapp_video"
	}
	stream := extractor.Stream{Quality: "source", URLs: []string{u}, Format: formatOf(u), Headers: map[string]string{"Referer": "https://www.xiaoeknow.com/"}}
	if strings.Contains(strings.ToLower(stream.Format), "m3u8") {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "xiaoeapp", Title: title, Streams: map[string]extractor.Stream{"default": stream}, Extra: extra}
}
func listUnder(v any, key string) []map[string]any {
	for _, m := range mapsUnder(v) {
		if a, ok := m[key].([]any); ok {
			out := []map[string]any{}
			for _, x := range a {
				if mm, ok := x.(map[string]any); ok {
					out = append(out, mm)
				}
			}
			return out
		}
	}
	return nil
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
func val(v any, k string) string {
	if m, ok := v.(map[string]any); ok {
		if x, ok := m[k]; ok && x != nil {
			return strings.TrimSpace(fmt.Sprint(x))
		}
	}
	return ""
}
func firstVal(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := val(m, k); v != "" {
			return v
		}
	}
	return ""
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}
func code(root map[string]any) string { return fmt.Sprint(root["code"]) }
func toInt(s string) int              { var n int; fmt.Sscanf(s, "%d", &n); return n }
func normalizeURL(u string) string {
	u = strings.TrimSpace(strings.ReplaceAll(u, `\/`, "/"))
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
func formatOf(u string) string {
	l := strings.ToLower(u)
	if strings.Contains(l, ".m3u8") {
		return "m3u8"
	}
	if strings.Contains(l, ".mp3") {
		return "mp3"
	}
	return "mp4"
}

func containsPrivateXiaoeFlow(v any) bool {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"private_info", "private_m3u8", "aliveVideoUrlEncrypt"} {
			if s := strings.ToLower(val(m, k)); s != "" && s != "false" && s != "0" && s != "<nil>" {
				return true
			}
		}
		for _, k := range []string{"url", "video_url", "video_audio_url", "aliveVideoUrlEncrypt"} {
			if s := strings.ToLower(val(m, k)); strings.Contains(s, "__ba") || strings.Contains(s, "distribute.vod.pri.get") {
				return true
			}
		}
	}
	return false
}
