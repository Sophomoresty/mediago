// Package itbaizhan implements source-aligned Itbaizhan course extraction.
package itbaizhan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	COURSENAME                       = "{1}--课程"
	FILENAME                         = "{2}--资料"
	IS_FHD                           = 1
	IS_HD                            = 2
	IS_SD                            = 3
	ONLY_PDF                         = 4
	ITBAIZHAN_FIXED_COURSE_PRICE     = 999
	USER_AGENT                       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0"
	referer                          = "https://www.itbaizhan.com/"
	origin                           = "https://www.itbaizhan.com"
	navlist_url                      = "https://www.itbaizhan.com/index/stage/navlist?id={course_id}&stage=0"
	stage_url                        = "https://www.itbaizhan.com/index/stage/rightlist?id={stage_id}"
	play_url                         = "https://www.itbaizhan.com/course/id/{course_id}.html"
	check_url                        = "https://www.itbaizhan.com/index_new/index/checkUserLogin"
	course_list_url                  = "https://www.itbaizhan.com/mine/courseschedule"
	permission_check_limit           = 1
	permission_check_timeout_seconds = 3
)

var patterns = []string{`\s*((https?://(?:www\.)?itbaizhan\.com/(?:course/id/(?P<video_id>\d+)\.html|stages/id/(?P<cid1>\d+)|course/(?P<slug>[\w-]+)|user/[\w.%-]+|vips|bzvip\.html)[^\s]*)|(?P<itbaizhan_url>https?://(?:www\.)?itbaizhan\.com/?[^\s]*)|(?P<itbaizhan_name>itbaizhan|百战未来|百战程序员|百战))`}

func init() {
	extractor.Register(&Itbaizhan{}, extractor.SiteInfo{Name: "Itbaizhan", URL: "itbaizhan.com", NeedAuth: true})
}

type Itbaizhan struct{}

func (i *Itbaizhan) Patterns() []string { return patterns }

type itbzCtx struct {
	c       *util.Client
	headers map[string]string
	cookie  string
	cid     string
	title   string
}

type stageInfo struct {
	Identity any `json:"identity"`
	Type     struct {
		TypeName any `json:"type_name"`
	} `json:"type"`
	Specific []specificInfo `json:"specific"`
}

type specificInfo struct {
	SID      any            `json:"s_id"`
	SName    any            `json:"s_name"`
	Child    []videoChild   `json:"child"`
	Training []trainingInfo `json:"training"`
}

type videoChild struct {
	CourseID   any `json:"course_id"`
	CourseName any `json:"course_name"`
	VideoTime  any `json:"video_time"`
	InputTime  any `json:"input_time"`
	IsFree     any `json:"is_free"`
	Free       any `json:"free"`
}

type trainingInfo struct {
	TID   any `json:"t_id"`
	TName any `json:"t_name"`
}

type courseRef struct{ CourseID, VideoID, Slug string }

type itbzVideo struct {
	Name       string
	VideoID    string
	PolyvVID   string
	Playsafe   string
	StageID    string
	ChapterID  string
	StageIndex int
	Extra      map[string]any
}

type itbzFile struct{ Name, URL, Fmt, FileID string }

func (i *Itbaizhan) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("itbaizhan requires login cookies")
	}
	x := newCtx(opts.Cookies)
	if err := x.checkCookie(); err != nil {
		return nil, err
	}
	ref := parseCourseRef(rawURL)
	if ref.VideoID != "" && ref.CourseID == "" {
		return x.resolveSingleLesson(ref.VideoID)
	}
	x.cid = firstNonEmpty(ref.CourseID, x.courseIDForSlug(rawURL, ref.Slug))
	if x.cid == "" {
		x.cid, x.title = x.firstPurchasedCourse()
	}
	if x.cid == "" {
		return nil, fmt.Errorf("itbaizhan: cannot parse course id from URL or purchased course list")
	}

	stageIDs, err := x.loadTitleAndStages()
	if err != nil {
		return nil, err
	}
	if len(stageIDs) == 0 {
		stageIDs = []string{x.cid}
	}
	videos, files, err := x.loadInfos(stageIDs)
	if err != nil {
		return nil, err
	}
	return x.mediaFromItems(videos, files)
}

func newCtx(jar http.CookieJar) *itbzCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{referer, origin + "/"})
	headers := map[string]string{
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/javascript, */*; q=0.01",
		"Origin":           origin,
		"Referer":          referer,
		"User-Agent":       USER_AGENT,
		"cookie":           cookie,
	}
	return &itbzCtx{c: c, headers: headers, cookie: cookie}
}

func (x *itbzCtx) checkCookie() error {
	if strings.TrimSpace(x.cookie) == "" {
		return fmt.Errorf("itbaizhan: missing login cookie")
	}
	body, err := x.c.GetString(check_url, x.headers)
	if err != nil {
		return err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return fmt.Errorf("parse checkUserLogin: %w", err)
	}
	if str(root["code"]) == "1" || str(root["user_id"]) != "" {
		return nil
	}
	return fmt.Errorf("itbaizhan cookie check failed: code=%v", root["code"])
}

func (x *itbzCtx) getJSON(raw string, out any) error {
	body, err := x.c.GetString(raw, x.headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("parse %s: %w", raw, err)
	}
	return nil
}

func (x *itbzCtx) requestText(raw string, headers map[string]string) (string, error) {
	h := cloneHeaders(x.headers)
	for k, v := range headers {
		h[k] = v
	}
	return x.c.GetString(raw, h)
}

func (x *itbzCtx) loadTitleAndStages() ([]string, error) {
	navIDs := x.getNavStageIDs()
	pageURL := fmt.Sprintf(strings.ReplaceAll(play_url, "{course_id}", "%s"), url.PathEscape(x.cid))
	body, err := x.requestText(pageURL, map[string]string{"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"})
	if err == nil && body != "" {
		if title := extractTitle(body); title != "" {
			x.title = title
		}
		if htmlIDs := extractStageIDs(body, x.cid); len(htmlIDs) > 0 {
			navIDs = mergeUnique(navIDs, htmlIDs...)
		}
	}
	if len(navIDs) == 0 {
		return []string{x.cid}, nil
	}
	return navIDs, nil
}

func (x *itbzCtx) getNavStageIDs() []string {
	if x.cid == "" {
		return nil
	}
	raw := strings.ReplaceAll(navlist_url, "{course_id}", url.QueryEscape(x.cid))
	var root map[string]any
	if err := x.getJSON(raw, &root); err != nil {
		return nil
	}
	var ids []string
	seen := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if id := str(t["id"]); id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			walk(t["child"])
			walk(t["children"])
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(root["nav"])
	return ids
}

func (x *itbzCtx) loadInfos(stageIDs []string) ([]itbzVideo, []itbzFile, error) {
	var videos []itbzVideo
	var files []itbzFile
	for si, stageID := range stageIDs {
		var st stageInfo
		raw := strings.ReplaceAll(stage_url, "{stage_id}", url.QueryEscape(stageID))
		if err := x.getJSON(raw, &st); err != nil {
			return nil, nil, err
		}
		stageName := firstNonEmpty(str(st.Type.TypeName), fmt.Sprintf("阶段%d", si+1))
		for ci, ch := range st.Specific {
			chapterName := firstNonEmpty(str(ch.SName), fmt.Sprintf("章节%d", ci+1))
			prefix := fmt.Sprintf("{%d}--%s/{%d}--%s", si+1, cleanTitle(stageName), ci+1, cleanTitle(chapterName))
			for vi, child := range ch.Child {
				if v := parseVideoInfo(child, []int{si + 1, ci + 1, vi + 1}, stageID, str(ch.SID), prefix); v.VideoID != "" {
					videos = append(videos, v)
				}
			}
			for fi, tr := range ch.Training {
				if f := parseFileInfo(tr, []int{si + 1, ci + 1, fi + 1}, prefix); f.FileID != "" {
					files = append(files, f)
				}
			}
		}
	}
	if len(videos) == 0 && len(files) == 0 {
		return nil, nil, fmt.Errorf("itbaizhan: empty rightlist resources")
	}
	return videos, files, nil
}

func (x *itbzCtx) resolveSingleLesson(videoID string) (*extractor.MediaInfo, error) {
	v := itbzVideo{VideoID: videoID, Name: videoID, Extra: map[string]any{"source": "course_page"}}
	entry, err := x.resolveVideo(v)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func (x *itbzCtx) resolveVideo(v itbzVideo) (*extractor.MediaInfo, error) {
	if v.PolyvVID == "" || v.Playsafe == "" {
		play, err := x.getPlayInfo(v.VideoID)
		if err != nil {
			return nil, err
		}
		v.PolyvVID = firstNonEmpty(v.PolyvVID, play.PolyvVID)
		v.Playsafe = firstNonEmpty(v.Playsafe, play.Playsafe)
		v.Name = firstNonEmpty(v.Name, play.Title, v.VideoID)
	}
	if v.PolyvVID == "" {
		return nil, fmt.Errorf("itbaizhan: empty polyv vid for %s", v.VideoID)
	}
	polyvHeaders := map[string]string{"Accept": "application/json, text/plain, */*", "Origin": origin, "Referer": referer, "User-Agent": USER_AGENT}
	sec, err := shared.PolyvResolveSecure(x.c, formatPolyvVID(v.PolyvVID), polyvHeaders)
	if err != nil {
		return nil, err
	}
	mediaURL, err := shared.PolyvPickBestManifest(sec)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(urlPath(mediaURL)), ".pdx") {
		return nil, fmt.Errorf("polyv pdx: blocked needs DRM JS engine")
	}
	extra := map[string]any{"video_id": v.VideoID, "polyv_vid": v.PolyvVID, "stage_id": v.StageID, "chapter_id": v.ChapterID}
	for k, val := range v.Extra {
		extra[k] = val
	}
	token := firstNonEmpty(v.Playsafe, sec.Data.Playsafe.Token)
	if strings.Contains(mediaURL, ".m3u8") {
		if text, err := x.c.GetString(mediaURL, map[string]string{"Referer": referer}); err == nil {
			if rewritten, err := shared.PolyvRewriteM3U8Keys(x.c, text, token, referer); err == nil {
				extra["m3u8_text"] = rewritten
			}
		}
	}
	format := extFormat(mediaURL)
	if format == "" {
		format = "m3u8"
	}
	return &extractor.MediaInfo{Site: "itbaizhan", Title: firstNonEmpty(v.Name, v.VideoID), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, NeedMerge: format == "m3u8", Headers: map[string]string{"Referer": referer}}}, Extra: extra}, nil
}

func (x *itbzCtx) getPlayInfo(videoID string) (playInfo, error) {
	raw := strings.ReplaceAll(play_url, "{course_id}", url.PathEscape(videoID))
	body, err := x.requestText(raw, map[string]string{"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"})
	if err != nil {
		return playInfo{}, err
	}
	return parsePlayInfo(body), nil
}

func (x *itbzCtx) mediaFromItems(videos []itbzVideo, files []itbzFile) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	var lastErr error
	for _, v := range videos {
		entry, err := x.resolveVideo(v)
		if err != nil {
			lastErr = err
			continue
		}
		entries = append(entries, entry)
	}
	for _, f := range files {
		name := firstNonEmpty(f.Name, f.FileID)
		entries = append(entries, &extractor.MediaInfo{Site: "itbaizhan", Title: name, Streams: map[string]extractor.Stream{f.Fmt: {Quality: f.Fmt, URLs: []string{f.URL}, Format: f.Fmt, Headers: cloneHeaders(x.headers)}}, Extra: map[string]any{"kind": "file", "file_id": f.FileID}})
	}
	if len(entries) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("itbaizhan: no playable entries")
	}
	if len(entries) == 1 {
		if x.title != "" {
			entries[0].Extra["course_title"] = x.title
		}
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "itbaizhan", Title: firstNonEmpty(x.title, x.cid, "itbaizhan"), Entries: entries, Extra: map[string]any{"course_id": x.cid, "price": ITBAIZHAN_FIXED_COURSE_PRICE}}, nil
}
