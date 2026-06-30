package xiaoetech

import (
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

func fetchCourseList(c *util.Client, jar http.CookieJar) ([]xetItem, error) {
	out := []xetItem{}
	seen := map[string]bool{}
	for _, spec := range []struct {
		tpl   string
		extra string
	}{{courseListURL, ""}, {courseListURL, "&resource_type=12"}, {courseListURL, "&resource_type=51"}, {quanziListURL, ""}} {
		for page := 1; page <= 32; page++ {
			body, err := c.GetString(fmt.Sprintf(spec.tpl, page)+spec.extra, headers(jar, refererURL))
			if err != nil {
				if len(out) > 0 {
					return out, nil
				}
				return nil, err
			}
			var root map[string]any
			if err := json.Unmarshal([]byte(body), &root); err != nil {
				return nil, fmt.Errorf("xiaoetech parse course list: %w", err)
			}
			list := listUnder(root["data"], "list")
			if len(list) == 0 {
				break
			}
			for _, m := range list {
				it := itemFromMap(m)
				if it.id == "" || seen[it.id] || val(m, "is_available") == "0" {
					continue
				}
				seen[it.id] = true
				out = append(out, it)
			}
			if len(list) < pageSize {
				break
			}
		}
	}
	if body, err := c.GetString(fmt.Sprintf(livingLiveListURL, pageSize, url.QueryEscape("1-0-0")), headers(jar, refererURL)); err == nil {
		var root map[string]any
		if json.Unmarshal([]byte(body), &root) == nil {
			for _, m := range listUnder(root["data"], "list") {
				it := itemFromMap(m)
				it.typ = "live"
				if it.id != "" && !seen[it.id] {
					seen[it.id] = true
					out = append(out, it)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xiaoetech attend list is empty")
	}
	return out, nil
}

func resolveItem(c *util.Client, jar http.CookieJar, ctx xetCtx, it xetItem) (string, map[string]any) {
	typ := normType(firstNonEmpty(ctx.typ, it.typ))
	baseExtra := map[string]any{"resource_id": it.id, "resource_type": typ, "app_id": ctx.appID}
	switch typ {
	case "live":
		if u, extra := liveMediaURL(c, jar, ctx, it.id); u != "" {
			for k, v := range extra {
				baseExtra[k] = v
			}
			return u, baseExtra
		}
		return "", map[string]any{"blocked_reason": "blocked: no playable lookback URL found for live resource", "resource_id": it.id, "resource_type": "live", "app_id": ctx.appID}
	case "audio":
		if u := firstMediaURL(it.raw); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "audio", "api": "source"}
		}
		if u := postDetailURL(c, jar, ctx, it.id, audioURL, pcAudioURL, map[string]string{"bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "audio", "api": audioURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: no audio URL returned by audio endpoint", "resource_id": it.id, "resource_type": "audio"}
	case "text":
		if u := firstMediaURL(it.raw); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "text", "api": "source"}
		}
		if u := postDetailURL(c, jar, ctx, it.id, textURL, pcTextURL, map[string]string{"bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "text", "api": textURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: no text HTML or media URL returned by text endpoint", "resource_id": it.id, "resource_type": "text"}
	case "book":
		if u := postDetailURL(c, jar, ctx, it.id, ebookURL, pcEbookURL, map[string]string{"bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "book", "api": ebookURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: no ebook URL returned by ebook endpoint", "resource_id": it.id, "resource_type": "book"}
	case "clock":
		return "", map[string]any{"blocked_reason": "blocked: clock resource has no playable media in source APIs", "resource_id": it.id, "resource_type": "clock"}
	case "document", "file":
		if typ == "document" {
			if u := documentDetailURL(c, jar, ctx, it); u != "" {
				return u, map[string]any{"resource_id": it.id, "resource_type": typ, "api": documentInfoURL}
			}
		}
		if u := firstMediaURL(it.raw); u != "" {
			baseExtra["api"] = "source"
			return u, baseExtra
		}
		fileResType := firstNonEmpty(val(it.raw, "resource_type"), resourceTypeNumber(typ, ""), normType(typ))
		if u := postDetailURL(c, jar, ctx, it.id, fileURL, pcFileURL, map[string]string{"bizData[resource_type]": fileResType, "bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": typ, "api": fileURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: no file URL returned by document/courseware endpoints", "resource_id": it.id, "resource_type": typ}
	case "column", "bigcolumn", "member", "ecourse", "train":
		// Try column items endpoint first; for member type also try member-specific endpoint.
		if u := postDetailURL(c, jar, ctx, it.id, infoURL, pcInfoURL, map[string]string{"bizData[column_id]": it.id, "bizData[page_size]": "100", "bizData[page_index]": "1", "bizData[sort]": "desc"}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": typ, "api": infoURL}
		}
		if typ == "member" {
			if u := postDetailURL(c, jar, ctx, it.id, memberInfoURL, pcMemberInfoURL, map[string]string{"bizData[column_id]": it.id, "bizData[page_size]": "100", "bizData[page_index]": "1", "bizData[sort]": "desc"}); u != "" {
				return u, map[string]any{"resource_id": it.id, "resource_type": typ, "api": memberInfoURL}
			}
		}
		return "", map[string]any{"blocked_reason": "blocked: no child item or direct URL returned by container endpoints", "resource_id": it.id, "resource_type": typ}
	}
	if u := firstMediaURL(it.raw); u != "" {
		// Try to decode __ba-obfuscated URLs instead of blocking.
		if strings.Contains(strings.ToLower(u), "__ba") {
			decoded := decryptLookbackPrivateURL(u)
			if decoded != "" && isMediaURL(normalizeURL(decoded)) {
				baseExtra["api"] = "source"
				baseExtra["private_decoded"] = true
				decoded = appendLookbackAccessParams(normalizeURL(decoded), ctx.userID)
				if dataURL, text := preparePrivateLookbackM3U8(c, headers(jar, referer(ctx)), ctx.userID, decoded, referer(ctx)); dataURL != "" {
					baseExtra["source_type"] = "m3u8_text"
					baseExtra["m3u8_text"] = text
					return dataURL, baseExtra
				}
				return decoded, baseExtra
			}
			return "", map[string]any{"blocked_reason": "blocked: failed to decode private lookback URL", "resource_id": it.id, "resource_type": typ}
		}
		baseExtra["api"] = "source"
		if sourceType := val(it.raw, "_source_type"); sourceType != "" {
			baseExtra["source_type"] = sourceType
		}
		return u, baseExtra
	}
	u, extra := videoMediaURL(c, jar, ctx, it.id)
	if u == "" {
		return "", map[string]any{"blocked_reason": "blocked: no playable URL returned by source APIs", "resource_id": it.id, "resource_type": typ}
	}
	for k, v := range extra {
		baseExtra[k] = v
	}
	return u, baseExtra
}

func videoMediaURL(c *util.Client, jar http.CookieJar, ctx xetCtx, vid string) (string, map[string]any) {
	h := headers(jar, referer(ctx))
	api := fmt.Sprintf(sourceURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
	form := map[string]string{"bizData[opr_sys]": "Win32", "bizData[product_id]": firstNonEmpty(ctx.cid, vid), "bizData[resource_id]": vid}
	if ctx.pc && ctx.domain != "" {
		api = fmt.Sprintf(pcSourceURL, ctx.domain)
		form = map[string]string{"opr_sys": "Win32", "product_id": firstNonEmpty(ctx.cid, vid), "resource_id": vid}
	}
	body, err := c.PostForm(api, form, h)
	if err != nil {
		return "", nil
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return "", nil
	}
	if u := firstMediaURL(root["data"]); u != "" {
		return u, map[string]any{"api": api}
	}
	for _, cand := range extractPrivateLookbackCandidates(root["data"]) {
		u := appendLookbackAccessParams(cand.url, ctx.userID)
		extra := map[string]any{"api": api, "private_decoded": true}
		if dataURL, text := preparePrivateLookbackM3U8WithExt(c, h, ctx.userID, u, referer(ctx), cand.ext); dataURL != "" {
			extra["source_type"] = "m3u8_text"
			extra["m3u8_text"] = text
			return dataURL, extra
		}
		return u, extra
	}
	if u := decodeXETVideoURLField(root["data"]); u != "" {
		return u, map[string]any{"api": api, "decoded_video_urls": true}
	}
	if s := deepText(root, "video_urls", "play_urls", "url"); s != "" {
		if u := firstURLInString(s); u != "" {
			return u, map[string]any{"api": api}
		}
	}
	if ps := deepText(root, "play_sign", "playSign"); ps != "" && ctx.appID != "" {
		if u := postDetailURL(c, jar, ctx, vid, videoPlayURL, "", map[string]string{"opr_sys": "Win32", "play_sign": ps}); u != "" {
			return u, map[string]any{"api": videoPlayURL}
		}
	}
	return "", nil
}

// decryptLookbackPrivateURL decodes a xiaoetech __ba-obfuscated lookback URL.
//
// The algorithm (from Xiaoetech_Course._decrypt_lookback_private_url):
//  1. If the URL already looks like a normal http(s) m3u8 URL, return as-is.
//  2. Strip the "__ba" marker.
//  3. Apply character substitution: @ -> 1, # -> 2, $ -> 3, % -> 4.
//  4. Convert from URL-safe base64: - -> +, _ -> /.
//  5. Strip any non-base64 characters.
//  6. Pad with = for alignment.
//  7. base64-decode to recover the original URL.
func decryptLookbackPrivateURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Already a normal URL with m3u8 — nothing to decrypt.
	if strings.HasPrefix(raw, "http") && strings.Contains(raw, ".m3u8") {
		return raw
	}
	// No __ba marker — not an obfuscated URL.
	if !strings.Contains(raw, "__ba") {
		return raw
	}

	s := strings.ReplaceAll(raw, "__ba", "")

	// Character substitution.
	r := strings.NewReplacer("@", "1", "#", "2", "$", "3", "%", "4")
	s = r.Replace(s)

	// URL-safe base64 -> standard base64.
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")

	// Strip non-base64 characters.
	s = regexp.MustCompile(`[^A-Za-z0-9+/]`).ReplaceAllString(s, "")

	// Pad.
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}

	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(decoded)
}

// decryptLookbackKey XORs the raw key bytes from the distribute.vod.pri.get
// endpoint with the user ID string to derive the actual AES key.
//
// From Xiaoetech_Config.get_xiaoetech_key_func: each byte of key_bytes is
// XORed with the corresponding byte of the user ID via Python zip(), i.e. it
// stops at the shorter input. If the result is not 16, 24, or 32 bytes, it is
// truncated to 16.
func decryptLookbackKey(keyBytes []byte, userID string) []byte {
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

// extractPrivateLookbackURLs extracts all media URLs from a protected live
// lookback API response, decoding any __ba-obfuscated URLs.
func extractPrivateLookbackURLs(data any) []string {
	candidates := extractPrivateLookbackCandidates(data)
	urls := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		urls = append(urls, cand.url)
	}
	return urls
}

type privateLookbackCandidate struct {
	url string
	ext map[string]any
}

func extractPrivateLookbackCandidates(data any) []privateLookbackCandidate {
	var out []privateLookbackCandidate
	seen := map[string]bool{}
	for _, m := range mapsUnder(data) {
		for _, k := range []string{"url", "aliveVideoUrl", "alive_video_url", "aliveVideoMp4Url",
			"aliveVideoUrlEncrypt", "miniAliveVideoUrl", "aliveReviewUrl", "m3u8_url", "video_url"} {
			raw := val(m, k)
			if raw == "" {
				continue
			}
			u := decryptLookbackPrivateURL(raw)
			u = normalizeURL(u)
			if u == "" || !isMediaURL(u) || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, privateLookbackCandidate{url: u, ext: privateExtInfo(m)})
		}
		// Also check nested list structures (lookback_list, etc.)
		for _, listKey := range []string{"lookback_list", "lookbackList", "video_list",
			"videoList", "record_list", "recordList", "list", "items"} {
			if sub, ok := m[listKey]; ok {
				for _, cand := range extractPrivateLookbackCandidates(sub) {
					if !seen[cand.url] {
						seen[cand.url] = true
						out = append(out, cand)
					}
				}
			}
		}
	}
	return out
}

func privateExtInfo(m map[string]any) map[string]any {
	for _, k := range []string{"ext", "ext_info", "extInfo"} {
		switch v := m[k].(type) {
		case map[string]any:
			return v
		case string:
			var decoded map[string]any
			if json.Unmarshal([]byte(v), &decoded) == nil {
				return decoded
			}
		}
	}
	if val(m, "host") != "" || val(m, "path") != "" || val(m, "param") != "" {
		return m
	}
	return nil
}

var xetM3U8URIRe = regexp.MustCompile(`URI="([^"]+)"`)

func appendLookbackAccessParams(raw, userID string) string {
	return appendXETParams(raw, [][2]string{
		{"time", fmt.Sprintf("%d", time.Now().UnixMilli())},
		{"uuid", userID},
	})
}

func appendXETParams(raw string, params [][2]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for _, kv := range params {
		if kv[0] == "" || kv[1] == "" || q.Has(kv[0]) {
			continue
		}
		q.Set(kv[0], kv[1])
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func preparePrivateLookbackM3U8(c *util.Client, h map[string]string, userID, m3u8URL, ref string) (string, string) {
	return preparePrivateLookbackM3U8WithExt(c, h, userID, m3u8URL, ref, nil)
}

func preparePrivateLookbackM3U8WithExt(c *util.Client, h map[string]string, userID, m3u8URL, ref string, extInfo map[string]any) (string, string) {
	m3u8URL = normalizeURL(m3u8URL)
	if m3u8URL == "" || !strings.Contains(strings.ToLower(m3u8URL), ".m3u8") {
		return "", ""
	}
	reqHeaders := cloneHeaderMap(h)
	if ref != "" {
		reqHeaders["Referer"] = ref
		reqHeaders["referer"] = ref
	}
	body, err := c.GetString(m3u8URL, reqHeaders)
	if err != nil || !strings.Contains(body, "#EXTM3U") {
		return "", ""
	}
	rewritten := rewritePrivateLookbackM3U8WithExt(c, body, m3u8URL, reqHeaders, userID, extInfo)
	if rewritten == "" {
		return "", ""
	}
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(rewritten)), rewritten
}

func rewritePrivateLookbackM3U8(c *util.Client, text, sourceURL string, h map[string]string, userID string) string {
	return rewritePrivateLookbackM3U8WithExt(c, text, sourceURL, h, userID, nil)
}

func rewritePrivateLookbackM3U8WithExt(c *util.Client, text, sourceURL string, h map[string]string, userID string, extInfo map[string]any) string {
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
				line = xetM3U8URIRe.ReplaceAllStringFunc(line, func(match string) string {
					m := xetM3U8URIRe.FindStringSubmatch(match)
					if len(m) < 2 {
						return match
					}
					keyURL := resolveXETAgainst(m[1], sourceURL)
					if strings.Contains(keyURL, "distribute.vod.pri.get/1.0.0") {
						fetchURL := appendXETParams(keyURL, [][2]string{{"uid", userID}})
						if key := fetchPrivateLookbackKey(c, h, fetchURL, userID); len(key) > 0 {
							return `URI="data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(key) + `"`
						}
						keyURL = fetchURL
					}
					return `URI="` + keyURL + `"`
				})
			}
			out = append(out, line)
			continue
		}
		segmentURL, br := buildPrivateLookbackSegmentURL(trimmed, extInfo)
		if segmentURL == "" {
			segmentURL = resolveXETAgainst(trimmed, sourceURL)
		}
		if br != nil {
			out = append(out, fmt.Sprintf("#EXT-X-BYTERANGE:%d@%d", br.end-br.start+1, br.start))
		}
		out = append(out, resolveXETAgainst(segmentURL, sourceURL))
	}
	return strings.Join(out, "\n")
}

type lookbackByteRange struct {
	start int64
	end   int64
}

func buildPrivateLookbackSegmentURL(segmentLine string, extInfo map[string]any) (string, *lookbackByteRange) {
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
	var br *lookbackByteRange
	if parsed, err := url.Parse(segmentURL); err == nil {
		q := parsed.Query()
		start, startOK := parseInt64Param(q.Get("start"))
		end, endOK := parseInt64Param(q.Get("end"))
		q.Del("start")
		q.Del("end")
		if strings.EqualFold(q.Get("type"), "mpegts") {
			q.Del("type")
		}
		if startOK && endOK && end >= start {
			br = &lookbackByteRange{start: start, end: end}
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

func parseInt64Param(s string) (int64, bool) {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

func fetchPrivateLookbackKey(c *util.Client, h map[string]string, rawURL, userID string) []byte {
	keyBytes, err := c.GetBytes(rawURL, h)
	if err != nil || len(keyBytes) == 0 {
		return nil
	}
	key := decryptLookbackKey(keyBytes, userID)
	switch len(key) {
	case 16, 24, 32:
		return key
	default:
		return nil
	}
}

func resolveXETAgainst(raw, base string) string {
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

func cloneHeaderMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func liveMediaURL(c *util.Client, jar http.CookieJar, ctx xetCtx, id string) (string, map[string]any) {
	h := headers(jar, referer(ctx))
	apiURLs := []string{}
	if ctx.appID != "" {
		apiURLs = append(apiURLs,
			fmt.Sprintf(protectedLiveURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault), ctx.appID, id),
			fmt.Sprintf(liveURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault), ctx.appID, id),
		)
	}
	if ctx.pc && ctx.domain != "" {
		apiURLs = append(apiURLs, fmt.Sprintf(pcLiveURL, ctx.domain, ctx.appID))
	}
	for _, api := range apiURLs {
		body, err := c.GetString(api, h)
		if err != nil {
			continue
		}
		var root map[string]any
		if json.Unmarshal([]byte(body), &root) != nil {
			continue
		}
		data := root["data"]

		// First try to get a direct, non-obfuscated media URL.
		if u := firstMediaURL(data); u != "" && !strings.Contains(strings.ToLower(u), "__ba") {
			return u, map[string]any{"api": api}
		}

		// If this response contains private/protected flow data, try to
		// decode any __ba-obfuscated URLs from it.
		for _, cand := range extractPrivateLookbackCandidates(data) {
			u := cand.url
			u = appendLookbackAccessParams(u, ctx.userID)
			liveRef := referer(ctx)
			if ctx.appID != "" {
				liveRef = fmt.Sprintf("https://%s%s/v3/course/alive/%s?app_id=%s&type=2", ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault), id, ctx.appID)
			}
			if dataURL, text := preparePrivateLookbackM3U8WithExt(c, h, ctx.userID, u, liveRef, cand.ext); dataURL != "" {
				return dataURL, map[string]any{"api": api, "private_decoded": true, "source_type": "m3u8_text", "m3u8_text": text}
			}
			return u, map[string]any{"api": api, "private_decoded": true}
		}
	}
	return "", nil
}

func postDetailURL(c *util.Client, jar http.CookieJar, ctx xetCtx, id, h5Tpl, pcTpl string, form map[string]string) string {
	api := ""
	actualForm := form
	if ctx.pc && pcTpl != "" && ctx.domain != "" {
		api = fmt.Sprintf(pcTpl, ctx.domain)
		// PC endpoints use plain keys instead of bizData[] wrapped keys.
		actualForm = unwrapBizData(form)
	} else if ctx.appID != "" {
		api = fmt.Sprintf(h5Tpl, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
	}
	if api == "" {
		return ""
	}
	body, err := c.PostForm(api, actualForm, headers(jar, referer(ctx)))
	if err != nil {
		return ""
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return ""
	}
	if u := firstMediaURL(root["data"]); u != "" {
		return u
	}
	return firstMediaURLInString(body)
}

func documentDetailURL(c *util.Client, jar http.CookieJar, ctx xetCtx, it xetItem) string {
	if it.id == "" {
		return ""
	}
	productID := firstNonEmpty(ctx.productID, val(it.raw, "product_id"), val(it.raw, "pro_id"), val(it.raw, "course_id"))
	var (
		body string
		err  error
	)
	if ctx.pc && ctx.domain != "" {
		api := fmt.Sprintf(pcDocumentInfoURL, ctx.domain)
		body, err = c.PostForm(api, map[string]string{"opr_sys": "Win32", "resource_id": it.id}, headers(jar, referer(ctx)))
	} else if ctx.appID != "" {
		api := fmt.Sprintf(documentInfoURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
		bizData := map[string]any{"resource_id": it.id}
		if productID != "" {
			bizData["product_id"] = productID
		}
		payload, marshalErr := json.Marshal(map[string]any{"bizData": bizData})
		if marshalErr != nil {
			return ""
		}
		resp, postErr := c.Post(api, strings.NewReader(string(payload)), jsonHeaders(jar, referer(ctx)))
		if postErr != nil {
			return ""
		}
		defer resp.Body.Close()
		buf, readErr := io.ReadAll(resp.Body)
		if readErr != nil || resp.StatusCode >= 400 {
			return ""
		}
		body = string(buf)
	} else {
		return ""
	}
	if err != nil || body == "" {
		return ""
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return ""
	}
	if u := firstMediaURL(root["data"]); u != "" {
		return u
	}
	return firstMediaURLInString(body)
}

func postJSONDetail(c *util.Client, jar http.CookieJar, ctx xetCtx, h5Tpl string, data map[string]any) map[string]any {
	if ctx.appID == "" || h5Tpl == "" {
		return nil
	}
	api := fmt.Sprintf(h5Tpl, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
	payload := map[string]any{"bizData": data}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	resp, err := c.Post(api, strings.NewReader(string(b)), jsonHeaders(jar, referer(ctx)))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var root map[string]any
	if json.NewDecoder(resp.Body).Decode(&root) != nil {
		return nil
	}
	return root
}

// unwrapBizData converts h5-style "bizData[key]" form keys to plain "key" for PC endpoints.
func unwrapBizData(form map[string]string) map[string]string {
	out := make(map[string]string, len(form))
	for k, v := range form {
		if strings.HasPrefix(k, "bizData[") && strings.HasSuffix(k, "]") {
			plain := k[len("bizData[") : len(k)-1]
			out[plain] = v
		} else {
			out[k] = v
		}
	}
	return out
}

func itemFromMap(m map[string]any) xetItem {
	u := firstNonEmpty(val(m, "h5_url"), val(m, "url"), val(m, "live_share_url"))
	p := parseCtx(u)
	typ := normType(firstNonEmpty(val(m, "resource_type"), val(m, "course_type"), p.typ))
	return xetItem{id: firstNonEmpty(val(m, "resource_id"), val(m, "cid"), val(m, "id"), p.cid), title: firstNonEmpty(val(m, "title"), val(m, "resource_title"), val(m, "name")), typ: typ, appID: firstNonEmpty(val(m, "app_id"), p.appID), userID: firstNonEmpty(val(m, "user_id"), p.userID), pageURL: u, raw: m}
}

func headers(jar http.CookieJar, ref string) map[string]string {
	ref = firstNonEmpty(ref, refererURL)
	h := map[string]string{"Accept": "application/json, text/plain, */*", "Referer": ref, "Origin": originOf(ref), "X-Requested-With": "XMLHttpRequest"}
	if ck := cookieHeader(jar, ref); ck != "" {
		h["Cookie"] = ck
	}
	return h
}
func jsonHeaders(jar http.CookieJar, ref string) map[string]string {
	h := headers(jar, ref)
	h["Content-Type"] = "application/json"
	return h
}
func cookieHeader(jar http.CookieJar, refs ...string) string {
	parts := []string{}
	raws := append([]string{}, refs...)
	raws = append(raws, refererURL, "https://study.xiaoe-tech.com", "https://www.xiaoeknow.com")
	seen := map[string]bool{}
	for _, raw := range raws {
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func originOf(ref string) string {
	if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return refererURL
}
func referer(c xetCtx) string {
	if c.referer != "" {
		return c.referer
	}
	if c.pc && c.domain != "" {
		return "https://" + c.domain
	}
	if c.appID != "" {
		return fmt.Sprintf("https://%s%s", c.appID, firstNonEmpty(c.xetDomain, xetDomainDefault))
	}
	return refererURL
}
func normType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if v := map[string]string{"1": "text", "2": "audio", "3": "video", "4": "live", "5": "member", "6": "column", "7": "column", "8": "bigcolumn", "10": "live", "12": "live", "16": "clock", "20": "book", "25": "train", "50": "ecourse", "51": "document", "64": "ecourse", "alive": "live", "ebook": "book"}[t]; v != "" {
		return v
	}
	return t
}
func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"video_m3u8_url", "video_hls", "video_url", "audio_m3u8_url", "audio_url", "video_audio_url", "aliveVideoUrl", "alive_video_url", "aliveVideoMp4Url", "miniAliveVideoUrl", "aliveReviewUrl", "epub_url", "file_url", "download_url", "downloadUrl", "doc_url", "document_url", "courseware_url", "url", "m3u8_url", "play_url", "PlayURL"} {
			if u := normalizeURL(val(m, k)); isMediaURL(u) {
				return u
			}
		}
	}
	return ""
}
func firstURLInString(s string) string {
	if m := httpRe.FindString(s); m != "" {
		return normalizeURL(m)
	}
	return ""
}
func firstMediaURLInString(s string) string {
	for _, raw := range httpRe.FindAllString(s, -1) {
		if u := normalizeURL(raw); isMediaURL(u) {
			return u
		}
	}
	return ""
}
func isMediaURL(u string) bool {
	if strings.HasPrefix(u, "data:text/html") {
		return true
	}
	return (strings.HasPrefix(u, "http") || strings.HasPrefix(u, "//")) && !isXETPageURL(u) && !regexp.MustCompile(`(?i)\.(?:jpg|jpeg|png|gif|webp)(?:[?#]|$)`).MatchString(u)
}
func isXETPageURL(raw string) bool {
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
func media(title, u string, extra map[string]any, ref ...string) *extractor.MediaInfo {
	if title == "" {
		title = "xiaoetech_video"
	}
	stream := extractor.Stream{Quality: "source", URLs: []string{u}, Format: formatOf(u), Headers: map[string]string{"Referer": firstNonEmpty(append(ref, refererURL)...)}}
	if strings.Contains(strings.ToLower(stream.Format), "m3u8") {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "xiaoetech", Title: title, Streams: map[string]extractor.Stream{"default": stream}, Extra: extra}
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
func deepText(v any, keys ...string) string {
	for _, m := range mapsUnder(v) {
		for _, k := range keys {
			if s := val(m, k); s != "" {
				return s
			}
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
func firstRegex(pat, s string) string {
	m := regexp.MustCompile(pat).FindStringSubmatch(s)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
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
	if strings.HasPrefix(l, "data:text/html") {
		return "html"
	}
	if strings.HasPrefix(l, "data:application/vnd.apple.mpegurl") {
		return "m3u8"
	}
	if strings.Contains(l, ".m3u8") {
		return "m3u8"
	}
	pathPart := l
	if parsed, err := url.Parse(l); err == nil && parsed.Path != "" {
		pathPart = parsed.Path
	}
	if i := strings.LastIndex(pathPart, "."); i >= 0 && i < len(pathPart)-1 {
		ext := pathPart[i+1:]
		if regexp.MustCompile(`^[a-z0-9]{1,8}$`).MatchString(ext) {
			return ext
		}
	}
	return "mp4"
}
