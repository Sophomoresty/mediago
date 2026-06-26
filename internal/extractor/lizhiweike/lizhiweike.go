// Package lizhiweike implements an extractor for lizhiweike.com (荔枝微课) courses.
package lizhiweike

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlCheckToken = "https://open.lizhiweike.com/oauth2/check_token?token=%s"
	urlBuyRecord  = "https://apiv1.lizhiweike.com/api/history/buy_record"
	urlMobile     = "https://m.lizhiweike.com"
	urlCourseList = "https://apiv1.lizhiweike.com/api/personal_center/my_weike/%s/my_lectures?token=%s&offset=%s&limit=10"
	urlInfo       = "https://apiv1.lizhiweike.com/api/%s/%s/info?token=%s"
	urlVideo      = "https://apiv1.lizhiweike.com/api/lecture/%s/info?token=%s"
	urlLive       = "https://gateway-weike.lizhiweike.com/tic/record?lecture_id=%s&object_type=lecture&version=1.0&token=%s"
	urlM3U8       = "https://apiv1.lizhiweike.com/api/bridge/qcvideo/%s?token=%s&al=drm"
	urlJoin       = "https://apiv1.lizhiweike.com/api/channel/%s/subscribe?token=%s"
	urlAudioList  = "https://apiv1.lizhiweike.com/api/classroom/%s/message/get/voice?token=%s"
	urlVideoList  = "https://apiv1.lizhiweike.com/api/classroom/%s/message/list?token=%s&new_classroom=1&is_reverse=0&limit=2000"
)

var patterns = []string{`(?:[\w-]+\.)?(?:lizhiweike\.com|tenexer\.cn|szbaimao\.com|shifangfm\.com|shifangwk\.cn|xrcox\.cn|ckkzk\.cn|tenclass\.cn|liveweike\.com)/`, `#小程序://荔课`}

func init() {
	extractor.Register(&Lizhiweike{}, extractor.SiteInfo{Name: "Lizhiweike", URL: "lizhiweike.com", NeedAuth: true})
}

type Lizhiweike struct{}

func (l *Lizhiweike) Patterns() []string { return patterns }

var (
	channelRe = regexp.MustCompile(`/channel\d?/([0-9]+)`)
	lectureRe = regexp.MustCompile(`/(?:lecture|liveplay|classroom|liveroom)\d?/([0-9]+)`)
)

type lizhiSession struct {
	Cookie, Token, WID string
	Headers            map[string]string
}

type lizhiTarget struct {
	ID, Type, Title, LiveID, FallbackLectureID string
	Single                                     bool
}

type lizhiLecture struct{ Mode, LiveID, VideoID, Title string }

type lizhiCourseRef struct{ Type, ID, LiveID, Title string }

func (l *Lizhiweike) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("lizhiweike requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess, err := lizhiBuildSession(c, opts.Cookies)
	if err != nil {
		return nil, err
	}

	target, err := lizhiResolveTarget(c, rawURL, sess)
	if err != nil {
		return nil, err
	}
	info, err := lizhiGetJSON(c, fmt.Sprintf(urlInfo, target.Type, target.ID, sess.Token), sess.Headers)
	if err != nil {
		return nil, fmt.Errorf("lizhiweike info %s/%s: %w", target.Type, target.ID, err)
	}
	title := firstText(target.Title, nestedText(info, "data", "share_info", "share_title"), "lizhiweike_"+target.ID)
	price := lizhiExtractPrice(info)
	purchased := lizhiExtractPurchased(info, target)
	if price > 0 && purchased {
		if refined := lizhiRefineOrderPrice(c, sess, target.ID); refined > 0 {
			price = refined
		}
	}
	lectures := lizhiLecturesFromInfo(info, target)
	entries := make([]*extractor.MediaInfo, 0, len(lectures))
	for i, item := range lectures {
		entry, err := lizhiBuildEntry(c, sess, item, i+1)
		if err == nil && entry != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("lizhiweike: no playable lecture entries for %s/%s", target.Type, target.ID)
	}
	extra := map[string]any{"course_type": target.Type, "course_id": target.ID}
	if price > 0 {
		extra["price"] = price
	}
	if purchased {
		extra["purchased"] = true
	}
	return &extractor.MediaInfo{Site: "lizhiweike", Title: title, Entries: entries, Extra: extra}, nil
}

// lizhiExtractPrice extracts the initial course price from the info response.
// Source: Lizhiweike_Base._update_common_info – checks data.resell.money then data.channel.money (cents→yuan).
func lizhiExtractPrice(info map[string]any) float64 {
	if v := numOf(nested(info, "data", "resell", "money")); v > 0 {
		return v / 100
	}
	if v := numOf(nested(info, "data", "channel", "money")); v > 0 {
		return v / 100
	}
	return 0
}

// lizhiExtractPurchased returns whether the current user has purchased/subscribed the course.
// Source: Lizhiweike_Course._get_infos – checks data.channel_access or data.lecture_access (granted || subscribed).
func lizhiExtractPurchased(info map[string]any, target lizhiTarget) bool {
	if target.Type == "channel" || !target.Single {
		access := mapAny(nested(info, "data", "channel_access"))
		return boolOf(access["granted"]) || boolOf(access["subscribed"])
	}
	access := mapAny(nested(info, "data", "lecture_access"))
	return boolOf(access["granted"]) || boolOf(access["subscribed"])
}

// lizhiRefineOrderPrice fetches the buy_record endpoint and finds the actual paid fee
// for the given course id. Returns fee/100 (yuan) or 0 if not found.
// Source: Lizhiweike_Base._get_order_price.
func lizhiRefineOrderPrice(c *util.Client, sess *lizhiSession, courseID string) float64 {
	body, err := c.GetString(urlBuyRecord, sess.Headers)
	if err != nil {
		return 0
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return 0
	}
	for _, rec := range records(nested(resp, "data", "records")) {
		invoice := mapAny(rec["invoice"])
		if firstText(invoice["object_option_id"]) == courseID {
			if fee := numOf(invoice["fee"]); fee > 0 {
				return fee / 100
			}
			break
		}
	}
	return 0
}

func lizhiBuildSession(c *util.Client, jar http.CookieJar) (*lizhiSession, error) {
	cookie := lizhiCookieString(jar)
	token := cookieValue(cookie, "token")
	if token == "" {
		return nil, fmt.Errorf("lizhiweike requires token cookie")
	}
	headers := map[string]string{"referer": urlMobile, "cookie": cookie}
	if idToken := cookieValue(cookie, "id_token"); idToken != "" {
		headers["authorization"] = idToken
	}
	body, err := c.GetString(fmt.Sprintf(urlCheckToken, token), map[string]string{"cookie": cookie, "referer": urlMobile})
	if err != nil {
		return nil, fmt.Errorf("lizhiweike check_token: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("lizhiweike check_token parse: %w", err)
	}
	valid := intOf(out["code"]) == 0 && (boolOf(out["is_valid"]) || boolOf(mapAny(out["data"])["is_valid"]))
	if !valid && !(strings.Contains(body, `"code":0`) && strings.Contains(body, `"is_valid":true`)) {
		return nil, fmt.Errorf("lizhiweike requires valid token cookie")
	}
	return &lizhiSession{Cookie: cookie, Token: token, WID: cookieValue(cookie, "id"), Headers: headers}, nil
}

func lizhiResolveTarget(c *util.Client, rawURL string, sess *lizhiSession) (lizhiTarget, error) {
	var t lizhiTarget
	if m := channelRe.FindStringSubmatch(rawURL); len(m) > 1 {
		t = lizhiTarget{ID: m[1], Type: "channel", Single: false}
	} else if m := lectureRe.FindStringSubmatch(rawURL); len(m) > 1 {
		t = lizhiTarget{ID: m[1], Type: "lecture", Single: true, FallbackLectureID: m[1]}
		if info, err := lizhiGetJSON(c, fmt.Sprintf(urlVideo, t.ID, sess.Token), sess.Headers); err == nil {
			if chID := nestedText(info, "data", "channel", "id"); chID != "" {
				if chInfo, err := lizhiGetJSON(c, fmt.Sprintf(urlInfo, "channel", chID, sess.Token), sess.Headers); err == nil {
					if objectID := nestedText(chInfo, "data", "object_id"); objectID != "" {
						chID = objectID
					}
				}
				t.ID, t.Type, t.Single = chID, "channel", false
			}
		}
	}
	courses, _ := lizhiFetchCourseList(c, sess)
	if picked := lizhiPickCourse(courses, t.ID); picked.ID != "" {
		t.ID, t.Type, t.Title, t.LiveID = picked.ID, picked.Type, picked.Title, picked.LiveID
	} else if t.ID == "" && len(courses) > 0 {
		t.ID, t.Type, t.Title, t.LiveID = courses[0].ID, courses[0].Type, courses[0].Title, courses[0].LiveID
	}
	if t.ID == "" || t.Type == "" {
		return t, fmt.Errorf("cannot parse lizhiweike channel/lecture id from URL: %s", rawURL)
	}
	t.Single = t.Type != "channel"
	return t, nil
}

func lizhiFetchCourseList(c *util.Client, sess *lizhiSession) ([]lizhiCourseRef, error) {
	if sess.WID == "" || sess.Token == "" {
		return nil, nil
	}
	var courses []lizhiCourseRef
	offset := 0
	for i := 0; i < 31; i++ {
		body, err := c.GetString(fmt.Sprintf(urlCourseList, sess.WID, sess.Token, strconv.Itoa(offset)), sess.Headers)
		if err != nil {
			return courses, err
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			break
		}
		lectures := records(nested(resp, "data", "lectures"))
		if len(lectures) == 0 {
			break
		}
		for _, rec := range lectures {
			if rec["status"] == "deleted" {
				continue
			}
			courses = append(courses, lizhiCourseRef{Type: firstText(rec["type"]), LiveID: firstText(rec["liveroom_id"]), ID: firstText(rec["id"]), Title: firstText(rec["name"])})
		}
		offset += len(lectures)
	}
	return courses, nil
}

func lizhiPickCourse(courses []lizhiCourseRef, id string) lizhiCourseRef {
	for _, c := range courses {
		if id != "" && c.ID == id {
			return c
		}
	}
	return lizhiCourseRef{}
}

func lizhiLecturesFromInfo(info map[string]any, target lizhiTarget) []lizhiLecture {
	if !target.Single {
		lectures := records(nested(info, "data", "lectures"))
		out := make([]lizhiLecture, 0, len(lectures))
		for i, rec := range lectures {
			mode := firstText(rec["lecture_mode"])
			if mode != "video" && mode != "audio" && mode != "live_v" && mode != "default" {
				continue
			}
			id := firstText(rec["id"])
			if id == "" {
				continue
			}
			title := fmt.Sprintf("[%d]--%s", i+1, strings.TrimSuffix(firstText(rec["name"]), ".mp4"))
			out = append(out, lizhiLecture{Mode: mode, VideoID: id, LiveID: firstText(rec["liveroom_id"]), Title: title})
		}
		if len(out) > 0 {
			return out
		}
	}
	lec := mapAny(nested(info, "data", "lecture"))
	return []lizhiLecture{{Mode: firstText(lec["lecture_mode"]), VideoID: firstText(lec["id"], target.ID), LiveID: firstText(lec["liveroom_id"], target.LiveID), Title: strings.TrimSuffix(firstText(lec["name"], target.Title, target.ID), ".mp4")}}
}

func lizhiBuildEntry(c *util.Client, sess *lizhiSession, item lizhiLecture, idx int) (*extractor.MediaInfo, error) {
	if item.Title == "" {
		item.Title = fmt.Sprintf("[%d]--%s", idx, item.VideoID)
	}
	var urls []string
	var size int64
	switch item.Mode {
	case "audio":
		if u := lizhiAudioURL(c, sess, item.VideoID); u != "" {
			urls = []string{u}
		}
	case "live_v":
		if u, n := lizhiLiveURL(c, sess, firstText(item.LiveID, item.VideoID)); u != "" {
			urls, size = []string{u}, n
		}
	case "default":
		urls = append(urls, lizhiVideoURLList(c, sess, item.VideoID)...)
		if len(urls) == 0 {
			urls = append(urls, lizhiAudioURLList(c, sess, item.VideoID)...)
		}
	}
	if len(urls) == 0 {
		if u, n := lizhiVideoURL(c, sess, item.VideoID); u != "" {
			urls, size = []string{u}, n
		}
	}
	if len(urls) == 0 {
		urls = append(urls, lizhiVideoURLList(c, sess, item.VideoID)...)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("lizhiweike: empty media url for lecture=%s", item.VideoID)
	}
	return &extractor.MediaInfo{Site: "lizhiweike", Title: item.Title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: urls, Format: mediaExt(urls[0]), Size: size, NeedMerge: len(urls) > 1, Headers: sess.Headers}}, Extra: map[string]any{"lecture_mode": item.Mode, "video_id": item.VideoID, "live_id": item.LiveID}}, nil
}

func lizhiVideoURL(c *util.Client, sess *lizhiSession, id string) (string, int64) {
	info, err := lizhiGetJSON(c, fmt.Sprintf(urlVideo, id, sess.Token), sess.Headers)
	if err != nil {
		return "", 0
	}
	vfid := nestedText(info, "data", "video_info", "qcloud_video_file_id")
	if vfid == "" {
		return firstText(nested(info, "data", "video_info", "video_url"), nested(info, "data", "video_info", "url")), 0
	}
	play, err := lizhiGetJSON(c, fmt.Sprintf(urlM3U8, vfid, sess.Token), sess.Headers)
	if err != nil {
		return "", 0
	}
	list := records(nested(play, "data", "play_list"))
	sort.SliceStable(list, func(i, j int) bool { return lizhiDefinitionRank(list[i]) > lizhiDefinitionRank(list[j]) })
	for _, rec := range list {
		if u := firstText(rec["url"]); u != "" {
			return u, int64(numOf(rec["size"]))
		}
	}
	return "", 0
}

func lizhiLiveURL(c *util.Client, sess *lizhiSession, id string) (string, int64) {
	resp, err := lizhiGetJSON(c, fmt.Sprintf(urlLive, id, sess.Token), sess.Headers)
	if err != nil {
		return "", 0
	}
	return nestedText(resp, "data", "mp4", "media_url"), int64(numOf(nested(resp, "data", "mp4", "file_size")))
}

func lizhiAudioURL(c *util.Client, sess *lizhiSession, id string) string {
	resp, err := lizhiGetJSON(c, fmt.Sprintf(urlVideo, id, sess.Token), sess.Headers)
	if err != nil {
		return ""
	}
	return nestedText(resp, "data", "audio_info", "audio_url")
}

func lizhiAudioURLList(c *util.Client, sess *lizhiSession, id string) []string {
	resp, err := lizhiGetJSON(c, fmt.Sprintf(urlAudioList, id, sess.Token), sess.Headers)
	if err != nil {
		return nil
	}
	var out []string
	for _, rec := range records(resp["data"]) {
		if u := firstText(rec["audio"]); u != "" {
			out = append(out, u)
		}
	}
	return out
}

func lizhiVideoURLList(c *util.Client, sess *lizhiSession, id string) []string {
	resp, err := lizhiGetJSON(c, fmt.Sprintf(urlVideoList, id, sess.Token), sess.Headers)
	if err != nil {
		return nil
	}
	var out []string
	for _, rec := range records(nested(resp, "data", "messages")) {
		if firstText(rec["type"]) == "video" {
			if u := nestedText(rec, "meta", "video_url"); strings.HasPrefix(u, "http") {
				out = append(out, u)
			}
		}
	}
	return out
}

func lizhiGetJSON(c *util.Client, apiURL string, headers map[string]string) (map[string]any, error) {
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}
