// Package youdao implements an extractor for ydshengxue.com courses.
package youdao

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL        = "https://ydshengxue.com"
	checkURL          = "https://ke.ydshengxue.com/api/user_status.jsonp"
	orderGaokaoURL    = "https://ai.ydshengxue.com/ai-gw-sale/api/app/v2/order/my-orders"
	orderZhongkaoURL  = "https://ec-server-c.ydlingshi.com/ai-gw-sale/api/app/v2/order/my-orders"
	courseGaokaoURL   = "https://ai.ydshengxue.com/ai-product/api/app/v1/products/after-sale"
	courseZhongkaoURL = "https://ec-server-c.ydlingshi.com/ai-product/api/app/v1/products/after-sale"
	infoGaokaoURL     = "https://ai.ydshengxue.com/ai-product/api/app/v2/products/after-sale/%s"
	infoZhongkaoURL   = "https://ec-server-c.ydlingshi.com/ai-product/api/app/v2/products/after-sale/%s"
	keyURL            = "https://live.ydshengxue.com/hikari-live/api/consumer/v1/key"
)

var (
	patterns       = []string{`(?:[\w-]+\.)?(?:ydshengxue|ydlingshi)\.com/`}
	cidRe          = regexp.MustCompile(`(?i)(?:productId|courseId|course_id|cid|id)=([A-Za-z0-9_-]+)|/after-sale/([A-Za-z0-9_-]+)|/(\d{4,})`)
	loginSuccessRe = regexp.MustCompile(`"success"\s*:\s*true`)
	titleCleanRe   = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
	keyURIRe       = regexp.MustCompile(`URI="([^"]+)"`)
)

func init() {
	extractor.Register(&Youdao{}, extractor.SiteInfo{Name: "Youdao", URL: "ydshengxue.com", NeedAuth: true})
}

type Youdao struct{}

func (s *Youdao) Patterns() []string { return patterns }

type ydContext struct {
	c          *util.Client
	cid        string
	courseType string
	headers    map[string]string
}

type ydVideo struct {
	Title, URL, ID, CardID, LiveID string
}

func (s *Youdao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("youdao requires login cookies")
	}
	cid := parseCID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("youdao: cannot parse product/course id from URL")
	}
	ctx := &ydContext{c: util.NewClient(), cid: cid, courseType: parseCourseType(rawURL)}
	ctx.c.SetCookieJar(opts.Cookies)
	ctx.headers = headersFromJar(opts.Cookies)
	if err := ctx.checkCookie(); err != nil {
		return nil, err
	}
	info, err := ctx.loadInfo()
	if err != nil {
		return nil, err
	}
	title := firstNonEmpty(firstString(asMap(info["data"]), "title", "name"), firstString(info, "title", "name"), "youdao_"+cid)
	videos := collectVideos(info)
	if len(videos) == 0 {
		return nil, fmt.Errorf("youdao: no videos found in after-sale payload")
	}
	entries := make([]*extractor.MediaInfo, 0, len(videos))
	seen := map[string]bool{}
	for _, v := range videos {
		if v.URL == "" || seen[v.URL] {
			continue
		}
		seen[v.URL] = true
		extra := map[string]any{"video_id": v.ID, "card_id": v.CardID, "live_id": v.LiveID}
		mediaURL := v.URL
		format := pickFormat(mediaURL)
		if manifest, err := ctx.rewriteM3U8IfNeeded(v); err == nil && manifest != "" {
			mediaURL = youdaoM3U8DataURL(manifest)
			format = "m3u8"
			extra["source_type"] = "m3u8_text"
			extra["m3u8_text"] = manifest
			extra["m3u8_url"] = v.URL
		}
		entries = append(entries, &extractor.MediaInfo{Site: "youdao", Title: cleanTitle(v.Title), Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{mediaURL}, Format: format, NeedMerge: format == "m3u8", Headers: map[string]string{"Referer": refererURL}}}, Extra: extra})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("youdao: no playable URLs resolved")
	}
	return &extractor.MediaInfo{Site: "youdao", Title: cleanTitle(title), Entries: entries}, nil
}

func parseCID(raw string) string {
	m := cidRe.FindStringSubmatch(raw)
	if len(m) == 0 {
		return ""
	}
	for _, s := range m[1:] {
		if s != "" {
			return s
		}
	}
	return ""
}

func parseCourseType(raw string) string {
	if strings.Contains(strings.ToLower(raw), "zhongkao") || strings.Contains(raw, "ydlingshi") {
		return "zhongkao"
	}
	return "gaokao"
}

func headersFromJar(jar http.CookieJar) map[string]string {
	h := map[string]string{"Referer": refererURL, "referer": refererURL, "Accept": "application/json, text/plain, */*"}
	var parts []string
	for _, raw := range []string{refererURL, "https://ke.ydshengxue.com/", "https://ai.ydshengxue.com/", "https://ec-server-c.ydlingshi.com/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	if len(parts) > 0 {
		h["cookie"] = strings.Join(parts, "; ")
	}
	return h
}

func (x *ydContext) checkCookie() error {
	body, err := x.c.GetString(checkURL, x.headers)
	if err != nil {
		return fmt.Errorf("youdao user status: %w", err)
	}
	if !loginSuccessRe.MatchString(body) {
		return fmt.Errorf("youdao user status failed")
	}
	return nil
}

func (x *ydContext) loadInfo() (map[string]any, error) {
	urls := []string{fmt.Sprintf(infoGaokaoURL, url.PathEscape(x.cid)), fmt.Sprintf(infoZhongkaoURL, url.PathEscape(x.cid))}
	if x.courseType == "zhongkao" {
		urls[0], urls[1] = urls[1], urls[0]
	}
	for _, apiURL := range urls {
		body, err := x.c.GetString(apiURL, x.headers)
		if err != nil {
			continue
		}
		var resp map[string]any
		if json.Unmarshal([]byte(body), &resp) == nil && len(resp) > 0 {
			return resp, nil
		}
	}
	return nil, fmt.Errorf("youdao after-sale info empty")
}

func collectVideos(root map[string]any) []ydVideo {
	var out []ydVideo
	var walk func(any, []string)
	walk = func(v any, path []string) {
		switch t := v.(type) {
		case []any:
			for i, it := range t {
				walk(it, append(path, fmt.Sprint(i+1)))
			}
		case map[string]any:
			name := cleanTitle(firstString(t, "title", "name"))
			if name != "" {
				path = append(path, name)
			}
			if u := firstString(t, "downloadUrl", "url", "playUrl", "videoUrl"); strings.HasPrefix(u, "http") {
				out = append(out, ydVideo{Title: firstNonEmpty(strings.Join(path, "--"), firstString(t, "title", "name")), URL: u, ID: firstString(t, "id", "videoId", "video_id"), CardID: firstString(t, "cardPackageId", "card_id"), LiveID: firstString(t, "liveCenterId", "live_id")})
			}
			for _, k := range []string{"videoPackageTab", "questionPackage", "skillPackage", "servicePackage", "videoLiveTab", "subOutlines", "outlines", "videos", "videoList", "clarityInfoList", "data"} {
				walk(t[k], path)
			}
		}
	}
	walk(root, nil)
	return out
}

func (x *ydContext) rewriteM3U8IfNeeded(v ydVideo) (string, error) {
	if !strings.Contains(strings.ToLower(v.URL), ".m3u8") {
		return "", nil
	}
	manifest, err := x.c.GetString(v.URL, x.headers)
	if err != nil {
		return "", err
	}
	if !strings.Contains(manifest, "#EXTM3U") {
		return "", nil
	}
	if !strings.Contains(manifest, "#EXT-X-KEY") {
		return absolutizeYoudaoM3U8(manifest, v.URL), nil
	}
	m := keyURIRe.FindStringSubmatch(manifest)
	if len(m) < 2 {
		return absolutizeYoudaoM3U8(manifest, v.URL), nil
	}
	keyBody, err := x.fetchKey(v, m[1])
	if err != nil || len(keyBody) == 0 {
		return absolutizeYoudaoM3U8(manifest, v.URL), err
	}
	manifest = strings.ReplaceAll(manifest, m[1], "0x"+strings.ToUpper(hex.EncodeToString(keyBody)))
	return absolutizeYoudaoM3U8(manifest, v.URL), nil
}

func (x *ydContext) fetchKey(v ydVideo, keyURI string) ([]byte, error) {
	u, _ := url.Parse(keyURL)
	q := u.Query()
	q.Set("url", keyURI)
	q.Set("cardPackageContentId", v.ID)
	q.Set("cardPackageId", v.CardID)
	q.Set("cid", x.cid)
	q.Set("productId", x.cid)
	q.Set("liveId", v.LiveID)
	u.RawQuery = q.Encode()
	return x.c.GetBytes(u.String(), x.headers)
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(fmt.Sprint(m[k])); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }
func pickFormat(u string) string {
	low := strings.ToLower(strings.TrimSpace(u))
	if strings.Contains(low, ".m3u8") || strings.HasPrefix(low, "data:application/vnd.apple.mpegurl") || strings.HasPrefix(low, "data:application/x-mpegurl") || strings.HasPrefix(low, "#extm3u") {
		return "m3u8"
	}
	return "mp4"
}

func youdaoM3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func absolutizeYoudaoM3U8(text, baseURL string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = rewriteYoudaoM3U8URIAttrs(line, baseURL)
			continue
		}
		lines[i] = resolveYoudaoURL(trimmed, baseURL)
	}
	return strings.Join(lines, "\n")
}

func rewriteYoudaoM3U8URIAttrs(line, baseURL string) string {
	return keyURIRe.ReplaceAllStringFunc(line, func(match string) string {
		parts := keyURIRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		uri := strings.TrimSpace(parts[1])
		if uri == "" || strings.HasPrefix(strings.ToLower(uri), "0x") || strings.HasPrefix(strings.ToLower(uri), "data:") {
			return match
		}
		return strings.Replace(match, uri, resolveYoudaoURL(uri, baseURL), 1)
	})
}

func resolveYoudaoURL(raw, baseURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") || strings.HasPrefix(strings.ToLower(raw), "0x") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}
