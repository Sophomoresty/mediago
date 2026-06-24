package douyin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`douyin\.com/video/\d+`,
	`v\.douyin\.com/\w+`,
	`iesdouyin\.com/share/video/\d+`,
}

const (
	uaIOS         = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1"
	uaApp         = "com.ss.android.ugc.aweme/310101 (Linux; U; Android 13; zh_CN; Pixel 6; Build/TP1A.221005.002)"
	twidRegister  = "https://ttwid.bytedance.com/ttwid/union/register/"
	twidBody      = `{"region":"cn","aid":1768,"needFid":false,"service":"www.ixigua.com","migrate_info":{"ticket":"","source":"node"},"cbUrlProtocol":"https","union":true}`
	playHost      = "https://aweme.snssdk.com/aweme/v1"
	shareTemplate = "https://www.iesdouyin.com/share/video/%s/"
)

var (
	idRe    = regexp.MustCompile(`(?:video|note|modal_id=)/?(\d{16,21})`)
	idFall  = regexp.MustCompile(`(\d{16,21})`)
	shortRe = regexp.MustCompile(`^https?://v\.douyin\.com/`)
)

func init() {
	extractor.Register(&Douyin{}, extractor.SiteInfo{
		Name: "Douyin",
		URL:  "douyin.com",
	})
}

type Douyin struct{}

func (d *Douyin) Patterns() []string { return patterns }

func (d *Douyin) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	ttwid := getTTWID()
	item, err := resolve(url, ttwid)
	if err != nil {
		return nil, err
	}

	desc, _ := item["desc"].(string)
	author := ""
	if a, ok := item["author"].(map[string]interface{}); ok {
		author, _ = a["nickname"].(string)
	}

	videoObj, ok := item["video"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no video object in item")
	}

	playAddr, _ := videoObj["play_addr"].(map[string]interface{})
	if playAddr == nil {
		return nil, fmt.Errorf("no play_addr in video")
	}

	videoID := ""
	if uri, ok := playAddr["uri"].(string); ok {
		videoID = uri
	}
	if videoID == "" {
		return nil, fmt.Errorf("empty video_id (uri)")
	}

	streams := buildStreams(videoID)
	if len(streams) == 0 {
		return nil, fmt.Errorf("no playable streams found")
	}

	return &extractor.MediaInfo{
		Site:    "douyin",
		Title:   desc,
		Artist:  author,
		Streams: streams,
	}, nil
}

func getTTWID() string {
	req, err := http.NewRequest("POST", twidRegister, strings.NewReader(twidBody))
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", uaIOS)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
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

func resolve(rawURL string, ttwid string) (map[string]interface{}, error) {
	awemeID := extractID(rawURL)

	if shortRe.MatchString(rawURL) {
		req, err := http.NewRequest("GET", rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("User-Agent", uaIOS)
		if ttwid != "" {
			req.AddCookie(&http.Cookie{Name: "ttwid", Value: ttwid})
		}
		resp, err := http.DefaultClient.Do(req)
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
	req, err := http.NewRequest("GET", shareURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create share request: %w", err)
	}
	req.Header.Set("User-Agent", uaIOS)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if ttwid != "" {
		req.AddCookie(&http.Cookie{Name: "ttwid", Value: ttwid})
	}

	resp, err := http.DefaultClient.Do(req)
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

func buildStreams(videoID string) map[string]extractor.Stream {
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

	client := util.NewClient()

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
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Range", "bytes=0-1")

	resp, err := http.DefaultClient.Do(req)
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

func extractID(text string) string {
	if m := idRe.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	if m := idFall.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}
