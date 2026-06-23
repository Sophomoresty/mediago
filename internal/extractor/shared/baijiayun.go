// Baijiayun (api.baijiayun.com / www.baijiayun.com) helpers — used by
// Baijiayunxiao, Jinbangshidai, Kaimingzhixue, Orangevip, Youyuan.
//
// Baijiayun has two playback flows (from Baijiayun_Video.pyc constants):
//
//   Live replay:
//     GET https://api.baijiayun.com/web/playback/getPlayInfo
//          ?room_id={room_id}&token={token}&use_encrypt=0&render=jsonp
//
//   VOD playback:
//     GET https://www.baijiayun.com/vod/video/getPlayUrl
//          ?vid={video_id}&render=jsonp&token={token}&use_encrypt=0
//
// Both endpoints return JSONP with the JSON payload wrapped in `(...)`. We
// strip the wrapper and parse the inner JSON.
package shared

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/util"
)

const (
	BaijiayunGetPlayInfoURL = "https://api.baijiayun.com/web/playback/getPlayInfo"
	BaijiayunGetPlayURLURL  = "https://www.baijiayun.com/vod/video/getPlayUrl"
)

// BaijiayunVideo describes one playable variant.
type BaijiayunVideo struct {
	URL        string `json:"url"`
	Definition string `json:"definition"`
}

// BaijiayunPlaybackResponse parses the JSONP response from getPlayInfo or
// getPlayUrl. Different endpoints use slightly different keys; we accept both.
type BaijiayunPlaybackResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		PlaybackURL string           `json:"playback_url"`
		VideoURL    string           `json:"video_url"`
		Title       string           `json:"title"`
		Videos      []BaijiayunVideo `json:"video"` // VOD format
	} `json:"data"`
}

// BaijiayunResolveVOD fetches getPlayUrl for a VOD video and returns the
// best playable URL.
func BaijiayunResolveVOD(c *util.Client, vid, token string, headers map[string]string) (string, error) {
	if vid == "" {
		return "", fmt.Errorf("baijiayun: missing vid")
	}
	apiURL := fmt.Sprintf("%s?vid=%s&render=jsonp&token=%s&use_encrypt=0",
		BaijiayunGetPlayURLURL, url.QueryEscape(vid), url.QueryEscape(token))
	resp, err := fetchAndUnwrapJSONP(c, apiURL, headers)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 && resp.Code != 200 {
		return "", fmt.Errorf("baijiayun VOD: code=%d msg=%q", resp.Code, resp.Msg)
	}
	if resp.Data.VideoURL != "" {
		return resp.Data.VideoURL, nil
	}
	if len(resp.Data.Videos) > 0 {
		return resp.Data.Videos[0].URL, nil
	}
	return "", fmt.Errorf("baijiayun VOD: no playable URL")
}

// BaijiayunResolvePlayback fetches getPlayInfo for a live replay and returns
// the playback m3u8 URL.
func BaijiayunResolvePlayback(c *util.Client, roomID, token string, headers map[string]string) (string, error) {
	if roomID == "" {
		return "", fmt.Errorf("baijiayun: missing roomID")
	}
	apiURL := fmt.Sprintf("%s?room_id=%s&token=%s&use_encrypt=0&render=jsonp",
		BaijiayunGetPlayInfoURL, url.QueryEscape(roomID), url.QueryEscape(token))
	resp, err := fetchAndUnwrapJSONP(c, apiURL, headers)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 && resp.Code != 200 {
		return "", fmt.Errorf("baijiayun playback: code=%d msg=%q", resp.Code, resp.Msg)
	}
	if resp.Data.PlaybackURL != "" {
		return resp.Data.PlaybackURL, nil
	}
	return "", fmt.Errorf("baijiayun playback: no playback_url in response")
}

var jsonpUnwrapRe = regexp.MustCompile(`(?s)^[\w_$]*\((.*)\);?$`)

func fetchAndUnwrapJSONP(c *util.Client, apiURL string, headers map[string]string) (*BaijiayunPlaybackResponse, error) {
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("baijiayun fetch: %w", err)
	}
	body = strings.TrimSpace(body)
	if m := jsonpUnwrapRe.FindStringSubmatch(body); m != nil {
		body = m[1]
	}
	var resp BaijiayunPlaybackResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("baijiayun parse JSONP: %w", err)
	}
	return &resp, nil
}
