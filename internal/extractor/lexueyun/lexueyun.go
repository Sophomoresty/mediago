// Package lexueyun implements an extractor for lexue-cloud.com (乐学云) courses.
//
// Endpoints from decompiled Mooc/Courses/Lexueyun/:
//
//	https://my.lexue-cloud.com
//	/happyStudy/user/userInfo
//	/happyStudy/proxy/lexuesv/pc/getLessonsBySubject
//	/happyStudy/live/getPlayUrl
//	/happyStudy/livePro/getPlayUrl
//	https://video.sunlands.com/video/thirdLogin
package lexueyun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlOrigin          = "https://my.lexue-cloud.com"
	urlReferer         = urlOrigin + "/home"
	channelCode        = "lexueyun-pc"
	userInfoPath       = "/happyStudy/user/userInfo"
	merchantListPath   = "/happyStudy/proxy/lexuesv/app/myMerchantList/v2"
	orderListPath      = "/happyStudy/proxy/lexuesv/app/getOrdersByMerchant/v2"
	subjectDetailPath  = "/happyStudy/proxy/lexuesv/pc/getSubjectDetail"
	lessonListPath     = "/happyStudy/proxy/lexuesv/pc/getLessonsBySubject"
	datumPath          = "/happyStudy/proxy/lexuesv/pc/getDatum"
	orderInfoPath      = "/happyStudy/proxy/lexuesv/pc/getOrderInfo"
	lessonProgressPath = "/happyStudy/proxy/lexuesv/app/getLessonLearnProgress"
	livePlayPath       = "/happyStudy/live/getPlayUrl"
	liveProPlayPath    = "/happyStudy/livePro/getPlayUrl"
	sunlandsVideoEntry = "https://video.sunlands.com/video"
	defaultHiddenPrice = 999
)

var patterns = []string{`(?:[\w-]+\.)?lexue-cloud\.com/|(?:lexueyun|lexue-cloud|乐学云课堂|乐学云)`}

func init() {
	extractor.Register(&Lexueyun{}, extractor.SiteInfo{Name: "Lexueyun", URL: "lexue-cloud.com", NeedAuth: true})
}

type Lexueyun struct{}

func (l *Lexueyun) Patterns() []string { return patterns }

type lexueSession struct {
	auth, stuID string
	user        map[string]any
}
type courseSel struct {
	subjectID, ordSerialNo, orderID, title string
	packageID, merchantID                  string
}

type userInfoResp struct {
	Flag any `json:"flag"`
	Data struct {
		ID            any    `json:"id"`
		StuID         any    `json:"stuId"`
		StuID2        any    `json:"stu_id"`
		MerchantID    any    `json:"merchantId"`
		MerchantID2   any    `json:"merchant_id"`
		MerchantName  string `json:"merchantName"`
		MerchantName2 string `json:"merchant_name"`
	} `json:"data"`
}

type subjectDetailResp struct {
	Data struct {
		PackageID  any `json:"packageId"`
		MerchantID any `json:"merchantId"`
	} `json:"data"`
}
type lessonsResp struct {
	Data struct {
		ResourceList []resource `json:"resourceList"`
	} `json:"data"`
}
type resource struct {
	ResourceName string   `json:"resourceName"`
	ResourceID   any      `json:"resourceId"`
	ResourceType any      `json:"resourceType"`
	LessonList   []lesson `json:"lessonList"`
}
type lesson struct {
	LessonName         string           `json:"lessonName"`
	Name               string           `json:"name"`
	Title              string           `json:"title"`
	LessonID           any              `json:"lessonId"`
	LivePlaybackID     any              `json:"livePlaybackId"`
	LiveLessonID       any              `json:"liveLessonId"`
	TeachUnitID        any              `json:"teachUnitId"`
	ResourceType       any              `json:"resourceType"`
	ResourceID         any              `json:"resourceId"`
	ResourceName       string           `json:"resourceName"`
	LiveSource         any              `json:"liveSource"`
	LiveReplaySource   any              `json:"liveReplaySource"`
	LiveStatus         any              `json:"liveStatus"`
	IsNewLive          any              `json:"isNewLive"`
	ActualLiveDuration any              `json:"actualLiveDuration"`
	Duration           any              `json:"duration"`
	CourseDataList     []map[string]any `json:"courseDataList"`
}

type playResp struct {
	Data struct {
		PlayURL string `json:"playUrl"`
	} `json:"data"`
}
type sunlandsResp struct {
	Token         string          `json:"token"`
	VideoPlayURLs []sunlandsVideo `json:"videoPlayUrls"`
	RoomInfo      map[string]any  `json:"roomInfo"`
}
type sunlandsVideo struct {
	SHttpsURL string `json:"sHttpsUrl"`
	SURL      string `json:"sUrl"`
	LFileSize any    `json:"lFileSize"`
	LSequence any    `json:"lSequence"`
}

func (l *Lexueyun) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("lexueyun requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess, err := loginSession(c, opts.Cookies)
	if err != nil {
		return nil, err
	}
	sel := parseCourse(rawURL)
	if sel.subjectID == "" {
		sel, err = firstCourse(c, sess)
		if err != nil {
			return nil, err
		}
	}
	if sel.title == "" {
		sel.title = "lexueyun_" + sel.subjectID
	}
	if err := fillSubjectDetail(c, sess, &sel); err != nil {
		return nil, err
	}
	lessons, err := getLessons(c, sess, sel)
	if err != nil {
		return nil, err
	}
	entries := make([]*extractor.MediaInfo, 0)
	for ri, res := range lessons {
		for li, les := range res.LessonList {
			entry, err := resolveLesson(c, sess, sel, res, les, ri+1, li+1)
			if err == nil && entry != nil {
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("lexueyun: no playable lesson video resolved")
	}
	return &extractor.MediaInfo{Site: "lexueyun", Title: sel.title, Entries: entries}, nil
}

func loginSession(c *util.Client, jar http.CookieJar) (lexueSession, error) {
	auth := userAuthFromJar(jar)
	if auth == "" {
		return lexueSession{}, fmt.Errorf("lexueyun requires lexueyun-pc-userAuth")
	}
	body, err := requestLexue(c, auth, userInfoPath, map[string]any{})
	if err != nil {
		return lexueSession{}, err
	}
	var resp userInfoResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return lexueSession{}, fmt.Errorf("lexueyun userInfo parse: %w", err)
	}
	stuID := firstNonEmpty(anyString(resp.Data.ID), anyString(resp.Data.StuID), anyString(resp.Data.StuID2))
	if fmt.Sprint(resp.Flag) != "1" || stuID == "" {
		return lexueSession{}, fmt.Errorf("lexueyun userInfo did not return logged-in stuId")
	}
	user := map[string]any{"merchantId": resp.Data.MerchantID, "merchant_id": resp.Data.MerchantID2, "merchantName": resp.Data.MerchantName, "merchant_name": resp.Data.MerchantName2}
	return lexueSession{auth: auth, stuID: stuID, user: user}, nil
}

func firstCourse(c *util.Client, sess lexueSession) (courseSel, error) {
	merchants := extractList(requestMap(c, sess, merchantListPath, map[string]any{"stuId": sess.stuID}), []string{"merchantList", "myMerchantList", "list", "records", "items", "rows"})
	if len(merchants) == 0 && firstNonEmpty(anyString(sess.user["merchantId"]), anyString(sess.user["merchant_id"])) != "" {
		merchants = []map[string]any{sess.user}
	}
	for _, m := range merchants {
		mid := firstNonEmpty(anyString(m["merchantId"]), anyString(m["merchant_id"]), anyString(m["id"]))
		orders := extractList(requestMap(c, sess, orderListPath, map[string]any{"merchantId": mid, "stuId": sess.stuID}), []string{"orderList", "orders", "courseList", "list", "records", "items", "rows"})
		for _, o := range orders {
			ord := firstNonEmpty(anyString(o["ordSerialNo"]), anyString(o["orderSerialNo"]), anyString(o["ordNo"]))
			orderID := firstNonEmpty(anyString(o["orderId"]), anyString(o["order_id"]))
			product := firstNonEmpty(anyString(o["productName"]), anyString(o["goodsName"]), anyString(o["title"]))
			for _, sub := range extractList(o, []string{"subjectList", "subjects", "courseList", "courses", "courseInfoList", "subjectInfoList", "classList"}) {
				sid := firstNonEmpty(anyString(sub["subjectId"]), anyString(sub["subject_id"]), anyString(sub["id"]))
				name := firstNonEmpty(anyString(sub["subjectName"]), anyString(sub["name"]), anyString(sub["courseName"]), anyString(sub["title"]), product)
				if sid != "" {
					return courseSel{subjectID: sid, ordSerialNo: ord, orderID: orderID, title: name, packageID: anyString(sub["packageId"]), merchantID: mid}, nil
				}
			}
		}
	}
	return courseSel{}, fmt.Errorf("lexueyun course list is empty")
}

func fillSubjectDetail(c *util.Client, sess lexueSession, sel *courseSel) error {
	body, err := requestLexue(c, sess.auth, subjectDetailPath, map[string]any{"ordSerialNo": sel.ordSerialNo, "subjectId": sel.subjectID, "stuId": sess.stuID})
	if err != nil {
		return err
	}
	var resp subjectDetailResp
	if json.Unmarshal(body, &resp) == nil {
		sel.packageID = firstNonEmpty(sel.packageID, anyString(resp.Data.PackageID))
		sel.merchantID = firstNonEmpty(sel.merchantID, anyString(resp.Data.MerchantID))
	}
	return nil
}

func getLessons(c *util.Client, sess lexueSession, sel courseSel) ([]resource, error) {
	body, err := requestLexue(c, sess.auth, lessonListPath, map[string]any{"ordSerialNo": sel.ordSerialNo, "subjectId": sel.subjectID, "stuId": sess.stuID})
	if err != nil {
		return nil, err
	}
	var resp lessonsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("lexueyun lesson list parse: %w", err)
	}
	if len(resp.Data.ResourceList) == 0 {
		return nil, fmt.Errorf("lexueyun lesson list is empty")
	}
	return resp.Data.ResourceList, nil
}

func resolveLesson(c *util.Client, sess lexueSession, sel courseSel, res resource, les lesson, ri, li int) (*extractor.MediaInfo, error) {
	roomID := firstNonEmpty(anyString(les.LivePlaybackID), anyString(les.LiveLessonID))
	if roomID == "" {
		return nil, fmt.Errorf("lexueyun lesson has empty room id")
	}
	path := liveProPlayPath
	if toInt(les.LiveSource) == 1 {
		path = livePlayPath
	}
	body, err := requestLexue(c, sess.auth, path, map[string]any{"teachUnitId": anyString(les.TeachUnitID), "ordSerialNo": sel.ordSerialNo, "liveType": liveType(les), "roomId": roomID, "userId": sess.stuID})
	if err != nil {
		return nil, err
	}
	var pr playResp
	if err := json.Unmarshal(body, &pr); err != nil || pr.Data.PlayURL == "" {
		return nil, fmt.Errorf("lexueyun playUrl parse failed")
	}
	mediaURL, stream, err := sunlandsMediaURL(c, pr.Data.PlayURL)
	if err != nil {
		mediaURL = pr.Data.PlayURL
	}
	title := firstNonEmpty(les.LessonName, les.Name, les.Title, fmt.Sprintf("[%d.%d]--未命名课时", ri, li))
	extra := map[string]any{"lesson_id": anyString(les.LessonID), "livePlaybackId": anyString(les.LivePlaybackID), "liveLessonId": anyString(les.LiveLessonID), "resourceName": res.ResourceName, "roomId": roomID, "playUrl": pr.Data.PlayURL}
	if stream.SURL != "" || stream.SHttpsURL != "" {
		extra["selected_stream"] = stream
	}
	return &extractor.MediaInfo{Site: "lexueyun", Title: title, Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{mediaURL}, Format: pickFormat(mediaURL), Headers: map[string]string{"Referer": pr.Data.PlayURL}}}, Extra: extra}, nil
}

func requestLexue(c *util.Client, auth, path string, params map[string]any) ([]byte, error) {
	if params == nil {
		params = map[string]any{}
	}
	params["channelCode"] = channelCode
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("lexueyun marshal request %s: %w", path, err)
	}
	apiURL := path
	if !strings.HasPrefix(apiURL, "http") {
		apiURL = urlOrigin + path
	}
	body, err := c.PostForm(apiURL, map[string]string{"channelCode": channelCode, "data": string(payload)}, lexueHeaders(auth))
	if err != nil {
		return nil, fmt.Errorf("lexueyun request %s: %w", path, err)
	}
	return []byte(body), nil
}

func requestMap(c *util.Client, sess lexueSession, path string, params map[string]any) map[string]any {
	body, err := requestLexue(c, sess.auth, path, params)
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if d, ok := m["data"].(map[string]any); ok {
		return d
	}
	return m
}

func sunlandsMediaURL(c *util.Client, playURL string) (string, sunlandsVideo, error) {
	liveData := decodeLiveData(playURL)
	if len(liveData) == 0 {
		return playURL, sunlandsVideo{}, nil
	}
	liveData["terminalType"] = 3
	payload, err := json.Marshal(liveData)
	if err != nil {
		return "", sunlandsVideo{}, err
	}
	resp, err := c.Post(sunlandsVideoEntry+"/thirdLogin", bytes.NewReader(payload), map[string]string{"Accept": "application/json, text/plain, */*", "Content-Type": "application/json", "Referer": playURL, "Origin": urlOrigin})
	if err != nil {
		return "", sunlandsVideo{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", sunlandsVideo{}, err
	}
	var sr sunlandsResp
	if err := json.Unmarshal(b, &sr); err != nil {
		return "", sunlandsVideo{}, err
	}
	if sr.Token == "" {
		return "", sunlandsVideo{}, fmt.Errorf("sunlands thirdLogin returned empty token")
	}
	sort.SliceStable(sr.VideoPlayURLs, func(i, j int) bool {
		return toFloat(sr.VideoPlayURLs[i].LFileSize) > toFloat(sr.VideoPlayURLs[j].LFileSize)
	})
	for _, v := range sr.VideoPlayURLs {
		u := firstNonEmpty(v.SHttpsURL, v.SURL)
		if u != "" && strings.Contains(strings.ToLower(u), ".mp4") {
			return normalizeURL(u), v, nil
		}
	}
	for _, v := range sr.VideoPlayURLs {
		if u := firstNonEmpty(v.SHttpsURL, v.SURL); u != "" {
			return normalizeURL(u), v, nil
		}
	}
	return "", sunlandsVideo{}, fmt.Errorf("sunlands thirdLogin has no videoPlayUrls")
}
