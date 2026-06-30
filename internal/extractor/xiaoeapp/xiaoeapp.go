// Package xiaoeapp implements an extractor for xiaoeknow.com app shops.
package xiaoeapp

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
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
	h5LoginAPI         = "/app/xe.user.h5login/1.0.0"
	fileListAPI        = "/xe.course.business.courseware_list.get/2.0.0"
	ebookInfoAPI       = "/xe.course.business.ebook.info/2.0.0"
	privateKeyAPI      = "/app/xe.vod.privatekey.get/1.0.0"
	learnColumnAPI     = "/xe.data.learn_center.user_learn_package/1.0.0"
	learnTrainAPI      = "/xe.course.business.e_course.user.learn.records.list/1.0.0"
	courseListPageSize = 200
	classroomPageSize  = 10
	appUA              = "okhttp/3.12.0;xet-android-app 6.1.1"
)

var patterns = []string{`(?:^|://)(?:app|xiaoeapp-server)\.xiaoeknow\.com/`}
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
	listed, listErr := fetchCourseList(c, sess)
	if opts.ListOnly {
		if listErr != nil {
			return nil, listErr
		}
		return buildXiaoeListMedia(listed), nil
	}
	wantID := firstMatch(idRe, rawURL)
	items := []xeItem{}
	if wantID != "" {
		items = append(items, xeItem{id: wantID, title: wantID, typ: typeFromURL(rawURL), appID: sess.appID, cUserID: sess.cUserID, raw: map[string]any{"resource_id": wantID, "app_id": sess.appID, "resource_type": typeFromURL(rawURL)}})
	}
	if listErr == nil {
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
		var delegateErr error
		if shouldDelegateToH5(it) {
			if info, err := resolveDelegateInfo(c, sess, it, opts.Cookies); err == nil {
				if appended := appendDelegateEntries(&entries, seen, info, it); appended > 0 {
					continue
				}
			} else {
				delegateErr = err
			}
		}
		u, extra := resolveItemURL(c, sess, it)
		if reason := val(extra, "blocked_reason"); reason != "" {
			blockedReasons = append(blockedReasons, reason)
			continue
		}
		if u == "" || seen[u] {
			if delegateErr != nil {
				blockedReasons = append(blockedReasons, "delegate: "+delegateErr.Error())
			}
			continue
		}
		seen[u] = true
		extra = enrichXiaoeExtra(extra, it)
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
	return &extractor.MediaInfo{Site: "xiaoeapp", Title: title, Entries: entries, Extra: map[string]any{"target_id": wantID, "course_count": len(listed), "login_checked": true}}, nil
}

func fetchCourseList(c *util.Client, sess xeSession) ([]xeItem, error) {
	out := []xeItem{}
	seen := map[string]bool{}
	classrooms := []xeItem{}
	for _, rt := range []string{"0", "4", "10", "12", "8", "50", "51", "64", "5", "6", "7", "16", "20", "25"} {
		typeCount := 0
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
				typeCount++
				out = append(out, it)
				if firstVal(m, "resource_type", "goods_type") == "7" {
					classrooms = append(classrooms, it)
				}
			}
			if len(list) < courseListPageSize || (toInt(firstNonEmpty(val(root["data"], "total"))) > 0 && typeCount >= toInt(firstNonEmpty(val(root["data"], "total")))) {
				break
			}
		}
	}
	for _, parent := range classrooms {
		for _, child := range fetchClassroomChildren(c, sess, parent) {
			if child.id == "" || seen[child.id] {
				continue
			}
			seen[child.id] = true
			out = append(out, child)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xiaoeapp course list is empty")
	}
	return out, nil
}

func fetchClassroomChildren(c *util.Client, sess xeSession, parent xeItem) []xeItem {
	appID := strings.ToLower(firstNonEmpty(parent.appID, firstVal(parent.raw, "app_id", "content_app_id"), sess.appID))
	cUserID := firstNonEmpty(parent.cUserID, firstVal(parent.raw, "c_user_id", "user_id"), sess.cUserID)
	parentID := parent.id
	if appID == "" || parentID == "" {
		return nil
	}
	out := []xeItem{}
	seen := map[string]bool{}
	for _, api := range []string{learnColumnAPI, learnTrainAPI} {
		apiCount := 0
		for page := 1; page <= 50; page++ {
			root := postH5JSONAPI(c, sess, h5Domain(appID), api, map[string]any{"bizData": map[string]any{"page": page, "page_size": classroomPageSize}}, appID, cUserID)
			if code(root) != "0" {
				break
			}
			data, _ := root["data"].(map[string]any)
			list := listUnder(data, "list")
			if len(list) == 0 {
				break
			}
			for _, m := range list {
				child := classroomChildFromMap(m, appID, cUserID, parentID)
				if child.id == "" || seen[child.id] {
					continue
				}
				seen[child.id] = true
				apiCount++
				out = append(out, child)
			}
			if truthy(firstVal(data, "is_finish", "isFinish")) || len(list) < classroomPageSize {
				break
			}
			if total := toInt(firstNonEmpty(val(data, "total"))); total > 0 && apiCount >= total {
				break
			}
		}
	}
	return out
}

func classroomChildFromMap(m map[string]any, appID, cUserID, parentID string) xeItem {
	rawType := firstVal(m, "resource_type", "goods_type")
	switch rawType {
	case "5", "6", "8", "25", "50", "64":
	default:
		return xeItem{}
	}
	if !itemAvailable(m) {
		return xeItem{}
	}
	id := firstVal(m, "product_id", "course_id", "resource_id", "goods_id", "id")
	title := firstVal(m, "title", "resource_title", "goods_title", "name", "goods_name")
	if id == "" || title == "" {
		return xeItem{}
	}
	raw := copyXiaoeMap(m)
	raw["resource_id"] = id
	raw["resource_type"] = rawType
	raw["title"] = title
	raw["app_id"] = appID
	raw["c_user_id"] = cUserID
	raw["user_id"] = cUserID
	raw["parent_resource_id"] = parentID
	raw["parent_resource_type"] = "7"
	return xeItem{id: id, title: title, typ: typeMap(rawType), appID: appID, cUserID: cUserID, productID: firstVal(raw, "product_id", "term_id"), raw: raw}
}

func resolveItemURL(c *util.Client, sess xeSession, it xeItem) (string, map[string]any) {
	it.appID = firstNonEmpty(strings.ToLower(it.appID), firstVal(it.raw, "app_id", "content_app_id"), sess.appID)
	it.cUserID = firstNonEmpty(it.cUserID, firstVal(it.raw, "c_user_id", "user_id"), sess.cUserID)
	if isLiveType(it.typ) {
		if containsPrivateXiaoeFlow(it.raw) {
			if u, extra := protectedLiveURL(c, sess, it); u != "" {
				return u, extra
			}
			if u, extra := resolvePrivateXiaoeMedia(c, sess, it, it.raw, "source"); u != "" {
				return u, extra
			}
			return "", map[string]any{"blocked_reason": "blocked: no decodable xiaoe private lookback m3u8 candidate", "resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
		}
		if u := pickURL(it.raw); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
		}
		if u, extra := protectedLiveURL(c, sess, it); u != "" {
			return u, extra
		}
		body := map[string]any{"type": "1", "app_id": firstNonEmpty(it.appID, sess.appID), "resource_id": it.id}
		if it.productID != "" {
			body["product_id"] = it.productID
		}
		if root, err := postAppAPI(c, sess, lookbackURLAPI, body); err == nil && code(root) == "0" {
			if containsPrivateXiaoeFlow(root["data"]) {
				if u, extra := resolvePrivateXiaoeMedia(c, sess, it, root["data"], lookbackURLAPI); u != "" {
					return u, extra
				}
				return "", map[string]any{"blocked_reason": "blocked: no decodable xiaoe private lookback m3u8 candidate", "resource_id": it.id, "resource_type": it.typ, "app_id": it.appID, "api": lookbackURLAPI}
			}
			if u := pickURL(root["data"]); u != "" {
				return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": it.appID, "api": lookbackURLAPI}
			}
		}
	}
	if containsPrivateXiaoeFlow(it.raw) {
		if u, extra := resolvePrivateXiaoeMedia(c, sess, it, it.raw, "source"); u != "" {
			return u, extra
		}
		return "", map[string]any{"blocked_reason": "blocked: no decodable xiaoe private lookback m3u8 candidate", "resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
	}
	if u := pickURL(it.raw); u != "" {
		return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": it.appID}
	}
	if u := firstURLFromEncodedFields(it.raw); u != "" {
		return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": it.appID, "decoded_video_urls": true}
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
		if u, extra := resolvePrivateXiaoeMedia(c, sess, it, data, videoInfoAPI); u != "" {
			extra["goods_type"] = goodsType
			return u, extra
		}
		return "", map[string]any{"blocked_reason": "blocked: no decodable xiaoe private lookback m3u8 candidate", "resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI}
	}
	if u := pickURL(data); u != "" {
		return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI}
	}
	if u := firstURLFromEncodedFields(data); u != "" {
		return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI, "decoded_video_urls": true}
	}
	switch typeMap(it.typ) {
	case "text":
		if u := textHTMLDataURL(data); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI, "source_type": "html_text"}
		}
	case "book":
		if u := resolveEbookURL(c, sess, it); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": ebookInfoAPI}
		}
	case "document", "file":
		if u := resolveFileListURL(c, sess, it, fmt.Sprint(goodsType)); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": fileListAPI}
		}
	}
	for _, m := range mapsUnder(data) {
		if u := pickURL(m); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": it.typ, "goods_type": goodsType, "api": videoInfoAPI}
		}
	}
	return "", nil
}

func shouldDelegateToH5(it xeItem) bool {
	switch typeMap(it.typ) {
	case "train", "ecourse", "clock", "column", "bigcolumn", "member":
		return true
	default:
		return false
	}
}

func resolveDelegateInfo(c *util.Client, sess xeSession, it xeItem, jar http.CookieJar) (*extractor.MediaInfo, error) {
	appID := strings.ToLower(firstNonEmpty(it.appID, firstVal(it.raw, "app_id", "content_app_id"), sess.appID))
	cUserID := firstNonEmpty(it.cUserID, firstVal(it.raw, "c_user_id", "user_id"), sess.cUserID)
	if appID == "" || it.id == "" {
		return nil, fmt.Errorf("missing app_id or course_id")
	}
	h5URL := delegateH5URL(it, appID)
	if h5URL == "" {
		return nil, fmt.Errorf("missing h5 url")
	}
	token := h5Token(c, sess, appID, cUserID)
	if token == "" {
		return nil, fmt.Errorf("missing h5 token")
	}
	installDelegateCookies(jar, appID, token)
	ext, site, err := extractor.MatchWithSite(h5URL)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(site.Name, "Xiaoeapp") {
		return nil, fmt.Errorf("h5 url matched xiaoeapp instead of h5 delegate")
	}
	info, err := ext.Extract(h5URL, &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("empty delegate result")
	}
	if info.Extra == nil {
		info.Extra = map[string]any{}
	}
	info.Extra["delegate"] = true
	info.Extra["delegate_url"] = h5URL
	info.Extra["delegate_site"] = site.Name
	return info, nil
}

func appendDelegateEntries(entries *[]*extractor.MediaInfo, seen map[string]bool, info *extractor.MediaInfo, it xeItem) int {
	if info == nil {
		return 0
	}
	before := len(*entries)
	var add func(*extractor.MediaInfo)
	add = func(mi *extractor.MediaInfo) {
		if mi == nil {
			return
		}
		if len(mi.Entries) > 0 {
			for _, child := range mi.Entries {
				add(child)
			}
			return
		}
		u := firstStreamURL(mi)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		if mi.Extra == nil {
			mi.Extra = map[string]any{}
		}
		mi.Extra["delegate"] = true
		mi.Extra["delegate_parent_id"] = it.id
		mi.Extra["delegate_parent_type"] = typeMap(it.typ)
		if mi.Title == "" {
			mi.Title = firstNonEmpty(it.title, it.id)
		}
		*entries = append(*entries, mi)
	}
	add(info)
	return len(*entries) - before
}

func firstStreamURL(mi *extractor.MediaInfo) string {
	if mi == nil {
		return ""
	}
	for _, stream := range mi.Streams {
		for _, u := range stream.URLs {
			if strings.TrimSpace(u) != "" {
				return u
			}
		}
	}
	return ""
}

func h5Token(c *util.Client, sess xeSession, appID, cUserID string) string {
	body := map[string]any{"app_id": appID}
	if cUserID != "" {
		body["c_user_id"] = cUserID
	}
	root, err := postAppAPI(c, sess, h5LoginAPI, body)
	if err != nil || code(root) != "0" {
		return ""
	}
	return val(root["data"], "token")
}

func installDelegateCookies(jar http.CookieJar, appID, token string) {
	if jar == nil || appID == "" || token == "" {
		return
	}
	for _, raw := range []string{
		"https://" + appID + ".h5.xiaoeknow.com",
		"https://" + appID + ".h5.xet.citv.cn",
		"https://www.xiaoeknow.com",
		"https://study.xiaoe-tech.com",
	} {
		if u, err := url.Parse(raw); err == nil {
			jar.SetCookies(u, []*http.Cookie{
				{Name: "app_id", Value: appID, Path: "/"},
				{Name: "ko_token", Value: token, Path: "/"},
			})
		}
	}
}

func delegateH5URL(it xeItem, appID string) string {
	if it.id == "" || appID == "" {
		return ""
	}
	domain := appID + ".h5.xiaoeknow.com"
	for _, k := range []string{"h5_url", "url", "jump_url"} {
		if u := normalizeDelegateURL(val(it.raw, k), domain); u != "" {
			return u
		}
	}
	switch typeMap(it.typ) {
	case "clock":
		return fmt.Sprintf("https://%s/p/t/v1/clock/e_clock/clock_h5/clockIntroduce?activity_id=%s", domain, url.QueryEscape(it.id))
	case "live":
		return fmt.Sprintf("https://%s/v3/course/alive/%s?app_id=%s&type=2", domain, url.PathEscape(it.id), url.QueryEscape(appID))
	case "book":
		return fmt.Sprintf("https://%s/p/course/ebook/%s", domain, url.PathEscape(it.id))
	case "text", "audio", "video", "ecourse", "member", "column":
		return fmt.Sprintf("https://%s/p/course/%s/%s", domain, typeMap(it.typ), url.PathEscape(it.id))
	case "bigcolumn":
		return fmt.Sprintf("https://%s/p/course/big_column/%s", domain, url.PathEscape(it.id))
	case "train":
		if strings.HasPrefix(it.id, "term_") {
			return fmt.Sprintf("https://%s/p/course/camp/%s", domain, url.PathEscape(it.id))
		}
		return fmt.Sprintf("https://%s/p/course/ecourse/%s", domain, url.PathEscape(it.id))
	default:
		return ""
	}
}

func normalizeDelegateURL(raw, domain string) string {
	raw = normalizeURL(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://" + domain + raw
	}
	return "https://" + domain + "/" + strings.TrimLeft(raw, "/")
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
		if appID := strings.TrimSpace(u.Query().Get("app_id")); appID != "" {
			host = appID
		} else if strings.Contains(strings.ToLower(host), ".h5.") {
			host = strings.Split(host, ".")[0]
		} else if strings.HasPrefix(host, "app.") || host == "app.xiaoeknow.com" {
			host = ""
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
	return xeItem{id: firstVal(m, "resource_id", "goods_id", "course_id", "id", "product_id"), title: firstVal(m, "title", "resource_title", "goods_title", "name", "goods_name"), typ: typeMap(typ), appID: firstNonEmpty(firstVal(m, "app_id", "content_app_id"), sess.appID), cUserID: firstNonEmpty(firstVal(m, "user_id", "c_user_id"), sess.cUserID), productID: firstVal(m, "product_id", "term_id"), raw: m}
}

func copyXiaoeMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
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
		if u := directXiaoeURL(m); u != "" {
			return u
		}
	}
	return firstURLFromEncodedFields(v)
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
func truthy(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes"
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
	u = strings.ReplaceAll(u, `\u002F`, "/")
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
func formatOf(u string) string {
	l := strings.ToLower(u)
	if strings.HasPrefix(l, "data:application/vnd.apple.mpegurl") {
		return "m3u8"
	}
	if strings.HasPrefix(l, "data:text/html") {
		return "html"
	}
	if strings.Contains(l, ".m3u8") {
		return "m3u8"
	}
	if strings.Contains(l, ".mp3") {
		return "mp3"
	}
	for _, ext := range []string{"m4a", "aac", "pdf", "epub", "docx", "doc", "pptx", "ppt", "xlsx", "xls", "zip", "rar", "7z", "txt", "html"} {
		if strings.Contains(l, "."+ext) {
			return ext
		}
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

func firstPrivateXiaoeMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"aliveVideoUrlEncrypt", "private_m3u8", "aliveVideoUrl", "alive_video_url", "aliveVideoMp4Url", "miniAliveVideoUrl", "aliveReviewUrl", "video_m3u8_url", "video_url", "url", "m3u8_url"} {
			raw := val(m, k)
			if raw == "" {
				continue
			}
			u := normalizeURL(decryptXiaoePrivateURL(raw))
			if isXiaoePlayableURL(u) {
				return u
			}
		}
	}
	return ""
}

func decryptXiaoePrivateURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http") && strings.Contains(strings.ToLower(raw), ".m3u8") {
		return raw
	}
	if !strings.Contains(raw, "__ba") {
		return raw
	}
	s := strings.ReplaceAll(raw, "__ba", "")
	s = strings.NewReplacer("@", "1", "#", "2", "$", "3", "%", "4").Replace(s)
	s = strings.ReplaceAll(strings.ReplaceAll(s, "-", "+"), "_", "/")
	s = regexp.MustCompile(`[^A-Za-z0-9+/]`).ReplaceAllString(s, "")
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func appendXiaoeURLParams(raw string, params [][2]string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	q := parsed.Query()
	for _, kv := range params {
		if kv[0] == "" || kv[1] == "" || q.Has(kv[0]) {
			continue
		}
		q.Set(kv[0], kv[1])
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func isXiaoePlayableURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(u, "data:application/vnd.apple.mpegurl") || strings.HasPrefix(u, "data:text/html") {
		return true
	}
	if !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) || isXiaoePageURL(u) {
		return false
	}
	return strings.Contains(u, ".m3u8") || strings.Contains(u, ".mp4") || strings.Contains(u, ".mp3") || strings.Contains(u, ".m4a") || strings.Contains(u, ".aac") || strings.Contains(u, ".pdf") || strings.Contains(u, ".epub") || strings.Contains(u, ".doc") || strings.Contains(u, ".ppt") || strings.Contains(u, ".xls") || strings.Contains(u, ".zip") || strings.Contains(u, ".rar") || strings.Contains(u, ".7z")
}

func isUsableXiaoeURL(raw string) bool {
	u := normalizeURL(raw)
	if !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "//")) {
		return false
	}
	if isXiaoePageURL(u) || regexp.MustCompile(`(?i)\.(?:jpg|jpeg|png|gif|webp)(?:[?#]|$)`).MatchString(u) {
		return false
	}
	return true
}

func isXiaoePageURL(raw string) bool {
	u, err := url.Parse(normalizeURL(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.EscapedPath())
	if host == "iframe.xiaoeknow.com" && strings.HasPrefix(path, "/page/") {
		return true
	}
	if !(strings.Contains(host, "xiaoeknow.com") || strings.Contains(host, "xiaoecloud.com") || strings.Contains(host, "xiaoe-tech.com") || strings.Contains(host, "xet.citv.cn")) {
		return false
	}
	return strings.Contains(path, "/p/course/") || strings.Contains(path, "/p/t_pc/") || regexp.MustCompile(`(?i)/v\d+/(?:course|goods)/`).MatchString(path)
}
