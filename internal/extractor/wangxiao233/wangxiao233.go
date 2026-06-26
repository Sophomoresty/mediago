// Package wangxiao233 implements an extractor for wx.233.com (网校233 / 233网校).
package wangxiao233

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL     = "https://wx.233.com/"
	loginURL       = "https://passport.233.com/login/?redirecturl=https%3A%2F%2Fwx.233.com%2Fcenter%2Fstudy%3Fdomain%3Daq%26type%3D0"
	urlUserInfo    = "https://japi.233.com/ess-ucs-api/doz/members/userInfo"
	urlVktCourse   = "https://japi.233.com/ess-study-api/vkt-course/list"
	urlUserCourse  = "https://japi.233.com/ess-study-api/user-course/list"
	urlBuyDomain   = "https://japi.233.com/ess-study-api/user-course/buy-domain"
	urlTag         = "https://japi.233.com/ess-study-api/learn/do/get-class-tag"
	urlVersion     = "https://japi.233.com/ess-study-api/learn/do/list-version"
	urlChapter     = "https://japi.233.com/ess-study-api/learn/do/list-chapter-by-version-id"
	urlLecture     = "https://japi.233.com/ess-study-api/learn/do/get-lecture-url"
	urlDatum       = "https://japi.233.com/ess-study-api/datum-api/page-list"
	urlDatum2      = "https://japi.233.com/ess-study-api/datum-api/do/page-list"
	urlDatumDown   = "https://japi.233.com/ess-study-api/datum-info/do/download"
	urlVodDetail   = "https://japi.233.com/ess-bms-api/vod-play/do/by-detailids"
	urlVodPoly     = "https://japi.233.com/ess-bms-api/vod-play/do/by-polyvid"
	urlVodEss      = "https://japi.233.com/ess-bms-api/vod-play/do/by-essvid"
	urlPlayAuth    = "https://japi.233.com/ess-open-api/vod/do/getPlayInfoAndAuth"
	urlProductInfo = "https://japi.233.com/ess-study-api/user-course/product-info"
	urlVktProduct  = "https://japi.233.com/ess-study-api/vkt-course/product"
	urlOrderDetail = "https://japi.233.com/ess-ots-api/order-center/get-order-detail"
	urlPolyvToken  = "https://wx.233.com/search/v1/study/getvideotoken"
	urlPolyvSecure = "https://player.polyv.net/secure/%s.json"
	urlPolyvKey    = "https://hls.videocc.net/playsafe/%s/%s/%s_%s.key?token=%s"
	signSecret     = "RZRRNN9RXYCP"
	sidPrefix      = "study"
)

var patterns = []string{`(?:[\w-]+\.)?233\.com/`}

func init() {
	extractor.Register(&Wangxiao233{}, extractor.SiteInfo{Name: "Wangxiao233", URL: "233.com", NeedAuth: true})
}

type Wangxiao233 struct{}

func (w *Wangxiao233) Patterns() []string { return patterns }

type wx233Session struct{ token string }
type wx233Course struct{ productID, childProductID, versionProductID, versionID, teacherID, domain, title string }
type wx233Video struct{ title, detailID, polyVid, essVid, aliyunVid, mp3URL string }

func (w *Wangxiao233) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wangxiao233 requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess := wx233Session{token: tokenFromJar(opts.Cookies)}
	if sess.token == "" {
		return nil, fmt.Errorf("wangxiao233 requires clientauthentication cookie")
	}
	if _, err := apiGet(c, sess, urlUserInfo, nil); err != nil {
		return nil, fmt.Errorf("wangxiao233 userInfo: %w", err)
	}
	course := parseCourse(rawURL)
	if course.productID == "" {
		var err error
		course, err = firstPurchasedCourse(c, sess, course.domain)
		if err != nil {
			return nil, err
		}
	}
	tagData, _ := apiGetData(c, sess, urlTag, map[string]string{"teacherId": course.teacherID, "childProductId": course.childProductID, "systemType": "2", "lmProductId": "", "productId": course.productID, "clientType": "3"})
	children := childCourses(tagData, course)
	if len(children) == 0 {
		children = []wx233Course{course}
	}
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}
	for _, child := range children {
		child = fillVersion(c, sess, child)
		chapterData, err := apiGetData(c, sess, urlChapter, map[string]string{"versionId": child.versionID, "productId": course.productID, "currentParentProductId": course.productID, "groupProductId": "", "clientType": "3"})
		if err != nil || chapterData == nil {
			continue
		}
		for _, v := range collectVideos(chapterData) {
			mi, err := resolveVideo(c, sess, v, opts)
			if err != nil || mi == nil || len(mi.Streams) == 0 {
				continue
			}
			u := mi.Streams["default"].URLs[0]
			if u != "" && !seen[u] {
				seen[u] = true
				entries = append(entries, mi)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wangxiao233: no playable video resolved from chapter list")
	}
	title := firstNonEmpty(course.title, "wangxiao233_"+course.productID)
	return &extractor.MediaInfo{Site: "wangxiao233", Title: title, Entries: entries}, nil
}

func apiGetData(c *util.Client, sess wx233Session, apiURL string, params map[string]string) (any, error) {
	m, err := apiGet(c, sess, apiURL, params)
	if err != nil {
		return nil, err
	}
	if d, ok := m["data"]; ok {
		return d, nil
	}
	return m, nil
}
func apiGet(c *util.Client, sess wx233Session, apiURL string, params map[string]string) (map[string]any, error) {
	qs := encodeParams(params)
	if qs != "" {
		apiURL += "?" + qs
	}
	body, err := c.GetString(apiURL, signedHeaders(sess, "get", qs))
	if err != nil {
		return nil, err
	}
	return parseJSON(body)
}
func apiPost(c *util.Client, sess wx233Session, apiURL string, data map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("wangxiao233 marshal: %w", err)
	}
	resp, err := c.Post(apiURL, bytes.NewReader(payload), signedHeaders(sess, "post", string(payload)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wangxiao233 read body: %w", err)
	}
	return parseJSON(string(b))
}
func signedHeaders(sess wx233Session, method, text string) map[string]string {
	sid := sidPrefix + time.Now().Format("20060102150405") + "1000099999"
	seed := signSecret + sid + text
	sum := md5.Sum([]byte(seed))
	return map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/plain, */*", "Origin": "https://wx.233.com", "Referer": refererURL, "token": sess.token, "sid": sid, "sign": strings.ToUpper(hex.EncodeToString(sum[:]))}
}

func parseCourse(raw string) wx233Course {
	c := wx233Course{domain: "aq"}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		c.productID = q.Get("productId")
		c.childProductID = q.Get("childProductId")
		c.versionProductID = q.Get("versionProductId")
		c.teacherID = q.Get("teacherId")
		c.domain = firstNonEmpty(q.Get("domain"), c.domain)
	}
	return c
}
func firstPurchasedCourse(c *util.Client, sess wx233Session, domain string) (wx233Course, error) {
	domain = firstNonEmpty(domain, "aq")
	for _, apiURL := range []string{urlUserCourse, urlVktCourse} {
		data, err := apiGetData(c, sess, apiURL, map[string]string{"domain": domain, "types": "1"})
		if err != nil {
			continue
		}
		for _, m := range mapsUnder(data) {
			pid := val(m, "productId")
			if pid != "" {
				return wx233Course{productID: pid, childProductID: val(m, "childProductId"), versionProductID: val(m, "versionProductId"), teacherID: val(m, "teacherId"), domain: domain, title: firstNonEmpty(val(m, "name"), val(m, "title"), val(m, "productName"))}, nil
			}
		}
	}
	return wx233Course{}, fmt.Errorf("wangxiao233: purchased course list is empty")
}
func childCourses(data any, base wx233Course) []wx233Course {
	out := []wx233Course{}
	for _, m := range mapsUnder(data) {
		pid := firstNonEmpty(val(m, "productId"), base.productID)
		child := firstNonEmpty(val(m, "childProductId"), val(m, "currentProductId"), base.childProductID)
		if pid == "" && child == "" {
			continue
		}
		out = append(out, wx233Course{productID: pid, childProductID: child, versionProductID: firstNonEmpty(val(m, "versionProductId"), base.versionProductID), versionID: val(m, "versionId"), teacherID: firstNonEmpty(val(m, "teacherId"), base.teacherID), domain: base.domain, title: firstNonEmpty(val(m, "courseName"), val(m, "childProductName"), val(m, "productName"), val(m, "name"), base.title)})
	}
	return out
}
func fillVersion(c *util.Client, sess wx233Session, in wx233Course) wx233Course {
	if in.versionID != "" {
		return in
	}
	pid := firstNonEmpty(in.childProductID, in.versionProductID, in.productID)
	data, err := apiGetData(c, sess, urlVersion, map[string]string{"productId": pid, "clientType": "3"})
	if err != nil {
		return in
	}
	for _, m := range mapsUnder(data) {
		if v := val(m, "versionId"); v != "" {
			in.versionID = v
			in.childProductID = firstNonEmpty(val(m, "productId"), in.childProductID)
			in.versionProductID = firstNonEmpty(val(m, "childProductId"), in.versionProductID)
			in.teacherID = firstNonEmpty(val(m, "teacherId"), in.teacherID)
			return in
		}
	}
	return in
}
func collectVideos(data any) []wx233Video {
	out := []wx233Video{}
	for _, m := range mapsUnder(data) {
		v := wx233Video{title: firstNonEmpty(val(m, "detailName"), val(m, "name"), val(m, "title")), detailID: firstNonEmpty(val(m, "detailId"), val(m, "id")), polyVid: firstNonEmpty(val(m, "polyVid"), val(m, "polyvVid")), essVid: val(m, "essVid"), aliyunVid: firstNonEmpty(val(m, "aliyunVid"), val(m, "aliyunVideoId")), mp3URL: val(m, "mp3Url")}
		if v.detailID != "" || v.polyVid != "" || v.essVid != "" || v.aliyunVid != "" || v.mp3URL != "" {
			out = append(out, v)
		}
	}
	return out
}
func resolveVideo(c *util.Client, sess wx233Session, v wx233Video, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if v.detailID != "" && v.polyVid == "" && v.essVid == "" && v.aliyunVid == "" {
		if data, err := apiGetData(c, sess, urlVodDetail, map[string]string{"detailIds": v.detailID}); err == nil {
			for _, m := range mapsUnder(data) {
				v.polyVid = firstNonEmpty(v.polyVid, val(m, "polyVid"), val(m, "polyvVid"))
				v.essVid = firstNonEmpty(v.essVid, val(m, "essVid"))
				v.aliyunVid = firstNonEmpty(v.aliyunVid, val(m, "aliyunVid"), val(m, "aliyunVideoId"))
			}
		}
	}
	if v.polyVid != "" {
		return resolvePolyv(c, v)
	}
	if v.mp3URL != "" {
		return media(v.title, v.mp3URL, "mp3", map[string]any{"detail_id": v.detailID}), nil
	}
	if v.aliyunVid != "" {
		if m, err := apiGet(c, sess, urlPlayAuth, map[string]string{"videoId": v.aliyunVid}); err == nil {
			data, _ := m["data"].(map[string]any)
			payload := shared.AliyunDecodePlayAuth(data["playAuth"])
			payload.Region = firstNonEmpty(payload.Region, val(data, "regionId"))
			if payload.Region == "" {
				payload.Region = "cn-shanghai"
			}
			playOpts := shared.AliyunPlayOptions{
				Referer:         refererURL,
				Origin:          "https://wx.233.com",
				Quality:         firstNonEmpty(qualityFromOpts(opts)),
				FetchM3U8:       true,
				RewriteM3U8Keys: true,
			}
			if playCfg, err := json.Marshal(map[string]string{"EncryptType": "AliyunVoDEncryption"}); err == nil {
				playOpts.ExtraParams = map[string]string{"PlayConfig": string(playCfg)}
			}
			info, err := shared.AliyunResolvePlayInfo(c, payload, v.aliyunVid, playOpts)
			if err != nil {
				return nil, fmt.Errorf("blocked: needs Aliyun STS SDK / DRM engine: %w", err)
			}
			extra := map[string]any{"aliyun_vid": v.aliyunVid, "aliyun_api": info.APIURL, "source_type": info.SourceType}
			if info.M3U8Text != "" {
				extra["m3u8_text"] = info.M3U8Text
			}
			return media(v.title, info.URL, info.Format, extra), nil
		}
	}
	return nil, fmt.Errorf("wangxiao233: unsupported video source")
}
func resolvePolyv(c *util.Client, v wx233Video) (*extractor.MediaInfo, error) {
	if tokenBody, err := c.GetString(urlPolyvToken+"?videoid="+url.QueryEscape(v.polyVid), map[string]string{"Referer": refererURL}); err == nil {
		_, _ = parseJSON(parseJSONP(tokenBody))
	}
	sec, err := shared.PolyvResolveSecure(c, v.polyVid, map[string]string{"Referer": refererURL})
	if err != nil {
		return nil, err
	}
	manifest, err := shared.PolyvPickBestManifest(sec)
	if err != nil {
		return nil, err
	}
	extra := map[string]any{"poly_vid": v.polyVid, "detail_id": v.detailID, "polyv_secure_url": fmt.Sprintf(urlPolyvSecure, v.polyVid)}
	if txt, err := c.GetString(manifest, map[string]string{"Referer": refererURL}); err == nil && sec.Data.Playsafe.Token != "" {
		if rewritten, err := shared.PolyvRewriteM3U8Keys(c, txt, sec.Data.Playsafe.Token, refererURL); err == nil {
			extra["m3u8_text"] = rewritten
		}
	}
	return media(v.title, manifest, "m3u8", extra), nil
}

func parseJSON(text string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &m); err != nil {
		return nil, err
	}
	return m, nil
}
func parseJSONP(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			return text[i : j+1]
		}
	}
	return text
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
func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"PlayURL", "PlayUrl", "playUrl", "url", "source", "videoUrl"} {
			if u := val(m, k); strings.HasPrefix(u, "http") {
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
func encodeParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	q := url.Values{}
	for _, k := range keys {
		q.Set(k, params[k])
	}
	return q.Encode()
}
func tokenFromJar(jar http.CookieJar) string {
	for _, raw := range []string{refererURL, "https://japi.233.com/"} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				if strings.EqualFold(c.Name, "clientauthentication") || strings.EqualFold(c.Name, "token") {
					return c.Value
				}
			}
		}
	}
	return ""
}
func media(title, u, fmtName string, extra map[string]any) *extractor.MediaInfo {
	if title == "" {
		title = "video"
	}
	stream := extractor.Stream{Quality: "best", URLs: []string{u}, Format: fmtName, Headers: map[string]string{"Referer": refererURL}}
	if strings.Contains(strings.ToLower(fmtName), "m3u8") {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "wangxiao233", Title: title, Streams: map[string]extractor.Stream{"default": stream}, Extra: extra}
}
func mediaFormat(u string) string {
	u = strings.ToLower(u)
	if strings.Contains(u, ".m3u8") {
		return "m3u8"
	}
	if strings.Contains(u, ".mp3") {
		return "mp3"
	}
	return "mp4"
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func qualityFromOpts(opts *extractor.ExtractOpts) string {
	if opts == nil {
		return ""
	}
	return opts.Quality
}
