package xiaoetech

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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
		if u := postDetailURL(c, jar, ctx, it.id, audioURL, pcAudioURL, map[string]string{"bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "audio", "api": audioURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: needs audio endpoint resolution", "resource_id": it.id, "resource_type": "audio"}
	case "text":
		if u := postDetailURL(c, jar, ctx, it.id, textURL, pcTextURL, map[string]string{"bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "text", "api": textURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: needs text endpoint resolution", "resource_id": it.id, "resource_type": "text"}
	case "book":
		if u := postDetailURL(c, jar, ctx, it.id, ebookURL, pcEbookURL, map[string]string{"bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": "book", "api": ebookURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: needs ebook endpoint resolution", "resource_id": it.id, "resource_type": "book"}
	case "document", "file":
		fileResType := firstNonEmpty(val(it.raw, "resource_type"), normType(typ))
		if u := postDetailURL(c, jar, ctx, it.id, fileURL, pcFileURL, map[string]string{"bizData[resource_type]": fileResType, "bizData[resource_id]": it.id}); u != "" {
			return u, map[string]any{"resource_id": it.id, "resource_type": typ, "api": fileURL}
		}
		return "", map[string]any{"blocked_reason": "blocked: needs file endpoint resolution", "resource_id": it.id, "resource_type": typ}
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
		return "", map[string]any{"blocked_reason": "blocked: needs column endpoint resolution", "resource_id": it.id, "resource_type": typ}
	}
	if u := firstMediaURL(it.raw); u != "" {
		// Try to decode __ba-obfuscated URLs instead of blocking.
		if strings.Contains(strings.ToLower(u), "__ba") {
			decoded := decryptLookbackPrivateURL(u)
			if decoded != "" && isMediaURL(normalizeURL(decoded)) {
				baseExtra["api"] = "source"
				baseExtra["private_decoded"] = true
				return normalizeURL(decoded), baseExtra
			}
			return "", map[string]any{"blocked_reason": "blocked: failed to decode private lookback URL", "resource_id": it.id, "resource_type": typ}
		}
		baseExtra["api"] = "source"
		return u, baseExtra
	}
	u, extra := videoMediaURL(c, jar, ctx, it.id)
	if u == "" {
		return "", map[string]any{"blocked_reason": "blocked: needs protected live or video source resolution", "resource_id": it.id, "resource_type": typ}
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
	if s := deepText(root, "video_urls", "play_urls", "url"); s != "" {
		if u := firstURLInString(s); u != "" {
			return u, map[string]any{"api": api}
		}
	}
	if ps := deepText(root, "play_sign", "playSign"); ps != "" && ctx.appID != "" {
		if u := postDetailURL(c, jar, ctx, vid, videoPlayURL, "", map[string]string{"play_sign": ps}); u != "" {
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
// XORed with the corresponding byte of the user ID (cycling). If the result
// is not 16, 24, or 32 bytes, it is truncated to 16.
func decryptLookbackKey(keyBytes []byte, userID string) []byte {
	if userID == "" || len(keyBytes) == 0 {
		return keyBytes
	}
	uid := []byte(userID)
	result := make([]byte, len(keyBytes))
	for i, b := range keyBytes {
		result[i] = b ^ uid[i%len(uid)]
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
	var urls []string
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
			urls = append(urls, u)
		}
		// Also check nested list structures (lookback_list, etc.)
		for _, listKey := range []string{"lookback_list", "lookbackList", "video_list",
			"videoList", "record_list", "recordList", "list", "items"} {
			if sub, ok := m[listKey]; ok {
				for _, u := range extractPrivateLookbackURLs(sub) {
					if !seen[u] {
						seen[u] = true
						urls = append(urls, u)
					}
				}
			}
		}
	}
	return urls
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
		decoded := extractPrivateLookbackURLs(data)
		for _, u := range decoded {
			// Append time and uuid query params as the source does.
			ts := fmt.Sprintf("%d", time.Now().UnixMilli())
			sep := "?"
			if strings.Contains(u, "?") {
				sep = "&"
			}
			u = u + sep + "time=" + url.QueryEscape(ts)
			if ctx.userID != "" {
				u = u + "&uuid=" + url.QueryEscape(ctx.userID)
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
	return firstURLInString(body)
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
	h := map[string]string{"Accept": "application/json, text/plain, */*", "Referer": firstNonEmpty(ref, refererURL), "Origin": refererURL, "X-Requested-With": "XMLHttpRequest"}
	if ck := cookieHeader(jar); ck != "" {
		h["Cookie"] = ck
	}
	return h
}
func cookieHeader(jar http.CookieJar) string {
	parts := []string{}
	for _, raw := range []string{refererURL, "https://study.xiaoe-tech.com", "https://www.xiaoeknow.com"} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func referer(c xetCtx) string {
	if c.referer != "" {
		return c.referer
	}
	if c.pc && c.domain != "" {
		return "https://" + c.domain
	}
	if c.appID != "" {
		return fmt.Sprintf(courseURL, c.appID, firstNonEmpty(c.xetDomain, xetDomainDefault), firstNonEmpty(c.typ, "video"), firstNonEmpty(c.cid, ""))
	}
	return refererURL
}
func normType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if v := map[string]string{"1": "text", "2": "audio", "3": "video", "4": "live", "5": "member", "6": "column", "7": "column", "8": "bigcolumn", "10": "live", "12": "live", "20": "book", "25": "train", "50": "ecourse", "51": "document", "64": "ecourse", "alive": "live", "ebook": "book"}[t]; v != "" {
		return v
	}
	return t
}
func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"video_m3u8_url", "video_hls", "video_url", "audio_m3u8_url", "audio_url", "video_audio_url", "aliveVideoUrl", "alive_video_url", "aliveVideoMp4Url", "miniAliveVideoUrl", "aliveReviewUrl", "epub_url", "file_url", "url", "m3u8_url", "play_url", "PlayURL"} {
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
func isMediaURL(u string) bool {
	return (strings.HasPrefix(u, "http") || strings.HasPrefix(u, "//")) && !regexp.MustCompile(`(?i)\.(?:jpg|jpeg|png|gif|webp)(?:[?#]|$)`).MatchString(u)
}
func media(title, u string, extra map[string]any) *extractor.MediaInfo {
	if title == "" {
		title = "xiaoetech_video"
	}
	stream := extractor.Stream{Quality: "source", URLs: []string{u}, Format: formatOf(u), Headers: map[string]string{"Referer": refererURL}}
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
	if strings.Contains(l, ".m3u8") {
		return "m3u8"
	}
	if strings.Contains(l, ".mp3") {
		return "mp3"
	}
	if strings.Contains(l, ".epub") {
		return "epub"
	}
	if strings.Contains(l, ".pdf") {
		return "pdf"
	}
	return "mp4"
}
