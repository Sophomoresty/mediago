package shanxiang

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

func resolveShanxiangCsslPlayInfo(c *util.Client, p playbackInfo, cc map[string]string) (*shared.CssLcloudPlayInfo, string, error) {
	accessID := firstNonEmpty(cc["userId"], cc["groupId"])
	roomID := firstNonEmpty(cc["roomId"], cc["liveId"])
	recordID := firstNonEmpty(cc["recordId"], cc["videoId"], p.PlaybackID)
	viewerName := firstNonEmpty(cc["viewername"], cc["viewerName"], cc["viewerId"], "viewer")
	viewerToken := cc["viewertoken"]
	if accessID == "" || roomID == "" || recordID == "" || viewerToken == "" {
		return nil, "", fmt.Errorf("shanxiang: missing CSSLcloud fields userId/roomId/recordId/viewertoken")
	}

	play, replayErr := resolveShanxiangReplayAPI(c, p.PlaybackURL, accessID, roomID, recordID, viewerName, firstNonEmpty(cc["viewerId"], viewerName), viewerToken)
	if replayErr == nil {
		return play, "shanxiang_replay_api", nil
	}

	fallback, fallbackErr := shared.CssLcloudResolvePlayInfo(c, shared.CssLcloudPayload{
		LiveRoomID: roomID, AccessID: accessID, RecordID: recordID,
		UserID: accessID, ViewerName: viewerName, ViewerToken: viewerToken, Referer: p.PlaybackURL,
	})
	if fallbackErr != nil {
		return nil, "", fmt.Errorf("shanxiang replay api: %v; csslcloud legacy api: %w", replayErr, fallbackErr)
	}
	return fallback, "csslcloud_legacy_api", nil
}

func resolveShanxiangReplayAPI(c *util.Client, referer, accessID, roomID, recordID, viewerName, viewerID, viewerToken string) (*shared.CssLcloudPlayInfo, error) {
	loginPayload := map[string]any{
		"accountId":     accessID,
		"userId":        accessID,
		"roomId":        roomID,
		"replayId":      recordID,
		"recordId":      recordID,
		"userName":      viewerName,
		"viewerId":      viewerID,
		"userToken":     viewerToken,
		"viewerToken":   viewerToken,
		"deviceType":    csslDeviceType,
		"deviceVersion": csslDeviceVersion,
		"tpl":           csslTpl,
	}
	rawPayload, _ := json.Marshal(loginPayload)
	resp, err := c.Post(urlCsslLogin, bytes.NewReader(rawPayload), shanxiangCsslHeaders(referer, ""))
	if err != nil {
		return nil, fmt.Errorf("replay login: %w", err)
	}
	defer resp.Body.Close()
	loginBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("replay login read: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("replay login HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(loginBody)))
	}
	var loginRoot any
	if err := json.Unmarshal(loginBody, &loginRoot); err != nil {
		return nil, fmt.Errorf("replay login parse: %w", err)
	}
	token := findJSONFirstString(loginRoot, "token")
	if token == "" {
		return nil, fmt.Errorf("replay login returned empty X-HD-Token")
	}

	q := url.Values{}
	q.Set("account_id", accessID)
	q.Set("accountId", accessID)
	q.Set("replay_id", recordID)
	q.Set("replayId", recordID)
	q.Set("terminal", strconv.Itoa(csslTerminal))
	playURL := urlCsslPlay + "?" + q.Encode()
	playBody, err := c.GetString(playURL, shanxiangCsslHeaders(referer, token))
	if err != nil {
		return nil, fmt.Errorf("replay play: %w", err)
	}
	var playRoot any
	if err := json.Unmarshal([]byte(playBody), &playRoot); err != nil {
		return nil, fmt.Errorf("replay play parse: %w", err)
	}

	data := mapField(playRoot, "data")
	videos := parseShanxiangCsslStreams(firstNonNil(data["video"], data["videos"], data["videoList"], nestedField(data, "vod_info", "video")))
	if len(videos) == 0 {
		videos = parseShanxiangCsslStreams(findJSONFirstValue(playRoot, "video"))
	}
	if len(videos) == 0 {
		return nil, fmt.Errorf("replay play returned no video streams")
	}
	sort.SliceStable(videos, func(i, j int) bool { return videos[i].Definition > videos[j].Definition })
	best, ok := firstPlayableShanxiangCsslStream(videos)
	if !ok {
		return nil, fmt.Errorf("replay play returned no playable video URL")
	}

	audios := parseShanxiangCsslStreams(firstNonNil(data["audio"], data["audios"], nestedField(data, "vod_info", "audio")))
	out := &shared.CssLcloudPlayInfo{
		SessionID: token,
		VideoURL:  best.URL,
		VideoList: videos,
	}
	if len(audios) > 0 {
		out.AudioURL = audios[0].URL
	}
	return out, nil
}

func firstPlayableShanxiangCsslStream(videos []shared.CssLcloudStreamInfo) (shared.CssLcloudStreamInfo, bool) {
	for _, video := range videos {
		if strings.TrimSpace(video.URL) != "" {
			return video, true
		}
	}
	return shared.CssLcloudStreamInfo{}, false
}

func shanxiangCsslHeaders(referer, token string) map[string]string {
	h := map[string]string{
		"Accept":       "*/*",
		"Content-Type": "application/json",
		"Origin":       urlCsslOrigin,
		"Referer":      firstNonEmpty(referer, urlCsslOrigin+"/"),
		"User-Agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}
	if token != "" {
		h["X-HD-Token"] = token
	}
	return h
}

func parseShanxiangCsslStreams(value any) []shared.CssLcloudStreamInfo {
	var out []shared.CssLcloudStreamInfo
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case nil:
			return
		case []any:
			for _, child := range x {
				walk(child)
			}
		case map[string]any:
			if u := firstNonEmpty(anyString(x["url"]), anyString(x["playUrl"]), anyString(x["m3u8"]), anyString(x["src"])); u != "" {
				out = append(out, shared.CssLcloudStreamInfo{URL: u, Definition: shanxiangQualityRank(x)})
				return
			}
			for _, key := range []string{"primary", "secondary", "video", "videos", "videoList", "list", "items", "audio", "audios"} {
				if child, ok := x[key]; ok {
					walk(child)
				}
			}
		}
	}
	walk(value)
	return dedupeCsslStreams(out)
}

func dedupeCsslStreams(in []shared.CssLcloudStreamInfo) []shared.CssLcloudStreamInfo {
	seen := map[string]bool{}
	out := in[:0]
	for _, stream := range in {
		stream.URL = strings.TrimSpace(stream.URL)
		if stream.URL == "" || seen[stream.URL] {
			continue
		}
		seen[stream.URL] = true
		out = append(out, stream)
	}
	return out
}

func shanxiangQualityRank(video map[string]any) int {
	text := strings.ToUpper(firstNonEmpty(anyString(video["definition"]), anyString(video["desc"]), anyString(video["qualityDesc"]), anyString(video["code"]), anyString(video["quality"])))
	switch {
	case strings.Contains(text, "原画"), strings.Contains(text, "蓝光"), strings.Contains(text, "1080"), strings.Contains(text, "FHD"), strings.Contains(text, "4K"):
		return 400
	case strings.Contains(text, "超清"):
		return 320
	case strings.Contains(text, "高清"), strings.Contains(text, "720"), strings.Contains(text, "HD"):
		return 240
	case strings.Contains(text, "标清"), strings.Contains(text, "流畅"), strings.Contains(text, "480"), strings.Contains(text, "360"), strings.Contains(text, "SD"):
		return 160
	default:
		if n := intValue(video["definition"]); n > 0 {
			return n
		}
		if n := intValue(video["quality"]); n > 0 {
			return n
		}
		return 0
	}
}

func mapField(root any, key string) map[string]any {
	if m, ok := root.(map[string]any); ok {
		if child, ok := m[key].(map[string]any); ok {
			return child
		}
	}
	return map[string]any{}
}

func nestedField(m map[string]any, keys ...string) any {
	var cur any = m
	for _, key := range keys {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = cm[key]
	}
	return cur
}

func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func findJSONFirstString(root any, key string) string {
	if v := findJSONFirstValue(root, key); v != nil {
		return anyString(v)
	}
	return ""
}

func findJSONFirstValue(root any, key string) any {
	switch x := root.(type) {
	case map[string]any:
		if v, ok := x[key]; ok {
			return v
		}
		for _, child := range x {
			if v := findJSONFirstValue(child, key); v != nil {
				return v
			}
		}
	case []any:
		for _, child := range x {
			if v := findJSONFirstValue(child, key); v != nil {
				return v
			}
		}
	}
	return nil
}

func intValue(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		n, _ := strconv.Atoi(anyString(v))
		return n
	}
}

func shanxiangM3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}
