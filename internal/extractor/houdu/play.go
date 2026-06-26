package houdu

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor/shared"
)

func (x *hdCtx) getPlayURLForMode(lessonID, mode string) string {
	body := map[string]any{"lesson_id": coerceAPIID(lessonID)}
	tryAPI := func(path string) string {
		resp, err := x.requestHoudu(path, body, "phoenix")
		if err != nil {
			return ""
		}
		if u := x.extractPlayURL(resp); u != "" {
			return u
		}
		data := asMap(x.extractData(resp))
		if u := x.resolveBaijiayunFromMap(data); u != "" {
			return u
		}
		if u := x.resolvePlayURL(buildPlayStubURL(data)); u != "" {
			return u
		}
		return ""
	}
	switch mode {
	case "record":
		for _, path := range []string{"/mini/mini/recordLessonPlayURLForPC", "/mini/mini/recordLessonPlayURL", "/mini/mini/recordLessonPlayParams"} {
			if u := tryAPI(path); u != "" {
				return u
			}
		}
	case "playback":
		for _, path := range []string{"/mini/mini/lessonPlaybackURLForPC", "/mini/mini/lessonPlaybackURL", "/mini/mini/liveLessonPlayParams"} {
			if u := tryAPI(path); u != "" {
				return u
			}
		}
	default:
		// Source: first try lessonRoomURLForPC, then fall back to liveLessonPlayParams
		if u := tryAPI("/mini/mini/lessonRoomURLForPC"); u != "" {
			return u
		}
		return tryAPI("/mini/mini/liveLessonPlayParams")
	}
	return ""
}

func (x *hdCtx) extractPlayURL(resp map[string]any) string {
	data := x.extractData(resp)
	if u := bestVideoURL(data); u != "" {
		return x.resolvePlayURL(u)
	}
	m := asMap(data)
	for _, key := range []string{"url", "play_url", "playUrl", "hls_url", "hlsUrl", "video_url", "videoUrl"} {
		if u := x.resolvePlayURL(str(m[key])); u != "" {
			return u
		}
	}
	for _, s := range walkStrings(data) {
		if strings.HasPrefix(strings.TrimSpace(s), "http") {
			if u := x.resolvePlayURL(s); u != "" {
				return u
			}
		}
	}
	return ""
}

func (x *hdCtx) resolvePlayURL(raw string) string {
	if raw == "" {
		return ""
	}
	if u := normalizeMediaURL(raw); u != "" {
		return appendMiniToken(u, x.token)
	}
	if u := x.resolveBaijiayunURL(raw); u != "" {
		return u
	}
	return ""
}

func (x *hdCtx) resolveBaijiayunFromMap(data map[string]any) string {
	vid := firstString(data, "video_id", "vid", "live_id")
	roomID := firstString(data, "room_id", "roomid", "classid", "class_id")
	token := firstString(data, "token", "play_token", "playToken")
	if token == "" {
		return ""
	}
	headers := map[string]string{"User-Agent": USER_AGENT, "Referer": referer}
	if vid != "" {
		if u, err := shared.BaijiayunResolveVOD(x.c, vid, token, headers); err == nil {
			return u
		}
	}
	if roomID != "" {
		if u, err := shared.BaijiayunResolvePlayback(x.c, roomID, token, headers); err == nil {
			return u
		}
	}
	return ""
}

func (x *hdCtx) resolveBaijiayunURL(playURL string) string {
	u, err := url.Parse(strings.TrimSpace(playURL))
	if err != nil {
		return ""
	}
	q := u.Query()
	data := map[string]any{
		"video_id": firstNonEmpty(q.Get("video_id"), q.Get("vid"), q.Get("live_id")),
		"room_id":  firstNonEmpty(q.Get("room_id"), q.Get("roomid"), q.Get("classid"), q.Get("class_id")),
		"token":    q.Get("token"),
	}
	return x.resolveBaijiayunFromMap(data)
}

func buildPlayStubURL(data map[string]any) string {
	token := firstString(data, "token", "play_token", "playToken")
	vid := firstString(data, "video_id", "vid", "live_id")
	roomID := firstString(data, "room_id", "roomid", "classid", "class_id")
	if token != "" && vid != "" {
		return fmt.Sprintf("https://h5.houduweilai.com/recordedCourses/play?video_id=%s&token=%s", url.QueryEscape(vid), url.QueryEscape(token))
	}
	if token != "" && roomID != "" {
		return fmt.Sprintf("https://h5.houduweilai.com/live/play?room_id=%s&token=%s", url.QueryEscape(roomID), url.QueryEscape(token))
	}
	return ""
}

func appendMiniToken(playURL, token string) string {
	if playURL == "" || token == "" || strings.Contains(playURL, "miniToken=") {
		return playURL
	}
	sep := "?"
	if strings.Contains(playURL, "?") {
		sep = "&"
	}
	return playURL + sep + "miniToken=" + url.QueryEscape(token)
}

func bestVideoURL(value any) string {
	m := asMap(value)
	if data := asMap(m["data"]); len(data) > 0 {
		m = data
	}
	playInfo := asMap(m["play_info"])
	if len(playInfo) == 0 {
		playInfo = asMap(m["playInfo"])
	}
	order := []string{"1080p", "superHD", "720p", "high", "480p", "standard"}
	for _, key := range order {
		variant := asMap(playInfo[key])
		for _, cdn := range listAt(variant, "cdn_list") {
			for _, urlKey := range []string{"enc_url", "url"} {
				if u := normalizeMediaURL(str(cdn[urlKey])); u != "" {
					return u
				}
			}
		}
		for _, urlKey := range []string{"enc_url", "url"} {
			if u := normalizeMediaURL(str(variant[urlKey])); u != "" {
				return u
			}
		}
	}
	for _, s := range walkStrings(value) {
		if u := normalizeMediaURL(s); u != "" {
			return u
		}
	}
	return ""
}
