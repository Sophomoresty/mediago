package huatu

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
)

type playSource struct {
	URL        string
	SourceType string
	Size       int64
	Extra      map[string]any
}

func (x *huatuCtx) mediaFromItems(items []sourceItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		if item.Kind == "file" {
			entries = append(entries, fileEntry(item, x.cookie))
			continue
		}
		src, err := x.videoSource(item.LessonID)
		if err != nil || src.URL == "" {
			continue
		}
		format := mediaFormat(src.URL)
		extra := map[string]any{"lesson_id": item.LessonID, "source_type": src.SourceType}
		for k, v := range src.Extra {
			extra[k] = v
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "huatu",
			Title: firstNonEmpty(cleanName(item.Name), "huatu_"+item.LessonID),
			Streams: map[string]extractor.Stream{
				format: {Quality: "best", URLs: []string{src.URL}, Format: format, Size: src.Size, NeedMerge: format == "m3u8", Headers: x.streamHeaders()},
			},
			Extra: extra,
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("huatu: no playable videos or files for goodsNum=%s", x.cid)
	}
	return &extractor.MediaInfo{Site: "huatu", Title: firstNonEmpty(x.title, "huatu_"+x.cid), Entries: entries}, nil
}

func fileEntry(item sourceItem, cookie string) *extractor.MediaInfo {
	format := firstNonEmpty(item.Format, mediaFormat(item.URL))
	return &extractor.MediaInfo{
		Site:  "huatu",
		Title: item.Name,
		Streams: map[string]extractor.Stream{
			"file": {Quality: "file", URLs: []string{item.URL}, Format: format, Headers: map[string]string{"Referer": referer, "User-Agent": USER_AGENT, "Cookie": cookie}},
		},
		Extra: map[string]any{"kind": "file"},
	}
}

func (x *huatuCtx) streamHeaders() map[string]string {
	return map[string]string{"Referer": referer, "User-Agent": USER_AGENT, "Cookie": x.cookie}
}

func (x *huatuCtx) videoSource(lessonID string) (*playSource, error) {
	root, err := x.getJSON(player_url, map[string]string{"goodsNum": x.cid, "lessonId": lessonID}, nil, nil)
	if err != nil {
		return nil, err
	}
	if !successCode(root["code"]) {
		return nil, nil
	}
	data := asMap(root["data"])
	if len(data) == 0 {
		return nil, nil
	}
	if src, err := x.extractBaijiayunPlaySource(data); err == nil && src.URL != "" {
		return src, nil
	}
	appID := str(data["appId"])
	videoID := str(data["videoId"])
	psign := str(data["token"])
	if appID == "" || videoID == "" || psign == "" {
		return nil, nil
	}
	vodURL := fmt.Sprintf(vod_info_url, appID, videoID, psign)
	vodRoot, err := x.getJSON(vodURL, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	media := asMap(vodRoot["media"])
	return x.extractMediaPlaySource(media)
}

func (x *huatuCtx) extractBaijiayunPlaySource(data map[string]any) (*playSource, error) {
	roomID, token, isVOD := resolveBaijiayunFields(data)
	if roomID == "" || token == "" {
		return nil, nil
	}
	var u string
	var err error
	if isVOD {
		u, err = shared.BaijiayunResolveVOD(x.c, roomID, token, x.streamHeaders())
	} else {
		u, err = shared.BaijiayunResolvePlayback(x.c, roomID, token, x.streamHeaders())
	}
	if err != nil {
		return nil, err
	}
	return &playSource{URL: u, SourceType: "baijiayun", Extra: map[string]any{"baijiayun_id": roomID}}, nil
}

func resolveBaijiayunFields(data map[string]any) (id, token string, isVOD bool) {
	if u := findPlaybackURL(data, 0); u != "" {
		if id, token, isVOD := parseBaijiayunPlaybackQuery(u); id != "" && token != "" {
			return id, token, isVOD
		}
	}
	for _, node := range candidateNodes(data, 0) {
		id = firstNonEmpty(str(node["roomId"]), str(node["room_id"]), str(node["classId"]), str(node["classid"]), str(node["playbackRoomId"]), str(node["playback_room_id"]), str(node["bjyRoomId"]), str(node["bjy_room_id"]), str(node["bjyId"]), str(node["bjy_id"]), str(node["vid"]))
		token = firstNonEmpty(str(node["token"]), str(node["playToken"]), str(node["play_token"]), str(node["playbackToken"]), str(node["playback_token"]), str(node["bjyToken"]), str(node["bjy_token"]))
		if id != "" && token != "" {
			return id, token, str(node["vid"]) != ""
		}
	}
	return "", "", false
}

func candidateNodes(v any, depth int) []map[string]any {
	if depth > 4 {
		return nil
	}
	var out []map[string]any
	if m := asMap(v); len(m) > 0 {
		out = append(out, m)
		for _, child := range m {
			out = append(out, candidateNodes(child, depth+1)...)
		}
		return out
	}
	for _, child := range listMaps(v) {
		out = append(out, candidateNodes(child, depth+1)...)
	}
	return out
}

func findPlaybackURL(v any, depth int) string {
	for _, node := range candidateNodes(v, depth) {
		for _, k := range []string{"playbackUrl", "playbackURL", "playUrl", "url", "videoUrl"} {
			s := str(node[k])
			if strings.Contains(s, "baijiayun") || strings.Contains(s, "token=") && (strings.Contains(s, "vid=") || strings.Contains(s, "room_id=") || strings.Contains(s, "classid=")) {
				return s
			}
		}
	}
	return ""
}

func parseBaijiayunPlaybackQuery(raw string) (id, token string, isVOD bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	q := u.Query()
	token = firstQuery(q, "token")
	vid := firstQuery(q, "vid")
	room := firstQuery(q, "room_id", "classid")
	if token != "" && vid != "" {
		return vid, token, true
	}
	if token != "" && room != "" {
		return room, token, false
	}
	return "", "", false
}

func (x *huatuCtx) extractMediaPlaySource(media map[string]any) (*playSource, error) {
	if len(media) == 0 {
		return nil, nil
	}
	size := int64(0)
	if s := atoi(str(asMap(media["basicInfo"])["size"])); s > 0 {
		size = int64(s)
	}
	streaming := asMap(media["streamingInfo"])
	drmToken := str(streaming["drmToken"])
	for _, key := range []string{"drmOutput", "plainOutput"} {
		for _, item := range listMaps(streaming[key]) {
			if u := str(item["url"]); u != "" {
				finalURL, text := x.finalM3U8(u, drmToken)
				extra := map[string]any{"drm_token": drmToken}
				if text != "" {
					extra["m3u8_text"] = text
				}
				return &playSource{URL: firstNonEmpty(finalURL, u), SourceType: "m3u8_text", Size: size, Extra: extra}, nil
			}
		}
	}
	for _, key := range []string{"adaptive_streaming", "video_list"} {
		for _, item := range listMaps(media[key]) {
			if u := str(item["url"]); u != "" {
				return &playSource{URL: u, SourceType: "video_url", Size: size, Extra: map[string]any{}}, nil
			}
		}
	}
	return nil, nil
}

func (x *huatuCtx) finalM3U8(masterURL, drmToken string) (string, string) {
	body, err := x.c.GetString(masterURL, x.streamHeaders())
	if err != nil || !strings.Contains(body, "#EXTM3U") {
		return masterURL, ""
	}
	variantURL := masterURL
	text := body
	if strings.Contains(body, "#EXT-X-STREAM-INF") {
		variantURL = selectVariantURL(body, masterURL)
		if variantURL != masterURL {
			if b, err := x.c.GetString(variantURL, x.streamHeaders()); err == nil {
				text = b
			}
		}
	}
	return variantURL, rewriteM3U8Text(text, variantURL, drmToken)
}

func selectVariantURL(masterText, masterURL string) string {
	type candidate struct {
		bw  int
		url string
	}
	var cs []candidate
	lines := strings.Split(masterText, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			continue
		}
		bw := 0
		if m := regexp.MustCompile(`BANDWIDTH=(\d+)`).FindStringSubmatch(line); len(m) > 1 {
			bw = atoi(m[1])
		}
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, "#") {
				continue
			}
			cs = append(cs, candidate{bw: bw, url: joinURL(masterURL, next)})
			break
		}
	}
	if len(cs) == 0 {
		return masterURL
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].bw > cs[j].bw })
	return cs[0].url
}

func rewriteM3U8Text(text, baseURL, drmToken string) string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "#EXT-X-KEY") {
				line = regexp.MustCompile(`URI="([^"]+)"`).ReplaceAllStringFunc(line, func(match string) string {
					m := regexp.MustCompile(`URI="([^"]+)"`).FindStringSubmatch(match)
					if len(m) < 2 {
						return match
					}
					return fmt.Sprintf(`URI="%s"`, appendToken(joinURL(baseURL, m[1]), drmToken))
				})
			}
			out = append(out, line)
			continue
		}
		out = append(out, joinURL(baseURL, trimmed))
	}
	return strings.Join(out, "\n") + "\n"
}

func joinURL(base, ref string) string {
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
