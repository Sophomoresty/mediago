package douyin

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`douyin\.com/video/\d+`,
	`v\.douyin\.com/\w+`,
	`iesdouyin\.com/share/video/\d+`,
	`douyin\.com/user/[^/?#]+`,
}

const (
	uaIOS         = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1"
	uaApp         = "com.ss.android.ugc.aweme/310101 (Linux; U; Android 13; zh_CN; Pixel 6; Build/TP1A.221005.002)"
	uaMobileWeb   = "Mozilla/5.0 (Linux; Android 8.0.0; SM-G955U Build/R16NW) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36"
	twidRegister  = "https://ttwid.bytedance.com/ttwid/union/register/"
	twidBody      = `{"region":"cn","aid":1768,"needFid":false,"service":"www.ixigua.com","migrate_info":{"ticket":"","source":"node"},"cbUrlProtocol":"https","union":true}`
	playHost      = "https://aweme.snssdk.com/aweme/v1"
	shareTemplate = "https://www.iesdouyin.com/share/video/%s/"
	userPostAPI   = "https://www.douyin.com/aweme/v1/web/aweme/post/"
	homeReferer   = "https://www.douyin.com/?is_from_mobile_home=1&recommend=1"
	pageSize      = 100
	maxUserPages  = 99
)

var (
	idRe    = regexp.MustCompile(`(?:video|note|modal_id=)/?(\d{16,21})`)
	idFall  = regexp.MustCompile(`(\d{16,21})`)
	shortRe = regexp.MustCompile(`^https?://v\.douyin\.com/`)
	userRe  = regexp.MustCompile(`(?i)douyin\.com/user/([^/?#]+)`)
)

func init() {
	extractor.Register(&Douyin{}, extractor.SiteInfo{
		Name: "Douyin",
		URL:  "douyin.com",
	})
}

type Douyin struct{}

func (d *Douyin) Patterns() []string { return patterns }

func (d *Douyin) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	client := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		client.SetCookieJar(opts.Cookies)
	}

	ttwid := getTTWID(client)
	if secUID := extractSecUID(rawURL); secUID != "" {
		return d.extractUser(rawURL, secUID, client, ttwid)
	}

	item, err := resolve(rawURL, client, ttwid)
	if err != nil {
		return nil, err
	}

	return mediaInfoFromItem(client, item, 0, "")
}

func getTTWID(client *util.Client) string {
	if client == nil {
		client = util.NewClient()
	}
	resp, err := client.Post(twidRegister, strings.NewReader(twidBody), map[string]string{
		"User-Agent":   uaIOS,
		"Content-Type": "application/json",
	})
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		return ""
	}

	for _, c := range resp.Cookies() {
		if c.Name == "ttwid" {
			return c.Value
		}
	}
	return ""
}

func resolve(rawURL string, client *util.Client, ttwid string) (map[string]interface{}, error) {
	awemeID := extractID(rawURL)

	if shortRe.MatchString(rawURL) {
		resp, err := client.Get(rawURL, douyinHeaders(uaIOS, "", ttwid))
		if err != nil {
			return nil, fmt.Errorf("failed to follow short URL: %w", err)
		}
		defer resp.Body.Close()
		finalURL := resp.Request.URL.String()
		if id := extractID(finalURL); id != "" {
			awemeID = id
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read short URL response: %w", err)
		}
		return parseRouterData(string(body), awemeID)
	}

	if awemeID == "" {
		return nil, fmt.Errorf("cannot extract video ID from: %s", rawURL)
	}

	shareURL := fmt.Sprintf(shareTemplate, awemeID)
	resp, err := client.Get(shareURL, douyinHeaders(uaIOS, "", ttwid))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch share page: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read share page: %w", err)
	}

	return parseRouterData(string(body), awemeID)
}

func parseRouterData(html, awemeID string) (map[string]interface{}, error) {
	anchor := strings.Index(html, "window._ROUTER_DATA")
	if anchor < 0 {
		return nil, fmt.Errorf("share page has no _ROUTER_DATA (anti-bot or unavailable)")
	}

	eqIdx := strings.Index(html[anchor:], "=")
	if eqIdx < 0 {
		return nil, fmt.Errorf("malformed _ROUTER_DATA")
	}
	start := anchor + eqIdx + 1
	for start < len(html) && html[start] != '{' {
		start++
	}
	if start >= len(html) {
		return nil, fmt.Errorf("no JSON object in _ROUTER_DATA")
	}

	depth := 0
	inStr := false
	esc := false
	end := start
	for i := start; i < len(html); i++ {
		ch := html[i]
		if inStr {
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				inStr = false
			}
		} else {
			switch ch {
			case '"':
				inStr = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i + 1
					goto done
				}
			}
		}
	}
	return nil, fmt.Errorf("unterminated _ROUTER_DATA")

done:
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(html[start:end]), &data); err != nil {
		return nil, fmt.Errorf("failed to parse _ROUTER_DATA: %w", err)
	}

	item := findVideoItem(data)
	if item == nil {
		return nil, fmt.Errorf("no playable item found")
	}
	if _, ok := item["aweme_id"]; !ok {
		item["aweme_id"] = awemeID
	}
	return item, nil
}

func findVideoItem(node interface{}) map[string]interface{} {
	switch v := node.(type) {
	case map[string]interface{}:
		if video, ok := v["video"].(map[string]interface{}); ok {
			if _, ok := video["play_addr"].(map[string]interface{}); ok {
				return v
			}
		}
		for _, val := range v {
			if hit := findVideoItem(val); hit != nil {
				return hit
			}
		}
	case []interface{}:
		for _, val := range v {
			if hit := findVideoItem(val); hit != nil {
				return hit
			}
		}
	}
	return nil
}

func (d *Douyin) extractUser(rawURL, secUID string, client *util.Client, ttwid string) (*extractor.MediaInfo, error) {
	items, author, err := fetchUserItems(client, secUID, ttwid)
	if err != nil {
		return nil, err
	}

	entries := make([]*extractor.MediaInfo, 0, len(items))
	for i, item := range items {
		entry, err := mediaInfoFromItem(client, item, i+1, secUID)
		if err != nil {
			continue
		}
		if entry.Artist == "" {
			entry.Artist = author
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no playable videos found for Douyin user: %s", rawURL)
	}

	title := author
	if title == "" {
		title = userFallbackTitle(secUID)
	}
	return &extractor.MediaInfo{
		Site:    "douyin",
		Title:   title,
		Artist:  author,
		Entries: entries,
		Extra: map[string]any{
			"sec_user_id": secUID,
			"count":       len(entries),
		},
	}, nil
}

func fetchUserItems(client *util.Client, secUID, ttwid string) ([]map[string]interface{}, string, error) {
	var items []map[string]interface{}
	seenIDs := make(map[string]bool)
	seenCursors := make(map[int64]bool)
	author := ""
	cursor := int64(0)
	headers := douyinHeaders(uaMobileWeb, homeReferer, ttwid)

	for page := 0; page < maxUserPages; page++ {
		if seenCursors[cursor] {
			break
		}
		seenCursors[cursor] = true

		body, err := client.GetString(userVideosURL(secUID, cursor), headers)
		if err != nil {
			if len(items) > 0 {
				break
			}
			return nil, "", fmt.Errorf("failed to fetch Douyin user videos: %w", err)
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(body), &data); err != nil {
			return nil, "", fmt.Errorf("failed to parse Douyin user videos response: %w", err)
		}
		if status, ok := asInt64(data["status_code"]); ok && status != 0 {
			return nil, "", fmt.Errorf("Douyin user videos API status_code=%d", status)
		}

		list, _ := data["aweme_list"].([]interface{})
		if len(list) == 0 {
			break
		}

		for _, raw := range list {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			awemeID := stringValue(item["aweme_id"])
			if awemeID != "" {
				if seenIDs[awemeID] {
					continue
				}
				seenIDs[awemeID] = true
			}
			if author == "" {
				author = authorName(item)
			}
			items = append(items, item)
		}

		hasMore := boolValue(data["has_more"])
		nextCursor, hasCursor := asInt64(data["max_cursor"])
		if !hasMore || !hasCursor || nextCursor == cursor {
			break
		}
		cursor = nextCursor
	}

	if len(items) == 0 {
		return nil, author, fmt.Errorf("no Douyin user videos returned for sec_user_id=%s", secUID)
	}
	if author == "" {
		author = userFallbackTitle(secUID)
	}
	return items, author, nil
}

func mediaInfoFromItem(client *util.Client, item map[string]interface{}, index int, secUID string) (*extractor.MediaInfo, error) {
	desc := stringValue(item["desc"])
	awemeID := stringValue(item["aweme_id"])
	author := authorName(item)

	videoID := videoIDFromItem(item)
	if videoID == "" {
		return nil, fmt.Errorf("empty video_id (uri)")
	}

	streams := buildStreams(client, videoID)
	if len(streams) == 0 {
		return nil, fmt.Errorf("no playable streams found")
	}

	title := desc
	if title == "" && awemeID != "" {
		title = "douyin_" + awemeID
	}
	if title == "" {
		title = "douyin_video"
	}
	if index > 0 {
		title = fmt.Sprintf("[%03d]--%s", index, title)
	}

	extra := map[string]any{}
	if awemeID != "" {
		extra["aweme_id"] = awemeID
	}
	if secUID != "" {
		extra["sec_user_id"] = secUID
	}
	if createTime := item["create_time"]; createTime != nil {
		extra["create_time"] = createTime
	}

	return &extractor.MediaInfo{
		Site:    "douyin",
		Title:   title,
		Artist:  author,
		Streams: streams,
		Extra:   extra,
	}, nil
}

func buildStreams(client *util.Client, videoID string) map[string]extractor.Stream {
	ladder := []struct {
		ratio   string
		quality string
		ua      string
		aid     string
	}{
		{"default", "original", uaApp, "1128"},
		{"1080p", "1080p", uaIOS, ""},
		{"720p", "720p", uaIOS, ""},
		{"540p", "540p", uaIOS, ""},
		{"360p", "360p", uaIOS, ""},
	}

	streams := make(map[string]extractor.Stream)
	seen := make(map[int64]bool)

	if client == nil {
		client = util.NewClient()
	}

	for _, l := range ladder {
		playURL := fmt.Sprintf("%s/play/?video_id=%s&ratio=%s&line=0", playHost, videoID, l.ratio)
		if l.aid != "" {
			playURL += "&a=" + l.aid
		}

		headers := map[string]string{"User-Agent": l.ua}
		if l.aid == "" {
			headers["Referer"] = "https://www.douyin.com/"
		}

		size := probeSize(client, playURL, headers)
		if size <= 0 || seen[size] {
			continue
		}
		seen[size] = true

		streams[l.quality] = extractor.Stream{
			Quality: l.quality,
			URLs:    []string{playURL},
			Format:  "mp4",
			Size:    size,
			Headers: headers,
		}
	}
	return streams
}

func probeSize(client *util.Client, url string, headers map[string]string) int64 {
	h := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		h[k] = v
	}
	h["Range"] = "bytes=0-1"
	resp, err := client.Get(url, h)
	if err != nil {
		return 0
	}
	resp.Body.Close()

	if cr := resp.Header.Get("Content-Range"); cr != "" {
		parts := strings.Split(cr, "/")
		if len(parts) == 2 {
			var size int64
			if _, err := fmt.Sscanf(parts[1], "%d", &size); err == nil && size > 0 {
				return size
			}
		}
	}
	return resp.ContentLength
}

func userVideosURL(secUID string, cursor int64) string {
	q := url.Values{}
	q.Set("aid", "6383")
	q.Set("sec_user_id", secUID)
	q.Set("max_cursor", strconv.FormatInt(cursor, 10))
	q.Set("count", strconv.Itoa(pageSize))
	return userPostAPI + "?" + q.Encode()
}

func douyinHeaders(userAgent, referer, ttwid string) map[string]string {
	headers := map[string]string{
		"User-Agent":      userAgent,
		"Accept-Language": "zh-CN,zh;q=0.9",
	}
	if referer != "" {
		headers["Referer"] = referer
	}
	if ttwid != "" {
		headers["Cookie"] = "ttwid=" + ttwid
	}
	return headers
}

func extractSecUID(text string) string {
	if m := userRe.FindStringSubmatch(text); len(m) > 1 {
		if v, err := url.QueryUnescape(m[1]); err == nil {
			return v
		}
		return m[1]
	}
	return ""
}

func videoIDFromItem(item map[string]interface{}) string {
	videoObj, ok := item["video"].(map[string]interface{})
	if !ok {
		return ""
	}
	playAddr, _ := videoObj["play_addr"].(map[string]interface{})
	if playAddr == nil {
		return ""
	}
	return stringValue(playAddr["uri"])
}

func authorName(item map[string]interface{}) string {
	if a, ok := item["author"].(map[string]interface{}); ok {
		if nick := stringValue(a["nickname"]); nick != "" {
			return nick
		}
		if uniqueID := stringValue(a["unique_id"]); uniqueID != "" {
			return uniqueID
		}
	}
	return ""
}

func userFallbackTitle(secUID string) string {
	if secUID == "" {
		return "douyin_user"
	}
	sum := fmt.Sprintf("%x", md5.Sum([]byte(secUID)))
	return "douyin_user_" + sum[:4]
}

func stringValue(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case float32:
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	default:
		return ""
	}
}

func boolValue(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	default:
		return false
	}
}

func asInt64(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int64:
		return t, true
	case float64:
		return int64(t), true
	case json.Number:
		i, err := t.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

func extractID(text string) string {
	if m := idRe.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	if m := idFall.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}
