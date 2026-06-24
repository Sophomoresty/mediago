package ahu

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	course_list_url = "https://www.ahuyikao.com/center/mycourse.html"
	course_info_url = "https://www.ahuyikao.com/course/courseinfo.html?courseId=%s"
	video_play_url  = "https://www.ahuyikao.com/video/videoplay.html?courseId=%s&lessonId=%s#%s"
)

var patterns = []string{`(?:[\w-]+\.)*(?:ahuyikao|ahumooc)\.com/`}

func init() {
	extractor.Register(&Ahu{}, extractor.SiteInfo{Name: "Ahu", URL: "ahuyikao.com", NeedAuth: true})
}

type Ahu struct{}

func (a *Ahu) Patterns() []string { return patterns }

var (
	cidRe        = regexp.MustCompile(`(?i)(?:courseId|course_id)=([0-9]+)|/course/(?:courseinfo\.html)?/?([0-9]+)`)
	lessonIDRe   = regexp.MustCompile(`(?i)(?:lessonId|lesson_id)=([0-9]+)`)
	titleRe      = regexp.MustCompile(`(?is)<(?:h4|h1)[^>]*>(.*?)</(?:h4|h1)>|<title[^>]*>(.*?)</title>`)
	lessonLinkRe = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']*/video/videoplay\.html\?[^"']*lessonId=[0-9][^"']*)["'][^>]*>(.*?)</a>`)
	jsVarRe      = regexp.MustCompile(`(?is)var\s+%s\s*=\s*["']([^"']+)["']`)
	directURLRe  = regexp.MustCompile(`(?is)var\s+(?:videoSrc|m3u8_url)\s*=\s*["']([^"']+)["']`)
)

func (a *Ahu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("ahu requires login cookies")
	}

	cid := extractFirst(cidRe, rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse courseId from URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := map[string]string{
		"Referer": course_list_url,
		"referer": course_list_url,
	}

	if lessonID := extractFirst(lessonIDRe, rawURL); lessonID != "" {
		stream, err := resolveLesson(c, headers, cid, lessonID)
		if err != nil {
			return nil, err
		}
		return &extractor.MediaInfo{
			Site:  "ahu",
			Title: "ahu_" + cid + "_" + lessonID,
			Streams: map[string]extractor.Stream{
				"best": stream,
			},
		}, nil
	}

	detailURL := fmt.Sprintf(course_info_url, cid)
	body, err := c.GetString(detailURL, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch ahu course info: %w", err)
	}

	title := firstNonEmpty(extractTitle(body), "ahu_"+cid)
	lessons := parseLessons(body)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("ahu: no lessons found in course page")
	}

	entries := make([]*extractor.MediaInfo, 0, len(lessons))
	for i, lesson := range lessons {
		stream, err := resolveLesson(c, headers, cid, lesson.ID)
		if err != nil {
			continue
		}
		entryTitle := firstNonEmpty(lesson.Title, fmt.Sprintf("%02d %s", i+1, lesson.ID))
		entries = append(entries, &extractor.MediaInfo{
			Site:  "ahu",
			Title: util.SanitizeFilename(entryTitle),
			Streams: map[string]extractor.Stream{
				"best": stream,
			},
			Extra: map[string]any{"course_id": cid, "lesson_id": lesson.ID},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("ahu: no playable video URLs found in parsed lessons")
	}

	return &extractor.MediaInfo{Site: "ahu", Title: util.SanitizeFilename(title), Entries: entries}, nil
}

type lessonRef struct {
	ID    string
	Title string
}

func parseLessons(body string) []lessonRef {
	seen := map[string]bool{}
	var lessons []lessonRef
	for _, m := range lessonLinkRe.FindAllStringSubmatch(body, -1) {
		id := extractFirst(lessonIDRe, html.UnescapeString(m[1]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		lessons = append(lessons, lessonRef{
			ID:    id,
			Title: cleanText(stripTags(m[2])),
		})
	}
	return lessons
}

func resolveLesson(c *util.Client, headers map[string]string, cid, lessonID string) (extractor.Stream, error) {
	playURL := fmt.Sprintf(video_play_url, cid, lessonID, lessonID)
	playHeaders := cloneHeaders(headers)
	playHeaders["Referer"] = fmt.Sprintf(course_info_url, cid)
	playHeaders["referer"] = fmt.Sprintf(course_info_url, cid)
	body, err := c.GetString(playURL, playHeaders)
	if err != nil {
		return extractor.Stream{}, fmt.Errorf("fetch ahu play page: %w", err)
	}

	if direct := normalizeResourceURL(extractFirst(directURLRe, body)); direct != "" {
		return mediaStream(direct, playHeaders), nil
	}

	videoID := firstNonEmpty(jsVar(body, "aliyunVideoId"), jsVar(body, "vodVideoId"), jsVar(body, "aliyunVid"))
	playAuth := jsVar(body, "playAuth")
	if videoID != "" && playAuth != "" {
		mediaURL, err := requestAliyunPlayInfo(c, videoID, playAuth, playHeaders)
		if err == nil && mediaURL != "" {
			return mediaStream(mediaURL, playHeaders), nil
		}
	}

	// Baijiayun playback flow (source _download_baijiayun_playback):
	// extract hlsToken/playId from page, call shared.BaijiayunResolvePlayback.
	hlsToken := jsVar(body, "hlsToken")
	roomID := firstNonEmpty(jsVar(body, "roomId"), jsVar(body, "room_id"))
	if hlsToken != "" && roomID != "" {
		playbackURL, err := shared.BaijiayunResolvePlayback(c, roomID, hlsToken, playHeaders)
		if err == nil && playbackURL != "" {
			return mediaStream(playbackURL, playHeaders), nil
		}
	}

	// Source also records hlsToken/playId/baijiayun markers; if no direct or
	// Aliyun or Baijiayun media URL is present, there is no downloadable stream.
	return extractor.Stream{}, fmt.Errorf("ahu: no direct/aliyun/baijiayun media URL for lesson %s", lessonID)
}

type aliyunPlayAuth struct {
	AccessKeyID     string `json:"AccessKeyId"`
	AccessKeySecret string `json:"AccessKeySecret"`
	SecurityToken   string `json:"SecurityToken"`
	Region          string `json:"Region"`
	AuthInfo        any    `json:"AuthInfo"`
	AuthTimeout     any    `json:"AuthTimeout"`

	AccessKeyIDAlt     string `json:"accessKeyId"`
	AccessKeySecretAlt string `json:"accessKeySecret"`
	SecurityTokenAlt   string `json:"securityToken"`
	RegionAlt          string `json:"region"`
	AuthInfoAlt        any    `json:"authInfo"`
	AuthTimeoutAlt     any    `json:"authTimeout"`
}

type aliyunPlayInfoResp struct {
	PlayInfoList struct {
		PlayInfo []struct {
			PlayURL    string `json:"PlayURL"`
			Definition string `json:"Definition"`
			Format     string `json:"Format"`
		} `json:"PlayInfo"`
	} `json:"PlayInfoList"`
}

func requestAliyunPlayInfo(c *util.Client, videoID, playAuth string, headers map[string]string) (string, error) {
	payload, err := decodeAliyunPlayAuth(playAuth)
	if err != nil {
		return "", err
	}
	accessID := firstNonEmpty(payload.AccessKeyID, payload.AccessKeyIDAlt)
	accessSecret := firstNonEmpty(payload.AccessKeySecret, payload.AccessKeySecretAlt)
	region := firstNonEmpty(payload.Region, payload.RegionAlt, "cn-shanghai")
	authInfo := firstNonEmpty(anyString(payload.AuthInfo), anyString(payload.AuthInfoAlt))
	authTimeout := firstNonEmpty(anyString(payload.AuthTimeout), anyString(payload.AuthTimeoutAlt), "7200")
	if accessID == "" || accessSecret == "" || authInfo == "" {
		return "", fmt.Errorf("ahu aliyun playAuth missing access/authInfo")
	}

	params := map[string]string{
		"AccessKeyId":      accessID,
		"Action":           "GetPlayInfo",
		"AuthInfo":         authInfo,
		"AuthTimeout":      authTimeout,
		"Format":           "JSON",
		"Formats":          "m3u8,mp4",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   nonceHex(),
		"SignatureVersion": "1.0",
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          "2017-03-21",
		"VideoId":          videoID,
	}
	if token := firstNonEmpty(payload.SecurityToken, payload.SecurityTokenAlt); token != "" {
		params["SecurityToken"] = token
	}
	params["Signature"] = aliyunSignature(params, accessSecret)

	u := fmt.Sprintf("https://vod.%s.aliyuncs.com/?%s", region, sortedQuery(params))
	body, err := c.GetString(u, headers)
	if err != nil {
		return "", fmt.Errorf("ahu aliyun GetPlayInfo: %w", err)
	}
	var resp aliyunPlayInfoResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", fmt.Errorf("parse ahu aliyun GetPlayInfo: %w", err)
	}
	sort.SliceStable(resp.PlayInfoList.PlayInfo, func(i, j int) bool {
		return qualityRank(resp.PlayInfoList.PlayInfo[i].Definition) > qualityRank(resp.PlayInfoList.PlayInfo[j].Definition)
	})
	for _, item := range resp.PlayInfoList.PlayInfo {
		if item.PlayURL != "" {
			return normalizeResourceURL(item.PlayURL), nil
		}
	}
	return "", fmt.Errorf("ahu aliyun GetPlayInfo returned no PlayURL")
}

func decodeAliyunPlayAuth(playAuth string) (aliyunPlayAuth, error) {
	var payload aliyunPlayAuth
	raw := strings.TrimSpace(playAuth)
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return payload, err
		}
		return payload, nil
	}
	raw += strings.Repeat("=", (4-len(raw)%4)%4)
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(raw)
	}
	if err != nil {
		return payload, err
	}
	return payload, json.Unmarshal(decoded, &payload)
}

func aliyunSignature(params map[string]string, secret string) string {
	stringToSign := "GET&%2F&" + percentEncode(sortedQuery(params))
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func sortedQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, percentEncode(k)+"="+percentEncode(params[k]))
	}
	return strings.Join(parts, "&")
}

func percentEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func nonceHex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func jsVar(body, name string) string {
	re := regexp.MustCompile(fmt.Sprintf(jsVarRe.String(), regexp.QuoteMeta(name)))
	return html.UnescapeString(extractFirst(re, body))
}

func mediaStream(u string, headers map[string]string) extractor.Stream {
	return extractor.Stream{
		Quality: "best",
		URLs:    []string{u},
		Format:  pickFormat(u),
		Headers: cloneHeaders(headers),
	}
}

func extractTitle(body string) string {
	for _, m := range titleRe.FindAllStringSubmatch(body, -1) {
		for _, g := range m[1:] {
			if s := cleanText(stripTags(g)); s != "" {
				return strings.TrimSuffix(s, "_阿虎医考")
			}
		}
	}
	return ""
}

func extractFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

func cleanText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

func stripTags(s string) string {
	return regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, " ")
}

func normalizeResourceURL(s string) string {
	s = strings.TrimSpace(html.UnescapeString(strings.ReplaceAll(s, `\/`, `/`)))
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	return s
}

func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}

func qualityRank(q string) int {
	switch strings.ToUpper(q) {
	case "4K":
		return 6
	case "2K", "OD":
		return 5
	case "HD":
		return 4
	case "SD":
		return 3
	case "LD":
		return 2
	case "FD":
		return 1
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func cloneHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}
