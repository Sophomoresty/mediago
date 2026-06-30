// Package zhaozhao implements an extractor for yikao88.com (昭昭医考) courses.
package zhaozhao

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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
	refererURL           = "https://wx.yikao88.com/"
	originURL            = "https://wx.yikao88.com"
	myProductAPI         = "https://api.yikao88.com/api-order/order/pc/v5/myBuyProductList"
	packageListAPI       = "https://api.yikao88.com/api-shop/course/pc/v5/getPackagelistByProduct"
	courseDetailAPI      = "https://api.yikao88.com/api-shop/course/pc/v5/selectDetail"
	productDetailAPI     = "https://api.yikao88.com/api-shop/product/pc/v5/selectPcProductById"
	childFileAPI         = "https://api.yikao88.com/api-shop/learningPackage/pc/v5/getChildIdToAllZiliaoInfo"
	playSafeTokenAPI     = "https://api.yikao88.com/api-play/play-safe/token"
	polyvSecureURL       = "https://player.polyv.net/secure/{vid}.json"
	polyvKeyURL          = "https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}"
	polyvPDXLibPlayerURL = "https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js"
	yikao88Client        = "wx-web"
	yikao88Version       = "1.0.0"
	yikao88AppID         = "1001"
	yikao88Platform      = "PC"
	yikao88APISignSecret = "4ad2d8f07ee9a358455375c2982f8a9a"
	playSafePublicKeyPEM = "-----BEGIN PUBLIC KEY-----\nMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCTjFALEDjmjD2/0HVoWtHuAmEptQrV\nUy1bZxoSoDrpiyllHI9UtVMkt7fGcaX5eifaIpkF/cmvD4LUlv7ioPyUiSQ9SpRqZEsI\nWfvYOyXgFF0REo2cULp49PK6glN00NEUAi6VW1CCHBetQJau/HeDojzPWacSq7UlG2/e\nnEDTlQIDAQAB\n-----END PUBLIC KEY-----"
	polyvPDXSecret       = "OWtjN9xcDcc2cwXKxECpRgKw7piD4RwCdfOUlyNHFdSV0gHi="
	// POLYV_IV used for key decryption (Zhaozhao_Config).
	polyvIVHex = "01020305070B0D1113171D0705030201"
)

var (
	patterns      = []string{`(?:[\w-]+\.)?yikao88\.com/`}
	idRe          = regexp.MustCompile(`(?:productId|product_id|pid|courseId|course_id|cid)=([0-9A-Za-z_\-]+)`)
	polyvVidRe    = regexp.MustCompile(`^[0-9A-Za-z]+_[0-9A-Za-z]+$`)
	mediaURLRe    = regexp.MustCompile(`https?://[^"'\s<>]+(?:\.m3u8|\.mp4|\.flv|\.pdf|\.pptx?|\.docx?|\.xlsx?|\.zip|\.rar|\.7z|\.txt|\.png|\.jpe?g)[^"'\s<>]*`)
	titleCleanRe  = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
	m3u8URIRe     = regexp.MustCompile(`URI="([^"]+)"`)
	m3u8IVRe      = regexp.MustCompile(`IV\s*=\s*0x([0-9a-fA-F]+)`)
	bitrateRe     = regexp.MustCompile(`_(\d+)\.m3u8(?:\?|$)`)
	priceNumberRe = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
	priceKeys     = []string{"sellPrice", "specialPrice", "productPrice", "finalPrice", "discountPrice", "activityPrice", "price"}
	// polyvPDXIVBytes from Zhaozhao_Base.polyv_pdx_iv_bytes.
	polyvPDXIVBytes = []byte{13, 22, 8, 12, 7, 6, 13, 1, 50, 11, 12, 8, 5, 16, 4, 1}
	playTokenAPIs   = []string{
		"https://api.yikao88.com/api-shop/course/pc/v5/getPolyvPlaySafe",
		"https://api.yikao88.com/api-shop/course/pc/v5/getPlaySafe",
		"https://api.yikao88.com/api-shop/course/pc/v5/getPlayToken",
		"https://api.yikao88.com/api-shop/course/pc/v5/getPlayTokenByVideoId",
		"https://api.yikao88.com/api-shop/course/pc/v5/getVideoPlayToken",
		"https://api.yikao88.com/api-shop/learningPackage/pc/v5/getPolyvPlaySafe",
		"https://api.yikao88.com/api-shop/learningPackage/pc/v5/getPlayToken",
		"https://api.yikao88.com/api-shop/video/pc/v5/getPolyvPlaySafe",
		"https://api.yikao88.com/api-shop/video/pc/v5/getPlayToken",
	}
)

func init() {
	extractor.Register(&Zhaozhao{}, extractor.SiteInfo{Name: "Zhaozhao", URL: "yikao88.com", NeedAuth: true})
}

type Zhaozhao struct{}

func (s *Zhaozhao) Patterns() []string { return patterns }

type zzContext struct {
	c        *util.Client
	headers  map[string]string
	token    string
	memberID string
	pid      string
	cid      string
}

type zzVideo struct {
	VideoID     string
	DirectURL   string
	Title       string
	CourseID    string
	ProductID   string
	ChildID     string
	Definitions []string
}

type zzFile struct {
	URL    string
	Title  string
	Format string
}

func (s *Zhaozhao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zhaozhao requires login cookies")
	}
	pid, cid := parseIDs(rawURL)
	ctx := newContext(opts.Cookies, pid, cid)
	if pid == "" && cid == "" {
		entries, err := ctx.courseListEntries()
		if err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("zhaozhao: purchased course list is empty")
		}
		return &extractor.MediaInfo{Site: "zhaozhao", Title: "zhaozhao_courses", Entries: entries}, nil
	}

	coursePayloads, title, err := ctx.loadCoursePayloads()
	if err != nil {
		return nil, err
	}
	videos := collectVideos(coursePayloads)
	files := collectFiles(coursePayloads)
	for _, v := range videos {
		if v.ChildID == "" {
			continue
		}
		files = append(files, ctx.fetchChildFiles(v.ChildID)...)
	}
	files = dedupeFiles(files)
	if len(videos) == 0 && len(files) == 0 {
		return nil, fmt.Errorf("zhaozhao: no video/file nodes found for productId=%s courseId=%s", pid, cid)
	}

	entries := make([]*extractor.MediaInfo, 0, len(videos)+len(files))
	if !onlyFilesMode(opts) {
		for i, v := range videos {
			entry, err := ctx.resolveVideo(v, i+1)
			if err == nil {
				entries = append(entries, entry)
			}
		}
	}
	for i, f := range files {
		entries = append(entries, ctx.fileEntry(f, i+1))
	}
	if len(entries) == 0 {
		if onlyFilesMode(opts) {
			return nil, fmt.Errorf("zhaozhao: no courseware files resolved for productId=%s courseId=%s", pid, cid)
		}
		return nil, fmt.Errorf("zhaozhao: discovered %d video nodes but no playable polyv manifest resolved", len(videos))
	}
	return &extractor.MediaInfo{Site: "zhaozhao", Title: cleanTitle(firstNonEmpty(title, cid, pid)), Entries: entries, Extra: ctx.courseExtra(coursePayloads)}, nil
}

func newContext(jar http.CookieJar, pid, cid string) *zzContext {
	ctx := &zzContext{c: util.NewClient(), pid: pid, cid: cid}
	ctx.c.SetCookieJar(jar)
	ctx.headers, ctx.token, ctx.memberID = headersFromJar(jar)
	return ctx
}

func parseIDs(raw string) (productID, courseID string) {
	u, err := url.Parse(raw)
	if err == nil {
		q := u.Query()
		if strings.Contains(u.Fragment, "?") {
			if fq, e := url.ParseQuery(strings.SplitN(u.Fragment, "?", 2)[1]); e == nil {
				for k, vs := range fq {
					for _, v := range vs {
						q.Add(k, v)
					}
				}
			}
		}
		productID = firstNonEmpty(q.Get("productId"), q.Get("product_id"), q.Get("pid"))
		courseID = firstNonEmpty(q.Get("courseId"), q.Get("course_id"), q.Get("cid"))
	}
	for _, m := range idRe.FindAllStringSubmatch(raw, -1) {
		k := strings.ToLower(strings.SplitN(m[0], "=", 2)[0])
		if strings.Contains(k, "product") || k == "pid" {
			if productID == "" {
				productID = m[1]
			}
		} else if strings.Contains(k, "course") || k == "cid" {
			if courseID == "" {
				courseID = m[1]
			}
		}
	}
	return productID, courseID
}

func headersFromJar(jar http.CookieJar) (map[string]string, string, string) {
	h := map[string]string{
		"sec-ch-ua-platform": "\"Windows\"",
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua":          "\"Microsoft Edge\";v=\"141\", \"Not?A_Brand\";v=\"8\", \"Chromium\";v=\"141\"",
		"User-Agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0",
		"Sec-Fetch-Site":     "same-site",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Referer":            refererURL,
		"Pragma":             "no-cache",
		"Origin":             originURL,
		"Content-Type":       "application/x-www-form-urlencoded;charset=UTF-8",
		"Connection":         "keep-alive",
		"Cache-Control":      "no-cache",
		"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
		"Accept":             "application/json, text/plain, */*",
	}
	var parts []string
	var token, memberID string
	for _, raw := range []string{refererURL, originURL + "/", "https://api.yikao88.com/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
			switch strings.ToLower(ck.Name) {
			case "token":
				token = ck.Value
			case "memberid", "member_id":
				memberID = ck.Value
			}
		}
	}
	parts = uniqueStrings(parts)
	if len(parts) > 0 {
		h["cookie"] = strings.Join(parts, "; ")
		h["Cookie"] = h["cookie"]
	}
	if token != "" {
		h["token"] = token
		h["authorization"] = "Bearer " + token
		h["Authorization"] = "Bearer " + token
		h["x-token"] = token
	}
	if memberID != "" {
		h["memberId"] = memberID
		h["memberid"] = memberID
	}
	return h, token, memberID
}

func (x *zzContext) loadCoursePayloads() ([]any, string, error) {
	payloads := make([]any, 0, 6)
	productList, _ := x.signedGet(myProductAPI, map[string]string{"productTypeId": "1,7"}, nil)
	if productList != nil {
		payloads = append(payloads, productList)
	}
	if x.pid != "" {
		if detail, err := x.signedGet(productDetailAPI, map[string]string{"productId": x.pid}, nil); err == nil {
			payloads = append(payloads, detail)
		}
		if packages, err := x.signedGet(packageListAPI, map[string]string{"productId": x.pid}, nil); err == nil {
			payloads = append(payloads, packages)
			for _, pkg := range extractItems(packages["data"]) {
				courseID := firstString(pkg, "courseId", "course_id", "id")
				if courseID == "" || (x.cid != "" && courseID != x.cid) {
					continue
				}
				if detail, err := x.signedGet(courseDetailAPI, map[string]string{"courseId": courseID, "productId": x.pid}, nil); err == nil {
					payloads = append(payloads, detail)
				}
			}
		}
	}
	if x.cid != "" {
		params := map[string]string{"courseId": x.cid}
		if x.pid != "" {
			params["productId"] = x.pid
		}
		if detail, err := x.signedGet(courseDetailAPI, params, nil); err == nil {
			payloads = append(payloads, detail)
		}
	}
	if len(payloads) == 0 {
		return nil, "", fmt.Errorf("zhaozhao: all course APIs failed")
	}
	for _, p := range payloads {
		if title := firstTitle(p); title != "" {
			return payloads, title, nil
		}
	}
	return payloads, firstNonEmpty(x.cid, x.pid), nil
}

func (x *zzContext) courseListEntries() ([]*extractor.MediaInfo, error) {
	productList, err := x.signedGet(myProductAPI, map[string]string{"productTypeId": "1,7"}, nil)
	if err != nil {
		return nil, err
	}
	rows := extractItems(productList["data"])
	if len(rows) == 0 {
		rows = walkMaps(productList)
	}
	entries := make([]*extractor.MediaInfo, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		productID := firstString(row, "productId", "product_id", "id")
		courseID := firstString(row, "courseId", "course_id")
		if productID == "" && courseID == "" {
			continue
		}
		key := firstNonEmpty(productID, "-") + ":" + firstNonEmpty(courseID, "-")
		if seen[key] {
			continue
		}
		seen[key] = true
		title := cleanTitle(firstNonEmpty(firstString(row, "productName", "courseName", "packageName", "name", "title"), key))
		extra := map[string]any{"raw": row}
		if productID != "" {
			extra["product_id"] = productID
		}
		if courseID != "" {
			extra["course_id"] = courseID
		}
		if price, ok := priceFromAny(row); ok {
			extra["price"] = price
		}
		extra["purchased"] = true
		entries = append(entries, &extractor.MediaInfo{Site: "zhaozhao", Title: title, Extra: extra})
	}
	return entries, nil
}

func (x *zzContext) signedGet(api string, params map[string]string, extraHeaders map[string]string) (map[string]any, error) {
	variants := x.requestVariants(params, extraHeaders)
	var last map[string]any
	var lastErr error
	for _, v := range variants {
		body, err := x.c.GetString(urlWithQuery(api, v.params), v.headers)
		if err != nil {
			lastErr = err
			continue
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			lastErr = fmt.Errorf("parse %s: %w", api, err)
			continue
		}
		last = out
		if responseUsable(out) {
			return out, nil
		}
	}
	if last != nil {
		return last, nil
	}
	return nil, lastErr
}

type requestVariant struct {
	params  map[string]string
	headers map[string]string
}

func (x *zzContext) requestVariants(params map[string]string, extra map[string]string) []requestVariant {
	p1 := copyMap(params)
	t := nowMS()
	if x.token != "" {
		p1["token"] = x.token
	}
	if x.memberID != "" {
		p1["memberId"] = x.memberID
	}
	p1["t"] = t
	p2 := copyMap(params)
	p2["t"] = t
	h1 := x.buildRequestHeaders(extra, t)
	h2 := copyMap(h1)
	h2["X-Requested-With"] = "XMLHttpRequest"
	return []requestVariant{{params: p1, headers: h1}, {params: p1, headers: h2}, {params: p2, headers: h2}}
}

func (x *zzContext) buildRequestHeaders(extra map[string]string, ts string) map[string]string {
	h := copyMap(x.headers)
	for k, v := range extra {
		h[k] = v
	}
	h["client"] = yikao88Client
	h["version"] = yikao88Version
	h["appId"] = yikao88AppID
	h["platform"] = yikao88Platform
	h["ts"] = ts
	sig := md5.Sum([]byte(yikao88AppID + yikao88Platform + yikao88Version + ts + yikao88APISignSecret))
	h["apiSign"] = hex.EncodeToString(sig[:])
	if x.token != "" {
		h["token"] = x.token
		h["authorization"] = "Bearer " + x.token
		h["Authorization"] = "Bearer " + x.token
		h["x-token"] = x.token
	}
	if x.memberID != "" {
		h["memberId"] = x.memberID
		h["memberid"] = x.memberID
	}
	return h
}

func responseUsable(out map[string]any) bool {
	if out == nil {
		return false
	}
	code := strings.TrimSpace(fmt.Sprint(out["code"]))
	if code == "401" || code == "403" || code == "500" || code == "5000" {
		return false
	}
	msg := strings.TrimSpace(fmt.Sprint(firstNonNil(out["msg"], out["message"])))
	return !strings.Contains(msg, "未登录") && out["data"] != nil
}

func (x *zzContext) courseExtra(payloads []any) map[string]any {
	extra := map[string]any{"product_id": x.pid, "course_id": x.cid, "purchased": true}
	if price, ok := priceFromAny(payloads); ok {
		extra["price"] = price
	} else if x.pid != "" {
		extra["price"] = float64(999)
	}
	return compactAnyMap(extra)
}

func (x *zzContext) resolveVideo(v zzVideo, index int) (*extractor.MediaInfo, error) {
	if v.DirectURL != "" {
		u := normalizeAssetURL(v.DirectURL)
		name := cleanTitle(firstNonEmpty(v.Title, fmt.Sprintf("[%02d]--视频", index)))
		format := pickFormat(u, "")
		stream := extractor.Stream{Quality: "best", URLs: []string{u}, Format: format, Headers: x.downloadHeaders()}
		if strings.Contains(strings.ToLower(format), "m3u8") {
			stream.NeedMerge = true
		}
		return &extractor.MediaInfo{Site: "zhaozhao", Title: name, Streams: map[string]extractor.Stream{"default": stream}, Extra: map[string]any{"source_type": "direct", "video_url": u}}, nil
	}
	vid := formatPolyvVID(v.VideoID)
	if vid == "" {
		return nil, fmt.Errorf("zhaozhao: empty polyv video id")
	}
	if v.ProductID != "" {
		x.pid = v.ProductID
	}
	if v.CourseID != "" {
		x.cid = v.CourseID
	}
	playToken := x.getPlayToken(v.VideoID)
	sec, err := shared.PolyvResolveSecure(x.c, vid, x.headers)
	if err != nil {
		return nil, err
	}
	manifest, err := shared.PolyvPickBestManifest(sec)
	if err != nil {
		return nil, err
	}
	manifest = normalizePolyvManifestURL(manifest)
	if strings.Contains(strings.ToLower(manifestPath(manifest)), ".pdx") {
		return x.resolvePolyvPDX(v, vid, manifest, playToken, sec, index)
	}
	playToken = firstNonEmpty(playToken, sec.PlayToken())
	name := cleanTitle(firstNonEmpty(v.Title, sec.Data.Title, fmt.Sprintf("[%02d]--%s", index, v.VideoID)))
	extra := map[string]any{"video_id": v.VideoID, "polyv_vid": vid, "secure_url_template": polyvSecureURL}
	if playToken != "" {
		extra["play_safe_token"] = playToken
	}
	if v.ChildID != "" {
		extra["child_id"] = v.ChildID
	}
	streamURL := manifest
	if m3u8Text := x.fetchPolyvM3U8Text(manifest, playToken, sec.SeedConst()); m3u8Text != "" {
		extra["m3u8_text"] = m3u8Text
		extra["m3u8_url"] = manifest
		extra["source_type"] = "m3u8_text"
		streamURL = dataM3U8URL(m3u8Text)
	}
	return &extractor.MediaInfo{Site: "zhaozhao", Title: name, Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{streamURL}, Format: "m3u8", NeedMerge: true, Headers: x.downloadHeaders()}}, Extra: extra}, nil
}

func (x *zzContext) resolvePolyvPDX(v zzVideo, polyvVID, pdxURL, playToken string, sec *shared.PolyvSecure, index int) (*extractor.MediaInfo, error) {
	playToken = firstNonEmpty(playToken, sec.PlayToken())
	name := cleanTitle(firstNonEmpty(v.Title, sec.Data.Title, fmt.Sprintf("[%02d]--%s", index, v.VideoID)))
	pdxInfo, err := x.buildPolyvPDXInfo(pdxURL, v.VideoID, playToken)
	if err != nil {
		return nil, err
	}
	streamURL := normalizeAssetURL(firstNonEmpty(pdxInfo["m3u8_url"], pdxURL))
	format := "pdx"
	extra := map[string]any{
		"video_id":          v.VideoID,
		"polyv_vid":         polyvVID,
		"source_type":       "polyv_pdx",
		"pdx_url":           pdxURL,
		"play_safe_token":   playToken,
		"secure_url":        fmt.Sprintf(shared.PolyvSecureURLTmpl, url.PathEscape(polyvVID)),
		"pdx_lib_player_js": polyvPDXLibPlayerURL,
	}
	for k, val := range pdxInfo {
		if val != "" {
			extra[k] = val
		}
	}
	if text := pdxInfo["m3u8_text"]; text != "" {
		extra["m3u8_text"] = text
		streamURL = dataM3U8URL(text)
		format = "m3u8"
	}
	stream := extractor.Stream{Quality: "best", URLs: []string{streamURL}, Format: format, NeedMerge: true, Headers: x.downloadHeaders(), Extra: map[string]any{"cryptor": "polyv_pdx"}}
	return &extractor.MediaInfo{Site: "zhaozhao", Title: name, Streams: map[string]extractor.Stream{"default": stream}, Extra: extra}, nil
}

func (x *zzContext) buildPolyvPDXInfo(pdxURL, videoID, playToken string) (map[string]string, error) {
	if pdxURL == "" || playToken == "" {
		return nil, fmt.Errorf("zhaozhao polyv pdx: missing pdx URL or play token")
	}
	pid := fmt.Sprintf("%dX%07d", time.Now().UnixMilli(), time.Now().Nanosecond()%10000000)
	pdxReqURL := addURLQueryParams(pdxURL, map[string]string{"pid": pid, "device": "desktop", "token": playToken})
	headers := x.polyvHeaders()
	body, err := x.c.GetString(pdxReqURL, headers)
	if err != nil {
		return nil, fmt.Errorf("zhaozhao polyv pdx fetch: %w", err)
	}
	text, seed, err := decryptPolyvPDXText(body)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(text, "#EXTM3U") {
		return nil, fmt.Errorf("zhaozhao polyv pdx: decrypted body is not m3u8")
	}
	text = absolutizeM3U8Text(text, pdxReqURL)
	keyURL := buildPolyvPDXKeyURL(firstM3U8URI(text), pdxReqURL, playToken, pid)
	keyHex := ""
	if keyURL != "" {
		if keyBytes, err := x.c.GetBytes(keyURL, headers); err == nil && len(keyBytes) > 0 {
			keyHex = strings.ToUpper(hex.EncodeToString(keyBytes))
		}
	}
	return map[string]string{
		"type":            "pdx",
		"video_id":        videoID,
		"pdx_url":         pdxURL,
		"m3u8_url":        pdxReqURL,
		"m3u8_text":       text,
		"pid":             pid,
		"play_safe_token": playToken,
		"seed_const":      seed,
		"iv_hex":          firstM3U8IV(text),
		"key_url":         keyURL,
		"key_hex":         keyHex,
		"pdx_version":     "2",
	}, nil
}

func (x *zzContext) polyvHeaders() map[string]string {
	return map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Origin":     originURL,
		"Referer":    refererURL,
		"User-Agent": x.headers["User-Agent"],
	}
}

func (x *zzContext) getPlayToken(videoID string) string {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return ""
	}
	if tok := x.getPlaySafeToken(videoID); tok != "" {
		return tok
	}
	candidates := []map[string]string{
		{"productId": x.pid, "courseId": x.cid, "videoId": videoID},
		{"productId": x.pid, "courseId": x.cid, "vid": videoID},
		{"videoId": videoID, "courseId": x.cid},
		{"vid": videoID, "courseId": x.cid},
		{"videoId": videoID},
		{"vid": videoID},
		{"courseVideoId": videoID},
	}
	for _, api := range playTokenAPIs {
		for _, params := range candidates {
			clean := withoutEmpty(params)
			if out, err := x.signedGet(api, clean, nil); err == nil {
				if tok := pickPlayToken(out, x.token); tok != "" {
					return tok
				}
			}
		}
	}
	return ""
}

func (x *zzContext) getPlaySafeToken(videoID string) string {
	if x.memberID == "" {
		return ""
	}
	payload, _ := json.Marshal(map[string]string{"videoId": videoID, "viewerId": x.memberID})
	enc, err := encryptPlaySafeParams(string(payload))
	if err != nil || enc == "" {
		return ""
	}
	h := x.buildRequestHeaders(map[string]string{"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-Requested-With": "XMLHttpRequest"}, nowMS())
	body, err := x.c.PostForm(playSafeTokenAPI, map[string]string{"params": enc}, h)
	if err != nil {
		return ""
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return ""
	}
	if data := asMap(out["data"]); len(data) > 0 {
		if tok := firstString(data, "token"); tok != "" {
			return tok
		}
	}
	return pickPlayToken(out, x.token)
}

func encryptPlaySafeParams(payload string) (string, error) {
	block, _ := pem.Decode([]byte(playSafePublicKeyPEM))
	if block == nil {
		return "", fmt.Errorf("missing public key")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("not rsa public key")
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(payload))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func pickPlayToken(payload any, loginToken string) string {
	priority := []string{"playsafe", "playSafe", "play_safe", "playSafeToken", "playToken", "play_token"}
	for _, node := range walkMaps(payload) {
		for _, k := range priority {
			if s := strings.TrimSpace(fmt.Sprint(node[k])); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	for _, node := range walkMaps(payload) {
		if s := strings.TrimSpace(fmt.Sprint(node["token"])); s != "" && s != "<nil>" && s != loginToken {
			return s
		}
	}
	return ""
}

func collectVideos(payloads []any) []zzVideo {
	seen := map[string]bool{}
	var out []zzVideo
	var walk func(any, []string, string, string)
	walk = func(v any, titles []string, productID, courseID string) {
		switch t := v.(type) {
		case []any:
			for _, it := range t {
				walk(it, titles, productID, courseID)
			}
		case map[string]any:
			productID = firstNonEmpty(firstString(t, "productId", "product_id"), productID)
			courseID = firstNonEmpty(firstString(t, "courseId", "course_id"), courseID)
			title := firstString(t, "videoName", "appName", "childName", "chapterName", "stationName", "courseName", "productName", "name", "title")
			if title != "" {
				titles = append(titles, title)
			}
			vid := firstString(t, "videoId", "video_id", "polyvVideoId", "polyv_video_id", "vid")
			directURL := ""
			if strings.HasPrefix(strings.ToLower(vid), "http") && isVideoURL(vid) {
				directURL = vid
				vid = ""
			} else {
				for _, key := range []string{"videoUrl", "video_url", "playUrl", "play_url", "m3u8Url", "m3u8_url", "mediaUrl", "media_url", "url", "path", "src"} {
					if u := firstString(t, key); strings.HasPrefix(strings.ToLower(u), "http") && isVideoURL(u) {
						directURL = u
						break
					}
				}
			}
			key := firstNonEmpty(directURL, vid)
			if key != "" && (directURL != "" || looksLikeVideoID(vid)) && !seen[key] {
				seen[key] = true
				out = append(out, zzVideo{VideoID: vid, DirectURL: directURL, Title: buildTitle(titles, len(out)+1), ProductID: productID, CourseID: courseID, ChildID: firstString(t, "childId", "child_id"), Definitions: parseDefinitions(t["definitionList"])})
			}
			for _, val := range t {
				walk(val, titles, productID, courseID)
			}
		}
	}
	for _, p := range payloads {
		walk(p, nil, "", "")
	}
	return out
}

func collectFiles(payloads []any) []zzFile {
	seen := map[string]bool{}
	var out []zzFile
	fileKeys := map[string]bool{"coursewareUrl": true, "learningUrl": true, "fileUrl": true, "downloadUrl": true, "url": true, "previewUrl": true, "ossUrl": true}
	var walk func(any, []string)
	walk = func(v any, titles []string) {
		switch t := v.(type) {
		case string:
			for _, u := range mediaURLRe.FindAllString(strings.ReplaceAll(t, `\/`, `/`), -1) {
				addFile(&out, seen, u, buildTitle(titles, len(out)+1), "")
			}
		case []any:
			for _, it := range t {
				walk(it, titles)
			}
		case map[string]any:
			if name := firstString(t, "coursewareName", "learningName", "fileName", "name", "title", "packageName", "childName"); name != "" {
				titles = append(titles, name)
			}
			for k, val := range t {
				if fileKeys[k] {
					if u := strings.TrimSpace(fmt.Sprint(val)); strings.HasPrefix(u, "http") && !looksLikeVideoID(u) {
						addFile(&out, seen, u, buildTitle(titles, len(out)+1), firstString(t, "fileType", "typeName", "format", "suffix", "ext"))
					}
				}
				walk(val, titles)
			}
		}
	}
	for _, p := range payloads {
		walk(p, nil)
	}
	return out
}

func (x *zzContext) fetchChildFiles(childID string) []zzFile {
	childID = strings.TrimSpace(childID)
	if childID == "" {
		return nil
	}
	out, err := x.signedGet(childFileAPI, map[string]string{"childId": childID}, nil)
	if err != nil {
		return nil
	}
	files := collectFiles([]any{out})
	for i := range files {
		if files[i].Title == "" {
			files[i].Title = "资料"
		}
	}
	return files
}

func dedupeFiles(files []zzFile) []zzFile {
	seen := map[string]bool{}
	out := make([]zzFile, 0, len(files))
	for _, f := range files {
		key := strings.Join([]string{f.URL, f.Title, f.Format}, "\x00")
		if f.URL == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

func addFile(out *[]zzFile, seen map[string]bool, rawURL, title, fmtHint string) {
	rawURL = normalizeAssetURL(rawURL)
	if rawURL == "" || seen[rawURL] || isVideoURL(rawURL) {
		return
	}
	seen[rawURL] = true
	(*out) = append(*out, zzFile{URL: rawURL, Title: title, Format: pickFormat(rawURL, fmtHint)})
}

func (x *zzContext) fileEntry(f zzFile, index int) *extractor.MediaInfo {
	name := cleanTitle(firstNonEmpty(f.Title, fmt.Sprintf("[%02d]--资料", index)))
	return &extractor.MediaInfo{Site: "zhaozhao", Title: name, Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{f.URL}, Format: f.Format, Headers: x.downloadHeaders()}}, Extra: map[string]any{"type": "file", "file_url": f.URL}}
}

func looksLikeVideoID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http") {
		return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".flv")
	}
	return polyvVidRe.MatchString(value) || (len(value) >= 8 && regexp.MustCompile(`^[0-9A-Za-z]+$`).MatchString(value))
}

func formatPolyvVID(videoID string) string {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return ""
	}
	if strings.HasPrefix(videoID, "http") {
		return videoID
	}
	return shared.PolyvNormalizeVID(videoID)
}

func parseDefinitions(v any) []string {
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			out := make([]string, 0, len(arr))
			for _, m := range arr {
				out = append(out, firstNonEmpty(firstString(m, "quality"), firstString(m, "desp")))
			}
			return out
		}
	}
	out := []string{}
	for _, m := range extractItems(v) {
		if q := firstNonEmpty(firstString(m, "quality"), firstString(m, "desp")); q != "" {
			out = append(out, q)
		}
	}
	return out
}

func urlWithQuery(base string, params map[string]string) string {
	u, _ := url.Parse(base)
	q := u.Query()
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if params[k] != "" {
			q.Set(k, params[k])
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func nowMS() string { return fmt.Sprint(time.Now().UnixMilli()) }

func copyMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func withoutEmpty(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

func extractItems(v any) []map[string]any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, it := range x {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, k := range []string{"data", "list", "records", "items", "courseStationList", "courseChapterList", "childVideoList", "children"} {
			child, ok := x[k]
			if !ok {
				continue
			}
			if out := extractItems(child); len(out) > 0 {
				return out
			}
		}
		return nil
	default:
		return nil
	}
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, v := range t {
				walk(v)
			}
		case []any:
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(v)
	return out
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func firstTitle(v any) string {
	for _, node := range walkMaps(v) {
		if s := firstString(node, "productName", "courseName", "packageName", "title", "name"); s != "" {
			return s
		}
	}
	return ""
}

func buildTitle(parts []string, index int) string {
	clean := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			clean = append(clean, p)
		}
	}
	if len(clean) == 0 {
		return fmt.Sprintf("[%02d]--未命名课时", index)
	}
	return fmt.Sprintf("[%02d]--%s", index, strings.Join(clean, "--"))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
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

func uniqueStrings(vals []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }

func isVideoURL(u string) bool {
	lower := strings.ToLower(u)
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".flv")
}

func pickFormat(rawURL, hint string) string {
	hint = strings.Trim(strings.ToLower(hint), ". ")
	if hint != "" && hint != "0" && hint != "1" {
		return hint
	}
	u, _ := url.Parse(rawURL)
	path := strings.ToLower(u.Path)
	if idx := strings.LastIndex(path, "."); idx >= 0 && idx+1 < len(path) {
		return path[idx+1:]
	}
	return "pdf"
}

func onlyFilesMode(opts *extractor.ExtractOpts) bool {
	if opts == nil {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(opts.Quality))
	q = strings.NewReplacer("-", "", "_", "", " ", "").Replace(q)
	switch q {
	case "2", "pdf", "onlypdf", "courseware", "material", "materials", "document", "documents", "file", "files", "课件", "资料":
		return true
	default:
		return false
	}
}

func (x *zzContext) downloadHeaders() map[string]string {
	h := map[string]string{
		"Referer":    refererURL,
		"User-Agent": x.headers["User-Agent"],
	}
	for _, k := range []string{"Cookie", "cookie", "token", "authorization", "Authorization", "x-token", "memberId", "memberid"} {
		if v := x.headers[k]; v != "" {
			h[k] = v
		}
	}
	return h
}

func (x *zzContext) fetchPolyvM3U8Text(manifest, token, seedConst string) string {
	if manifest == "" || !strings.Contains(strings.ToLower(manifest), ".m3u8") {
		return ""
	}
	headers := map[string]string{"Referer": refererURL, "User-Agent": x.headers["User-Agent"]}
	text, err := x.c.GetString(manifest, headers)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
		return ""
	}
	text = absolutizeM3U8Text(text, manifest)
	if token != "" {
		if rewritten, err := shared.PolyvRewriteM3U8KeysWithOptions(x.c, text, shared.PolyvRewriteOptions{Token: token, Referer: refererURL, ManifestURL: manifest, SeedConst: seedConst}); err == nil && rewritten != "" {
			text = inlineHexKeyURIs(rewritten)
		}
	}
	return text
}

func normalizeAssetURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

func normalizePolyvManifestURL(raw string) string {
	raw = normalizeAssetURL(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return strings.TrimRight(shared.PolyvHLSPlayBase, "/") + "/" + strings.TrimLeft(raw, "/")
}

func manifestPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Path
}

func absolutizeM3U8Text(text, manifest string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#EXT-X-KEY:") {
			lines[i] = m3u8URIRe.ReplaceAllStringFunc(line, func(match string) string {
				parts := m3u8URIRe.FindStringSubmatch(match)
				if len(parts) != 2 {
					return match
				}
				return `URI="` + resolveAgainst(parts[1], manifest) + `"`
			})
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines[i] = resolveAgainst(trimmed, manifest)
	}
	return strings.Join(lines, "\n")
}

func resolveAgainst(raw, baseRaw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "0x") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	base, err := url.Parse(baseRaw)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func inlineHexKeyURIs(text string) string {
	return m3u8URIRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := m3u8URIRe.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		uri := strings.TrimSpace(parts[1])
		if !strings.HasPrefix(strings.ToLower(uri), "0x") {
			return match
		}
		keyBytes, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(uri, "0x"), "0X"))
		if err != nil || len(keyBytes) == 0 {
			return match
		}
		return `URI="data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(keyBytes) + `"`
	})
}

func dataM3U8URL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func addURLQueryParams(raw string, params map[string]string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func decryptPolyvPDXText(pdxText string) (string, string, error) {
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(pdxText)), &envelope); err != nil {
		return "", "", fmt.Errorf("zhaozhao polyv pdx parse: %w", err)
	}
	body := firstString(envelope, "body")
	seed := firstNonEmpty(firstString(envelope, "seed_const"), firstString(envelope, "seedConst"), firstString(envelope, "seed"))
	if body == "" || seed == "" {
		return "", "", fmt.Errorf("zhaozhao polyv pdx: missing body or seed")
	}
	seed = normalizeSeed(seed)
	keySeed := md5.Sum([]byte(polyvPDXSecret + seed))
	key := []byte(hex.EncodeToString(keySeed[:])[1:17])
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	ciphertext, err := safeB64Decode(body)
	if err != nil {
		return "", "", fmt.Errorf("zhaozhao polyv pdx base64: %w", err)
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", "", fmt.Errorf("zhaozhao polyv pdx: ciphertext not block aligned")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, polyvPDXIVBytes).CryptBlocks(plain, ciphertext)
	plain = stripPKCS7(plain)
	return strings.TrimSpace(string(plain)), seed, nil
}

func normalizeSeed(seed string) string {
	seed = strings.TrimSpace(seed)
	var f float64
	if _, err := fmt.Sscanf(seed, "%f", &f); err == nil {
		return fmt.Sprintf("%d", int64(f))
	}
	return seed
}

func safeB64Decode(text string) ([]byte, error) {
	text = strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(text))
	text = regexp.MustCompile(`[^A-Za-z0-9+/=]`).ReplaceAllString(text, "")
	text += strings.Repeat("=", (4-len(text)%4)%4)
	return base64.StdEncoding.DecodeString(text)
}

func stripPKCS7(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	pad := int(data[len(data)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(data) {
		return data
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return data
		}
	}
	return data[:len(data)-pad]
}

func buildPolyvPDXKeyURL(keyURL, m3u8URL, playToken, pid string) string {
	keyURL = resolveAgainst(keyURL, m3u8URL)
	if keyURL == "" || playToken == "" {
		return ""
	}
	u, err := url.Parse(keyURL)
	if err != nil {
		return ""
	}
	path := u.Path
	if strings.Contains(path, "/playsafe/") {
		path = regexp.MustCompile(`/playsafe/(?:v\d+/)?`).ReplaceAllString(path, "/playsafe/v13/")
	} else if strings.HasPrefix(path, "/") {
		path = "/playsafe/v13" + path
	} else {
		path = "/playsafe/v13/" + path
	}
	q := u.Query()
	q.Set("token", playToken)
	if pid != "" {
		q.Set("pid", pid)
	}
	u.Path = path
	u.RawQuery = q.Encode()
	return u.String()
}

func firstM3U8URI(text string) string {
	if m := m3u8URIRe.FindStringSubmatch(text); len(m) == 2 {
		return m[1]
	}
	return ""
}

func firstM3U8IV(text string) string {
	if m := m3u8IVRe.FindStringSubmatch(text); len(m) == 2 {
		return m[1]
	}
	return ""
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

func priceFromAny(v any) (float64, bool) {
	switch t := v.(type) {
	case map[string]any:
		for _, key := range priceKeys {
			if raw, ok := t[key]; ok {
				if price, ok := normalizePrice(raw); ok {
					return price, true
				}
			}
		}
		for _, child := range t {
			if price, ok := priceFromAny(child); ok {
				return price, true
			}
		}
	case []any:
		for _, child := range t {
			if price, ok := priceFromAny(child); ok {
				return price, true
			}
		}
	}
	return 0, false
}

func normalizePrice(v any) (float64, bool) {
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
	return float64(int(f*100+0.5)) / 100, true
}
