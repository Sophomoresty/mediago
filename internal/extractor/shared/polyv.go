// Polyv (player.polyv.net / hls.videocc.net) helpers — used by Mashibing,
// Wangxiao233, Kaoyanvip, Magedu, Luffycity, Minshi, Kuke, Gongxuanwang,
// Jinbangshidai, Plaso, Youyuan, Orangevip, Zhaozhao (~16 sites).
//
// Non-DRM polyv playback chain ported from Mashibing_Base.pyc constants:
//  1. GET  https://player.polyv.net/secure/{vid}.json
//     → returns { code: 200, data: { playsafe: { token }, paths: [...], dur } }
//  2. Manifest URL: https://hls.videocc.net/{path1}/{path2}/{vid}_{bitrate}.m3u8
//     (path1/path2 derived from vid; bitrate from polyv's quality picker)
//  3. EXT-X-KEY URI in manifest must be re-fetched with the playsafe token:
//     https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//
// DRM polyv (the `vod-player-drm/canary/next/lib_player.js` flow used by
// Mashibing premium courses) needs a JS sandbox to decrypt the per-session
// secret; that's marked blocked.
package shared

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/nichuanfang/medigo/internal/util"
)

// Polyv endpoints (verbatim from source).
const (
	PolyvSecureURLTmpl  = "https://player.polyv.net/secure/%s.json"
	PolyvHLSPlayBase    = "https://hls.videocc.net"
	PolyvDRMLibPlayerJS = "https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js"
)

// PolyvSecure is the response from player.polyv.net/secure/{vid}.json.
type PolyvSecure struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Playsafe struct {
			Token string `json:"token"`
		} `json:"playsafe"`
		// Paths is the per-quality m3u8 list. Each entry contains a path
		// fragment (path1/path2) and a quality marker.
		Paths     []string `json:"paths"`
		Dur       int      `json:"dur"`
		Title     string   `json:"title"`
		Encrypted bool     `json:"encrypted"`
	} `json:"data"`
}

// PolyvResolveSecure fetches secure/{vid}.json and returns the parsed envelope.
// Parent site provides cookies/headers via the *util.Client.
func PolyvResolveSecure(c *util.Client, vid string, headers map[string]string) (*PolyvSecure, error) {
	if vid == "" {
		return nil, fmt.Errorf("polyv: empty vid")
	}
	apiURL := fmt.Sprintf(PolyvSecureURLTmpl, url.PathEscape(vid))
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("polyv secure: %w", err)
	}
	var sec PolyvSecure
	if err := json.Unmarshal([]byte(body), &sec); err != nil {
		return nil, fmt.Errorf("polyv secure parse: %w (body=%q)", err, truncate(body, 200))
	}
	if sec.Code != 200 && sec.Code != 0 {
		return nil, fmt.Errorf("polyv secure: code=%d message=%q", sec.Code, sec.Message)
	}
	return &sec, nil
}

// PolyvPickBestManifest returns the highest-quality manifest URL from a
// secure response. Returns blocked if the content is DRM-protected.
func PolyvPickBestManifest(sec *PolyvSecure) (string, error) {
	if sec.Data.Encrypted {
		return "", fmt.Errorf("polyv: blocked needs DRM JS engine (lib_player.js)")
	}
	if len(sec.Data.Paths) == 0 {
		return "", fmt.Errorf("polyv: no playable paths in secure response")
	}
	// Paths are typically ordered highest-quality first.
	return sec.Data.Paths[0], nil
}

// PolyvRewriteM3U8Keys rewrites EXT-X-KEY URI entries to inline hex keys.
// Same pattern as csslcloud — fetch each key with referer, inline as 0x{hex}.
func PolyvRewriteM3U8Keys(c *util.Client, m3u8Text, token, referer string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(m3u8Text), "#EXTM3U") {
		return "", fmt.Errorf("polyv: input is not an m3u8 manifest")
	}
	headers := map[string]string{}
	if referer != "" {
		headers["Referer"] = referer
	}

	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(m3u8Text, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "#EXT-X-KEY") {
			out = append(out, line)
			continue
		}
		uri := extractM3U8URI(line)
		if uri == "" {
			out = append(out, line)
			continue
		}
		// Append token if missing.
		keyURL := uri
		if !strings.Contains(keyURL, "token=") && token != "" {
			sep := "?"
			if strings.Contains(keyURL, "?") {
				sep = "&"
			}
			keyURL = keyURL + sep + "token=" + url.QueryEscape(token)
		}
		keyBytes, err := c.GetBytes(keyURL, headers)
		if err != nil {
			return "", fmt.Errorf("polyv key fetch %s: %w", keyURL, err)
		}
		hexKey := strings.ToUpper(encodeHex(keyBytes))
		out = append(out, strings.ReplaceAll(line, uri, "0x"+hexKey))
	}
	return strings.Join(out, "\n"), nil
}

func encodeHex(b []byte) string {
	const digits = "0123456789ABCDEF"
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, c := range b {
		sb.WriteByte(digits[c>>4])
		sb.WriteByte(digits[c&0x0f])
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
