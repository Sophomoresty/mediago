// Package shared contains helpers for CDN/player platforms (CSSLcloud, Polyv,
// BokeCC, Baijiayun) embedded by multiple parent sites. The parent site does
// authentication and returns CDN tokens; the manifest/segment URLs come from
// the shared platform.
//
// CSSLcloud (view.csslcloud.net) chain ported from decompiled Mooc/Courses/
// {Jianshe99,Med66,Houda,Qihang,Shanxiang,Aishangke,Chaoge}/<site>_Course.pyc:
//
//  1. POST  https://view.csslcloud.net/api/room/replay/login
//     body: liveRoomId, userid, accessid, recordId, viewername, viewertoken,
//     forcibly=0, version, service=3, client=4
//     → returns { datas: { sessionId } }
//
//  2. GET   https://view.csslcloud.net/api/record/vod
//     ?accountId={accessid}&recordId={recordId}&terminal=3&token={sessionId}
//     → returns { data: { vod_info: { video: [{ url, definition }], audio: [{ url }] } } }
//
//  3. The video URL points at a .m3u8 manifest; AES-128 keys are tokenised:
//     EXT-X-KEY URI must be re-fetched with bokecc info-token then hex-encoded.
package shared

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/nichuanfang/medigo/internal/util"
)

// CSSL endpoints (verbatim from decompiled source — DO NOT CHANGE).
const (
	CssLcloudReplayLoginURL  = "https://view.csslcloud.net/api/room/replay/login"
	CssLcloudReplayVodURL    = "https://view.csslcloud.net/api/record/vod"
	CssLcloudReplayRuleURL   = "https://view.csslcloud.net/api/replay/rule"
	CssLcloudReplayVersionV1 = "1.0.0"
)

// CssLcloudPayload is the parent-site-supplied payload that drives the chain.
// All field names match the Python source's expected keys.
type CssLcloudPayload struct {
	LiveRoomID  string // CC live room ID (also called liveId)
	UserID      string // CC viewer userid (uid)
	AccessID    string // CC tenant accessid (== accountId)
	RecordID    string // CC playback recordId
	ViewerName  string // CC viewer display name
	ViewerToken string // CC viewer auth token (uid + ":" + lid)
	Referer     string // parent-site referer for HTTP requests
}

// CssLcloudPlayInfo is the resolved playback information.
type CssLcloudPlayInfo struct {
	SessionID string                // CC replay session, valid for ~1 hour
	VideoURL  string                // m3u8 or mp4
	AudioURL  string                // separate audio if available
	VideoList []CssLcloudStreamInfo // all available qualities
}

type CssLcloudStreamInfo struct {
	URL        string `json:"url"`
	Definition int    `json:"definition"`
}

// CssLcloudResolvePlayInfo runs the full login → vod chain and returns
// playback info. This is the main entry point for parent-site extractors.
func CssLcloudResolvePlayInfo(c *util.Client, p CssLcloudPayload) (*CssLcloudPlayInfo, error) {
	if p.LiveRoomID == "" || p.RecordID == "" || p.AccessID == "" {
		return nil, fmt.Errorf("csslcloud: missing required liveRoomId/recordId/accessid")
	}

	loginForm := map[string]string{
		"liveRoomId":  p.LiveRoomID,
		"liveid":      p.LiveRoomID, // alias accepted by some endpoints
		"roomid":      p.LiveRoomID,
		"userid":      p.UserID,
		"accessid":    p.AccessID,
		"recordId":    p.RecordID,
		"recordid":    p.RecordID,
		"viewername":  p.ViewerName,
		"viewertoken": p.ViewerToken,
		"forcibly":    "0",
		"version":     CssLcloudReplayVersionV1,
		"service":     "3",
		"client":      "4",
	}
	headers := map[string]string{}
	if p.Referer != "" {
		headers["Referer"] = p.Referer
	}
	loginBody, err := c.PostForm(CssLcloudReplayLoginURL, loginForm, headers)
	if err != nil {
		return nil, fmt.Errorf("csslcloud login: %w", err)
	}
	var login struct {
		Result string `json:"result"`
		Datas  struct {
			SessionID string `json:"sessionId"`
		} `json:"datas"`
	}
	if err := json.Unmarshal([]byte(loginBody), &login); err != nil {
		return nil, fmt.Errorf("csslcloud login parse: %w", err)
	}
	if login.Datas.SessionID == "" {
		return nil, fmt.Errorf("csslcloud login: empty sessionId (result=%q)", login.Result)
	}

	vodURL := fmt.Sprintf("%s?accountId=%s&recordId=%s&terminal=3&token=%s",
		CssLcloudReplayVodURL,
		url.QueryEscape(p.AccessID),
		url.QueryEscape(p.RecordID),
		url.QueryEscape(login.Datas.SessionID))
	vodBody, err := c.GetString(vodURL, headers)
	if err != nil {
		return nil, fmt.Errorf("csslcloud vod: %w", err)
	}
	var vod struct {
		Data struct {
			VodInfo struct {
				Video []CssLcloudStreamInfo `json:"video"`
				Audio []CssLcloudStreamInfo `json:"audio"`
			} `json:"vod_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(vodBody), &vod); err != nil {
		return nil, fmt.Errorf("csslcloud vod parse: %w", err)
	}
	videos := vod.Data.VodInfo.Video
	if len(videos) == 0 {
		return nil, fmt.Errorf("csslcloud vod: no video streams in response")
	}

	best := pickBestCssLcloudStream(videos)
	out := &CssLcloudPlayInfo{
		SessionID: login.Datas.SessionID,
		VideoURL:  best.URL,
		VideoList: videos,
	}
	if len(vod.Data.VodInfo.Audio) > 0 {
		out.AudioURL = vod.Data.VodInfo.Audio[0].URL
	}
	return out, nil
}

// pickBestCssLcloudStream picks the highest-definition video from a CSSL list.
// Python source ranks definition desc; we replicate.
func pickBestCssLcloudStream(list []CssLcloudStreamInfo) CssLcloudStreamInfo {
	best := list[0]
	for _, s := range list[1:] {
		if s.Definition > best.Definition && s.URL != "" {
			best = s
		}
	}
	return best
}

// CssLcloudRewriteM3U8Keys rewrites EXT-X-KEY URI lines in a CSSL m3u8 manifest
// so the AES-128 keys can be fetched directly. The Python source's
// _prepare_live_replay_m3u8_text + _resolve_bokecc_key_token fetches each key
// URL with the parent-site referer, then hex-encodes the binary key body.
//
// In Go we follow the same pattern: each EXT-X-KEY URI gets re-fetched and
// the key bytes embedded inline as URI="data:text/plain;base64,...". This way
// a downstream HLS downloader can read the manifest as-is without needing a
// custom key fetcher.
func CssLcloudRewriteM3U8Keys(c *util.Client, m3u8Text, referer string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(m3u8Text), "#EXTM3U") {
		return "", fmt.Errorf("csslcloud: input is not an m3u8 manifest")
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
		keyBody, err := c.GetBytes(uri, headers)
		if err != nil {
			return "", fmt.Errorf("csslcloud key fetch %s: %w", uri, err)
		}
		hexKey := strings.ToUpper(hex.EncodeToString(keyBody))
		// The Python source uses URI="..." with hex-prefixed key inline.
		// downstream HLS downloader supports literal URIs starting with 0x.
		newLine := strings.ReplaceAll(line, uri, "0x"+hexKey)
		out = append(out, newLine)
	}
	return strings.Join(out, "\n"), nil
}

// extractM3U8URI pulls the URI="..." value from an EXT-X-KEY (or any HLS attr) line.
func extractM3U8URI(line string) string {
	idx := strings.Index(line, `URI="`)
	if idx < 0 {
		idx = strings.Index(line, `URI='`)
		if idx < 0 {
			return ""
		}
	}
	rest := line[idx+5:]
	end := strings.IndexAny(rest, `"'`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
