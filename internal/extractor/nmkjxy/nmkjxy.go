// Package nmkjxy implements an extractor for nmkjxy.com (柠檬云课堂).
package nmkjxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer               = "https://www.nmkjxy.com/"
	origin                = "https://www.nmkjxy.com"
	check_login_url       = "https://api.nmkjxy.com/api/V520/RecentCourse?PageSize=1&PageIndex=1&RecentMonth=false&status=1"
	course_url            = "https://api.nmkjxy.com/api/V520/RecentCourse?PageSize=%s&PageIndex=%s&RecentMonth=false&status=1"
	product_url           = "https://api.nmkjxy.com/api/product/%s"
	video_list_url        = "https://api.nmkjxy.com/api/video/%s"
	courseware_url        = "https://api.nmkjxy.com/api/V310/Courseware/%s"
	legacy_courseware_url = "https://api.nmkjxy.com/api/Courseware/%s"
	recorded_video_url    = "https://apim.ningmengyun.com/api/MyOrder/RecordedVideoCourse?orderSn=%s&productId=%s"
	video_play_url        = "https://apim.ningmengyun.com/api/MyOrder/VideoPlayed?courseId=%s&videoSn=%s"
	video_played_url      = "https://apim.ningmengyun.com/api/MyOrder/VideoPlayed"
)

var patterns = []string{`(?:[\w-]+\.)?nmkjxy\.com/`}

func init() {
	extractor.Register(&Nmkjxy{}, extractor.SiteInfo{Name: "Nmkjxy", URL: "nmkjxy.com", NeedAuth: true})
}

type Nmkjxy struct{}

func (n *Nmkjxy) Patterns() []string { return patterns }

func (n *Nmkjxy) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("nmkjxy requires login cookies")
	}
	cid := parseCID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse nmkjxy courseId/productId from URL")
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := headers(opts.Cookies)
	product, _ := requestJSON(c, fmt.Sprintf(product_url, cid), h)
	title := firstText(dataMap(product), "prodName", "name", "productName", "title")
	if title == "" {
		title = "nmkjxy_" + cid
	}

	listJSON, err := requestJSON(c, fmt.Sprintf(video_list_url, cid), h)
	if err != nil {
		return nil, fmt.Errorf("nmkjxy video list: %w", err)
	}
	items := iterItems(listJSON)
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, item := range items {
		vi := parseVideo(item, cid, i+1)
		if vi.VideoSN == "" && vi.VideoID == "" {
			continue
		}
		if seen[vi.VideoSN+":"+vi.VideoID] {
			continue
		}
		seen[vi.VideoSN+":"+vi.VideoID] = true
		play, _ := requestJSON(c, fmt.Sprintf(video_play_url, cid, url.QueryEscape(first(vi.VideoSN, vi.VideoID))), h)
		playData := dataMap(play)
		picked := pickPlayInfo(playData["playInfoList"])
		playURL := firstText(picked, "playURL")
		if playURL == "" {
			continue
		}
		stream := extractor.Stream{Quality: firstText(picked, "definition"), URLs: []string{playURL}, Format: pickFormat(playURL), Size: sizeBytes(picked["size"]), Headers: map[string]string{"Referer": referer, "Origin": origin}}
		if stream.Quality == "" {
			stream.Quality = "best"
		}
		entries = append(entries, &extractor.MediaInfo{Site: "nmkjxy", Title: vi.Name, Streams: map[string]extractor.Stream{"best": stream}, Subtitles: subtitles(item, playData), Extra: map[string]any{"video_id": vi.VideoID, "video_sn": vi.VideoSN, "video_num": cid, "video_nid": vi.VideoNID}})
	}
	courseware := fetchCourseware(c, h, cid)
	cwSeen := map[string]bool{}
	for i, cw := range courseware {
		fileURL := cw["file_url"]
		if fileURL == "" || cwSeen[fileURL] {
			continue
		}
		cwSeen[fileURL] = true
		name := cw["file_name"]
		if name == "" {
			name = fileNameFromURL(fileURL)
		}
		if name == "" {
			name = fmt.Sprintf("courseware_%02d", i+1)
		}
		format := ext(fileURL, "bin")
		entries = append(entries, &extractor.MediaInfo{
			Site:  "nmkjxy",
			Title: sanitize(name),
			Streams: map[string]extractor.Stream{"file": {
				Quality: "source",
				URLs:    []string{fileURL},
				Format:  format,
				Headers: map[string]string{"Referer": referer, "Origin": origin},
			}},
			Extra: map[string]any{"kind": "file"},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("nmkjxy: no playable videos or courseware files found")
	}
	return &extractor.MediaInfo{Site: "nmkjxy", Title: sanitize(title), Entries: entries}, nil
}

type videoInfo struct{ VideoID, VideoSN, VideoNID, Name string }

func parseVideo(m map[string]any, cid string, fallback int) videoInfo {
	videoID := firstText(m, "videoId", "vodId")
	videoSN := firstText(m, "videoSn", "videoSN", "sectionSn", "id")
	parts := chapterIndexParts(m)
	base := firstText(m, "title", "videoTitle", "name", "videoName", "sectionName")
	if base == "" {
		base = first(videoSN, videoID, fmt.Sprint(fallback))
	}
	return videoInfo{VideoID: videoID, VideoSN: first(videoSN, videoID), VideoNID: firstText(m, "videoNId", "id"), Name: sanitize(fmt.Sprintf("[%s]--%s", formatIndex(parts, fallback), base))}
}

func requestJSON(c *util.Client, api string, h map[string]string) (any, error) {
	body, err := c.GetString(api, h)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func iterItems(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			if looksVideo(t) {
				out = append(out, t)
			}
			for _, k := range []string{"data", "rows", "list", "items", "result"} {
				if y, ok := t[k]; ok {
					walk(y)
				}
			}
		}
	}
	walk(v)
	return out
}
func looksVideo(m map[string]any) bool {
	return firstText(m, "videoId", "vodId", "videoSn", "videoSN") != ""
}
func dataMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		if d, ok := m["data"].(map[string]any); ok {
			return d
		}
		return m
	}
	return map[string]any{}
}

func pickPlayInfo(v any) map[string]any {
	list, ok := v.([]any)
	if !ok {
		return map[string]any{}
	}
	defs := map[string]map[string]any{}
	var playable []map[string]any
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if firstText(m, "playURL") != "" {
				playable = append(playable, m)
				defs[strings.ToUpper(firstText(m, "definition"))] = m
			}
		}
	}
	for _, d := range []string{"HD", "OD", "SD", "LD", "FD"} {
		if m := defs[d]; m != nil {
			return m
		}
	}
	var best map[string]any
	for _, m := range playable {
		if best == nil || sizeBytes(m["size"]) > sizeBytes(best["size"]) {
			best = m
		}
	}
	if best == nil {
		return map[string]any{}
	}
	return best
}

func fetchCourseware(c *util.Client, h map[string]string, cid string) []map[string]string {
	var out []map[string]string
	for _, api := range []string{fmt.Sprintf(courseware_url, cid), fmt.Sprintf(legacy_courseware_url, cid)} {
		v, err := requestJSON(c, api, h)
		if err != nil {
			continue
		}
		for _, m := range iterFiles(dataAny(v)) {
			fileURL := firstText(m, "contentFilePath", "coursewarePath", "coursewareUrl", "handoutPath", "handoutUrl", "lecturePath", "lectureUrl", "materialPath", "materialUrl", "attachmentPath", "attachmentUrl", "filePath", "fileUrl", "downloadUrl", "path", "url")
			if fileURL == "" {
				continue
			}
			out = append(out, map[string]string{"file_url": absURL(fileURL), "file_name": firstText(m, "contentFileName", "coursewareName", "handoutName", "lectureName", "materialName", "attachmentName", "fileName", "name", "title")})
		}
		if len(out) > 0 {
			break
		}
	}
	return out
}
func dataAny(v any) any {
	if m, ok := v.(map[string]any); ok {
		if d, ok := m["data"]; ok {
			return d
		}
	}
	return v
}
func iterFiles(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			if firstText(t, "fileUrl", "downloadUrl", "path", "url", "coursewareUrl", "contentFilePath") != "" {
				out = append(out, t)
			}
			for _, y := range t {
				switch y.(type) {
				case []any, map[string]any:
					walk(y)
				}
			}
		}
	}
	walk(v)
	return out
}

func subtitles(raw, play map[string]any) []extractor.Subtitle {
	seen := map[string]bool{}
	var out []extractor.Subtitle
	for _, m := range []map[string]any{raw, play} {
		for _, k := range []string{"subtitlePath", "subtitleUrl", "subTitlePath", "subTitleUrl", "srtPath", "vttPath"} {
			if u := absURL(firstText(m, k)); u != "" && !seen[u] {
				seen[u] = true
				out = append(out, extractor.Subtitle{Language: "字幕", URL: u, Format: ext(u, "srt")})
			}
		}
	}
	return out
}

func headers(j http.CookieJar) map[string]string {
	h := map[string]string{"Referer": referer, "Origin": origin, "Accept": "application/json, text/plain, */*", "Author": "ningmengyun"}
	if tok := tokenFromJar(j); tok != "" {
		h["Authorization"] = "Bearer " + tok
	}
	return h
}
func tokenFromJar(j http.CookieJar) string {
	if j == nil {
		return ""
	}
	for _, host := range []string{"https://www.nmkjxy.com/", "https://api.nmkjxy.com/", "https://apim.ningmengyun.com/"} {
		u, _ := url.Parse(host)
		for _, ck := range j.Cookies(u) {
			if t := parseToken(ck.Name, ck.Value); t != "" {
				return t
			}
		}
	}
	return ""
}
func parseToken(name, val string) string {
	v := strings.TrimSpace(val)
	if strings.EqualFold(name, "Authorization") && strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	if strings.HasPrefix(strings.ToLower(v), "token:") {
		v = strings.TrimSpace(v[6:])
	}
	if u, err := url.QueryUnescape(v); err == nil {
		v = u
	}
	if strings.HasPrefix(strings.TrimSpace(v), "{") {
		var m map[string]any
		if json.Unmarshal([]byte(v), &m) == nil {
			return firstText(m, "access_token", "accessToken", "token")
		}
	}
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		v = strings.TrimSpace(v[7:])
	}
	if strings.Contains(v, "=") && !strings.EqualFold(name, "token") && !strings.EqualFold(name, "Token") {
		return ""
	}
	return strings.TrimSpace(v)
}

var cidRe = regexp.MustCompile(`(?i)[?&](?:courseId|productId|prodId)=([0-9]+)`)

func parseCID(raw string) string {
	if m := cidRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}
func chapterIndexParts(m map[string]any) []string {
	p := firstText(m, "parentChapterSn", "parentChapterNum", "bigChapterSn", "bigChapterNum", "stageSn", "stageNum", "moduleSn", "moduleNum")
	c := firstText(m, "chapterSn", "chapterNum", "sectionSn", "sectionNum", "unitSn", "unitNum")
	if c == "" {
		c = "1"
	}
	if p != "" {
		return []string{p, c}
	}
	return []string{c}
}
func formatIndex(parts []string, fallback int) string {
	if len(parts) == 0 {
		return fmt.Sprint(fallback)
	}
	return strings.Join(parts, ".")
}
func firstText(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func absURL(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err == nil && u.IsAbs() {
		return s
	}
	b, _ := url.Parse(referer)
	r, _ := url.Parse(s)
	return b.ResolveReference(r).String()
}
func ext(u, def string) string {
	p := strings.ToLower(strings.Split(strings.Split(u, "?")[0], "#")[0])
	if i := strings.LastIndex(p, "."); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return def
}
func sizeBytes(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return int64(f)
	}
	return 0
}
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}

var badName = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func fileNameFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := u.Path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if decoded, err := url.QueryUnescape(base); err == nil {
		base = decoded
	}
	return strings.TrimSpace(base)
}

func sanitize(s string) string {
	s = badName.ReplaceAllString(strings.TrimSpace(s), "_")
	if s == "" {
		return "未命名视频"
	}
	return s
}
