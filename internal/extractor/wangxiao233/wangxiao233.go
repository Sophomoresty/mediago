// Package wangxiao233 implements an extractor for wx.233.com (网校233 / 233网校).
package wangxiao233

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL     = "https://wx.233.com/"
	loginURL       = "https://passport.233.com/login/?redirecturl=https%3A%2F%2Fwx.233.com%2Fcenter%2Fstudy%3Fdomain%3Daq%26type%3D0"
	urlUserInfo    = "https://japi.233.com/ess-ucs-api/doz/members/userInfo"
	urlVktCourse   = "https://japi.233.com/ess-study-api/vkt-course/list"
	urlUserCourse  = "https://japi.233.com/ess-study-api/user-course/list"
	urlBuyDomain   = "https://japi.233.com/ess-study-api/user-course/buy-domain"
	urlTag         = "https://japi.233.com/ess-study-api/learn/do/get-class-tag"
	urlVersion     = "https://japi.233.com/ess-study-api/learn/do/list-version"
	urlChapter     = "https://japi.233.com/ess-study-api/learn/do/list-chapter-by-version-id"
	urlLecture     = "https://japi.233.com/ess-study-api/learn/do/get-lecture-url"
	urlDatum       = "https://japi.233.com/ess-study-api/datum-api/page-list"
	urlDatum2      = "https://japi.233.com/ess-study-api/datum-api/do/page-list"
	urlDatumDown   = "https://japi.233.com/ess-study-api/datum-info/do/download"
	urlVodDetail   = "https://japi.233.com/ess-bms-api/vod-play/do/by-detailids"
	urlVodPoly     = "https://japi.233.com/ess-bms-api/vod-play/do/by-polyvid"
	urlVodEss      = "https://japi.233.com/ess-bms-api/vod-play/do/by-essvid"
	urlPlayAuth    = "https://japi.233.com/ess-open-api/vod/do/getPlayInfoAndAuth"
	urlProductInfo = "https://japi.233.com/ess-study-api/user-course/product-info"
	urlVktProduct  = "https://japi.233.com/ess-study-api/vkt-course/product"
	urlOrderDetail = "https://japi.233.com/ess-ots-api/order-center/get-order-detail"
	urlPolyvToken  = "https://wx.233.com/search/v1/study/getvideotoken"
	urlPolyvSecure = "https://player.polyv.net/secure/%s.json"
	urlPolyvKey    = "https://hls.videocc.net/playsafe/%s/%s/%s_%s.key?token=%s"
	signSecret     = "RZRRNN9RXYCP"
	sidPrefix      = "study"
	modeFHD        = 1
	modeHD         = 2
	modeSD         = 3
	modeOnlyPDF    = 4
)

var (
	patterns       = []string{`(?:[\w-]+\.)?233\.com/`}
	priceNumberRe  = regexp.MustCompile(`\d+(?:\.\d+)?`)
	priceKeyFamily = []string{
		"price", "salePrice", "sellPrice", "actualPrice", "productPrice", "coursePrice", "originalPrice", "marketPrice",
		"payPrice", "payMoney", "paymoney", "orderPrice", "orderMoney", "orderAmount", "totalAmount", "totalMoney",
		"amount", "realAmount", "cashAmount", "amt", "paymentAmount", "settleAmount",
	}
)

func init() {
	extractor.Register(&Wangxiao233{}, extractor.SiteInfo{Name: "Wangxiao233", URL: "233.com", NeedAuth: true})
}

type Wangxiao233 struct{}

func (w *Wangxiao233) Patterns() []string { return patterns }

type wx233Session struct {
	token   string
	cookies map[string]string
}
type wx233Course struct {
	productID, childProductID, versionProductID, versionID, teacherID string
	courseID, lmProductID, groupProductID, classType, domain, title   string
	orderNo, orderProductID, productNetID                             string
	purchased                                                         bool
	price                                                             float64
	source                                                            map[string]any
}
type wx233Video struct{ title, detailID, polyVid, essVid, aliyunVid, mp3URL, directURL string }
type wx233File struct {
	title, id, url, fmtName, fileType, downloadAPI string
	courseID, childProductID, versionProductID     string
	versionID, lmProductID, groupProductID         string
	size                                           int64
}

func (w *Wangxiao233) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wangxiao233 requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess := wx233Session{token: tokenFromJar(opts.Cookies), cookies: cookiesFromJar(opts.Cookies)}
	if sess.token == "" {
		return nil, fmt.Errorf("wangxiao233 requires clientauthentication cookie")
	}
	if _, err := apiGet(c, sess, urlUserInfo, nil); err != nil {
		return nil, fmt.Errorf("wangxiao233 userInfo: %w", err)
	}
	course := parseCourse(rawURL)
	if course.productID == "" {
		courses, err := purchasedCourses(c, sess, course)
		if err != nil {
			return nil, err
		}
		if opts.ListOnly || len(courses) != 1 {
			return courseListMedia(courses), nil
		}
		course = courses[0]
	} else if course.childProductID == "" || course.teacherID == "" {
		if filled, ok := findPurchasedCourse(c, sess, course); ok {
			course = mergeCourse(course, filled)
		}
	}
	tagData, _ := apiGetData(c, sess, urlTag, map[string]string{"teacherId": course.teacherID, "childProductId": course.childProductID, "systemType": "2", "lmProductId": "", "productId": course.productID, "clientType": "3"})
	course = mergeCourse(course, courseFromTag(tagData, course))
	if !course.purchased && purchasedFromAny(tagData) {
		course.purchased = true
	}
	children := childCourses(tagData, course)
	if len(children) == 0 {
		children = []wx233Course{course}
	}
	mode := selectMode(opts.Quality)
	entries := []*extractor.MediaInfo{}
	seen := map[string]bool{}
	for _, child := range children {
		child = fillVersion(c, sess, child)
		chapterData, err := apiGetData(c, sess, urlChapter, map[string]string{"versionId": child.versionID, "productId": course.productID, "currentParentProductId": course.productID, "groupProductId": chapterGroupProductID(course, tagData), "clientType": "3"})
		if err != nil || chapterData == nil {
			continue
		}
		if !course.purchased && purchasedFromAny(chapterData) {
			course.purchased = true
		}
		if mode != modeOnlyPDF {
			for _, v := range collectVideos(chapterData) {
				mi, err := resolveVideo(c, sess, v, opts)
				if err != nil || mi == nil || len(mi.Streams) == 0 {
					continue
				}
				u := firstStreamURL(mi)
				if u != "" && !seen[u] {
					seen[u] = true
					entries = append(entries, mi)
				}
			}
		}
		for _, f := range collectLectureFiles(chapterData, child, course) {
			if skipFileForMode(f, mode) {
				continue
			}
			mi, err := resolveFile(c, sess, f, course)
			if err != nil || mi == nil || len(mi.Streams) == 0 {
				continue
			}
			u := firstStreamURL(mi)
			if u != "" && !seen[u] {
				seen[u] = true
				entries = append(entries, mi)
			}
		}
	}
	for _, f := range collectDatumFiles(c, sess, course, tagData) {
		if skipFileForMode(f, mode) {
			continue
		}
		mi, err := resolveFile(c, sess, f, course)
		if err != nil || mi == nil || len(mi.Streams) == 0 {
			continue
		}
		u := firstStreamURL(mi)
		if u != "" && !seen[u] {
			seen[u] = true
			entries = append(entries, mi)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("wangxiao233: no playable video or file resolved from chapter list")
	}
	title := firstNonEmpty(course.title, "wangxiao233_"+course.productID)
	return &extractor.MediaInfo{Site: "wangxiao233", Title: title, Entries: entries, Extra: courseExtra(c, sess, course, tagData)}, nil
}

func apiGetData(c *util.Client, sess wx233Session, apiURL string, params map[string]string) (any, error) {
	m, err := apiGet(c, sess, apiURL, params)
	if err != nil {
		return nil, err
	}
	if d, ok := m["data"]; ok {
		return d, nil
	}
	return m, nil
}
func apiGet(c *util.Client, sess wx233Session, apiURL string, params map[string]string) (map[string]any, error) {
	qs := encodeParams(params)
	if qs != "" {
		apiURL += "?" + qs
	}
	body, err := c.GetString(apiURL, signedHeaders(sess, "get", qs))
	if err != nil {
		return nil, err
	}
	return parseJSON(body)
}
func apiPost(c *util.Client, sess wx233Session, apiURL string, data map[string]any) (map[string]any, error) {
	payload := []byte{}
	if len(data) > 0 {
		var err error
		payload, err = json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("wangxiao233 marshal: %w", err)
		}
	}
	resp, err := c.Post(apiURL, bytes.NewReader(payload), signedHeaders(sess, "post", string(payload)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wangxiao233 read body: %w", err)
	}
	return parseJSON(string(b))
}
func signedHeaders(sess wx233Session, method, text string) map[string]string {
	now := time.Now()
	r1 := 10000 + now.Nanosecond()%90000
	r2 := 10000 + (now.UnixNano()/1000)%90000
	sid := fmt.Sprintf("%s%s%05d%05d", sidPrefix, now.Format("20060102150405"), r1, r2)
	seed := signSecret + sid + text
	sum := md5.Sum([]byte(seed))
	return map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/plain, */*", "Origin": "https://wx.233.com", "Referer": refererURL, "token": sess.token, "sid": sid, "sign": strings.ToUpper(hex.EncodeToString(sum[:]))}
}

func parseCourse(raw string) wx233Course {
	c := wx233Course{domain: "aq"}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		c.productID = q.Get("productId")
		c.childProductID = q.Get("childProductId")
		c.versionProductID = q.Get("versionProductId")
		c.teacherID = q.Get("teacherId")
		c.courseID = firstNonEmpty(q.Get("courseId"), q.Get("course_id"))
		c.lmProductID = q.Get("lmProductId")
		c.groupProductID = q.Get("groupProductId")
		c.classType = q.Get("classType")
		c.domain = firstNonEmpty(q.Get("domain"), c.domain)
	}
	return c
}
func firstPurchasedCourse(c *util.Client, sess wx233Session, domain string) (wx233Course, error) {
	courses, err := purchasedCourses(c, sess, wx233Course{domain: domain})
	if err != nil {
		return wx233Course{}, err
	}
	return courses[0], nil
}

func purchasedCourses(c *util.Client, sess wx233Session, base wx233Course) ([]wx233Course, error) {
	var courses []wx233Course
	seen := map[string]bool{}
	for _, domain := range courseDomains(c, sess, base) {
		for _, apiURL := range []string{urlVktCourse, urlUserCourse} {
			data, err := apiGetData(c, sess, apiURL, map[string]string{"domain": domain, "types": "1"})
			if err != nil {
				continue
			}
			root := asMap(data)
			list := listFromResponseData(firstNonNil(root["courseList"], data))
			if len(list) == 0 {
				list = mapsUnder(data)
			}
			for _, m := range list {
				course := courseFromMap(m, domain)
				if course.productID == "" {
					continue
				}
				key := strings.Join([]string{course.productID, course.childProductID, course.versionProductID, course.teacherID}, "\x00")
				if seen[key] {
					continue
				}
				seen[key] = true
				courses = append(courses, course)
			}
		}
	}
	if len(courses) == 0 {
		return nil, fmt.Errorf("wangxiao233: purchased course list is empty")
	}
	return courses, nil
}

func findPurchasedCourse(c *util.Client, sess wx233Session, target wx233Course) (wx233Course, bool) {
	courses, err := purchasedCourses(c, sess, target)
	if err != nil {
		return wx233Course{}, false
	}
	for _, course := range courses {
		if course.productID == target.productID || course.courseID == target.productID || (target.courseID != "" && course.courseID == target.courseID) {
			return course, true
		}
	}
	return wx233Course{}, false
}

func courseDomains(c *util.Client, sess wx233Session, base wx233Course) []string {
	out := []string{}
	add := func(v string) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	add(base.domain)
	if base.source != nil {
		add(domainFromURL(val(base.source, "url")))
	}
	if data, err := apiGetData(c, sess, urlBuyDomain, nil); err == nil {
		for _, m := range mapsUnder(data) {
			for _, key := range []string{"domain", "childDomain", "parentDomain"} {
				add(val(m, key))
			}
		}
	}
	add("ccbp")
	add("aq")
	return uniqueStrings(out)
}

func domainFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Query().Get("domain")
}

func courseFromMap(m map[string]any, domain string) wx233Course {
	price, _ := priceFromAny(m)
	return wx233Course{
		source:           m,
		productID:        val(m, "productId"),
		childProductID:   val(m, "childProductId"),
		versionProductID: val(m, "versionProductId"),
		teacherID:        val(m, "teacherId"),
		courseID:         firstNonEmpty(val(m, "courseId"), val(m, "course_id"), val(m, "productId")),
		lmProductID:      val(m, "lmProductId"),
		groupProductID:   val(m, "groupProductId"),
		classType:        val(m, "classType"),
		domain:           firstNonEmpty(val(m, "domain"), domain),
		title:            cleanName(firstNonEmpty(val(m, "productName"), val(m, "name"), val(m, "title"))),
		orderNo:          val(m, "orderNo"),
		orderProductID:   val(m, "orderProductId"),
		productNetID:     val(m, "productNetId"),
		purchased:        true,
		price:            price,
	}
}

func mergeCourse(primary, fallback wx233Course) wx233Course {
	primary.productID = firstNonEmpty(primary.productID, fallback.productID)
	primary.childProductID = firstNonEmpty(primary.childProductID, fallback.childProductID)
	primary.versionProductID = firstNonEmpty(primary.versionProductID, fallback.versionProductID)
	primary.versionID = firstNonEmpty(primary.versionID, fallback.versionID)
	primary.teacherID = firstNonEmpty(primary.teacherID, fallback.teacherID)
	primary.courseID = firstNonEmpty(primary.courseID, fallback.courseID)
	primary.lmProductID = firstNonEmpty(primary.lmProductID, fallback.lmProductID)
	primary.groupProductID = firstNonEmpty(primary.groupProductID, fallback.groupProductID)
	primary.classType = firstNonEmpty(primary.classType, fallback.classType)
	primary.domain = firstNonEmpty(primary.domain, fallback.domain)
	primary.title = firstNonEmpty(primary.title, fallback.title)
	primary.orderNo = firstNonEmpty(primary.orderNo, fallback.orderNo)
	primary.orderProductID = firstNonEmpty(primary.orderProductID, fallback.orderProductID)
	primary.productNetID = firstNonEmpty(primary.productNetID, fallback.productNetID)
	primary.purchased = primary.purchased || fallback.purchased
	if primary.price == 0 {
		primary.price = fallback.price
	}
	if primary.source == nil {
		primary.source = fallback.source
	}
	return primary
}

func courseListMedia(courses []wx233Course) *extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(courses))
	for _, course := range courses {
		extra := map[string]any{
			"product_id":         course.productID,
			"course_id":          course.courseID,
			"child_product_id":   course.childProductID,
			"version_product_id": course.versionProductID,
			"teacher_id":         course.teacherID,
			"domain":             course.domain,
			"purchased":          course.purchased,
		}
		if course.price > 0 {
			extra["price"] = course.price
		}
		if course.source != nil {
			extra["raw"] = course.source
		}
		entries = append(entries, &extractor.MediaInfo{Site: "wangxiao233", Title: firstNonEmpty(course.title, course.productID), Extra: extra})
	}
	return &extractor.MediaInfo{Site: "wangxiao233", Title: "wangxiao233_courses", Entries: entries}
}

func courseFromTag(tagData any, base wx233Course) wx233Course {
	out := wx233Course{}
	for _, m := range mapsUnder(tagData) {
		out.domain = firstNonEmpty(out.domain, val(m, "domain"))
		out.classType = firstNonEmpty(out.classType, val(m, "classType"))
		out.childProductID = firstNonEmpty(out.childProductID, val(m, "currentParentProductId"), val(m, "currentProductId"))
		out.versionProductID = firstNonEmpty(out.versionProductID, val(m, "currentProductId"), val(m, "versionProductId"))
		out.teacherID = firstNonEmpty(out.teacherID, val(m, "currentTeacherId"))
		out.versionID = firstNonEmpty(out.versionID, val(m, "currentVersionId"))
		out.title = firstNonEmpty(out.title, val(m, "productName"), val(m, "className"), val(m, "currentProductName"))
	}
	if out.productID == "" {
		out.productID = base.productID
	}
	return out
}

func chapterGroupProductID(course wx233Course, tagData any) string {
	if course.classType == "1" || firstStringInAny(tagData, "classType") == "1" {
		return course.productID
	}
	return ""
}

func childCourses(data any, base wx233Course) []wx233Course {
	out := []wx233Course{}
	for _, m := range mapsUnder(data) {
		pid := firstNonEmpty(val(m, "productId"), base.productID)
		child := firstNonEmpty(val(m, "childProductId"), val(m, "currentProductId"), base.childProductID)
		if pid == "" && child == "" {
			continue
		}
		out = append(out, wx233Course{
			productID:        pid,
			childProductID:   child,
			versionProductID: firstNonEmpty(val(m, "versionProductId"), base.versionProductID),
			versionID:        val(m, "versionId"),
			teacherID:        firstNonEmpty(val(m, "teacherId"), base.teacherID),
			courseID:         firstNonEmpty(val(m, "courseId"), val(m, "course_id"), base.courseID),
			lmProductID:      firstNonEmpty(val(m, "lmProductId"), base.lmProductID),
			groupProductID:   firstNonEmpty(val(m, "groupProductId"), base.groupProductID),
			classType:        firstNonEmpty(val(m, "classType"), base.classType),
			domain:           base.domain,
			title:            firstNonEmpty(val(m, "courseName"), val(m, "childProductName"), val(m, "productName"), val(m, "name"), base.title),
			purchased:        base.purchased,
			price:            base.price,
		})
	}
	return out
}
func fillVersion(c *util.Client, sess wx233Session, in wx233Course) wx233Course {
	if in.versionID != "" {
		return in
	}
	pid := firstNonEmpty(in.childProductID, in.versionProductID, in.productID)
	data, err := apiGetData(c, sess, urlVersion, map[string]string{"productId": pid, "clientType": "3"})
	if err != nil {
		return in
	}
	for _, m := range mapsUnder(data) {
		if v := val(m, "versionId"); v != "" {
			in.versionID = v
			in.childProductID = firstNonEmpty(val(m, "productId"), in.childProductID)
			in.versionProductID = firstNonEmpty(val(m, "childProductId"), in.versionProductID)
			in.teacherID = firstNonEmpty(val(m, "teacherId"), in.teacherID)
			in.courseID = firstNonEmpty(val(m, "courseId"), val(m, "course_id"), in.courseID)
			in.lmProductID = firstNonEmpty(val(m, "lmProductId"), in.lmProductID)
			in.groupProductID = firstNonEmpty(val(m, "groupProductId"), in.groupProductID)
			return in
		}
	}
	return in
}
func collectVideos(data any) []wx233Video {
	out := []wx233Video{}
	for _, m := range mapsUnder(data) {
		v := wx233Video{title: firstNonEmpty(val(m, "detailName"), val(m, "name"), val(m, "title")), detailID: firstNonEmpty(val(m, "detailId"), val(m, "id")), polyVid: firstNonEmpty(val(m, "polyVid"), val(m, "polyvVid")), essVid: val(m, "essVid"), aliyunVid: firstNonEmpty(val(m, "aliyunVid"), val(m, "aliyunVideoId")), mp3URL: val(m, "mp3Url"), directURL: firstMediaURL(m)}
		if v.detailID != "" || v.polyVid != "" || v.essVid != "" || v.aliyunVid != "" || v.mp3URL != "" || v.directURL != "" {
			out = append(out, v)
		}
	}
	return out
}
func resolveVideo(c *util.Client, sess wx233Session, v wx233Video, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	playChannel := strings.ToUpper(fetchVideoPlayInfo(c, sess, &v))
	var lastErr error

	if v.aliyunVid != "" && (playChannel == "" || playChannel == "ALIYUN" || v.polyVid == "") {
		if mi, err := resolveAliyun(c, sess, v, opts); err == nil {
			return mi, nil
		} else {
			lastErr = err
		}
	}
	if v.polyVid != "" {
		if mi, err := resolvePolyv(c, v); err == nil {
			return mi, nil
		} else {
			lastErr = err
		}
	}
	if v.aliyunVid != "" {
		if mi, err := resolveAliyun(c, sess, v, opts); err == nil {
			return mi, nil
		} else {
			lastErr = err
		}
	}
	if v.directURL != "" {
		return media(v.title, v.directURL, mediaFormat(v.directURL), map[string]any{"detail_id": v.detailID, "ess_vid": v.essVid, "source_type": "vod_ess_url"}), nil
	}
	if v.mp3URL != "" {
		return media(v.title, v.mp3URL, "mp3", map[string]any{"detail_id": v.detailID, "source_type": "video_url"}), nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("wangxiao233: unsupported video source")
}

func fetchVideoPlayInfo(c *util.Client, sess wx233Session, v *wx233Video) string {
	channel := ""
	merge := func(data any) {
		v.directURL = firstNonEmpty(v.directURL, firstMediaURL(data))
		for _, m := range mapsUnder(data) {
			v.title = firstNonEmpty(v.title, val(m, "detailName"), val(m, "name"), val(m, "title"))
			v.detailID = firstNonEmpty(v.detailID, val(m, "detailId"), val(m, "id"))
			v.polyVid = firstNonEmpty(v.polyVid, val(m, "polyVid"), val(m, "polyvVid"))
			v.essVid = firstNonEmpty(v.essVid, val(m, "essVid"))
			v.aliyunVid = firstNonEmpty(v.aliyunVid, val(m, "aliyunVid"), val(m, "aliyunVideoId"))
			v.mp3URL = firstNonEmpty(v.mp3URL, val(m, "mp3Url"))
			v.directURL = firstNonEmpty(v.directURL, firstMediaURL(m))
			channel = firstNonEmpty(channel, val(m, "playChannel"), val(m, "channel"))
		}
	}
	if v.detailID != "" {
		if data, err := apiGetData(c, sess, urlVodDetail, map[string]string{"detailIds": v.detailID}); err == nil {
			merge(data)
		}
	}
	if v.polyVid != "" {
		if data, err := apiGetData(c, sess, urlVodPoly, map[string]string{"polyVid": v.polyVid}); err == nil {
			merge(data)
		}
	}
	if v.essVid != "" {
		if data, err := apiGetData(c, sess, urlVodEss, map[string]string{"essVid": v.essVid}); err == nil {
			merge(data)
		}
	}
	return channel
}

func resolveAliyun(c *util.Client, sess wx233Session, v wx233Video, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	m, err := apiGet(c, sess, urlPlayAuth, map[string]string{"videoId": v.aliyunVid})
	if err != nil {
		return nil, err
	}
	data, _ := m["data"].(map[string]any)
	payload := shared.AliyunDecodePlayAuth(data["playAuth"])
	payload.Region = firstNonEmpty(payload.Region, val(data, "regionId"))
	if payload.Region == "" {
		payload.Region = "cn-shanghai"
	}
	playOpts := shared.AliyunPlayOptions{
		Referer:         refererURL,
		Origin:          "https://wx.233.com",
		Quality:         firstNonEmpty(qualityFromOpts(opts)),
		FetchM3U8:       true,
		RewriteM3U8Keys: true,
	}
	if playCfg, err := json.Marshal(map[string]string{"EncryptType": "AliyunVoDEncryption"}); err == nil {
		playOpts.ExtraParams = map[string]string{"PlayConfig": string(playCfg)}
	}
	info, err := shared.AliyunResolvePlayInfo(c, payload, v.aliyunVid, playOpts)
	if err != nil {
		return nil, fmt.Errorf("blocked: needs Aliyun STS SDK / DRM engine: %w", err)
	}
	extra := map[string]any{"aliyun_vid": v.aliyunVid, "detail_id": v.detailID, "aliyun_api": info.APIURL, "source_type": info.SourceType}
	if info.M3U8Text != "" {
		extra["m3u8_text"] = info.M3U8Text
		extra["m3u8_url"] = info.URL
	}
	return media(v.title, info.URL, info.Format, extra), nil
}
func resolvePolyv(c *util.Client, v wx233Video) (*extractor.MediaInfo, error) {
	var tokenPayload map[string]any
	if tokenBody, err := c.GetString(urlPolyvToken+"?videoid="+url.QueryEscape(v.polyVid), map[string]string{"Referer": refererURL}); err == nil {
		tokenPayload, _ = parseJSON(parseJSONP(tokenBody))
	}
	polyvVID := shared.PolyvNormalizeVID(v.polyVid)
	sec, err := shared.PolyvResolveSecure(c, polyvVID, map[string]string{"Referer": refererURL})
	if err != nil {
		return nil, err
	}
	manifest, err := shared.PolyvPickBestManifest(sec)
	if err != nil {
		return nil, err
	}
	token := firstNonEmpty(firstPolyvToken(tokenPayload), sec.PlayToken())
	extra := map[string]any{"poly_vid": polyvVID, "raw_poly_vid": v.polyVid, "detail_id": v.detailID, "polyv_secure_url": fmt.Sprintf(urlPolyvSecure, polyvVID)}
	if txt, err := c.GetString(manifest, map[string]string{"Referer": refererURL}); err == nil && token != "" {
		if rewritten, err := shared.PolyvRewriteM3U8KeysWithOptions(c, txt, shared.PolyvRewriteOptions{Token: token, Referer: refererURL, ManifestURL: manifest, SeedConst: sec.SeedConst()}); err == nil {
			extra["m3u8_text"] = rewritten
			extra["m3u8_url"] = manifest
			extra["source_type"] = "m3u8_text"
		}
	}
	if _, ok := extra["source_type"]; !ok {
		extra["source_type"] = "m3u8_url"
	}
	return media(v.title, manifest, "m3u8", extra), nil
}

func firstPolyvToken(v any) string {
	priority := []string{"playsafe", "playSafe", "play_safe", "playSafeToken", "playToken", "play_token"}
	for _, m := range mapsUnder(v) {
		for _, key := range priority {
			if token := val(m, key); token != "" {
				return token
			}
		}
	}
	for _, m := range mapsUnder(v) {
		if token := val(m, "token"); token != "" {
			return token
		}
	}
	return ""
}

func collectLectureFiles(data any, child, base wx233Course) []wx233File {
	var out []wx233File
	seen := map[string]bool{}
	for _, m := range mapsUnder(data) {
		lectureID := firstNonEmpty(val(m, "lectureId"), val(m, "detailLectureId"), val(m, "chapterLectureId"), val(m, "coursePdfLectureId"), val(m, "pdfLectureId"))
		rawURL := firstFileishURL(m)
		if lectureID == "" && rawURL == "" {
			continue
		}
		title := firstNonEmpty(val(m, "detailName"), val(m, "chapterName"), val(m, "name"), val(m, "title"), "资料")
		if rawURL == "" && lectureID == "" && !looksLikeFileMeta(m) {
			continue
		}
		key := firstNonEmpty(lectureID, rawURL)
		if seen[key] {
			continue
		}
		seen[key] = true
		fileType := firstNonEmpty(val(m, "fileType"), "1")
		out = append(out, wx233File{
			title:            cleanName(title),
			id:               lectureID,
			url:              normalizeDownloadURL(rawURL),
			fmtName:          fileFormat(firstNonEmpty(rawURL, title), val(m, "extension"), val(m, "fmt")),
			fileType:         fileType,
			downloadAPI:      "lecture",
			size:             parseSizeBytes(firstNonEmpty(val(m, "detailLectureSize"), val(m, "size"), val(m, "fileSize"))),
			courseID:         firstNonEmpty(val(m, "courseId"), child.courseID, child.childProductID, base.courseID, base.childProductID, base.productID),
			childProductID:   firstNonEmpty(val(m, "childProductId"), child.childProductID, base.childProductID),
			versionProductID: firstNonEmpty(val(m, "versionProductId"), child.versionProductID, base.versionProductID),
			versionID:        firstNonEmpty(val(m, "versionId"), child.versionID, base.versionID),
			lmProductID:      firstNonEmpty(val(m, "lmProductId"), child.lmProductID, base.lmProductID),
			groupProductID:   firstNonEmpty(val(m, "groupProductId"), child.groupProductID, base.groupProductID),
		})
	}
	return out
}

func collectDatumFiles(c *util.Client, sess wx233Session, course wx233Course, tagData any) []wx233File {
	var out []wx233File
	for _, endpoint := range []string{urlDatum, urlDatum2} {
		files := collectDatumFilesFromEndpoint(c, sess, course, tagData, endpoint)
		out = append(out, files...)
		if len(files) > 0 {
			break
		}
	}
	return out
}

func collectDatumFilesFromEndpoint(c *util.Client, sess wx233Session, course wx233Course, tagData any, endpoint string) []wx233File {
	var out []wx233File
	domain := course.domain
	subjectID := "0"
	for _, m := range mapsUnder(tagData) {
		domain = firstNonEmpty(domain, val(m, "domain"))
		subjectID = firstNonEmpty(val(m, "subjectId"), subjectID)
		if domain != "" && subjectID != "0" {
			break
		}
	}
	for page := 1; page <= 20; page++ {
		resp, err := apiPost(c, sess, endpoint, map[string]any{
			"pageSize":  20,
			"pageNo":    page,
			"typeId":    0,
			"type":      3,
			"subjectId": subjectID,
			"sortType":  1,
			"productId": course.productID,
			"isCanDown": 1,
			"domain":    firstNonEmpty(domain, "aq"),
			"batch":     0,
		})
		if err != nil {
			break
		}
		items := listFromResponseData(resp["data"])
		if len(items) == 0 {
			break
		}
		for _, m := range items {
			name := firstNonEmpty(val(m, "title"), val(m, "name"), val(m, "fileName"))
			rawURL := normalizeDownloadURL(firstNonEmpty(val(m, "downloadUrl"), val(m, "path"), val(m, "url")))
			fileID := firstNonEmpty(val(m, "datumId"), val(m, "id"))
			if name == "" || (rawURL == "" && fileID == "") {
				continue
			}
			idx := len(out) + 1
			out = append(out, wx233File{
				title:       cleanName(fmt.Sprintf("(%d)--%s", idx, name)),
				id:          fileID,
				url:         rawURL,
				fmtName:     fileFormat(firstNonEmpty(rawURL, name), val(m, "fileType")),
				fileType:    val(m, "fileType"),
				downloadAPI: "datum",
				size:        parseSizeBytes(firstNonEmpty(val(m, "size"), val(m, "fileSize"))),
			})
		}
		if len(items) < 20 {
			break
		}
	}
	return out
}

func resolveFile(c *util.Client, sess wx233Session, f wx233File, course wx233Course) (*extractor.MediaInfo, error) {
	u := normalizeDownloadURL(f.url)
	if u == "" {
		u = resolveFileURL(c, sess, f, course)
	}
	if u == "" {
		return nil, fmt.Errorf("wangxiao233: empty file URL for %s", firstNonEmpty(f.title, f.id))
	}
	title := firstNonEmpty(f.title, f.id, "file")
	fmtName := fileFormat(firstNonEmpty(f.fmtName, u, title))
	stream := extractor.Stream{Quality: "file", URLs: []string{u}, Format: fmtName, Size: f.size, Headers: map[string]string{"Referer": refererURL}}
	return &extractor.MediaInfo{
		Site:    "wangxiao233",
		Title:   title,
		Streams: map[string]extractor.Stream{"file": stream},
		Extra: map[string]any{
			"type":         "file",
			"file_id":      f.id,
			"download_api": f.downloadAPI,
			"file_type":    f.fileType,
		},
	}, nil
}

func skipFileForMode(f wx233File, mode int) bool {
	if mode != modeOnlyPDF {
		return false
	}
	return strings.EqualFold(fileFormat(firstNonEmpty(f.fmtName, f.url, f.title)), "mp4")
}

func resolveFileURL(c *util.Client, sess wx233Session, f wx233File, course wx233Course) string {
	switch f.downloadAPI {
	case "lecture":
		fileType := firstNonEmpty(f.fileType, "1")
		data, err := apiGet(c, sess, urlLecture, map[string]string{
			"lectureId":       f.id,
			"courseId":        firstNonEmpty(f.courseID, course.courseID, course.childProductID, course.productID),
			"fileType":        fileType,
			"versionId":       firstNonEmpty(f.versionID, course.versionID),
			"productId":       firstNonEmpty(f.versionProductID, course.versionProductID, course.childProductID, course.productID),
			"lmProductId":     firstNonEmpty(f.lmProductID, course.lmProductID),
			"groupProductId":  groupProductForFile(f, course),
			"clientType":      "3",
			"childProductId":  firstNonEmpty(f.childProductID, course.childProductID),
			"versionCourseId": firstNonEmpty(f.versionProductID, course.versionProductID),
		})
		if err != nil {
			return ""
		}
		return firstDownloadURLFromAny(firstNonNil(data["data"], data["message"], data["url"], data["path"], data["downloadUrl"]))
	case "datum":
		data, err := apiPost(c, sess, urlDatumDown, map[string]any{"datumId": f.id, "platFrom": 1})
		if err != nil {
			return ""
		}
		return firstDownloadURLFromAny(firstNonNil(data["data"], data["url"], data["path"], data["downloadUrl"]))
	default:
		return normalizeDownloadURL(f.url)
	}
}

func courseExtra(c *util.Client, sess wx233Session, course wx233Course, tagData any) map[string]any {
	extra := map[string]any{
		"product_id":         course.productID,
		"course_id":          firstNonEmpty(course.courseID, course.productID),
		"child_product_id":   course.childProductID,
		"version_product_id": course.versionProductID,
		"version_id":         course.versionID,
		"teacher_id":         course.teacherID,
		"domain":             course.domain,
		"purchased":          course.purchased,
	}
	if course.source != nil {
		extra["raw"] = course.source
	}
	if course.purchased || course.source != nil || purchasedFromAny(tagData) {
		if price, ok := fetchCoursePrice(c, sess, course, tagData); ok {
			extra["price"] = price
		} else if course.purchased {
			extra["price"] = float64(999)
		}
	}
	return compactAnyMap(extra)
}

func fetchCoursePrice(c *util.Client, sess wx233Session, course wx233Course, tagData any) (float64, bool) {
	if course.price > 0 {
		return course.price, true
	}
	if price, ok := priceFromAny(tagData); ok {
		return price, true
	}
	params := map[string]string{"productId": course.productID}
	if vid := firstNonEmpty(course.versionID, firstStringInAny(tagData, "currentVersionId"), firstStringInAny(tagData, "versionId")); vid != "" {
		params["versionId"] = vid
	}
	for _, apiURL := range []string{urlProductInfo, urlVktProduct} {
		data, err := apiGet(c, sess, apiURL, params)
		if err == nil {
			if price, ok := priceFromAny(data); ok {
				return price, true
			}
		}
	}
	if course.orderNo != "" {
		data, err := apiPost(c, sess, urlOrderDetail, map[string]any{"orderNo": course.orderNo})
		if err == nil {
			if price, ok := priceFromAny(data); ok {
				return price, true
			}
		}
	}
	if price, ok := priceFromCookie(sess.cookies); ok {
		return price, true
	}
	return 0, false
}

func priceFromCookie(cookies map[string]string) (float64, bool) {
	if len(cookies) == 0 {
		return 0, false
	}
	raw := firstNonEmpty(cookies["wxuserinforset"], cookies["WXUSERINFOSET"])
	if raw == "" {
		return 0, false
	}
	if decoded, err := url.QueryUnescape(raw); err == nil && decoded != "" {
		raw = decoded
	}
	for _, part := range strings.Split(raw, "&") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "paymoney") {
			continue
		}
		if price, ok := normalizePrice(v); ok {
			return price, true
		}
	}
	return 0, false
}

func priceFromAny(v any) (float64, bool) {
	switch t := v.(type) {
	case map[string]any:
		for _, key := range priceKeyFamily {
			if raw, ok := t[key]; ok {
				if price, ok := normalizePrice(raw); ok {
					return price, true
				}
			}
		}
		for _, child := range t {
			if price, ok := priceFromAny(child); ok {
				return price, true
			}
		}
	case []any:
		for _, child := range t {
			if price, ok := priceFromAny(child); ok {
				return price, true
			}
		}
	}
	return 0, false
}

func normalizePrice(v any) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		if t > 0 {
			return round2(t), true
		}
	case float32:
		if t > 0 {
			return round2(float64(t)), true
		}
	case int:
		if t > 0 {
			return float64(t), true
		}
	case int64:
		if t > 0 {
			return float64(t), true
		}
	case json.Number:
		if f, err := t.Float64(); err == nil && f > 0 {
			return round2(f), true
		}
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			return 0, false
		}
		m := priceNumberRe.FindString(strings.ReplaceAll(s, ",", ""))
		if m == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(m, 64)
		if err == nil && f > 0 {
			return round2(f), true
		}
	}
	return 0, false
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func purchasedFromAny(v any) bool {
	for _, m := range mapsUnder(v) {
		if truthy(val(m, "isBuy")) && truthy(firstNonEmpty(val(m, "isCanLearn"), "1")) {
			return true
		}
		if m["learnCourseOrderRsp"] != nil || m["source"] != nil {
			return true
		}
	}
	return false
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func firstStringInAny(v any, keys ...string) string {
	for _, m := range mapsUnder(v) {
		for _, key := range keys {
			if s := val(m, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func groupProductForFile(f wx233File, course wx233Course) string {
	if course.classType == "1" {
		return firstNonEmpty(f.groupProductID, course.groupProductID)
	}
	return ""
}

func parseJSON(text string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &m); err != nil {
		return nil, err
	}
	return m, nil
}
func parseJSONP(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			return text[i : j+1]
		}
	}
	return text
}
func mapsUnder(v any) []map[string]any {
	out := []map[string]any{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(v)
	return out
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func compactAnyMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				out[k] = t
			}
		case nil:
			continue
		default:
			out[k] = v
		}
	}
	return out
}

func listFromResponseData(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range []string{"list", "records", "rows", "items", "content", "data"} {
		if out := listFromResponseData(m[k]); len(out) > 0 {
			return out
		}
	}
	return nil
}

func firstMediaURL(v any) string {
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"PlayURL", "PlayUrl", "playUrl", "videoUrl", "mediaUrl", "source", "url"} {
			if u := normalizePlayableMediaURL(val(m, k)); u != "" {
				return u
			}
		}
	}
	return ""
}

func normalizePlayableMediaURL(raw string) string {
	u := strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return ""
	}
	lower := strings.ToLower(u)
	for _, marker := range []string{".m3u8", ".mp4", ".flv", ".m4v", ".mov", ".mp3", ".m4a", ".aac", ".wav"} {
		if strings.Contains(lower, marker) {
			return u
		}
	}
	return ""
}
func firstDownloadURLFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return normalizeDownloadURL(t)
	case map[string]any:
		for _, k := range []string{"downloadUrl", "path", "url", "fileUrl", "source"} {
			if u := normalizeDownloadURL(val(t, k)); u != "" {
				return u
			}
		}
	case []any:
		for _, it := range t {
			if u := firstDownloadURLFromAny(it); u != "" {
				return u
			}
		}
	}
	for _, m := range mapsUnder(v) {
		for _, k := range []string{"downloadUrl", "path", "url", "fileUrl", "source"} {
			if u := normalizeDownloadURL(val(m, k)); u != "" {
				return u
			}
		}
	}
	return ""
}
func val(m map[string]any, k string) string {
	if v, ok := m[k]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}
func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}
func encodeParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k, v := range params {
		v = strings.TrimSpace(v)
		if v != "" && v != "null" && v != "undefined" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, wx233Escape(k)+"="+wx233Escape(params[k]))
	}
	return strings.Join(parts, "&")
}

func wx233Escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
func tokenFromJar(jar http.CookieJar) string {
	for _, raw := range []string{refererURL, "https://japi.233.com/"} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				if strings.EqualFold(c.Name, "clientauthentication") || strings.EqualFold(c.Name, "token") {
					return c.Value
				}
			}
		}
	}
	return ""
}

func cookiesFromJar(jar http.CookieJar) map[string]string {
	out := map[string]string{}
	for _, raw := range []string{refererURL, "https://japi.233.com/"} {
		if u, err := url.Parse(raw); err == nil {
			for _, c := range jar.Cookies(u) {
				out[c.Name] = c.Value
				out[strings.ToLower(c.Name)] = c.Value
			}
		}
	}
	return out
}
func media(title, u, fmtName string, extra map[string]any) *extractor.MediaInfo {
	if title == "" {
		title = "video"
	}
	fmtName = firstNonEmpty(fmtName, mediaFormat(u))
	if strings.EqualFold(fmtName, "m3u8") && extra != nil {
		if text, ok := extra["m3u8_text"].(string); ok && strings.TrimSpace(text) != "" {
			if _, ok := extra["m3u8_url"]; !ok {
				extra["m3u8_url"] = u
			}
			if _, ok := extra["source_type"]; !ok {
				extra["source_type"] = "m3u8_text"
			}
			u = shared.M3U8DataURL(text)
		}
	}
	stream := extractor.Stream{Quality: "best", URLs: []string{u}, Format: fmtName, Headers: map[string]string{"Referer": refererURL}}
	if strings.EqualFold(fmtName, "m3u8") {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "wangxiao233", Title: title, Streams: map[string]extractor.Stream{"default": stream}, Extra: extra}
}
func mediaFormat(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	if strings.HasPrefix(u, "data:application/vnd.apple.mpegurl") ||
		strings.HasPrefix(u, "data:application/x-mpegurl") ||
		strings.Contains(u, ".m3u8") ||
		strings.Contains(u, "format=m3u8") ||
		strings.Contains(u, "type=m3u8") {
		return "m3u8"
	}
	if strings.Contains(u, ".mp3") {
		return "mp3"
	}
	return "mp4"
}
func fileFormat(vals ...string) string {
	for _, raw := range vals {
		s := strings.ToLower(strings.TrimSpace(raw))
		s = strings.SplitN(strings.SplitN(s, "?", 2)[0], "#", 2)[0]
		if i := strings.LastIndex(s, "."); i >= 0 && i < len(s)-1 {
			ext := strings.Trim(s[i+1:], ". ")
			if ext != "" && len(ext) <= 6 {
				return ext
			}
		}
		switch strings.Trim(s, ". ") {
		case "pdf", "ppt", "pptx", "doc", "docx", "zip", "rar", "7z", "xls", "xlsx", "txt":
			return strings.Trim(s, ". ")
		}
	}
	return "pdf"
}
func firstFileishURL(m map[string]any) string {
	for _, k := range []string{"downloadUrl", "fileUrl", "file_url", "path", "url"} {
		u := val(m, k)
		if u == "" {
			continue
		}
		if k == "downloadUrl" || k == "fileUrl" || k == "file_url" || looksLikeFileURL(u) {
			return u
		}
	}
	return ""
}
func looksLikeFileMeta(m map[string]any) bool {
	return firstNonEmpty(val(m, "fileType"), val(m, "extension"), val(m, "fmt"), val(m, "detailLectureSize")) != "" ||
		looksLikeFileURL(firstNonEmpty(val(m, "title"), val(m, "name"), val(m, "fileName")))
}
func looksLikeFileURL(u string) bool {
	ext := fileFormat(u)
	return ext != "mp4" && ext != "m3u8" && ext != "mp3" && ext != "pdf" || strings.Contains(strings.ToLower(u), ".pdf") ||
		strings.Contains(strings.ToLower(u), ".ppt") || strings.Contains(strings.ToLower(u), ".doc") ||
		strings.Contains(strings.ToLower(u), ".xls") || strings.Contains(strings.ToLower(u), ".zip") ||
		strings.Contains(strings.ToLower(u), ".rar") || strings.Contains(strings.ToLower(u), ".7z")
}
func normalizeDownloadURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://japi.233.com" + raw
	}
	return raw
}
func parseSizeBytes(raw string) int64 {
	raw = strings.TrimSpace(strings.ToUpper(raw))
	if raw == "" {
		return 0
	}
	unit := ""
	for _, suffix := range []string{"GB", "G", "MB", "M", "KB", "K"} {
		if strings.Contains(raw, suffix) {
			unit = suffix
			raw = strings.TrimSpace(strings.ReplaceAll(raw, suffix, ""))
			break
		}
	}
	raw = strings.TrimSpace(strings.TrimRight(raw, "B"))
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0
	}
	switch unit {
	case "GB", "G":
		v *= 1024 * 1024 * 1024
	case "MB", "M":
		v *= 1024 * 1024
	case "KB", "K":
		v *= 1024
	default:
		if v < 1024 {
			v *= 1024 * 1024
		}
	}
	return int64(v)
}
func firstStreamURL(mi *extractor.MediaInfo) string {
	if mi == nil {
		return ""
	}
	for _, stream := range mi.Streams {
		if len(stream.URLs) > 0 && strings.TrimSpace(stream.URLs[0]) != "" {
			return strings.TrimSpace(stream.URLs[0])
		}
	}
	return ""
}
func cleanName(s string) string {
	s = strings.TrimSpace(s)
	return strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|', '\r', '\n', '\t':
			return '_'
		default:
			return r
		}
	}, s)
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func uniqueStrings(vals []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func qualityFromOpts(opts *extractor.ExtractOpts) string {
	if opts == nil {
		return ""
	}
	switch selectMode(opts.Quality) {
	case modeSD:
		return "sd"
	case modeHD:
		return "hd"
	case modeFHD:
		return "fhd"
	default:
		return ""
	}
}

func selectMode(quality string) int {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "4", "pdf", "only_pdf", "only-pdf", "file", "files":
		return modeOnlyPDF
	case "3", "sd":
		return modeSD
	case "2", "hd":
		return modeHD
	default:
		return modeFHD
	}
}
