// Package imooc implements an extractor for imooc.com / class.imooc.com / coding.imooc.com.
//
// API chain ported from decompiled Mooc/Courses/Imooc/Imooc_Class.pyc and Imooc_Code.pyc:
//
//  1. POST {host}/course/startlearn      (class) or
//     POST {host}/lesson/ajaxstartlearn  (coding) → heartbeat / open learn session
//  2. GET  /course/playlist/{mid}?t=m3u8&_id={cid}&cdn=aliyun1
//     or
//     GET  /lesson/m3u8h5?mid={mid}&cid={cid}&ssl=1&cdn=aliyun1
//     → JSON envelope with imooc_decode-encoded m3u8 manifest
//  3. POST {host}/course/endlearn / ajaxendlearn → close learn session
//
// The imooc_decode algorithm is a concrete crypto transform (XOR + swap +
// padding removal) implemented natively in Go — no JS runtime required.
// Both free and paid content are supported.
package imooc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`(?:[\w-]+\.)*imooc\.com/`,
}

func init() {
	extractor.Register(&Imooc{}, extractor.SiteInfo{
		Name:     "imooc",
		URL:      "imooc.com",
		NeedAuth: true,
	})
}

type Imooc struct{}

func (i *Imooc) Patterns() []string { return patterns }

func (i *Imooc) Extract(rawURL string, opts *extractor.ExtractOpts) (info *extractor.MediaInfo, err error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("imooc requires login cookies (use --cookies or --cookies-from-browser)")
	}

	cid, mid, host := parseURL(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse imooc URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": host + "/"}

	// startlearn heartbeat — class.imooc.com uses /course/startlearn, coding.imooc.com
	// uses /lesson/ajaxstartlearn. Free imooc.com has no lifecycle endpoint.
	startURL, endURL, hasLifecycle := lifecycleURLs(host)
	if hasLifecycle {
		if _, err := c.PostForm(startURL, map[string]string{"mid": mid, "cid": cid, "_id": cid}, h); err != nil {
			return nil, fmt.Errorf("imooc startlearn: %w", err)
		}
		defer func() {
			if _, endErr := c.PostForm(endURL, map[string]string{"mid": mid, "cid": cid, "_id": cid}, h); endErr != nil && err == nil {
				err = fmt.Errorf("imooc endlearn: %w", endErr)
			}
		}()
	}

	// Fetch the m3u8 manifest. The response is either:
	// (a) a JSON envelope with imooc_decode-encoded data in data.info or result, or
	// (b) a raw #EXTM3U manifest (rare, free content).
	apiURL := mediaURL(host, mid, cid)
	body, err := c.GetString(apiURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch m3u8 manifest: %w", err)
	}

	// Try raw m3u8 first.
	if isM3U8(body) {
		return buildResult(cid, body, host, nil), nil
	}

	// Try JSON with plain "result" field (some free content).
	var freeEnv struct {
		Result string `json:"result"`
		Mpath  string `json:"mpath"`
	}
	if json.Unmarshal([]byte(body), &freeEnv) == nil && freeEnv.Result != "" {
		if isM3U8(freeEnv.Result) {
			return buildResult(cid, freeEnv.Result, host, nil), nil
		}
	}

	// Try JSON with data.info field (paid content, requires imooc_decode).
	masterPlaylist, err := decryptJSONInfo(body)
	if err != nil {
		return nil, fmt.Errorf("imooc decode master playlist: %w", err)
	}

	// The master playlist contains variant stream URLs. Pick the best one
	// (highest bandwidth, last entry) and fetch+decode the actual playlist.
	videoURL := selectBestVariant(masterPlaylist, host)
	if videoURL == "" {
		// The master playlist IS the actual playlist (single quality).
		return buildResult(cid, masterPlaylist, host, nil), nil
	}

	// Fetch the variant playlist.
	variantBody, err := c.GetString(videoURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch variant playlist: %w", err)
	}

	var playlist string
	if isM3U8(variantBody) {
		playlist = variantBody
	} else {
		playlist, err = decryptJSONInfo(variantBody)
		if err != nil {
			return nil, fmt.Errorf("imooc decode variant playlist: %w", err)
		}
	}

	// Resolve HLS encryption keys (the key URI responses are also
	// imooc_decode encoded in JSON envelopes).
	hlsKeys := resolveHLSKeys(playlist, videoURL, c, h)

	return buildResult(cid, playlist, host, hlsKeys), nil
}

// decryptJSONInfo parses a JSON response and decrypts the data.info field.
func decryptJSONInfo(body string) (string, error) {
	// Try data.info structure.
	var env struct {
		Data struct {
			Info string `json:"info"`
		} `json:"data"`
		Result int    `json:"result"`
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(body), &env); err == nil && env.Data.Info != "" {
		decoded, err := decryptInfo(env.Data.Info)
		if err != nil {
			return "", fmt.Errorf("decrypt data.info: %w", err)
		}
		return string(decoded), nil
	}

	// Try "info" field at top level (older API format).
	var topInfo struct {
		Info string `json:"info"`
	}
	if err := json.Unmarshal([]byte(body), &topInfo); err == nil && topInfo.Info != "" {
		decoded, err := decryptInfo(topInfo.Info)
		if err != nil {
			return "", fmt.Errorf("decrypt info: %w", err)
		}
		return string(decoded), nil
	}

	// Try regex extraction as fallback.
	m := regexp.MustCompile(`"info"\s*:\s*"(.*?)"`).FindStringSubmatch(body)
	if len(m) > 1 && m[1] != "" {
		decoded, err := decryptInfo(m[1])
		if err != nil {
			return "", fmt.Errorf("decrypt info (regex): %w", err)
		}
		return string(decoded), nil
	}

	return "", fmt.Errorf("no imooc_decode-encoded info field found in response")
}

// selectBestVariant extracts the best (last, typically highest bandwidth)
// variant stream URL from an HLS master playlist.
func selectBestVariant(master string, host string) string {
	pattern := regexp.MustCompile(`(https?://[^\s]+)`)
	matches := pattern.FindAllString(master, -1)

	// Filter to variant URLs matching the host domain.
	var variants []string
	hostDomain := extractDomain(host)
	for _, u := range matches {
		if strings.Contains(u, hostDomain) && strings.Contains(u, "/video/") {
			variants = append(variants, u)
		}
	}
	if len(variants) == 0 {
		// Try any URL that looks like an m3u8.
		for _, u := range matches {
			if strings.HasSuffix(u, ".m3u8") || strings.Contains(u, ".m3u8?") {
				variants = append(variants, u)
			}
		}
	}
	if len(variants) == 0 {
		return ""
	}
	return variants[len(variants)-1] // last = highest bandwidth typically
}

// resolveHLSKeys fetches and decrypts all HLS encryption keys referenced
// in the playlist. Returns a map of key-URI -> base64-encoded key bytes.
func resolveHLSKeys(playlist, playlistURL string, c *util.Client, headers map[string]string) map[string]string {
	keyURIs := extractKeyURIs(playlist)
	if len(keyURIs) == 0 {
		return nil
	}

	keys := make(map[string]string)
	for _, uri := range keyURIs {
		keyURL := resolveURL(playlistURL, uri)
		body, err := c.GetBytes(keyURL, headers)
		if err != nil {
			continue
		}

		// If the key response is raw 16 bytes, use directly.
		if len(body) == 16 {
			keys[keyURL] = base64.StdEncoding.EncodeToString(body)
			continue
		}

		// Otherwise it's a JSON envelope with imooc_decode-encoded key.
		keyBytes, err := decryptKeyFromJSON(body)
		if err != nil {
			continue
		}
		if len(keyBytes) == 16 {
			keys[keyURL] = base64.StdEncoding.EncodeToString(keyBytes)
		}
	}
	return keys
}

// decryptKeyFromJSON decodes the JSON key response using imooc_decode.
func decryptKeyFromJSON(body []byte) ([]byte, error) {
	// Try JSON envelope with data.info.
	var env struct {
		Data struct {
			Info string `json:"info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Data.Info != "" {
		return decryptInfo(env.Data.Info)
	}

	// Try regex.
	m := regexp.MustCompile(`"info"\s*:\s*"(.*?)"`).FindSubmatch(body)
	if len(m) > 1 && len(m[1]) > 0 {
		return decryptInfo(string(m[1]))
	}

	return nil, fmt.Errorf("no info field in key response")
}

// extractKeyURIs finds all #EXT-X-KEY URI= values in an HLS playlist.
func extractKeyURIs(playlist string) []string {
	re := regexp.MustCompile(`#EXT-X-KEY:[^\n]*\bURI=["']([^"']+)["']`)
	matches := re.FindAllStringSubmatch(playlist, -1)
	var uris []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if !seen[m[1]] {
			uris = append(uris, m[1])
			seen[m[1]] = true
		}
	}
	return uris
}

// resolveURL resolves a potentially relative URI against a base URL.
func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	u, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return u.ResolveReference(r).String()
}

func extractDomain(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		return "imooc.com"
	}
	return u.Hostname()
}

func parseURL(u string) (cid, mid, host string) {
	host = "https://www.imooc.com"
	switch {
	case strings.Contains(u, "coding.imooc.com"):
		host = "https://coding.imooc.com"
	case strings.Contains(u, "class.imooc.com"):
		host = "https://class.imooc.com"
	}
	if m := regexp.MustCompile(`(?:class|sc|learn/list|learn|course/playlist)/(\d+)`).FindStringSubmatch(u); len(m) > 1 {
		cid = m[1]
	}
	if m := regexp.MustCompile(`(?:lesson/|video/|mid=)(\d+)`).FindStringSubmatch(u); len(m) > 1 {
		mid = m[1]
	}
	return cid, mid, host
}

func lifecycleURLs(host string) (start, end string, enabled bool) {
	if strings.Contains(host, "coding.imooc.com") {
		return host + "/lesson/ajaxstartlearn", host + "/lesson/ajaxendlearn", true
	}
	if strings.Contains(host, "class.imooc.com") {
		return host + "/course/startlearn", host + "/course/endlearn", true
	}
	return "", "", false
}

func mediaURL(host, mid, cid string) string {
	switch {
	case strings.Contains(host, "coding.imooc.com"):
		return fmt.Sprintf("%s/lesson/m3u8h5?mid=%s&cid=%s&ssl=1&cdn=aliyun1", host, mid, cid)
	case strings.Contains(host, "class.imooc.com"):
		return fmt.Sprintf("%s/lesson/m3u8h5?mid=%s&cid=%s&ssl=1&cdn=aliyun1", host, mid, cid)
	}
	return fmt.Sprintf("%s/course/playlist/%s?t=m3u8&_id=%s&cdn=aliyun1", host, mid, cid)
}

func buildResult(cid, m3u8, host string, hlsKeys map[string]string) *extractor.MediaInfo {
	stream := extractor.Stream{
		Quality: "default",
		URLs:    []string{m3u8},
		Format:  "m3u8",
		Headers: map[string]string{"Referer": host + "/"},
	}
	result := &extractor.MediaInfo{
		Site:  "imooc",
		Title: "imooc_" + cid,
		Streams: map[string]extractor.Stream{
			"hls": stream,
		},
	}
	if len(hlsKeys) > 0 {
		result.Extra = map[string]any{
			"hls_keys": hlsKeys,
		}
	}
	return result
}

func isM3U8(body string) bool {
	return strings.HasPrefix(strings.TrimSpace(body), "#EXTM3U")
}
