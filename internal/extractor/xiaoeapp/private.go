package xiaoeapp

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const h5UA = "Mozilla/5.0 (Linux; Android 5.1.1; SM-N976N Build/QP1A.190711.020; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/74.0.3729.136 Mobile Safari/537.36;XiaoEHelperApp;tongtongnewapp_platform=android;tongtongnewapp_version=6.1.1;XEGuardWhitelist"

var xiaoeM3U8URIRe = regexp.MustCompile(`URI="([^"]+)"`)

type xiaoePrivateCandidate struct {
	url string
	ext map[string]any
}

type xiaoeByteRange struct {
	start int64
	end   int64
}

func buildXiaoeListMedia(items []xeItem) *extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for i, it := range items {
		title := firstNonEmpty(it.title, fmt.Sprintf("课程%d", i+1))
		entries = append(entries, &extractor.MediaInfo{
			Site:  "xiaoeapp",
			Title: title,
			Extra: compactXiaoeMap(map[string]any{
				"resource_id":   it.id,
				"resource_type": typeMap(it.typ),
				"app_id":        it.appID,
				"c_user_id":     it.cUserID,
				"product_id":    it.productID,
				"price":         xiaoePrice(it.raw),
				"purchased":     xiaoePurchased(it.raw),
			}),
		})
	}
	return &extractor.MediaInfo{Site: "xiaoeapp", Title: "xiaoeapp courses", Entries: entries, Extra: map[string]any{"count": len(entries), "list_only": true}}
}

func protectedLiveURL(c *util.Client, sess xeSession, it xeItem) (string, map[string]any) {
	appID := strings.ToLower(firstNonEmpty(it.appID, sess.appID))
	userID := firstNonEmpty(it.cUserID, sess.cUserID, sess.appUserID)
	if appID == "" || it.id == "" {
		return "", nil
	}
	domain := h5Domain(appID)
	ref := fmt.Sprintf("https://%s/v3/course/alive/%s?app_id=%s&type=2", domain, url.PathEscape(it.id), url.QueryEscape(appID))
	root := h5JSONAPI(c, sess, domain, "/_alive/v3/get_lookback_list", map[string]string{
		"client":     "6",
		"protection": "1",
		"alive_id":   it.id,
		"app_id":     appID,
	}, appID, userID, ref)
	if code(root) != "0" {
		return "", nil
	}
	bestURL := ""
	bestText := ""
	bestCount := -1
	bestExt := map[string]any(nil)
	for _, cand := range extractXiaoePrivateCandidates(root["data"]) {
		u := appendXiaoeURLParams(cand.url, [][2]string{{"time", fmt.Sprintf("%d", time.Now().UnixMilli())}, {"uuid", userID}})
		dataURL, text := prepareXiaoePrivateM3U8WithExt(c, sess, appID, userID, u, ref, cand.ext)
		if dataURL == "" {
			if bestURL == "" {
				bestURL, bestExt = u, cand.ext
			}
			continue
		}
		count := m3u8SegmentCount(text)
		if count > bestCount {
			bestURL, bestText, bestCount, bestExt = dataURL, text, count, cand.ext
		}
	}
	if bestURL == "" {
		return "", nil
	}
	extra := map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": appID, "api": "/_alive/v3/get_lookback_list", "private_decoded": true}
	if bestText != "" {
		extra["source_type"] = "m3u8_text"
		extra["m3u8_text"] = bestText
	}
	if bestExt != nil {
		extra["private_ext"] = bestExt
	}
	return bestURL, extra
}

func resolvePrivateXiaoeMedia(c *util.Client, sess xeSession, it xeItem, data any, api string) (string, map[string]any) {
	appID := strings.ToLower(firstNonEmpty(it.appID, sess.appID))
	userID := firstNonEmpty(it.cUserID, sess.cUserID, sess.appUserID)
	ref := "https://www.xiaoeknow.com/"
	if appID != "" {
		ref = "https://" + h5Domain(appID) + "/"
	}
	for _, cand := range extractXiaoePrivateCandidates(data) {
		u := cand.url
		if isLiveType(it.typ) {
			u = appendXiaoeURLParams(u, [][2]string{{"time", fmt.Sprintf("%d", time.Now().UnixMilli())}, {"uuid", userID}})
		}
		extra := map[string]any{"resource_id": it.id, "resource_type": it.typ, "app_id": appID, "api": api, "private_decoded": true}
		if dataURL, text := prepareXiaoePrivateM3U8WithExt(c, sess, appID, userID, u, ref, cand.ext); dataURL != "" {
			extra["source_type"] = "m3u8_text"
			extra["m3u8_text"] = text
			if cand.ext != nil {
				extra["private_ext"] = cand.ext
			}
			return dataURL, extra
		}
		if u != "" {
			return u, extra
		}
	}
	return "", nil
}

func extractXiaoePrivateCandidates(data any) []xiaoePrivateCandidate {
	var out []xiaoePrivateCandidate
	seen := map[string]bool{}
	for _, m := range mapsUnder(data) {
		for _, k := range []string{"aliveVideoUrlEncrypt", "private_m3u8", "aliveVideoUrl", "alive_video_url", "aliveVideoMp4Url", "miniAliveVideoUrl", "aliveReviewUrl", "video_m3u8_url", "video_url", "video_audio_url", "url", "m3u8_url"} {
			raw := val(m, k)
			if raw == "" {
				continue
			}
			u := normalizeURL(decryptXiaoePrivateURL(raw))
			if u == "" || !isXiaoePlayableURL(u) || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, xiaoePrivateCandidate{url: u, ext: xiaoePrivateExt(m)})
		}
	}
	return out
}

func xiaoePrivateExt(m map[string]any) map[string]any {
	for _, key := range []string{"private_info", "privateInfo", "ext", "ext_info", "extInfo"} {
		switch v := m[key].(type) {
		case map[string]any:
			if sub, ok := v["ext"].(map[string]any); ok {
				return sub
			}
			return v
		case string:
			var decoded map[string]any
			if json.Unmarshal([]byte(v), &decoded) == nil {
				if sub, ok := decoded["ext"].(map[string]any); ok {
					return sub
				}
				return decoded
			}
		}
	}
	if val(m, "host") != "" || val(m, "path") != "" || val(m, "param") != "" {
		return m
	}
	return nil
}

func prepareXiaoePrivateM3U8WithExt(c *util.Client, sess xeSession, appID, userID, m3u8URL, ref string, extInfo map[string]any) (string, string) {
	m3u8URL = normalizeURL(m3u8URL)
	if m3u8URL == "" || !strings.Contains(strings.ToLower(m3u8URL), ".m3u8") {
		return "", ""
	}
	h := h5Headers(c, sess, appID, userID, ref)
	body, err := c.GetString(m3u8URL, h)
	if err != nil || !strings.Contains(body, "#EXTM3U") {
		return "", ""
	}
	text := rewriteXiaoePrivateM3U8WithExt(c, sess, body, m3u8URL, h, appID, userID, extInfo)
	if text == "" {
		return "", ""
	}
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text)), text
}

func rewriteXiaoePrivateM3U8WithExt(c *util.Client, sess xeSession, text, sourceURL string, h map[string]string, appID, userID string, extInfo map[string]any) string {
	if !strings.Contains(text, "#EXTM3U") {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines)*2)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, `URI="`) {
				line = xiaoeM3U8URIRe.ReplaceAllStringFunc(line, func(match string) string {
					m := xiaoeM3U8URIRe.FindStringSubmatch(match)
					if len(m) < 2 {
						return match
					}
					keyURL := resolveXiaoeAgainst(m[1], sourceURL)
					if strings.Contains(keyURL, "distribute.vod.pri.get/1.0.0") {
						fetchURL := appendXiaoeURLParams(keyURL, [][2]string{{"uid", userID}})
						if key := fetchXiaoePrivateLookbackKey(c, h, fetchURL, userID); len(key) > 0 {
							return `URI="data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(key) + `"`
						}
						keyURL = fetchURL
					} else if mid := queryValue(keyURL, "mid"); mid != "" {
						if key := getRealAESKey(c, sess, mid, appID); len(key) == 16 {
							return `URI="data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(key) + `"`
						}
					}
					return `URI="` + keyURL + `"`
				})
			}
			out = append(out, line)
			continue
		}
		segmentURL, br := buildXiaoePrivateSegmentURL(trimmed, extInfo)
		if segmentURL == "" {
			segmentURL = resolveXiaoeAgainst(trimmed, sourceURL)
		}
		if br != nil {
			out = append(out, fmt.Sprintf("#EXT-X-BYTERANGE:%d@%d", br.end-br.start+1, br.start))
		}
		out = append(out, resolveXiaoeAgainst(segmentURL, sourceURL))
	}
	return strings.Join(out, "\n")
}

func buildXiaoePrivateSegmentURL(segmentLine string, extInfo map[string]any) (string, *xiaoeByteRange) {
	segmentLine = strings.TrimSpace(segmentLine)
	if segmentLine == "" || strings.HasPrefix(segmentLine, "#") || extInfo == nil {
		return segmentLine, nil
	}
	host := strings.TrimRight(strings.TrimSpace(val(extInfo, "host")), "/")
	pathPart := strings.Trim(strings.TrimSpace(val(extInfo, "path")), "/")
	if host == "" || pathPart == "" {
		return segmentLine, nil
	}
	segmentURL := segmentLine
	if !strings.HasPrefix(strings.ToLower(segmentURL), "http") {
		segmentURL = host + "/" + pathPart + "/" + strings.TrimLeft(segmentURL, "/")
	}
	var br *xiaoeByteRange
	if parsed, err := url.Parse(segmentURL); err == nil {
		q := parsed.Query()
		start, startOK := parseXiaoeInt64(q.Get("start"))
		end, endOK := parseXiaoeInt64(q.Get("end"))
		q.Del("start")
		q.Del("end")
		if strings.EqualFold(q.Get("type"), "mpegts") {
			q.Del("type")
		}
		if startOK && endOK && end >= start {
			br = &xiaoeByteRange{start: start, end: end}
		}
		parsed.RawQuery = q.Encode()
		segmentURL = parsed.String()
	}
	param := strings.TrimLeft(strings.TrimSpace(val(extInfo, "param")), "?&")
	if param != "" {
		if parsed, err := url.Parse(segmentURL); err == nil && !parsed.Query().Has("sign") {
			if strings.Contains(segmentURL, "?") {
				segmentURL += "&" + param
			} else {
				segmentURL += "?" + param
			}
		}
	}
	return segmentURL, br
}

func fetchXiaoePrivateLookbackKey(c *util.Client, h map[string]string, rawURL, userID string) []byte {
	keyBytes, err := c.GetBytes(rawURL, h)
	if err != nil || len(keyBytes) == 0 {
		return nil
	}
	key := decryptXiaoeLookbackKey(keyBytes, userID)
	switch len(key) {
	case 16, 24, 32:
		return key
	default:
		return nil
	}
}

func decryptXiaoeLookbackKey(keyBytes []byte, userID string) []byte {
	if userID == "" || len(keyBytes) == 0 {
		return keyBytes
	}
	uid := []byte(userID)
	n := len(keyBytes)
	if len(uid) < n {
		n = len(uid)
	}
	result := make([]byte, n)
	for i := 0; i < n; i++ {
		result[i] = keyBytes[i] ^ uid[i]
	}
	switch len(result) {
	case 16, 24, 32:
		return result
	default:
		if len(result) > 16 {
			return result[:16]
		}
		return result
	}
}

func getRealAESKey(c *util.Client, sess xeSession, materialID, appID string) []byte {
	if materialID == "" || appID == "" {
		return nil
	}
	root, err := postAppAPI(c, sess, privateKeyAPI, map[string]any{"app_id": appID, "material_id": materialID})
	if err != nil || code(root) != "0" {
		return nil
	}
	enc := val(root["data"], "key")
	if enc == "" {
		return nil
	}
	cipherText, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil
	}
	key := []byte(util.MD5("xiaoeapp2021")[:16])
	plain, err := util.AESDecryptECB(cipherText, key)
	if err == nil && len(plain) >= 16 {
		return plain[:16]
	}
	if len(cipherText)%16 == 0 {
		if raw, err := aesECBNoUnpad(cipherText, key); err == nil && len(raw) >= 16 {
			pad := int(raw[len(raw)-1])
			if pad > 0 && pad <= 16 && len(raw)-pad >= 16 {
				return raw[:16]
			}
			return raw[:16]
		}
	}
	return nil
}

func aesECBNoUnpad(cipherText, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(cipherText)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("ciphertext is not a multiple of block size")
	}
	out := make([]byte, len(cipherText))
	for i := 0; i < len(cipherText); i += block.BlockSize() {
		block.Decrypt(out[i:i+block.BlockSize()], cipherText[i:i+block.BlockSize()])
	}
	return out, nil
}

func h5JSONAPI(c *util.Client, sess xeSession, domain, path string, params map[string]string, appID, cUserID, ref string) map[string]any {
	u := url.URL{Scheme: "https", Host: domain, Path: path}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	body, err := c.GetString(u.String(), h5Headers(c, sess, appID, cUserID, ref))
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil
	}
	return root
}

func postH5JSONAPI(c *util.Client, sess xeSession, domain, path string, body map[string]any, appID, cUserID string) map[string]any {
	api := "https://" + domain + path
	payload, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	h := h5Headers(c, sess, appID, cUserID, "https://"+domain+"/")
	h["Content-Type"] = "application/json"
	resp, err := c.Post(api, strings.NewReader(string(payload)), h)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(buf.Bytes(), &root) != nil {
		return nil
	}
	return root
}

func postH5API(c *util.Client, sess xeSession, domain, path string, form url.Values, appID, cUserID string) map[string]any {
	api := "https://" + domain + path
	h := h5Headers(c, sess, appID, cUserID, "https://"+domain+"/")
	h["Content-Type"] = "application/x-www-form-urlencoded"
	resp, err := c.Post(api, strings.NewReader(form.Encode()), h)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(buf.Bytes(), &root) != nil {
		return nil
	}
	return root
}

func h5Headers(c *util.Client, sess xeSession, appID, cUserID, ref string) map[string]string {
	token := ""
	if c != nil {
		token = h5Token(c, sess, appID, cUserID)
	}
	h := map[string]string{
		"Accept-Language":  "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
		"X-Requested-With": "com.xiaoe.client",
		"User-Agent":       h5UA,
		"Referer":          firstNonEmpty(ref, "https://"+h5Domain(appID)+"/"),
		"Origin":           "https://" + h5Domain(appID),
		"Accept":           "application/json, text/plain, */*",
	}
	if appID != "" {
		h["Cookie"] = fmt.Sprintf("app_id=%s; ko_token=%s", appID, token)
	}
	return h
}

func resolveFileListURL(c *util.Client, sess xeSession, it xeItem, resourceType string) string {
	appID := strings.ToLower(firstNonEmpty(it.appID, sess.appID))
	if appID == "" || it.id == "" {
		return ""
	}
	form := url.Values{}
	form.Set("bizData[resource_type]", resourceType)
	form.Set("bizData[resource_id]", it.id)
	root := postH5API(c, sess, h5Domain(appID), fileListAPI, form, appID, firstNonEmpty(it.cUserID, sess.cUserID))
	if code(root) != "0" {
		return ""
	}
	for _, m := range mapsUnder(root["data"]) {
		if u := pickURL(m); u != "" {
			return u
		}
	}
	return ""
}

func resolveEbookURL(c *util.Client, sess xeSession, it xeItem) string {
	appID := strings.ToLower(firstNonEmpty(it.appID, sess.appID))
	if appID == "" || it.id == "" {
		return ""
	}
	form := url.Values{}
	form.Set("bizData[resource_id]", it.id)
	root := postH5API(c, sess, h5Domain(appID), ebookInfoAPI, form, appID, firstNonEmpty(it.cUserID, sess.cUserID))
	if code(root) != "0" {
		return ""
	}
	return pickURL(root["data"])
}

func firstURLFromEncodedFields(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"video_urls", "play_urls", "urls"} {
			if u := firstURLInString(val(m, k)); u != "" {
				return u
			}
		}
	}
	return ""
}

func firstURLInString(raw string) string {
	raw = normalizeURL(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if json.Unmarshal([]byte(raw), &decoded) == nil {
		if u := bestURLFromDecodedVideoList(decoded); u != "" {
			return u
		}
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) > 0 {
		if json.Unmarshal(b, &decoded) == nil {
			if u := bestURLFromDecodedVideoList(decoded); u != "" {
				return u
			}
		}
		raw = string(b)
	}
	re := regexp.MustCompile(`https?://[^\s"'<>\\]+`)
	for _, m := range re.FindAllString(raw, -1) {
		if u := normalizeURL(m); isUsableXiaoeURL(u) {
			return u
		}
	}
	return ""
}

func bestURLFromDecodedVideoList(v any) string {
	type cand struct {
		url   string
		score int
	}
	cands := []cand{}
	for _, m := range mapsUnder(v) {
		u := directXiaoeURL(m)
		if u == "" {
			continue
		}
		def := strings.ToUpper(firstNonEmpty(val(m, "definition_p"), val(m, "definition"), val(m, "quality")))
		score := map[string]int{"360P": 1, "480P": 2, "720P": 3, "1080P": 4, "2K": 5, "4K": 6}[def]
		cands = append(cands, cand{url: u, score: score})
	}
	if len(cands) == 0 {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				if s, ok := item.(string); ok {
					if u := firstURLInString(s); u != "" {
						cands = append(cands, cand{url: u})
					}
				}
			}
		case string:
			return firstURLInString(x)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if len(cands) > 0 {
		return cands[0].url
	}
	return ""
}

func directXiaoeURL(m map[string]any) string {
	for _, k := range []string{"video_m3u8_url", "video_hls", "video_url", "videoAudioUrl", "video_audio_url", "audio_m3u8_url", "audio_url", "epub_url", "aliveVideoUrl", "alive_video_url", "aliveVideoMp4Url", "miniAliveVideoUrl", "aliveReviewUrl", "play_url", "playUrl", "url", "m3u8_url", "file_url", "download_url", "downloadUrl", "material_url", "doc_url", "document_url", "courseware_url"} {
		if u := normalizeURL(val(m, k)); isUsableXiaoeURL(u) {
			return normalizeURL(u)
		}
	}
	return ""
}

func textHTMLDataURL(data any) string {
	for _, m := range mapsUnder(data) {
		for _, k := range []string{"org_content", "content", "html", "text"} {
			s := val(m, k)
			if s != "" && strings.Contains(s, "<") {
				return "data:text/html;charset=utf-8," + url.PathEscape(s)
			}
		}
	}
	return ""
}

func enrichXiaoeExtra(extra map[string]any, it xeItem) map[string]any {
	if extra == nil {
		extra = map[string]any{}
	}
	if price := xiaoePrice(it.raw); price != nil {
		extra["price"] = price
	}
	if purchased := xiaoePurchased(it.raw); purchased != nil {
		extra["purchased"] = purchased
	}
	return compactXiaoeMap(extra)
}

func xiaoePrice(m map[string]any) any {
	for _, k := range []string{"price", "sale_price", "salePrice", "current_price", "currentPrice", "real_price", "realPrice", "fee"} {
		raw := firstVal(m, k)
		if raw == "" {
			continue
		}
		if f, ok := xiaoeParsePrice(raw); ok {
			return f
		}
	}
	return nil
}

func xiaoeParsePrice(raw string) (float64, bool) {
	raw = strings.ReplaceAll(raw, ",", "")
	m := regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`).FindString(raw)
	if m == "" {
		return 0, false
	}
	var f float64
	if _, err := fmt.Sscanf(m, "%f", &f); err != nil {
		return 0, false
	}
	if f >= 1000 && !strings.Contains(m, ".") {
		f = f / 100
	}
	return f, true
}

func xiaoePurchased(m map[string]any) any {
	for _, k := range []string{"is_paid", "isPaid", "is_buy", "isBuy", "is_purchased", "isPurchased", "purchased", "buy_status", "subscribe_status"} {
		raw := strings.ToLower(firstVal(m, k))
		switch raw {
		case "1", "true", "yes", "paid", "purchased":
			return true
		case "0", "false", "no", "unpaid":
			return false
		}
	}
	return nil
}

func compactXiaoeMap(in map[string]any) map[string]any {
	for k, v := range in {
		switch x := v.(type) {
		case nil:
			delete(in, k)
		case string:
			if strings.TrimSpace(x) == "" {
				delete(in, k)
			}
		}
	}
	return in
}

func h5Domain(appID string) string {
	if appID == "" {
		return "h5.xiaoeknow.com"
	}
	return appID + ".h5.xiaoeknow.com"
}

func m3u8SegmentCount(text string) int {
	return strings.Count(strings.ToUpper(text), "#EXTINF")
}

func resolveXiaoeAgainst(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return b.ResolveReference(ref).String()
}

func parseXiaoeInt64(s string) (int64, bool) {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

func queryValue(rawURL, key string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}
