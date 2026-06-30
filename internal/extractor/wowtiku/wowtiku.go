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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
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

var (
	patterns      = []string{`(?:[\w-]+\.)?wowtiku\.com/|(?:[\w-]+\.)?wowtiku\.net/`}
	priceNumberRe = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
)

func init() {
	extractor.Register(&Wowtiku{}, extractor.SiteInfo{Name: "Wowtiku", URL: "wowtiku.com", NeedAuth: true})
}

type Wowtiku struct{}

func (s *Wowtiku) Patterns() []string { return patterns }

type wtSession struct{ token string }
type wtVideo struct{ title, vid, directURL string }
type wtFile struct{ title, rawURL, format string }
type wtCourse struct {
	id, title, platformID string
	price                 float64
	purchased             bool
	raw                   map[string]any
}

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
	var selected wtCourse
	if cid == "" {
		courses, err := purchasedCourses(c, sess)
		if err != nil {
			return nil, err
		}
		if opts.ListOnly || len(courses) != 1 {
			return courseListMedia(courses), nil
		}
		selected = courses[0]
		cid = selected.id
	}
	detail, err := requestData(c, sess, detailAPI, map[string]string{"id": cid}, nil, "GET", "", "v1")
	if err != nil {
		return nil, err
	}
	videos := collectVideos(detail)
	files := collectFiles(detail)
	ids := classIDs(detail)
	for _, classID := range ids {
		if subset, err := requestData(c, sess, subsetAPI, map[string]string{"stage_goods_id": cid, "class_id": classID}, nil, "GET", "", "v1"); err == nil {
			videos = append(videos, collectVideos(subset)...)
			files = append(files, collectFiles(subset)...)
		}
		if docs, err := requestData(c, sess, documentAPI, map[string]string{"class_id": classID}, nil, "GET", "", "v1"); err == nil {
			files = append(files, collectFiles(docs)...)
		}
	}
	files = dedupeFiles(files)
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}
	if !onlyPDF(opts) {
		for _, v := range videos {
			entry, err := resolveVideo(c, sess, v, opts)
			if err != nil || entry == nil || len(entry.Streams) == 0 {
				continue
			}
			u := firstStreamURL(entry)
			if u != "" && !seen[u] {
				seen[u] = true
				entries = append(entries, entry)
			}
		}
	}
	for i, f := range files {
		entry := fileEntry(sess, f, i+1)
		if entry == nil || len(entry.Streams) == 0 {
			continue
		}
		u := firstStreamURL(entry)
		if u != "" && !seen["file:"+u] {
			seen["file:"+u] = true
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wowtiku: no playable Aliyun/direct video or downloadable file resolved")
	}
	return &extractor.MediaInfo{Site: "wowtiku", Title: detailTitle(detail, cid), Entries: entries, Extra: courseExtra(detail, selected, cid)}, nil
}

func firstCourseID(c *util.Client, sess wtSession) (string, error) {
	courses, err := purchasedCourses(c, sess)
	if err != nil {
		return "", err
	}
	if len(courses) == 0 {
		return "", fmt.Errorf("wowtiku: no purchased courses")
	}
	return courses[0].id, nil
}

func purchasedCourses(c *util.Client, sess wtSession) ([]wtCourse, error) {
	platforms := []string{"3"}
	if data, err := requestData(c, sess, platformListsAPI, nil, nil, "GET", "", "v1"); err == nil {
		for _, m := range mapsUnder(data) {
			if id := firstNonEmpty(val(m, "id"), val(m, "platform_id")); id != "" {
				platforms = append(platforms, id)
			}
		}
	}
	seenPlatform := map[string]bool{}
	seenCourse := map[string]bool{}
	var courses []wtCourse
	for _, pid := range platforms {
		if pid == "" || seenPlatform[pid] {
			continue
		}
		seenPlatform[pid] = true
		data, err := requestData(c, sess, buyListsAPI, map[string]string{"platform_id": pid}, nil, "GET", "", "v1")
		if err != nil {
			continue
		}
		for _, m := range courseRows(data) {
			course := normalizeCourse(m, pid)
			if course.id == "" || seenCourse[course.id] {
				continue
			}
			seenCourse[course.id] = true
			courses = append(courses, course)
		}
	}
	if len(courses) == 0 {
		return nil, fmt.Errorf("wowtiku: purchased course list is empty")
	}
	return courses, nil
}

func courseRows(data any) []map[string]any {
	rows := listFromAny(data)
	if len(rows) > 0 {
		return rows
	}
	return mapsUnder(data)
}

func normalizeCourse(m map[string]any, platformID string) wtCourse {
	stage := firstMap(firstNonNil(m["stage_goods_info"], m["detail"]))
	stageFromNested := stage != nil
	if stage == nil {
		stage = m
	}
	price, _ := coursePrice(m)
	if price == 0 && stageFromNested {
		price, _ = coursePrice(stage)
	}
	return wtCourse{
		id:         firstNonEmpty(val(stage, "id"), val(stage, "stage_goods_id"), val(m, "id"), val(m, "stage_goods_id")),
		title:      firstNonEmpty(val(stage, "name"), val(stage, "title"), val(m, "name"), val(m, "title")),
		platformID: platformID,
		price:      price,
		purchased:  true,
		raw:        m,
	}
}

func courseListMedia(courses []wtCourse) *extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(courses))
	for _, course := range courses {
		extra := map[string]any{"course_id": course.id, "platform_id": course.platformID, "purchased": course.purchased}
		if course.price > 0 {
			extra["price"] = course.price
		} else if course.purchased {
			extra["price"] = float64(999)
		}
		if course.raw != nil {
			extra["raw"] = course.raw
		}
		entries = append(entries, &extractor.MediaInfo{Site: "wowtiku", Title: firstNonEmpty(course.title, course.id), Extra: compactAnyMap(extra)})
	}
	return &extractor.MediaInfo{Site: "wowtiku", Title: "wowtiku_courses", Entries: entries}
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
		id := firstNonEmpty(val(m, "class_id"), val(m, "classId"))
		if id == "" && hasAnyKey(m, "subject_info", "subject_id", "subject_name", "class_name", "child") {
			id = val(m, "id")
		}
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
func resolveVideo(c *util.Client, sess wtSession, v wtVideo, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if v.directURL != "" {
		return media(sess, v.title, normalizeURL(v.directURL), mediaFormat(v.directURL), map[string]any{"vid": v.vid, "source_type": "video_url"}), nil
	}
	if v.vid == "" {
		return nil, fmt.Errorf("wowtiku: empty vid")
	}
	sts, err := requestData(c, sess, stsAPI, nil, nil, "POST", "www", "v1")
	if err != nil {
		return nil, err
	}
	payload := shared.AliyunPayloadFromMap(firstMap(sts), sts)
	payload.Region = firstNonEmpty(payload.Region, vodRegion)
	playCfg, _ := json.Marshal(map[string]string{"EncryptType": "AliyunVoDEncryption"})
	playOpts := shared.AliyunPlayOptions{
		Referer:     refererURL,
		Origin:      originURL,
		Quality:     qualityFromOpts(opts),
		Formats:     "m3u8",
		ExtraParams: map[string]string{"StreamType": "video", "Channel": "HTML5", "PlayerVersion": "2.32.0", "PlayConfig": string(playCfg)},
	}
	info, err := shared.AliyunResolvePlayInfo(c, payload, v.vid, playOpts)
	if err != nil {
		return nil, fmt.Errorf("blocked: needs Aliyun STS SDK / DRM engine: %w", err)
	}
	playToken := ""
	if tokenData, err := requestData(c, sess, playTokenAPI, nil, nil, "GET", "www", "v1"); err == nil {
		playToken = firstNonEmpty(val(firstMap(tokenData), "MtsHlsUriToken"), val(firstMap(tokenData), "mtsHlsUriToken"))
	}
	playURL := normalizeURL(info.URL)
	extra := map[string]any{"vid": v.vid, "vod_region": payload.Region, "aliyun_api": info.APIURL, "source_type": info.SourceType, "encrypt_type": info.EncryptType}
	if info.NeedMerge {
		if info.EncryptType == "HLSEncryption" && playToken != "" {
			playURL = appendQueryParam(playURL, "MtsHlsUriToken", playToken)
			extra["mts_hls_uri_token"] = playToken
		}
		text, err := c.GetString(playURL, map[string]string{"Origin": originURL, "Referer": refererURL, "Accept": "application/json, text/plain, */*"})
		if err == nil && text != "" {
			if info.Encrypted && info.EncryptType == "AliyunVoDEncryption" {
				rewritten, err := shared.AliyunRewriteM3U8Keys(c, text, payload, info.EncryptType, playURL, playOpts)
				if err != nil {
					return nil, fmt.Errorf("blocked: needs Aliyun STS SDK / DRM engine: %w", err)
				}
				text = rewritten
			}
			text = absolutizeM3U8Text(text, playURL)
			extra["m3u8_text"] = text
			extra["source_type"] = "m3u8_text"
			playURL = dataM3U8URL(text)
		}
	}
	return media(sess, v.title, playURL, firstNonEmpty(info.Format, mediaFormat(playURL)), extra), nil
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

func listFromAny(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"lists", "list", "data", "items", "records", "rows"} {
			if out := listFromAny(x[key]); len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func firstMediaURL(m map[string]any) string {
	for _, key := range []string{"video_url", "play_url", "playUrl", "m3u8_url", "url", "path", "src", "file_url"} {
		if u := val(m, key); (strings.HasPrefix(u, "http") || strings.HasPrefix(u, "//")) && isVideoMediaURL(u) {
			return u
		}
	}
	return ""
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

func courseExtra(detail any, selected wtCourse, cid string) map[string]any {
	extra := map[string]any{"course_id": firstNonEmpty(cid, selected.id), "purchased": true}
	if selected.platformID != "" {
		extra["platform_id"] = selected.platformID
	}
	if selected.raw != nil {
		extra["course"] = selected.raw
	}
	if price, ok := coursePrice(detail); ok {
		extra["price"] = price
	} else if selected.price > 0 {
		extra["price"] = selected.price
	} else {
		extra["price"] = float64(999)
	}
	return compactAnyMap(extra)
}

func coursePrice(v any) (float64, bool) {
	centKeys := []string{
		"priceCent", "priceCents", "price_cent", "price_cents", "priceFen", "price_fen",
		"moneyCent", "moneyCents", "money_cent", "money_cents", "moneyFen", "money_fen",
		"amountCent", "amountCents", "amount_cent", "amount_cents",
	}
	normalKeys := []string{"price", "salePrice", "sellPrice", "payPrice", "amount", "money", "coursePrice"}
	for _, m := range mapsUnder(v) {
		for _, key := range centKeys {
			if raw, ok := m[key]; ok {
				if price, ok := normalizePrice(raw, true); ok {
					return price, true
				}
			}
		}
		for _, key := range normalKeys {
			if raw, ok := m[key]; ok {
				if price, ok := normalizePrice(raw, false); ok {
					return price, true
				}
			}
		}
	}
	return 0, false
}

func normalizePrice(v any, cents bool) (float64, bool) {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return 0, false
	}
	m := priceNumberRe.FindString(strings.ReplaceAll(s, ",", ""))
	if m == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(m, 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	if cents || (f >= 1000 && !strings.Contains(m, ".") && !strings.ContainsAny(s, "元¥￥")) {
		f /= 100
	}
	return float64(int(f*100+0.5)) / 100, true
}

func compactAnyMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		if v != nil {
			out[k] = v
		}
	}
	return out
}

func firstStreamURL(entry *extractor.MediaInfo) string {
	if entry == nil {
		return ""
	}
	if stream, ok := entry.Streams["default"]; ok {
		for _, u := range stream.URLs {
			if u = strings.TrimSpace(u); u != "" {
				return u
			}
		}
	}
	for _, stream := range entry.Streams {
		for _, u := range stream.URLs {
			if u = strings.TrimSpace(u); u != "" {
				return u
			}
		}
	}
	return ""
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
	if strings.HasPrefix(raw, "/") {
		return originURL + raw
	}
	raw = strings.ReplaceAll(raw, " ", "%20")
	return raw
}
func media(sess wtSession, title, u, fmtName string, extra map[string]any) *extractor.MediaInfo {
	if title == "" {
		title = "video"
	}
	stream := extractor.Stream{Quality: "best", URLs: []string{u}, Format: fmtName, Headers: downloadHeaders(sess)}
	if strings.Contains(strings.ToLower(fmtName), "m3u8") {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "wowtiku", Title: title, Streams: map[string]extractor.Stream{"default": stream}, Extra: extra}
}
func mediaFormat(u string) string {
	l := strings.ToLower(u)
	if strings.Contains(l, ".m3u8") {
		return "m3u8"
	}
	if ext := fileExt(u); ext != "" {
		return ext
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

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func appendQueryParam(raw, key, value string) string {
	if raw == "" || key == "" || value == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		return raw + sep + url.QueryEscape(key) + "=" + url.QueryEscape(value)
	}
	q := u.Query()
	if q.Get(key) == "" {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func qualityFromOpts(opts *extractor.ExtractOpts) string {
	if opts == nil {
		return ""
	}
	return opts.Quality
}

func onlyPDF(opts *extractor.ExtractOpts) bool {
	if opts == nil {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(opts.Quality))
	q = strings.NewReplacer("-", "", "_", "", " ", "").Replace(q)
	switch q {
	case "4", "pdf", "onlypdf", "courseware", "material", "materials", "document", "documents", "file", "files", "课件", "资料":
		return true
	default:
		return false
	}
}

func collectFiles(data any) []wtFile {
	var out []wtFile
	seen := map[string]bool{}
	var walk func(any, []int)
	walk = func(v any, index []int) {
		switch x := v.(type) {
		case map[string]any:
			if f := parseFileInfo(x, index); f.rawURL != "" && !seen[f.rawURL] {
				seen[f.rawURL] = true
				out = append(out, f)
			}
			for _, child := range x {
				walk(child, index)
			}
		case []any:
			for i, child := range x {
				walk(child, append(append([]int{}, index...), i+1))
			}
		}
	}
	walk(data, nil)
	return out
}

func parseFileInfo(m map[string]any, index []int) wtFile {
	rawURL := ""
	for _, key := range []string{"file_url", "download_url", "document_url", "documentUrl", "url", "path", "file_path", "src"} {
		if u := val(m, key); strings.TrimSpace(u) != "" {
			rawURL = normalizeURL(u)
			break
		}
	}
	if rawURL == "" || isVideoMediaURL(rawURL) {
		return wtFile{}
	}
	if !strings.HasPrefix(strings.ToLower(rawURL), "http") {
		return wtFile{}
	}
	name := firstNonEmpty(val(m, "file_name"), val(m, "name"), val(m, "title"), urlBaseName(rawURL), "资料")
	format := strings.Trim(strings.ToLower(firstNonEmpty(val(m, "file_fmt"), val(m, "extension"), val(m, "ext"), val(m, "suffix"), fileExt(rawURL))), ". ")
	if format == "" {
		format = "bin"
	}
	if strings.Contains(name, ".") {
		name = strings.TrimSuffix(name, "."+format)
	}
	return wtFile{title: fmt.Sprintf("(%s)--%s", indexString(index), name), rawURL: rawURL, format: format}
}

func dedupeFiles(files []wtFile) []wtFile {
	seen := map[string]bool{}
	out := make([]wtFile, 0, len(files))
	for _, f := range files {
		key := strings.Join([]string{f.rawURL, f.title, f.format}, "\x00")
		if f.rawURL == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

func fileEntry(sess wtSession, f wtFile, index int) *extractor.MediaInfo {
	if f.rawURL == "" {
		return nil
	}
	title := firstNonEmpty(f.title, fmt.Sprintf("(%d)--资料", index))
	format := firstNonEmpty(f.format, fileExt(f.rawURL), "bin")
	stream := extractor.Stream{Quality: format, URLs: []string{f.rawURL}, Format: format, Headers: downloadHeaders(sess)}
	if strings.Contains(strings.ToLower(format), "m3u8") {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "wowtiku", Title: title, Streams: map[string]extractor.Stream{"default": stream}, Extra: map[string]any{"type": "file", "file_url": f.rawURL}}
}

func downloadHeaders(sess wtSession) map[string]string {
	h := map[string]string{"Referer": refererURL, "User-Agent": "Mozilla/5.0"}
	if sess.token != "" {
		h["Authorization"] = "Bearer " + sess.token
		h["token"] = sess.token
	}
	return h
}

func hasAnyKey(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func isVideoMediaURL(raw string) bool {
	lower := strings.ToLower(raw)
	for _, ext := range []string{".m3u8", ".mp4", ".flv", ".m4v", ".mov", ".mpg", ".mpeg", ".avi", ".wmv", ".ts"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func fileExt(raw string) string {
	u, err := url.Parse(raw)
	path := raw
	if err == nil {
		path = u.Path
	}
	if idx := strings.LastIndex(path, "."); idx >= 0 && idx+1 < len(path) {
		return strings.ToLower(path[idx+1:])
	}
	return ""
}

func urlBaseName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func indexString(index []int) string {
	if len(index) == 0 {
		return "1"
	}
	parts := make([]string, len(index))
	for i, n := range index {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ".")
}

func absolutizeM3U8Text(text, source string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines[i] = resolveM3U8URI(trimmed, source)
	}
	return strings.Join(lines, "\n")
}

func resolveM3U8URI(raw, source string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	base, err := url.Parse(source)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func dataM3U8URL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}
