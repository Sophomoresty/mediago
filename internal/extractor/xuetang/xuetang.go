// Package xuetang implements an extractor for next.xuetangx.com courses.
//
// API chain ported from decompiled Mooc/Courses/Xuetang/Xuetang_Course.pyc:
//  1. /api/v1/lms/learn/product/info?cid=&sign=    → classroom_name (course title)
//  2. /api/v1/lms/learn/course/chapter?cid=&sign=  → section/leaf tree (chapter list)
//  3. /api/v1/lms/learn/leaf_info/{cid}/{leaf_id}/?sign={sign} → content_info.media.ccid
//  4. /api/v1/lms/service/playurl/{ccid}/?appid=10000 → data.sources.quality10/quality20 (mp4 URLs)
//
// Sign + cid are pulled out of the URL ("/course/SIGN/CID" or "/learn[/space]/SIGN/.../CID").
// Supports xuetangx.com, cmgemooc.com, gradsmartedu.cn.
//
// Python source input examples kept for source-alignment audit and route
// regression coverage:
//   - https://www.xuetangx.com/course/xjtu08301000528/12424483?channel=i.area.learn_title
//   - https://next.xuetangx.com/course/szpt08071002217/26284632?channel=i.area.learn_title
//   - https://next.xuetangx.com/live/live20191205/live20191205001/1480012/1150601
//   - https://next.xuetangx.com/live/live20200611M001/live20200611M001/4127460/5786325?fromArray=home_live_ad
//   - https://www.xuetangx.com/training/NLP080910033761/16862187
package xuetang

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

var patterns = []string{
	`(?:[\w-]+\.)*(?:xuetangx\.com|cmgemooc\.com|gradsmartedu\.cn)/.*?(?:course|learn(?:/space)?|live|training)/`,
}

// URL forms ported from Mooc_Config.courses_re['Xuetang_Course']:
//
//	/course/{sign}/{cid}
//	/learn[/space]/{sign}/.../{cid}
var (
	urlCourseRe   = regexp.MustCompile(`https?://([^/]+)/.*?course/([^/]+)/(\d+)`)
	urlLearnRe    = regexp.MustCompile(`https?://([^/]+)/.*?learn(?:/space)?/([^/]+)/.*?/(\d+)`)
	urlLiveRe     = regexp.MustCompile(`https?://([^/]+)/.*?live/([^/]+)/[^/]*/(\d+)/(\d+)`)
	urlTrainingRe = regexp.MustCompile(`https?://([^/]+)/.*?training/([^/]+)/(\d+)`)
)

type xuetangURLKind string

const (
	xuetangURLCourse   xuetangURLKind = "course"
	xuetangURLLive     xuetangURLKind = "live"
	xuetangURLTraining xuetangURLKind = "training"
)

type xuetangURLParts struct {
	kind xuetangURLKind
	host string
	sign string
	cid  string
	tid  string
}

func init() {
	extractor.Register(&Xuetang{}, extractor.SiteInfo{
		Name:     "Xuetang",
		URL:      "next.xuetangx.com",
		NeedAuth: true,
	})
}

type Xuetang struct{}

func (x *Xuetang) Patterns() []string { return patterns }

func (x *Xuetang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xuetang requires login cookies (use --cookies or --cookies-from-browser)")
	}

	parts := parseURL(rawURL)
	if parts.host == "" || parts.sign == "" {
		return nil, fmt.Errorf("cannot parse xuetang URL: %s", rawURL)
	}
	base := xuetangOrigin(parts.host)

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := xuetangHeaders(opts.Cookies, rawURL, base)

	if parts.kind == xuetangURLLive {
		return extractLive(c, base, h, parts)
	}
	if parts.kind == xuetangURLTraining {
		cid, err := fetchTrainingClassroomID(c, base, h, parts.sign)
		if err != nil {
			return nil, err
		}
		parts.cid = cid
	}
	if parts.cid == "" {
		return nil, fmt.Errorf("cannot parse xuetang classroom id from URL: %s", rawURL)
	}

	return extractCourse(c, base, h, parts.sign, parts.cid, xuetangOnlyFilesMode(opts.Quality))
}

func extractCourse(c *util.Client, base string, h map[string]string, sign, cid string, onlyFiles bool) (*extractor.MediaInfo, error) {
	titleBody, _ := c.GetString(fmt.Sprintf("%s/api/v1/lms/learn/product/info?cid=%s&sign=%s", base, cid, sign), h)
	title := matchGroup1(titleBody, `"classroom_name"\s*:\s*"([^"]+)"`)
	if title == "" {
		title = "xuetang_" + cid
	}

	chapterRoot, err := fetchCourseChapterPayload(c, base, h, sign, cid)
	if err != nil {
		return nil, err
	}
	leafs := extractCourseLeaves(chapterRoot)

	var entries []*extractor.MediaInfo
	for _, leaf := range leafs {
		src := resolveLeafSource(c, base, h, sign, cid, leaf.ID, onlyFiles)
		if src == nil || src.empty() {
			continue
		}
		name := fmt.Sprintf("%02d.%02d.%02d %s", leaf.ChapterIndex, leaf.SectionIndex, leaf.LeafIndex, sanitize(firstNonEmpty(leaf.Name, src.Title, "Leaf "+leaf.ID)))
		entries = append(entries, mediaFromSource(base, h, name, leaf.ID, src, onlyFiles)...)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no playable media found (course locked or no purchase?)")
	}

	return &extractor.MediaInfo{
		Site:    "xuetang",
		Title:   title,
		Entries: entries,
	}, nil
}

func xuetangOnlyFilesMode(quality string) bool {
	mode := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(quality)))
	switch mode {
	case "3", "pdf", "onlypdf", "file", "files", "material", "materials", "courseware", "coursewares", "attachment", "attachments", "资料", "课件":
		return true
	default:
		return false
	}
}

func extractLive(c *util.Client, base string, h map[string]string, parts xuetangURLParts) (*extractor.MediaInfo, error) {
	body, err := c.GetString(fmt.Sprintf("%s/api/v1/lms/learn/live_info/%s/%s/?sign=%s", base, parts.cid, parts.tid, parts.sign), h)
	if err != nil {
		return nil, fmt.Errorf("live_info: %w", err)
	}

	var resp struct {
		Data struct {
			LeafData struct {
				Name        string `json:"name"`
				ContentInfo struct {
					Media struct {
						LivePlaybackURL string `json:"live_palyback_url"`
					} `json:"media"`
				} `json:"content_info"`
			} `json:"leaf_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parse live_info: %w", err)
	}

	videoURL := strings.TrimSpace(resp.Data.LeafData.ContentInfo.Media.LivePlaybackURL)
	if videoURL != "" && !isDirectMediaURL(videoURL) {
		videoURL = getLiveVideoURL(c, base, h, videoURL)
	}
	if videoURL == "" {
		return nil, fmt.Errorf("xuetang live %s/%s returned no playback URL", parts.cid, parts.tid)
	}

	title := sanitize(resp.Data.LeafData.Name)
	if title == "" {
		title = "xuetang_live_" + parts.tid
	}
	return &extractor.MediaInfo{
		Site:  "xuetang",
		Title: title,
		Streams: map[string]extractor.Stream{
			"default": {
				Quality: "best",
				URLs:    []string{videoURL},
				Format:  pickFormat(videoURL),
				Headers: map[string]string{"Referer": base + "/"},
			},
		},
		Extra: map[string]any{
			"type": "live",
			"cid":  parts.cid,
			"tid":  parts.tid,
			"sign": parts.sign,
		},
	}, nil
}

func fetchTrainingClassroomID(c *util.Client, base string, h map[string]string, sign string) (string, error) {
	body, err := c.GetString(fmt.Sprintf("%s/api/v1/lms/learn/training/camp/classrooms/?sign=%s", base, sign), h)
	if err != nil {
		return "", fmt.Errorf("training classrooms: %w", err)
	}
	var resp struct {
		Data []struct {
			ClassroomID any `json:"classroom_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", fmt.Errorf("parse training classrooms: %w", err)
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("xuetang training %s returned no classroom", sign)
	}
	cid := jsonScalarString(resp.Data[0].ClassroomID)
	if cid == "" {
		return "", fmt.Errorf("xuetang training %s returned empty classroom_id", sign)
	}
	return cid, nil
}

// getVideoURL implements _get_signature → _get_video_url:
//
//	leaf_info/{cid}/{leaf}/?sign={sign} → data.content_info.media.ccid
//	service/playurl/{ccid}/?appid=10000 → data.sources.quality10/20 (mp4 URLs)
func getVideoURL(c *util.Client, base string, h map[string]string, sign, cid, leafID string) string {
	src := resolveLeafSource(c, base, h, sign, cid, leafID, false)
	if src == nil {
		return ""
	}
	return src.URL
}

func getLiveVideoURL(c *util.Client, base string, h map[string]string, signature string) string {
	if signature == "" {
		return ""
	}
	liveURL := fmt.Sprintf("%s/api/v1/lms/service/video2ccsource/%s/", base, url.PathEscape(signature))
	body, err := c.GetString(liveURL, h)
	if err != nil {
		return ""
	}
	var resp struct {
		Data struct {
			Video []struct {
				Quality any    `json:"quality"`
				PlayURL string `json:"playurl"`
			} `json:"video"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ""
	}
	var q10, q20, first string
	for _, video := range resp.Data.Video {
		if video.PlayURL == "" {
			continue
		}
		if first == "" {
			first = video.PlayURL
		}
		switch jsonScalarString(video.Quality) {
		case "20":
			q20 = video.PlayURL
		case "10":
			q10 = video.PlayURL
		}
	}
	if q20 != "" {
		return q20
	}
	if q10 != "" {
		return q10
	}
	return first
}

func parseURL(u string) xuetangURLParts {
	if m := urlCourseRe.FindStringSubmatch(u); m != nil {
		return xuetangURLParts{kind: xuetangURLCourse, host: m[1], sign: m[2], cid: m[3]}
	}
	if m := urlLearnRe.FindStringSubmatch(u); m != nil {
		return xuetangURLParts{kind: xuetangURLCourse, host: m[1], sign: m[2], cid: m[3]}
	}
	if m := urlLiveRe.FindStringSubmatch(u); m != nil {
		return xuetangURLParts{kind: xuetangURLLive, host: m[1], sign: m[2], cid: m[3], tid: m[4]}
	}
	if m := urlTrainingRe.FindStringSubmatch(u); m != nil {
		return xuetangURLParts{kind: xuetangURLTraining, host: m[1], sign: m[2]}
	}
	return xuetangURLParts{}
}

func matchGroup1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

var sanitizeRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func sanitize(s string) string { return sanitizeRe.ReplaceAllString(strings.TrimSpace(s), "_") }

func pickFormat(u string) string {
	lower := strings.ToLower(u)
	if strings.HasPrefix(lower, "data:text/html") {
		return "html"
	}
	if strings.Contains(lower, ".m3u8") || strings.HasPrefix(lower, "data:application/vnd.apple.mpegurl") {
		return "m3u8"
	}
	if strings.Contains(lower, ".mp3") || strings.Contains(lower, ".m4a") || strings.Contains(lower, ".aac") || strings.Contains(lower, ".wav") {
		return "audio"
	}
	for _, ext := range []string{"pdf", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "zip", "rar", "7z", "txt", "csv"} {
		if strings.Contains(lower, "."+ext) {
			return ext
		}
	}
	return "mp4"
}

func isDirectMediaURL(u string) bool {
	lower := strings.ToLower(u)
	return strings.Contains(lower, ".mp4") || strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp3") || strings.Contains(lower, ".m4a") || strings.Contains(lower, ".aac") || strings.Contains(lower, ".wav")
}

func jsonScalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return strings.TrimSpace(t.String())
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%f", t)
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	default:
		s := strings.TrimSpace(fmt.Sprint(t))
		if s == "<nil>" {
			return ""
		}
		return s
	}
}
