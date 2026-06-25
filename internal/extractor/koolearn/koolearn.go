// Package koolearn implements an extractor for koolearn.com / roombox.xdf.cn playback.
//
// API endpoints from decompiled Mooc/Courses/Koolearn/:
//
//	https://www.koolearn.com
//	https://order.koolearn.com/ordercenter/user_order/index?status=1&page=%s
//	https://order.koolearn.com/ordercenter/user_order/detail/%s
//	https://study.koolearn.com
//	https://study.koolearn.com/my-data?type=%s
//	https://i.koolearn.com/logininfo
//	https://api.roombox.xdf.cn/api/login/fetchToken/%s
//	https://api.roombox.xdf.cn/api/schedule/class/lessons?classId=%s&token=%s
//	https://api.roombox.xdf.cn/api/client/module/info/playback?classroomId=%s
//	https://api.roombox.xdf.cn/api/client/module/info?classroomId=%s&module=playback
package koolearn

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlHome           = "https://www.koolearn.com"
	urlOrderIndex     = "https://order.koolearn.com/ordercenter/user_order/index?status=1&page=%s"
	urlOrderDetail    = "https://order.koolearn.com/ordercenter/user_order/detail/%s"
	urlStudyHome      = "https://study.koolearn.com"
	urlMyData         = "https://study.koolearn.com/my-data?type=%s"
	urlLoginInfo      = "https://i.koolearn.com/logininfo"
	urlFetchToken     = "https://api.roombox.xdf.cn/api/login/fetchToken/%s"
	urlRoomCourse     = "https://api.roombox.xdf.cn/api/schedule/my-classes?pageSize=2000&token=%s"
	urlRoomSchedule   = "https://api.roombox.xdf.cn/api/schedule/my?queryType=1&startDate=1000000000&endDate=2000000000&token=%s"
	urlRoomLessons    = "https://api.roombox.xdf.cn/api/schedule/class/lessons?classId=%s&token=%s"
	urlPlaybackInfo   = "https://api.roombox.xdf.cn/api/client/module/info/playback?classroomId=%s"
	urlPlaybackModule = "https://api.roombox.xdf.cn/api/client/module/info?classroomId=%s&module=playback"
	urlRoomReferer    = "https://roombox.xdf.cn"
)

var patterns = []string{
	`(?:[\w-]+\.)?koolearn\.com/`,
	`(?:[\w-]+\.)?roombox\.xdf\.cn/`,
}

func init() {
	extractor.Register(&Koolearn{}, extractor.SiteInfo{Name: "Koolearn", URL: "koolearn.com", NeedAuth: true})
}

type Koolearn struct{}

func (k *Koolearn) Patterns() []string { return patterns }

var roomIDRe = regexp.MustCompile(`(?:cid|classId|classroomId)=([0-9]+)`)

func (k *Koolearn) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("koolearn requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)

	// Discovery path: a koolearn study/my-courses URL (not a direct roombox
	// classId) enumerates purchased courses via study.koolearn.com/my-data.
	// Mirrors Koolearn_App._select_my_course / _get_course_list which calls
	// _get_course_list() + _get_course_list('hide') and filters by course-type.
	classID := parseClassID(rawURL)
	if classID == "" {
		return discoverMyCourses(c, opts.Cookies, rawURL)
	}

	h5Token := cookieValue(opts.Cookies, "XDF_H5_TOKEN")
	if h5Token == "" {
		return nil, fmt.Errorf("koolearn roombox requires XDF_H5_TOKEN cookie")
	}
	roomToken, err := fetchRoomboxToken(c, h5Token)
	if err != nil {
		return nil, err
	}

	lessons, err := fetchRoomboxLessons(c, classID, roomToken)
	if err != nil {
		return nil, err
	}
	entries := make([]*extractor.MediaInfo, 0, len(lessons))
	for i, lesson := range lessons {
		entry, err := buildRoomboxEntry(c, i+1, lesson)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		entry, err := fetchRoomboxLiveEntry(c, classID, h5Token)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return &extractor.MediaInfo{Site: "koolearn", Title: "koolearn_" + classID, Entries: entries, Extra: map[string]any{"class_id": classID}}, nil
}

// courseLinkRe filters my-data course URLs by learnable course type.
// Source: Koolearn_App._get_course_list regex
// ((/tongyong/)|(/schedule/)|(/ky/)|(/fer/)|(/1v1/)|(/chuguo/)|(/tiny-class/)|(small-?class))
var courseLinkRe = regexp.MustCompile(`(/tongyong/)|(/schedule/)|(/ky/)|(/fer/)|(/1v1/)|(/chuguo/)|(/tiny-class/)|(small-?class)`)

// loginStatusRe matches the logged-in marker from i.koolearn.com/logininfo.
// Source: Koolearn_Base._check_cookie ('"status"\s*:\s*0').
var loginStatusRe = regexp.MustCompile(`"status"\s*:\s*0`)

// myDataCourse models an item in the my-data `data` list.
// Source: Koolearn_App._get_course_list reads productName/productDisplayName/url,
// and per-item aggregatedItems[].productDisplayName/url.
type myDataCourse struct {
	ProductName     string `json:"productName"`
	URL             string `json:"url"`
	AggregatedItems []struct {
		ProductDisplayName string `json:"productDisplayName"`
		URL                string `json:"url"`
	} `json:"aggregatedItems"`
}

type courseLink struct {
	title string
	url   string
}

// discoverMyCourses enumerates purchased/learnable courses from the user's
// "我的课程" page. Mirrors Koolearn_App._select_my_course which combines
// _get_course_list() and _get_course_list('hide').
func discoverMyCourses(c *util.Client, jar http.CookieJar, rawURL string) (*extractor.MediaInfo, error) {
	if !koolearnLogined(c) {
		return nil, fmt.Errorf("koolearn my-courses discovery requires login cookies (i.koolearn.com status!=0)")
	}
	seen := make(map[string]struct{})
	var links []courseLink
	for _, ctype := range []string{"", "hide"} {
		got, err := fetchCourseList(c, ctype)
		if err != nil {
			return nil, err
		}
		for _, l := range got {
			if _, ok := seen[l.url]; ok {
				continue
			}
			seen[l.url] = struct{}{}
			links = append(links, l)
		}
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("koolearn: no learnable courses found in my-data")
	}
	entries := make([]*extractor.MediaInfo, 0, len(links))
	for _, l := range links {
		entries = append(entries, &extractor.MediaInfo{
			Site:  "koolearn",
			Title: l.title,
			Extra: map[string]any{"url": l.url},
		})
	}
	return &extractor.MediaInfo{Site: "koolearn", Title: "koolearn_my_courses", Entries: entries}, nil
}

// koolearnLogined checks the i.koolearn.com/logininfo status marker.
// Source: Koolearn_Base._check_cookie (course_name == 'koolearn').
func koolearnLogined(c *util.Client) bool {
	body, err := c.GetString(urlLoginInfo, map[string]string{"Referer": urlHome})
	if err != nil {
		return false
	}
	return loginStatusRe.MatchString(body)
}

// fetchCourseList retrieves and filters one my-data course page.
// Source: Koolearn_App._get_course_list(course_type).
func fetchCourseList(c *util.Client, courseType string) ([]courseLink, error) {
	body, err := c.GetString(fmt.Sprintf(urlMyData, url.QueryEscape(courseType)), map[string]string{"Referer": urlHome})
	if err != nil {
		return nil, fmt.Errorf("koolearn my-data: %w", err)
	}
	var out struct {
		Data []myDataCourse `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		// JSONDecodeError -> empty data in source; treat as no courses.
		return nil, nil
	}
	var links []courseLink
	for _, item := range out.Data {
		if len(item.AggregatedItems) > 0 {
			for _, agg := range item.AggregatedItems {
				links = appendCourseLink(links, agg.ProductDisplayName, agg.URL)
			}
		} else {
			links = appendCourseLink(links, item.ProductName, item.URL)
		}
	}
	return links, nil
}

// appendCourseLink applies the course-type filter from
// Koolearn_App._get_course_list before appending.
func appendCourseLink(links []courseLink, title, courseURL string) []courseLink {
	courseURL = strings.TrimSpace(courseURL)
	if courseURL == "" || !courseLinkRe.MatchString(courseURL) {
		return links
	}
	full := courseURL
	if strings.HasPrefix(full, "/") {
		full = urlStudyHome + full
	}
	return append(links, courseLink{title: strings.TrimSpace(title), url: full})
}

func fetchRoomboxToken(c *util.Client, h5Token string) (string, error) {
	body, err := c.GetString(fmt.Sprintf(urlFetchToken, url.PathEscape(h5Token)), map[string]string{"token": h5Token, "Referer": urlRoomReferer})
	if err != nil {
		return "", fmt.Errorf("koolearn fetchToken: %w", err)
	}
	var out struct {
		Token string `json:"token"`
		Data  struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return "", fmt.Errorf("koolearn fetchToken parse: %w", err)
	}
	if out.Token != "" {
		return out.Token, nil
	}
	if out.Data.Token != "" {
		return out.Data.Token, nil
	}
	return "", fmt.Errorf("koolearn fetchToken: empty token")
}

func fetchRoomboxLessons(c *util.Client, classID, token string) ([]roomboxLesson, error) {
	body, err := c.GetString(fmt.Sprintf(urlRoomLessons, url.QueryEscape(classID), url.QueryEscape(token)), roomHeaders(token))
	if err != nil {
		return nil, fmt.Errorf("koolearn class lessons: %w", err)
	}
	var out struct {
		Data struct {
			List []roomboxLesson `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("koolearn class lessons parse: %w", err)
	}
	return out.Data.List, nil
}

type roomboxLesson struct {
	ID            any             `json:"id"`
	RoomID        any             `json:"room_id"`
	ClassID       any             `json:"class_id"`
	Title         string          `json:"title"`
	Name          string          `json:"name"`
	ClassName     string          `json:"className"`
	ClassroomName string          `json:"classroom_name"`
	Playback      roomboxPlayback `json:"playback"`
	RecordedMedia roomboxRecorded `json:"recordedMedia"`
	StartTime     any             `json:"start_time"`
}

type roomboxPlayback struct {
	URLs          any             `json:"urls"`
	VideoURL      any             `json:"videoUrl"`
	RecordedMedia roomboxRecorded `json:"recordedMedia"`
}

type roomboxRecorded struct {
	URL string `json:"url"`
}

func buildRoomboxEntry(c *util.Client, index int, lesson roomboxLesson) (*extractor.MediaInfo, error) {
	title := firstText(lesson.ClassroomName, lesson.Title, lesson.Name, lesson.ClassName, "未命名")
	roomID := firstText(lesson.ID, lesson.RoomID)
	videoURL := extractPlaybackURL(lesson.Playback, lesson.RecordedMedia)
	if videoURL == "" && roomID != "" {
		var err error
		videoURL, err = fetchRoomboxModuleURL(c, roomID)
		if err != nil {
			return nil, err
		}
	}
	if videoURL == "" {
		return nil, nil
	}
	stream := extractor.Stream{Quality: "best", URLs: []string{videoURL}, Format: mediaExt(videoURL), Headers: map[string]string{"Referer": urlRoomReferer}}
	return &extractor.MediaInfo{Site: "koolearn", Title: fmt.Sprintf("[%d]-%s", index, title), Streams: map[string]extractor.Stream{"best": stream}, Extra: map[string]any{"room_id": roomID, "class_id": firstText(lesson.ClassID)}}, nil
}

func fetchRoomboxModuleURL(c *util.Client, roomID string) (string, error) {
	body, err := c.GetString(fmt.Sprintf(urlPlaybackModule, url.QueryEscape(roomID)), map[string]string{"Referer": urlRoomReferer})
	if err != nil {
		return "", fmt.Errorf("koolearn module playback: %w", err)
	}
	var out struct {
		Data struct {
			Playback      roomboxPlayback `json:"playback"`
			RecordedMedia roomboxRecorded `json:"recordedMedia"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return "", fmt.Errorf("koolearn module playback parse: %w", err)
	}
	return extractPlaybackURL(out.Data.Playback, out.Data.RecordedMedia), nil
}

func fetchRoomboxLiveEntry(c *util.Client, classID, h5Token string) (*extractor.MediaInfo, error) {
	body, err := c.GetString(fmt.Sprintf(urlPlaybackInfo, url.QueryEscape(classID)), map[string]string{"Referer": urlRoomReferer, "Token": h5Token})
	if err != nil {
		return nil, fmt.Errorf("koolearn live playback: %w", err)
	}
	var out struct {
		Data struct {
			Name          string          `json:"name"`
			VideoURL      any             `json:"videoUrl"`
			Playback      roomboxPlayback `json:"playback"`
			RecordedMedia roomboxRecorded `json:"recordedMedia"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("koolearn live playback parse: %w", err)
	}
	videoURL := firstText(out.Data.VideoURL)
	if videoURL == "" {
		videoURL = extractPlaybackURL(out.Data.Playback, out.Data.RecordedMedia)
	}
	if videoURL == "" {
		return nil, fmt.Errorf("koolearn: no playback URL for classroomId=%s", classID)
	}
	return &extractor.MediaInfo{Site: "koolearn", Title: firstText(out.Data.Name, "koolearn_"+classID), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{videoURL}, Format: mediaExt(videoURL), Headers: map[string]string{"Referer": urlRoomReferer}}}, Extra: map[string]any{"class_id": classID}}, nil
}

func extractPlaybackURL(p roomboxPlayback, recorded roomboxRecorded) string {
	if u := firstURL(p.URLs); u != "" {
		return u
	}
	if u := firstURL(p.VideoURL); u != "" {
		return u
	}
	if p.RecordedMedia.URL != "" {
		return p.RecordedMedia.URL
	}
	return recorded.URL
}

func firstURL(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		for _, item := range x {
			if s := firstText(item); s != "" {
				return s
			}
		}
	case []string:
		if len(x) > 0 {
			return strings.TrimSpace(x[0])
		}
	}
	return ""
}

func roomHeaders(token string) map[string]string {
	return map[string]string{"Referer": urlRoomReferer, "Token": token}
}

func cookieValue(jar http.CookieJar, name string) string {
	hosts := []string{"api.roombox.xdf.cn", "roombox.xdf.cn", "d.roombox.xdf.cn", "study.koolearn.com", "www.koolearn.com", "i.koolearn.com"}
	for _, host := range hosts {
		u := &url.URL{Scheme: "https", Host: host}
		for _, ck := range jar.Cookies(u) {
			if ck.Name == name {
				return ck.Value
			}
		}
	}
	return ""
}

func parseClassID(rawURL string) string {
	if m := roomIDRe.FindStringSubmatch(rawURL); len(m) > 1 {
		return m[1]
	}
	return ""
}

func mediaExt(u string) string {
	lu := strings.ToLower(u)
	switch {
	case strings.Contains(lu, ".m3u8"):
		return "m3u8"
	case strings.Contains(lu, ".flv"):
		return "flv"
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
