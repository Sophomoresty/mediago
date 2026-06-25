// Package houda implements an extractor for houdask.com courses that play via CSSLCloud.
//
// API endpoints from decompiled Mooc/Courses/Houda/Houda_Course.pyc:
//
//	http://www.houdask.com/api/center/online/myOnline/anon/getLearnFirstPage
//	http://www.houdask.com/api/center/online/myOnline/getXxStageAndLawList
//	http://www.houdask.com/api/center/myOnlineCourse/getLearnCourse
//	http://www.houdask.com/api/center/myOnlineCourse/getLearnCoursePage
//	http://www.houdask.com/api/center/myOnlineLive/anon/v202401/getById
//	http://www.houdask.com/api/center/myLibraryMaterial/v2/getList
//	http://www.houdask.com/api/center/live/cc/anon/viewPlayback/{room_id}/{record_id}
//	https://view.csslcloud.net/replay/user/login
//	https://view.csslcloud.net/replay/video/play
//	https://view.csslcloud.net/replay/data/meta
package houda

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlOrigin          = "http://www.houdask.com"
	urlHome            = "http://www.houdask.com/"
	urlLoginCheck      = "http://www.houdask.com/api/center/sysUserPower/anon/ifLogin"
	urlCourseList      = "http://www.houdask.com/api/center/online/myOnline/anon/getLearnFirstPage"
	urlStageLaw        = "http://www.houdask.com/api/center/online/myOnline/getXxStageAndLawList"
	urlLearnCourse     = "http://www.houdask.com/api/center/myOnlineCourse/getLearnCourse"
	urlLearnCoursePage = "http://www.houdask.com/api/center/myOnlineCourse/getLearnCoursePage"
	urlLiveDetail      = "http://www.houdask.com/api/center/myOnlineLive/anon/v202401/getById"
	urlMaterial        = "http://www.houdask.com/api/center/myLibraryMaterial/v2/getList"
	urlCCViewPlayback  = "http://www.houdask.com/api/center/live/cc/anon/viewPlayback/%s/%s"
	urlCsslLogin       = "https://view.csslcloud.net/replay/user/login"
	urlCsslPlay        = "https://view.csslcloud.net/replay/video/play"
	urlCsslMeta        = "https://view.csslcloud.net/replay/data/meta"
	urlCsslOrigin      = "https://view.csslcloud.net"
	csslDeviceType     = "h5-pc"
	csslDeviceVersion  = "3.21.0"
	csslTpl            = "20"
	csslTerminal       = "3"
	materialServiceTyp = "1"
)

var patterns = []string{
	`(?:[\w-]+\.)?houdask\.com/`,
	`(?:[\w-]+\.)?csslcloud\.net/`,
}

func init() {
	extractor.Register(&Houda{}, extractor.SiteInfo{Name: "Houda", URL: "houdask.com", NeedAuth: true})
}

type Houda struct{}

func (s *Houda) Patterns() []string { return patterns }

var classIDRe = regexp.MustCompile(`(?i)(?:classId|class_id|courseId|course_id|id)=([0-9]+)|/(?:online|course|class|learn|myOnline)[^?#/]*/([0-9]+)(?:[/?#]|$)`)

func (s *Houda) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("houda requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := houdaHeaders()

	if isCsslcloudURL(rawURL) {
		return s.extractCsslcloudURL(c, rawURL, headers)
	}
	if err := checkHoudaCookie(c, headers); err != nil {
		return nil, err
	}
	cid := parseClassID(rawURL)
	courses, _ := fetchHoudaCourseList(c, headers)
	course := chooseHoudaCourse(courses, cid, rawURL)
	if cid == "" {
		cid = course.ID
	}
	if cid == "" {
		return nil, fmt.Errorf("cannot parse houda classId from URL and course list is empty: %s", rawURL)
	}
	lessons, err := fetchHoudaLessons(c, cid, headers)
	if err != nil {
		return nil, err
	}
	stageLaw, _ := fetchHoudaStageLaw(c, cid, headers)

	entries := make([]*extractor.MediaInfo, 0, len(lessons))
	for i, lesson := range lessons {
		entry, err := buildHoudaEntry(c, cid, i+1, lesson, headers)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	materials := fetchHoudaMaterials(c, cid, stageLaw, headers)
	for i, material := range materials {
		if entry := buildHoudaMaterialEntry(i+1, material, headers); entry != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("houda: no playable video or material entries for classId=%s", cid)
	}
	title := firstNonEmpty(course.Title, "houda_"+cid)
	return &extractor.MediaInfo{Site: "houda", Title: title, Entries: entries, Extra: map[string]any{"course_id": cid, "course": course.Raw}}, nil
}

func houdaHeaders() map[string]string {
	return map[string]string{
		"appType":          "WEB",
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/plain, */*",
		"Origin":           urlOrigin,
		"Referer":          urlHome,
	}
}

func checkHoudaCookie(c *util.Client, headers map[string]string) error {
	body, err := c.GetString(urlLoginCheck, headers)
	if err != nil {
		return fmt.Errorf("houda cookie check: %w", err)
	}
	var out struct {
		Code any `json:"code"`
		Data any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return fmt.Errorf("houda cookie check parse: %w", err)
	}
	if stringValue(out.Code) != "1" {
		return fmt.Errorf("houda requires valid logged-in cookie (code=%s)", stringValue(out.Code))
	}
	return nil
}

func fetchHoudaLessons(c *util.Client, cid string, headers map[string]string) ([]houdaLesson, error) {
	payloads := []struct {
		endpoint string
		data     map[string]string
	}{
		{urlLearnCourse, map[string]string{"classId": cid}},
		{urlLearnCoursePage, map[string]string{"classId": cid, "pageNum": "1", "pageSize": "500", "page": "1", "size": "500"}},
	}
	var lastErr error
	for _, req := range payloads {
		root, err := requestHouda(c, req.endpoint, req.data, headers)
		if err != nil {
			lastErr = err
			continue
		}
		lessons := parseHoudaLessons(root)
		if len(lessons) > 0 {
			return lessons, nil
		}
		lastErr = fmt.Errorf("empty liveList from %s", req.endpoint)
	}
	return nil, fmt.Errorf("houda fetch lessons: %w", lastErr)
}

func requestHouda(c *util.Client, endpoint string, data map[string]string, headers map[string]string) (map[string]any, error) {
	if data == nil {
		data = map[string]string{}
	}
	wrappedBytes, _ := json.Marshal(data)
	forms := []map[string]string{
		data,
		{"data": string(wrappedBytes)},
	}
	var lastErr error
	for _, form := range forms {
		body, err := c.PostForm(endpoint, form, headers)
		if err != nil {
			lastErr = err
			continue
		}
		var root map[string]any
		if err := json.Unmarshal([]byte(body), &root); err != nil {
			lastErr = err
			continue
		}
		return root, nil
	}
	return nil, lastErr
}

type houdaCourse struct {
	ID    string
	Title string
	Raw   map[string]any
}

type houdaStageLaw struct {
	Raw  map[string]any
	Laws []houdaLawRef
}

type houdaLawRef struct {
	ID    string
	Title string
}

func fetchHoudaCourseList(c *util.Client, headers map[string]string) ([]houdaCourse, error) {
	root, err := requestHouda(c, urlCourseList, map[string]string{}, headers)
	if err != nil {
		return nil, err
	}
	return parseHoudaCourseList(root), nil
}

func parseHoudaCourseList(root map[string]any) []houdaCourse {
	data := unwrapHoudaData(root)
	var courses []houdaCourse
	seen := map[string]bool{}
	tabs := houdaMapList(data, "tabList", "tabs", "list", "items")
	if len(tabs) == 0 {
		tabs = []map[string]any{{"dataList": data}}
	}
	for _, tab := range tabs {
		tabName := firstMapText(tab, "name", "tabName", "title")
		code := strings.ToUpper(firstMapText(tab, "code"))
		if strings.Contains(tabName, "资料") || code == "ZL" {
			continue
		}
		rows := houdaMapList(tab, "dataList", "list", "items", "courseList", "courses")
		if len(rows) == 0 && hasAnyMap(tab, "id", "classId", "courseId") {
			rows = []map[string]any{tab}
		}
		for _, row := range rows {
			id := firstMapText(row, "id", "classId", "courseId", "class_id", "course_id")
			title := firstMapText(row, "name", "title", "courseName", "course_name")
			if id == "" || title == "" || seen[id] {
				continue
			}
			seen[id] = true
			raw := cloneMap(row)
			raw["tab_name"] = tabName
			courses = append(courses, houdaCourse{ID: id, Title: title, Raw: raw})
		}
	}
	return courses
}

func chooseHoudaCourse(courses []houdaCourse, cid, rawURL string) houdaCourse {
	if cid != "" {
		for _, course := range courses {
			if course.ID == cid {
				return course
			}
		}
		return houdaCourse{ID: cid, Title: "houda_" + cid}
	}
	if u, err := url.Parse(rawURL); err == nil {
		nameHint, _ := url.QueryUnescape(u.Query().Get("name"))
		nameHint = strings.TrimSpace(nameHint)
		if nameHint != "" {
			for _, course := range courses {
				if strings.Contains(course.Title, nameHint) || strings.Contains(nameHint, course.Title) {
					return course
				}
			}
		}
	}
	if len(courses) > 0 {
		return courses[0]
	}
	return houdaCourse{}
}

func fetchHoudaStageLaw(c *util.Client, cid string, headers map[string]string) (*houdaStageLaw, error) {
	root, err := requestHouda(c, urlStageLaw, map[string]string{"classId": cid}, headers)
	if err != nil {
		return nil, err
	}
	raw := unwrapHoudaData(root)
	stageLaw := &houdaStageLaw{Raw: raw, Laws: collectHoudaLawRefs(raw)}
	return stageLaw, nil
}

func fetchHoudaMaterials(c *util.Client, cid string, stageLaw *houdaStageLaw, headers map[string]string) []map[string]any {
	lawIDs := []string{""}
	if stageLaw != nil {
		for _, law := range stageLaw.Laws {
			lawIDs = appendStringUnique(lawIDs, law.ID)
		}
	}
	var out []map[string]any
	seen := map[string]bool{}
	for _, lawID := range lawIDs {
		root, err := requestHouda(c, urlMaterial, map[string]string{"lawId": lawID, "serviceType": materialServiceTyp, "classId": cid}, headers)
		if err != nil {
			continue
		}
		rows := houdaMapList(unwrapHoudaData(root), "data", "list", "items", "rows", "records")
		for _, row := range rows {
			key := houdaMaterialKey(row)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, row)
		}
	}
	return out
}

func hydrateHoudaLesson(c *util.Client, lesson houdaLesson, headers map[string]string) houdaLesson {
	lessonID := firstText(lesson.ID)
	if lessonID == "" {
		return lesson
	}
	roomID := firstText(lesson.RoomID, lesson.MainRoomID, lesson.CCLiveID)
	recordID := firstText(lesson.RecordID)
	direct := firstText(lesson.PlaybackMP4, lesson.PlaybackURL, lesson.LiveURL)
	if roomID != "" && (recordID != "" || direct != "") {
		return lesson
	}
	detail, err := fetchHoudaLiveDetail(c, lessonID, headers)
	if err != nil {
		return lesson
	}
	return mergeHoudaLesson(lesson, detail)
}

func fetchHoudaLiveDetail(c *util.Client, lessonID string, headers map[string]string) (houdaLesson, error) {
	for _, data := range []map[string]string{{"id": lessonID}, {"liveId": lessonID}} {
		root, err := requestHouda(c, urlLiveDetail, data, headers)
		if err != nil {
			continue
		}
		if lesson := houdaLessonFromAny(unwrapHoudaData(root)); firstText(lesson.ID, lesson.RecordID, lesson.RoomID, lesson.PlaybackURL, lesson.PlaybackMP4) != "" {
			if firstText(lesson.ID) == "" {
				lesson.ID = lessonID
			}
			return lesson, nil
		}
	}
	api := addHoudaQuery(urlLiveDetail, map[string]string{"id": lessonID})
	body, err := c.GetString(api, headers)
	if err != nil {
		return houdaLesson{}, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return houdaLesson{}, err
	}
	lesson := houdaLessonFromAny(unwrapHoudaData(root))
	if firstText(lesson.ID) == "" {
		lesson.ID = lessonID
	}
	return lesson, nil
}

func buildHoudaMaterialEntry(index int, item map[string]any, headers map[string]string) *extractor.MediaInfo {
	fileURL := normalizeHoudaURL(firstMapText(item, "downLoadUrl", "downloadUrl", "fileUrl", "url", "path"))
	if fileURL == "" {
		return nil
	}
	name := firstMapText(item, "title", "name", "fileName", "materialName", "coursewareName")
	if name == "" {
		name = "资料"
	}
	format := houdaFileExt(firstMapText(item, "fileType", "type", "ext", "format"), fileURL)
	return &extractor.MediaInfo{
		Site:  "houda",
		Title: fmt.Sprintf("(%d)--%s", index, name),
		Streams: map[string]extractor.Stream{"file": {
			Quality: "file",
			URLs:    []string{fileURL},
			Format:  format,
			Headers: map[string]string{"Referer": urlHome, "appType": "WEB"},
		}},
		Extra: map[string]any{"kind": "material", "raw": item},
	}
}

// houdaCsslVideoItem represents a single video stream from the Houda-specific
// CSSL /replay/video/play response. Each item carries primary and secondary
// URLs plus quality descriptors used for quality-based sorting.
type houdaCsslVideoItem struct {
	Primary     string `json:"primary"`
	Secondary   string `json:"secondary"`
	Desc        string `json:"desc"`
	QualityDesc string `json:"qualityDesc"`
	Code        string `json:"code"`
	Quality     string `json:"quality"`
}

// houdaCsslPlayResult holds the resolved CSSL play information for a lesson.
type houdaCsslPlayResult struct {
	Token     string
	VideoList []houdaCsslVideoItem
	AudioURL  string
}

type houdaLesson struct {
	ID          any `json:"id"`
	Title       any `json:"title"`
	Name        any `json:"name"`
	CourseName  any `json:"courseName"`
	CourseID    any `json:"courseId"`
	ClassID     any `json:"classId"`
	Type        any `json:"type"`
	CCLiveID    any `json:"ccLiveId"`
	RoomID      any `json:"roomId"`
	MainRoomID  any `json:"mainRoomId"`
	RecordID    any `json:"recordId"`
	LiveURL     any `json:"liveUrl"`
	PlaybackURL any `json:"playbackUrl"`
	PlaybackMP4 any `json:"playbackMp4"`
	PlaybackMP3 any `json:"playbackMp3"`
	StageID     any `json:"stageId"`
	StageName   any `json:"stageName"`
	LawID       any `json:"lawId"`
	LawName     any `json:"lawName"`
}

func buildHoudaEntry(c *util.Client, cid string, index int, lesson houdaLesson, headers map[string]string) (*extractor.MediaInfo, error) {
	lesson = hydrateHoudaLesson(c, lesson, headers)
	title := firstText(lesson.Title, lesson.Name, lesson.CourseName, "未命名")
	lessonID := firstText(lesson.ID)
	roomID := firstText(lesson.RoomID, lesson.MainRoomID, lesson.CCLiveID)
	recordID := firstText(lesson.RecordID)
	direct := firstText(lesson.PlaybackMP4, lesson.PlaybackURL, lesson.LiveURL)
	mp3 := firstText(lesson.PlaybackMP3)

	streams := map[string]extractor.Stream{}
	extra := map[string]any{"course_id": cid, "lesson_id": lessonID, "record_id": recordID, "room_id": roomID}
	if mp3 != "" {
		extra["playback_mp3"] = normalizeHoudaURL(mp3)
	}

	if roomID != "" && recordID != "" {
		info, err := resolveHoudaCSSL(c, roomID, recordID, title, headers)
		if err == nil {
			extra["csslcloud_token"] = info.Token
			extra["csslcloud_meta_url"] = urlCsslMeta
			sortedVideos := sortHoudaVideosByQuality(info.VideoList)
			for i, v := range sortedVideos {
				videoURL := firstNonEmpty(v.Primary, v.Secondary)
				if videoURL == "" {
					continue
				}
				quality := firstNonEmpty(v.Desc, v.QualityDesc, v.Code, v.Quality, fmt.Sprintf("definition_%d", i))
				streams[quality] = extractor.Stream{
					Quality:  quality,
					URLs:     []string{videoURL},
					Format:   mediaExt(videoURL),
					AudioURL: info.AudioURL,
					Headers:  map[string]string{"Referer": urlHome},
				}
			}
			// Expose best video URL for m3u8 rewriting.
			if len(sortedVideos) > 0 {
				bestURL := firstNonEmpty(sortedVideos[0].Primary, sortedVideos[0].Secondary)
				if bestURL != "" && mediaExt(bestURL) == "m3u8" {
					if rewritten, err := rewriteHoudaM3U8(c, bestURL, urlHome); err == nil {
						extra["m3u8_text"] = rewritten
					}
				}
			}
			// Board video: genuinely requires local cv2+ffmpeg rendering (Houda_Local).
			// Mark as blocked so callers know this is unavailable.
			extra["board_video_blocked"] = true
			extra["board_video_reason"] = "board video requires local OpenCV (cv2) frame rendering and ffmpeg compositing (Houda_Local); not reproducible via API extraction"
		} else if direct == "" {
			return nil, err
		} else {
			extra["csslcloud_error"] = err.Error()
		}
	}
	if len(streams) == 0 && direct != "" {
		directURL := normalizeHoudaURL(direct)
		fmtName := mediaExt(directURL)
		streams[fmtName] = extractor.Stream{Quality: "best", URLs: []string{directURL}, Format: fmtName, Headers: map[string]string{"Referer": urlHome}}
		if fmtName == "m3u8" {
			if rewritten, err := rewriteHoudaM3U8(c, directURL, urlHome); err == nil {
				extra["m3u8_text"] = rewritten
			}
		}
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("houda lesson %s has no stream", lessonID)
	}
	return &extractor.MediaInfo{Site: "houda", Title: fmt.Sprintf("[%d]--%s", index, title), Streams: streams, Extra: extra}, nil
}

// resolveHoudaCSSL runs the Houda-specific CSSL chain:
//
//  1. Resolve CC callback to get csslcloud userId/roomId/recordId/viewerToken.
//  2. POST /replay/user/login  (JSON body) to get X-HD-Token.
//  3. GET  /replay/video/play  (query params + X-HD-Token header) to get video list.
//
// Falls back to the shared CssLcloudResolvePlayInfo helper if the native chain
// fails, so existing tests and direct-csslcloud-URL flows keep working.
func resolveHoudaCSSL(c *util.Client, roomID, recordID, title string, headers map[string]string) (*houdaCsslPlayResult, error) {
	cc, err := resolveHoudaCCCallback(c, roomID, recordID, headers)
	if err != nil {
		return nil, err
	}
	liveRoomID := firstNonEmpty(cc.RoomID, roomID)
	accessID := firstNonEmpty(cc.UserID, cc.AccountID)
	viewerToken := firstNonEmpty(cc.ViewerToken, accessID+":"+liveRoomID)
	resolvedRecordID := firstNonEmpty(cc.RecordID, recordID)

	result, err := resolveHoudaCSSLNative(c, accessID, resolvedRecordID, firstNonEmpty(cc.ViewerName, title), viewerToken)
	if err == nil {
		return result, nil
	}

	// Fallback to shared helper.
	info, err2 := shared.CssLcloudResolvePlayInfo(c, shared.CssLcloudPayload{
		LiveRoomID:  liveRoomID,
		UserID:      accessID,
		AccessID:    accessID,
		RecordID:    resolvedRecordID,
		ViewerName:  firstNonEmpty(cc.ViewerName, title),
		ViewerToken: viewerToken,
		Referer:     urlHome,
	})
	if err2 != nil {
		return nil, fmt.Errorf("houda cssl native: %w; shared fallback: %w", err, err2)
	}
	items := make([]houdaCsslVideoItem, 0, len(info.VideoList))
	for _, v := range info.VideoList {
		items = append(items, houdaCsslVideoItem{
			Primary: v.URL,
			Code:    strconv.Itoa(v.Definition),
		})
	}
	return &houdaCsslPlayResult{Token: info.SessionID, VideoList: items, AudioURL: info.AudioURL}, nil
}

// resolveHoudaCSSLNative runs the Houda-specific CSSL chain using the
// /replay/user/login and /replay/video/play endpoints.
func resolveHoudaCSSLNative(c *util.Client, accountID, recordID, viewerName, viewerToken string) (*houdaCsslPlayResult, error) {
	// Step 1: Login — POST JSON.
	loginPayload := map[string]any{
		"replayId":      recordID,
		"userId":        accountID,
		"accountId":     accountID,
		"userName":      viewerName,
		"deviceType":    csslDeviceType,
		"deviceVersion": csslDeviceVersion,
		"tpl":           csslTpl,
		"userToken":     viewerToken,
	}
	bodyBytes, _ := json.Marshal(loginPayload)
	loginHeaders := map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Content-Type": "application/json;charset=UTF-8",
		"Origin":       urlCsslOrigin,
		"Referer":      urlCsslOrigin + "/",
	}
	loginResp, err := c.Post(urlCsslLogin, bytes.NewReader(bodyBytes), loginHeaders)
	if err != nil {
		return nil, fmt.Errorf("houda cssl login POST: %w", err)
	}
	defer loginResp.Body.Close()
	loginBody, _ := io.ReadAll(loginResp.Body)

	var login struct {
		Success bool `json:"success"`
		Data    struct {
			User struct {
				Token string `json:"token"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginBody, &login); err != nil {
		return nil, fmt.Errorf("houda cssl login parse: %w", err)
	}
	token := login.Data.User.Token
	if token == "" {
		return nil, fmt.Errorf("houda cssl login: empty token (success=%v, body=%s)", login.Success, truncate(string(loginBody), 200))
	}

	// Step 2: Play — GET with X-HD-Token and query params.
	playHeaders := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Origin":     urlCsslOrigin,
		"Referer":    urlCsslOrigin + "/",
		"X-HD-Token": token,
	}
	playURL := addHoudaQuery(urlCsslPlay, map[string]string{
		"terminal":   csslTerminal,
		"replay_id":  recordID,
		"account_id": accountID,
	})
	playBody, err := c.GetString(playURL, playHeaders)
	if err != nil {
		return nil, fmt.Errorf("houda cssl play GET: %w", err)
	}
	var play struct {
		Data struct {
			Video []houdaCsslVideoItem `json:"video"`
			Audio []struct {
				URL string `json:"url"`
			} `json:"audio"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(playBody), &play); err != nil {
		return nil, fmt.Errorf("houda cssl play parse: %w", err)
	}

	result := &houdaCsslPlayResult{Token: token, VideoList: play.Data.Video}
	if len(play.Data.Audio) > 0 {
		result.AudioURL = play.Data.Audio[0].URL
	}
	return result, nil
}

type houdaCCInfo struct {
	UserID      string
	AccountID   string
	RoomID      string
	RecordID    string
	ViewerName  string
	ViewerToken string
}

// resolveHoudaCCCallback resolves the CC playback callback URL to extract
// csslcloud viewer parameters. Matches source _resolve_cc_callback_url:
// uses allow_redirects=False, reads Location header, handles // and / prefixes.
func resolveHoudaCCCallback(c *util.Client, roomID, recordID string, headers map[string]string) (houdaCCInfo, error) {
	callbackURL := fmt.Sprintf(urlCCViewPlayback, url.PathEscape(roomID), url.PathEscape(recordID))
	finalURL, err := fetchCCCallbackLocation(callbackURL, headers)
	if err != nil {
		return houdaCCInfo{}, err
	}
	if finalURL == "" {
		return houdaCCInfo{}, fmt.Errorf("houda CSSL callback returned empty location: %s", callbackURL)
	}
	u, err := url.Parse(finalURL)
	if err != nil {
		return houdaCCInfo{}, fmt.Errorf("houda parse CSSL callback URL: %w", err)
	}
	q := u.Query()
	info := houdaCCInfo{
		UserID:      firstNonEmpty(q.Get("userId"), q.Get("userid"), q.Get("uid")),
		AccountID:   firstNonEmpty(q.Get("accountId"), q.Get("accessid"), q.Get("accessId")),
		RoomID:      firstNonEmpty(q.Get("roomId"), q.Get("roomid"), q.Get("room_id"), roomID),
		RecordID:    firstNonEmpty(q.Get("recordId"), q.Get("recordid"), q.Get("record_id"), recordID),
		ViewerName:  firstNonEmpty(q.Get("viewername"), q.Get("viewerName"), q.Get("userName")),
		ViewerToken: firstNonEmpty(q.Get("viewertoken"), q.Get("viewerToken"), q.Get("userToken")),
	}
	if info.AccountID == "" {
		info.AccountID = info.UserID
	}
	if info.UserID == "" || info.RoomID == "" || info.RecordID == "" {
		return houdaCCInfo{}, fmt.Errorf("houda CSSL callback missing userId/roomId/recordId: %s", finalURL)
	}
	return info, nil
}

// fetchCCCallbackLocation performs a GET without following redirects
// (allow_redirects=False in source) and returns the redirect target.
// Falls back to response.url if Location is empty and URL contains csslcloud.
// Handles // prefix (prepend https:) and / prefix (resolve against origin).
func fetchCCCallbackLocation(rawURL string, headers map[string]string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("houda CSSL callback: %w", err)
	}
	req.Header.Set("User-Agent", util.RandomUA())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("houda CSSL callback: %w", err)
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		finalURL := rawURL
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}
		if strings.Contains(finalURL, "view.csslcloud.net") {
			location = finalURL
		}
	}

	if strings.HasPrefix(location, "//") {
		location = "https:" + location
	} else if strings.HasPrefix(location, "/") {
		if base, err := url.Parse(urlOrigin); err == nil {
			if ref, err := url.Parse(location); err == nil {
				location = base.ResolveReference(ref).String()
			}
		}
	}
	return location, nil
}

func rewriteHoudaM3U8(c *util.Client, m3u8URL, referer string) (string, error) {
	body, err := c.GetString(m3u8URL, map[string]string{"Referer": referer})
	if err != nil {
		return "", err
	}
	return shared.CssLcloudRewriteM3U8Keys(c, body, referer)
}

func (s *Houda) extractCsslcloudURL(c *util.Client, rawURL string, headers map[string]string) (*extractor.MediaInfo, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	accessID := firstNonEmpty(q.Get("userId"), q.Get("userid"), q.Get("accountId"), q.Get("accessid"))
	recordID := firstNonEmpty(q.Get("recordId"), q.Get("recordid"), q.Get("record_id"))
	viewerName := firstNonEmpty(q.Get("viewername"), q.Get("viewerName"), "houda")
	roomID := firstNonEmpty(q.Get("roomId"), q.Get("roomid"), q.Get("liveRoomId"), q.Get("room_id"))
	viewerToken := firstNonEmpty(q.Get("viewertoken"), q.Get("viewerToken"), accessID+":"+roomID)

	// Try Houda-native CSSL chain first, then shared fallback.
	result, err := resolveHoudaCSSLNative(c, accessID, recordID, viewerName, viewerToken)
	if err != nil {
		info, err2 := shared.CssLcloudResolvePlayInfo(c, shared.CssLcloudPayload{LiveRoomID: roomID, UserID: accessID, AccessID: accessID, RecordID: recordID, ViewerName: viewerName, ViewerToken: viewerToken, Referer: urlHome})
		if err2 != nil {
			return nil, fmt.Errorf("houda cssl native: %w; shared fallback: %w", err, err2)
		}
		stream := extractor.Stream{Quality: "best", URLs: []string{info.VideoURL}, Format: mediaExt(info.VideoURL), AudioURL: info.AudioURL, Headers: map[string]string{"Referer": urlHome}}
		return &extractor.MediaInfo{Site: "houda", Title: "houda_csslcloud", Streams: map[string]extractor.Stream{"best": stream}}, nil
	}
	sorted := sortHoudaVideosByQuality(result.VideoList)
	streams := map[string]extractor.Stream{}
	for i, v := range sorted {
		videoURL := firstNonEmpty(v.Primary, v.Secondary)
		if videoURL == "" {
			continue
		}
		quality := firstNonEmpty(v.Desc, v.QualityDesc, v.Code, v.Quality, fmt.Sprintf("definition_%d", i))
		streams[quality] = extractor.Stream{Quality: quality, URLs: []string{videoURL}, Format: mediaExt(videoURL), AudioURL: result.AudioURL, Headers: map[string]string{"Referer": urlHome}}
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("houda csslcloud: no playable video in response")
	}
	return &extractor.MediaInfo{Site: "houda", Title: "houda_csslcloud", Streams: streams}, nil
}

// houdaQualityKey assigns a numeric sort key to a CSSL video item based on
// its quality descriptors. Matches source _quality_key logic exactly:
//
//	原画/蓝光/1080/FHD/4K → 400
//	超清                    → 320
//	高清/720/HD             → 240
//	标清/流畅/480/360/SD    → 160
//	fallback: parse code/quality as int
func houdaQualityKey(v houdaCsslVideoItem) int {
	desc := strings.TrimSpace(firstNonEmpty(v.Desc, v.QualityDesc))
	code := strings.TrimSpace(firstNonEmpty(v.Code, v.Quality))
	text := desc + " " + code

	for _, kw := range []string{"原画", "蓝光", "1080", "FHD", "4K"} {
		if strings.Contains(text, kw) {
			return 400
		}
	}
	if strings.Contains(text, "超清") {
		return 320
	}
	for _, kw := range []string{"高清", "720", "HD"} {
		if strings.Contains(text, kw) {
			return 240
		}
	}
	for _, kw := range []string{"标清", "流畅", "480", "360", "SD"} {
		if strings.Contains(text, kw) {
			return 160
		}
	}
	if n, err := strconv.Atoi(code); err == nil {
		return n
	}
	return 0
}

// sortHoudaVideosByQuality returns a copy sorted by quality (highest first),
// matching source _pick_video_url sort(key=_quality_key, reverse=True).
func sortHoudaVideosByQuality(items []houdaCsslVideoItem) []houdaCsslVideoItem {
	filtered := make([]houdaCsslVideoItem, 0, len(items))
	for _, v := range items {
		if v.Primary != "" || v.Secondary != "" {
			filtered = append(filtered, v)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return houdaQualityKey(filtered[i]) > houdaQualityKey(filtered[j])
	})
	return filtered
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseClassID(rawURL string) string {
	if m := classIDRe.FindStringSubmatch(rawURL); len(m) > 1 {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				return m[i]
			}
		}
	}
	return ""
}

func isCsslcloudURL(rawURL string) bool { return strings.Contains(rawURL, "csslcloud.net") }

func mediaExt(u string) string {
	lu := strings.ToLower(u)
	switch {
	case strings.Contains(lu, ".m3u8"):
		return "m3u8"
	case strings.Contains(lu, ".mp3"):
		return "mp3"
	default:
		return "mp4"
	}
}

func firstText(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func stringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func parseHoudaLessons(root map[string]any) []houdaLesson {
	data := unwrapHoudaData(root)
	rows := houdaMapList(data, "liveList", "list", "items", "records", "rows", "data")
	out := make([]houdaLesson, 0, len(rows))
	for _, row := range rows {
		lesson := houdaLessonFromAny(row)
		if firstText(lesson.ID, lesson.Title, lesson.Name, lesson.RoomID, lesson.RecordID, lesson.PlaybackURL, lesson.PlaybackMP4) == "" {
			continue
		}
		out = append(out, lesson)
	}
	return out
}

func unwrapHoudaData(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if child, ok := x["data"]; ok {
			switch c := child.(type) {
			case map[string]any:
				return c
			case []any:
				return map[string]any{"list": c}
			}
		}
		return x
	case []any:
		return map[string]any{"list": x}
	default:
		return map[string]any{}
	}
}

func houdaMapList(v any, keys ...string) []map[string]any {
	var root any = v
	if m, ok := v.(map[string]any); ok {
		root = unwrapHoudaData(m)
	}
	switch x := root.(type) {
	case []any:
		return houdaMapsFromAnyList(x)
	case map[string]any:
		for _, key := range keys {
			if child, ok := x[key]; ok {
				if rows := houdaMapList(child); len(rows) > 0 {
					return rows
				}
			}
		}
		for _, key := range []string{"liveList", "lawList", "stageList", "dataList", "list", "items", "rows", "records", "data"} {
			if child, ok := x[key]; ok {
				if rows := houdaMapList(child); len(rows) > 0 {
					return rows
				}
			}
		}
	}
	return nil
}

func houdaMapsFromAnyList(values []any) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if m, ok := value.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func firstMapText(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if s := stringValue(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func hasAnyMap(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func appendStringUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func collectHoudaLawRefs(raw map[string]any) []houdaLawRef {
	var out []houdaLawRef
	seen := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if id := firstMapText(x, "id", "lawId", "law_id"); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, houdaLawRef{ID: id, Title: firstMapText(x, "name", "lawName", "title")})
			}
			for _, key := range []string{"lawList", "stageList", "children", "list", "items", "data"} {
				if child, ok := x[key]; ok {
					walk(child)
				}
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(raw)
	return out
}

func houdaMaterialKey(item map[string]any) string {
	if id := firstMapText(item, "id", "materialId", "fileId"); id != "" {
		return "id:" + id
	}
	if u := normalizeHoudaURL(firstMapText(item, "downLoadUrl", "downloadUrl", "fileUrl", "url", "path")); u != "" {
		return "url:" + u
	}
	if title := firstMapText(item, "title", "name"); title != "" {
		return "title:" + title
	}
	return ""
}

func normalizeHoudaURL(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, `"'`))
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	switch {
	case raw == "":
		return ""
	case strings.HasPrefix(raw, "//"):
		return "http:" + raw
	case strings.HasPrefix(raw, "/"):
		u, err := url.Parse(urlOrigin)
		if err != nil {
			return raw
		}
		ref, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		return u.ResolveReference(ref).String()
	default:
		return raw
	}
}

func houdaFileExt(hint, rawURL string) string {
	hint = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(hint)), ".")
	if hint != "" && hint != "file" {
		return hint
	}
	if u, err := url.Parse(rawURL); err == nil {
		if ext := strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), "."); ext != "" {
			return ext
		}
	}
	if ext := strings.TrimPrefix(strings.ToLower(path.Ext(rawURL)), "."); ext != "" {
		return ext
	}
	return "pdf"
}

func addHoudaQuery(api string, params map[string]string) string {
	u, err := url.Parse(api)
	if err != nil {
		return api
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func houdaLessonFromAny(v any) houdaLesson {
	m, ok := v.(map[string]any)
	if !ok {
		return houdaLesson{}
	}
	var lesson houdaLesson
	b, err := json.Marshal(m)
	if err == nil {
		_ = json.Unmarshal(b, &lesson)
	}
	if firstText(lesson.ID) == "" {
		lesson.ID = firstMapText(m, "id", "liveId", "lessonId")
	}
	return lesson
}

func mergeHoudaLesson(base, extra houdaLesson) houdaLesson {
	if firstText(base.ID) == "" {
		base.ID = extra.ID
	}
	if firstText(base.Title) == "" {
		base.Title = extra.Title
	}
	if firstText(base.Name) == "" {
		base.Name = extra.Name
	}
	if firstText(base.CourseName) == "" {
		base.CourseName = extra.CourseName
	}
	if firstText(base.CourseID) == "" {
		base.CourseID = extra.CourseID
	}
	if firstText(base.ClassID) == "" {
		base.ClassID = extra.ClassID
	}
	if firstText(base.Type) == "" {
		base.Type = extra.Type
	}
	if firstText(base.CCLiveID) == "" {
		base.CCLiveID = extra.CCLiveID
	}
	if firstText(base.RoomID) == "" {
		base.RoomID = extra.RoomID
	}
	if firstText(base.MainRoomID) == "" {
		base.MainRoomID = extra.MainRoomID
	}
	if firstText(base.RecordID) == "" {
		base.RecordID = extra.RecordID
	}
	if firstText(base.LiveURL) == "" {
		base.LiveURL = extra.LiveURL
	}
	if firstText(base.PlaybackURL) == "" {
		base.PlaybackURL = extra.PlaybackURL
	}
	if firstText(base.PlaybackMP4) == "" {
		base.PlaybackMP4 = extra.PlaybackMP4
	}
	if firstText(base.PlaybackMP3) == "" {
		base.PlaybackMP3 = extra.PlaybackMP3
	}
	if firstText(base.StageID) == "" {
		base.StageID = extra.StageID
	}
	if firstText(base.StageName) == "" {
		base.StageName = extra.StageName
	}
	if firstText(base.LawID) == "" {
		base.LawID = extra.LawID
	}
	if firstText(base.LawName) == "" {
		base.LawName = extra.LawName
	}
	return base
}
