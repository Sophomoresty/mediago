// Package baijiayunxiao implements Baijiayun/Baijiayunxiao playback and course extraction.
//
// Source alignment:
//
//	Mooc/Courses/Baijiayunxiao/Baijiayun_Video.pyc.1shot.cdc.py
//	Mooc/Courses/Baijiayunxiao/Baijiayunxiao_Course.pyc.1shot.cdc.py
package baijiayunxiao

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlGetPlayInfo = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id=%s&token=%s&use_encrypt=0&render=jsonp"
	urlGetPlayURL  = "https://www.baijiayun.com/vod/video/getPlayUrl?vid=%s&render=jsonp&token=%s&use_encrypt=0"
	urlHome        = "https://www.baijiayun.com"

	urlCourseInfo    = "https://%s/api/app/myStudy/course/%s"
	urlToken         = "https://%s/api/app/getPcRoomCode/course_id=%s/chapter_id=%s?type=1"
	urlPlayToken     = "https://%s/api/app/getPlayToken/chapter_id=%s/course_id=%s"
	urlPreviewVideo  = "https://%s/api/app/user/CourseWare/video/preview?video_id=%s"
)

var patterns = []string{`(?:[\w-]+\.)?(?:baijiayun|baijicloud|baijiayunxiao)\.com/`}

func init() {
	extractor.Register(&Baijiayunxiao{}, extractor.SiteInfo{Name: "Baijiayunxiao", URL: "baijiayun.com", NeedAuth: true})
}

type Baijiayunxiao struct{}

func (b *Baijiayunxiao) Patterns() []string { return patterns }

type playbackParams struct {
	roomID string
	vid    string
	token  string
	isVOD  bool
}

type courseURL struct {
	domain string
	cid    string
}

type courseInfoResponse struct {
	Data struct {
		Title   string       `json:"title"`
		Name    string       `json:"name"`
		Periods []courseNode `json:"periods"`
		Chapter []courseNode `json:"chapter"`
	} `json:"data"`
}

type courseNode struct {
	ID           any          `json:"id"`
	VideoID      any          `json:"video_id"`
	RoomID       any          `json:"room_id"`
	Title        string       `json:"title"`
	Name         string       `json:"name"`
	PeriodsTitle string       `json:"periods_title"`
	Child        []courseNode `json:"child"`
	Children     []courseNode `json:"children"`
}

type lessonRef struct {
	ID    string
	Title string
}

type playTokenResponse struct {
	Token   string `json:"token"`
	VideoID any    `json:"video_id"`
	RoomID  any    `json:"room_id"`
	ClassID any    `json:"classid"`
	Data    struct {
		Token   string `json:"token"`
		VideoID any    `json:"video_id"`
		RoomID  any    `json:"room_id"`
		ClassID any    `json:"classid"`
	} `json:"data"`
}

func (b *Baijiayunxiao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("baijiayunxiao requires login cookies")
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := map[string]string{"Referer": refererFromRawURL(rawURL), "Origin": urlHome}

	if m := evURLRe.FindStringSubmatch(rawURL); m != nil {
		return mediaInfo("baijiayunxiao", util.SanitizeFilename(m[1]), rawURL, "."+strings.ToLower(m[2]), headers), nil
	}
	if p, ok := parsePlaybackParams(rawURL); ok {
		return resolvePlayback(c, p, headers, "baijiayunxiao")
	}
	if cu, ok := parseCourseURL(rawURL); ok {
		return resolveCourse(c, cu, headers)
	}

	if redirectURL, err := resolveLiveEnter(c, rawURL, headers); err == nil && redirectURL != "" {
		if p, ok := parsePlaybackParams(redirectURL); ok {
			return resolvePlayback(c, p, headers, "baijiayunxiao")
		}
	}
	body, err := c.GetString(rawURL, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch baijiayun source page: %w", err)
	}
	if embedded := findPlaybackURLInText(body); embedded != "" {
		if p, ok := parsePlaybackParams(embedded); ok {
			return resolvePlayback(c, p, headers, "baijiayunxiao")
		}
	}
	return nil, fmt.Errorf("baijiayunxiao: no tokenised playback URL found in source page")
}

func resolveCourse(c *util.Client, cu courseURL, headers map[string]string) (*extractor.MediaInfo, error) {
	infoURL := fmt.Sprintf(urlCourseInfo, cu.domain, url.PathEscape(cu.cid))
	body, err := c.GetString(infoURL, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch baijiayunxiao course info: %w", err)
	}
	var resp courseInfoResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parse baijiayunxiao course info: %w", err)
	}
	lessons := collectLessons(resp.Data.Periods, nil)
	lessons = append(lessons, collectLessons(resp.Data.Chapter, nil)...)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("baijiayunxiao: no lesson ids in data.periods/data.chapter")
	}

	entries := make([]*extractor.MediaInfo, 0, len(lessons))
	for i, lesson := range lessons {
		token, roomID, fromPcRoomCode, err := fetchLessonToken(c, cu, lesson.ID, headers)
		if err != nil || token == "" || roomID == "" {
			continue
		}

		entryTitle := firstNonEmpty(lesson.Title, fmt.Sprintf("课时%d", i+1))

		// Source: when token comes from getPlayToken (not getPcRoomCode),
		// the preview video API is tried first as a fallback before the
		// standard baijiayun playback APIs.
		if !fromPcRoomCode {
			if previewURL := fetchPreviewVideoURL(c, cu.domain, roomID, headers); previewURL != "" {
				entry := mediaInfo("baijiayunxiao", util.SanitizeFilename(entryTitle), previewURL, pickFormat(previewURL), headers)
				entry.Extra = map[string]any{"course_id": cu.cid, "video_id": lesson.ID, "room_id": roomID}
				entries = append(entries, entry)
				continue
			}
		}

		p := playbackParams{roomID: roomID, token: token}
		entry, err := resolvePlayback(c, p, headers, entryTitle)
		if err == nil {
			entry.Extra = map[string]any{"course_id": cu.cid, "video_id": lesson.ID, "room_id": roomID}
			entries = append(entries, entry)
			continue
		}

		// Fallback: when fromPcRoomCode is true but playback resolution
		// failed, also try the preview video API.
		if fromPcRoomCode {
			if previewURL := fetchPreviewVideoURL(c, cu.domain, roomID, headers); previewURL != "" {
				entry := mediaInfo("baijiayunxiao", util.SanitizeFilename(entryTitle), previewURL, pickFormat(previewURL), headers)
				entry.Extra = map[string]any{"course_id": cu.cid, "video_id": lesson.ID, "room_id": roomID}
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("baijiayunxiao: parsed lessons but no baijiayun stream resolved")
	}
	title := firstNonEmpty(resp.Data.Title, resp.Data.Name, "baijiayunxiao_"+cu.cid)
	return &extractor.MediaInfo{Site: "baijiayunxiao", Title: util.SanitizeFilename(title), Entries: entries}, nil
}

// fetchLessonToken returns (token, roomID, fromPcRoomCode, error).
// fromPcRoomCode is true when the token came from getPcRoomCode (first endpoint);
// false when it came from getPlayToken (second endpoint). The source uses this
// flag to decide the video resolution order: when fromPcRoomCode is false, the
// preview video API is tried first as a fallback.
func fetchLessonToken(c *util.Client, cu courseURL, lessonID string, headers map[string]string) (string, string, bool, error) {
	body, err := c.GetString(fmt.Sprintf(urlToken, cu.domain, url.PathEscape(cu.cid), url.PathEscape(lessonID)), headers)
	if err != nil {
		return "", "", false, err
	}
	token := pickRegex(tokenRe, body)
	roomID := pickRegex(classIDRe, body)
	if token != "" && roomID != "" {
		return token, roomID, true, nil
	}
	body, err = c.GetString(fmt.Sprintf(urlPlayToken, cu.domain, url.PathEscape(lessonID), url.PathEscape(cu.cid)), headers)
	if err != nil {
		return "", "", false, err
	}
	var resp playTokenResponse
	_ = json.Unmarshal([]byte(body), &resp)
	token = firstNonEmpty(resp.Token, resp.Data.Token, pickRegex(tokenRe, body))
	roomID = firstNonEmpty(anyString(resp.VideoID), anyString(resp.RoomID), anyString(resp.ClassID), anyString(resp.Data.VideoID), anyString(resp.Data.RoomID), anyString(resp.Data.ClassID), pickRegex(classIDRe, body), lessonID)
	return token, roomID, false, nil
}

func resolvePlayback(c *util.Client, p playbackParams, headers map[string]string, title string) (*extractor.MediaInfo, error) {
	var mediaURL string
	var err error
	if p.isVOD || p.vid != "" {
		mediaURL, err = shared.BaijiayunResolveVOD(c, p.vid, p.token, headers)
	} else {
		mediaURL, err = shared.BaijiayunResolvePlayback(c, p.roomID, p.token, headers)
	}
	if err != nil && p.vid == "" && p.roomID != "" {
		mediaURL, err = shared.BaijiayunResolveVOD(c, p.roomID, p.token, headers)
	}
	if err != nil {
		return nil, err
	}
	id := firstNonEmpty(p.vid, p.roomID, "playback")
	return mediaInfo("baijiayunxiao", util.SanitizeFilename(firstNonEmpty(title, "baijiayunxiao_"+id)), mediaURL, pickFormat(mediaURL), headers), nil
}

func resolveLiveEnter(c *util.Client, rawURL string, headers map[string]string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	liveID := firstNonEmpty(q.Get("liveId"), q.Get("meeting_id"))
	ticket := q.Get("ticket")
	if liveID == "" || ticket == "" {
		return "", nil
	}
	payload := fmt.Sprintf(`{"ticket":"%s","liveId":"%s"}`, ticket, liveID)
	timestamp := fmt.Sprint(time.Now().UnixMilli())
	parts := []string{"timestamp=" + timestamp, "body=" + payload}
	sort.Strings(parts)
	postHeaders := cloneHeaders(headers)
	postHeaders["B-Sign"] = strings.ToUpper(util.MD5(strings.Join(parts, "&")))
	postHeaders["B-Timestamp"] = timestamp
	postHeaders["Referer"] = rawURL
	postHeaders["Origin"] = u.Scheme + "://" + u.Host
	postHeaders["Content-Type"] = "application/json;charset=UTF-8"
	resp, err := c.Post(u.Scheme+"://"+u.Host+"/api/app/live/enter.json", bytes.NewReader([]byte(payload)), postHeaders)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return findPlaybackURLInText(string(b)), nil
}

func cloneHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
