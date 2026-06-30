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

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlGetPlayInfo = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id=%s&token=%s&use_encrypt=0&render=jsonp"
	urlGetPlayURL  = "https://www.baijiayun.com/vod/video/getPlayUrl?vid=%s&render=jsonp&token=%s&use_encrypt=0"
	urlHome        = "https://www.baijiayun.com"

	urlUserInfo          = "https://%s/api/app/userInfo"
	urlMyOrder           = "https://%s/api/app/myOrder"
	urlCourseList        = "https://%s/api/app/myStudy/%s?type=open"
	urlCourseInfo        = "https://%s/api/app/myStudy/course/%s"
	urlCourseBasis       = "https://%s/api/app/courseInfo/basis_id=%s"
	urlToken             = "https://%s/api/app/getPcRoomCode/course_id=%s/chapter_id=%s?type=1"
	urlPlayToken         = "https://%s/api/app/getPlayToken/chapter_id=%s/course_id=%s"
	urlPreviewVideo      = "https://%s/api/app/user/CourseWare/video/preview?video_id=%s"
	urlCourseWarePreview = "https://%s/api/app/user/CourseWare/preview?fid=%s"
)

var patterns = []string{
	`(?:[\w-]+\.)?(?:baijiayun|baijicloud|baijiayunxiao)\.com/`,
	`https?://[^"' <>\t\r\n]+/course/\d+(?:\?[^"' <>\t\r\n]*type=bjyx)?`,
	`https?://[^"' <>\t\r\n]+/s/[\w-]+`,
}

var defaultCourseTypes = []string{"2", "1", "22"}

// Source sample short link accepted by Baijiayun_Video.py:
// https://a53882449.dxb.baijiayun.com/s/uqmkGogZRD

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
	ctype  string
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
	ID      string
	Title   string
	Payload any
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
	headers := baijiayunxiaoHeaders(opts.Cookies, rawURL)

	if m := evURLRe.FindStringSubmatch(rawURL); m != nil {
		return mediaInfo("baijiayunxiao", util.SanitizeFilename(m[1]), rawURL, strings.ToLower(m[2]), headers), nil
	}
	if p, ok := parsePlaybackParams(rawURL); ok {
		return resolvePlayback(c, p, headers, "baijiayunxiao")
	}
	if yt, ok := parseYunduanTarget(rawURL); ok {
		return resolveYunduan(c, yt, opts.Cookies, headers)
	}
	if cu, ok := parseCourseURL(rawURL); ok {
		if err := validateBaijiayunxiaoLogin(c, cu.domain, headers); err != nil {
			return nil, err
		}
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
	catalogTitle := ""
	if cu.cid == "" {
		courses := fetchCourseList(c, cu.domain, cu.ctype, headers)
		if len(courses) == 0 {
			return nil, fmt.Errorf("baijiayunxiao: no courses from myStudy list")
		}
		cu.cid = courses[0].ID
		catalogTitle = courses[0].Title
	}
	infoURL := fmt.Sprintf(urlCourseInfo, cu.domain, url.PathEscape(cu.cid))
	body, err := c.GetString(infoURL, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch baijiayunxiao course info: %w", err)
	}
	var resp courseInfoResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parse baijiayunxiao course info: %w", err)
	}
	rawInfo := decodeJSONMap(body)
	rawData := valueMap(rawInfo["data"])
	lessons := collectLessonRefs(rawData["periods"], rawData["chapter"], rawData["chapter_list"], rawData["chapterList"])
	if len(lessons) == 0 {
		lessons = collectLessons(resp.Data.Periods, nil)
		lessons = append(lessons, collectLessons(resp.Data.Chapter, nil)...)
	}
	if len(lessons) == 0 {
		return nil, fmt.Errorf("baijiayunxiao: no lesson ids in course data")
	}

	basisTitle, price := fetchCourseBasis(c, cu, headers)
	orderPurchased, orderPrice := fetchOrderPrice(c, cu, headers)
	if price == nil {
		price = orderPrice
	}
	seenMaterials := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(lessons))
	appendMaterialEntries(c, cu.domain, &entries, seenMaterials, extractMaterials(rawInfo, "资料"), headers)
	for i, lesson := range lessons {
		appendMaterialEntries(c, cu.domain, &entries, seenMaterials, extractMaterials(lesson.Payload, firstNonEmpty(lesson.Title, "资料")), headers)

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
				mergeExtra(entry, map[string]any{"course_id": cu.cid, "video_id": lesson.ID, "room_id": roomID})
				entries = append(entries, entry)
				continue
			}
		}

		p := playbackParams{roomID: roomID, token: token}
		entry, err := resolvePlayback(c, p, headers, entryTitle)
		if err == nil {
			mergeExtra(entry, map[string]any{"course_id": cu.cid, "video_id": lesson.ID, "room_id": roomID})
			entries = append(entries, entry)
			if docURL := firstNonEmpty(anyString(entry.Extra["doc_url"]), anyString(entry.Extra["package_url"])); docURL != "" {
				appendMaterialEntries(c, cu.domain, &entries, seenMaterials, fetchDocMaterials(c, docURL, entryTitle, headers), headers)
			}
			continue
		}

		// Fallback: when fromPcRoomCode is true but playback resolution
		// failed, also try the preview video API.
		if fromPcRoomCode {
			if previewURL := fetchPreviewVideoURL(c, cu.domain, roomID, headers); previewURL != "" {
				entry := mediaInfo("baijiayunxiao", util.SanitizeFilename(entryTitle), previewURL, pickFormat(previewURL), headers)
				mergeExtra(entry, map[string]any{"course_id": cu.cid, "video_id": lesson.ID, "room_id": roomID})
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("baijiayunxiao: parsed lessons but no baijiayun stream resolved")
	}
	title := firstNonEmpty(basisTitle, catalogTitle, resp.Data.Title, resp.Data.Name, anyString(rawData["title"]), anyString(rawData["name"]), "baijiayunxiao_"+cu.cid)
	extra := map[string]any{"course_id": cu.cid}
	if orderPurchased {
		extra["purchased"] = true
	}
	if price != nil {
		extra["price"] = price
	}
	return &extractor.MediaInfo{Site: "baijiayunxiao", Title: util.SanitizeFilename(title), Entries: entries, Extra: extra}, nil
}

func validateBaijiayunxiaoLogin(c *util.Client, domain string, headers map[string]string) error {
	if domain == "" {
		return fmt.Errorf("baijiayunxiao login check requires domain")
	}
	auth := firstNonEmpty(headers["authorization"], headers["Authorization"])
	if auth == "" {
		return fmt.Errorf("baijiayunxiao requires studentToken cookie")
	}
	body, err := c.GetString(fmt.Sprintf(urlUserInfo, domain), headers)
	if err != nil {
		return fmt.Errorf("baijiayunxiao userInfo login check: %w", err)
	}
	payload := decodeJSONMap(body)
	if anyString(payload["code"]) == "200" {
		return nil
	}
	return fmt.Errorf("baijiayunxiao userInfo login check failed: code=%s message=%s", anyString(payload["code"]), firstNonEmpty(anyString(payload["message"]), anyString(payload["msg"])))
}

type baijiayunxiaoCourse struct{ ID, Title string }

func fetchCourseList(c *util.Client, domain, ctype string, headers map[string]string) []baijiayunxiaoCourse {
	queue := append([]string{}, ctype)
	queue = append(queue, defaultCourseTypes...)
	seenTypes := map[string]bool{}
	seenCourses := map[string]bool{}
	var out []baijiayunxiaoCourse
	for len(queue) > 0 {
		t := strings.TrimSpace(queue[0])
		queue = queue[1:]
		if t == "" || seenTypes[t] {
			continue
		}
		seenTypes[t] = true
		body, err := c.GetString(fmt.Sprintf(urlCourseList, domain, url.PathEscape(t)), headers)
		if err != nil {
			continue
		}
		payload := decodeJSONMap(body)
		for _, nextType := range extractCourseTypes(payload) {
			if !seenTypes[nextType] {
				queue = append(queue, nextType)
			}
		}
		for _, course := range extractCourseList(payload) {
			if course.ID != "" && !seenCourses[course.ID] {
				seenCourses[course.ID] = true
				out = append(out, course)
			}
		}
	}
	return out
}

func extractCourseTypes(payload map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	data := valueMap(payload["data"])
	for _, rec := range recordsValue(data["typeNum"]) {
		t := firstNonEmpty(anyString(rec["type"]), anyString(rec["ctype"]), anyString(rec["id"]))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func extractCourseList(payload map[string]any) []baijiayunxiaoCourse {
	data := valueMap(payload["data"])
	candidates := append(recordsValue(data["courseList"]), recordsValue(data["list"])...)
	candidates = append(candidates, recordsValue(data["records"])...)
	var out []baijiayunxiaoCourse
	for _, rec := range candidates {
		id := firstNonEmpty(anyString(rec["course_id"]), anyString(rec["courseId"]), anyString(rec["id"]))
		title := firstNonEmpty(anyString(rec["title"]), anyString(rec["name"]), anyString(rec["courseName"]))
		if id != "" && title != "" {
			out = append(out, baijiayunxiaoCourse{ID: id, Title: title})
		}
	}
	return out
}

func fetchCourseBasis(c *util.Client, cu courseURL, headers map[string]string) (string, any) {
	if cu.domain == "" || cu.cid == "" {
		return "", nil
	}
	body, err := c.GetString(fmt.Sprintf(urlCourseBasis, cu.domain, url.PathEscape(cu.cid)), headers)
	if err != nil {
		return "", nil
	}
	payload := decodeJSONMap(body)
	info := valueMap(valueMap(payload["data"])["info"])
	title := firstNonEmpty(anyString(info["title"]), anyString(info["name"]))
	return title, priceYuan(info["price"])
}

// fetchLessonToken returns (token, roomID, fromPcRoomCode, error).
// fromPcRoomCode is true when the token came from getPcRoomCode (first endpoint);
// false when it came from getPlayToken (second endpoint). The source uses this
// flag to decide the video resolution order: when fromPcRoomCode is false, the
// preview video API is tried first as a fallback.

func fetchOrderPrice(c *util.Client, cu courseURL, headers map[string]string) (bool, any) {
	if cu.domain == "" || cu.cid == "" {
		return false, nil
	}
	apiURL := fmt.Sprintf(urlMyOrder, cu.domain)
	for page := 1; page < 10; page++ {
		body, _ := json.Marshal(map[string]any{
			"page":       page,
			"order_type": "2",
			"limit":      15,
		})
		h := cloneHeaders(headers)
		h["Content-Type"] = "application/json"
		resp, err := c.Post(apiURL, bytes.NewReader(body), h)
		if err != nil {
			return false, nil
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return false, nil
		}
		payload := decodeJSONMap(string(respBody))
		orders := recordsValue(valueMap(payload["data"])["list"])
		if len(orders) == 0 {
			break
		}
		for _, order := range orders {
			if anyString(order["shop_ids"]) != cu.cid {
				continue
			}
			price := priceYuan(firstNonNil(order["ship_price"], order["order_price"], order["price"]))
			return true, price
		}
	}
	return false, nil
}

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
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		token = pickRegex(tokenRe, body)
		roomID = firstNonEmpty(pickRegex(classIDRe, body), lessonID)
		return token, roomID, false, nil
	}
	token = firstNonEmpty(resp.Token, resp.Data.Token, pickRegex(tokenRe, body))
	roomID = firstNonEmpty(anyString(resp.VideoID), anyString(resp.RoomID), anyString(resp.ClassID), anyString(resp.Data.VideoID), anyString(resp.Data.RoomID), anyString(resp.Data.ClassID), pickRegex(classIDRe, body), lessonID)
	return token, roomID, false, nil
}

func resolvePlayback(c *util.Client, p playbackParams, headers map[string]string, title string) (*extractor.MediaInfo, error) {
	res, resErr := resolvePlaybackDetailed(c, p, headers)
	if resErr == nil && res.VideoURL != "" {
		id := firstNonEmpty(p.vid, p.roomID, "playback")
		entry := mediaInfo("baijiayunxiao", util.SanitizeFilename(firstNonEmpty(title, res.Title, "baijiayunxiao_"+id)), res.VideoURL, pickFormat(res.VideoURL), headers)
		for key, stream := range entry.Streams {
			if res.AudioURL != "" {
				stream.AudioURL = res.AudioURL
			}
			entry.Streams[key] = stream
		}
		entry.Extra = map[string]any{
			"audio_url":                 res.AudioURL,
			"doc_url":                   res.DocURL,
			"mp4_url":                   res.MP4URL,
			"package_url":               res.PackageURL,
			"signal_chat_file_info_url": res.SignalChatFileInfoURL,
			"source":                    res.Source,
		}
		return entry, nil
	}

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
		if resErr != nil {
			return nil, resErr
		}
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
	var payloadBuf bytes.Buffer
	enc := json.NewEncoder(&payloadBuf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(struct {
		Ticket string `json:"ticket"`
		LiveID string `json:"liveId"`
	}{Ticket: ticket, LiveID: liveID}); err != nil {
		return "", err
	}
	payload := strings.TrimSpace(payloadBuf.String())
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
