// Package kuke implements an extractor for kuke99.com courses.
//
// API endpoints from decompiled Mooc/Courses/Kuke/:
//
//	https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//	https://player.polyv.net/secure/{vid}.js
//	https://www.kuke99.com/prod-api/kukecoregoods/pc/goods/getPackageList
//	https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyClassRoomGoodsPageProt
//	https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyCourseDetailNewProt
//	https://www.kuke99.com/prod-api/kukeonlineorder/pc/order/myOrderListProt
//	https://www.kuke99.com/prod-api/kukesearch/pc/kssUserBuyUnitGoods/v1/listMyOrderGoodsProt
//	https://www.kuke99.com/prod-api/kukestudentservice/userBroadcast/getPolyvNodeInfoProt
package kuke

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlPolyvKey       = "https://hls.videocc.net/playsafe/%s/%s/%s_%s.key?token=%s"
	urlPolyvSecureJS  = "https://player.polyv.net/secure/%s.js"
	urlPackageList    = "https://www.kuke99.com/prod-api/kukecoregoods/pc/goods/getPackageList"
	urlSvipCourseList = "https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyClassRoomGoodsPageProt"
	urlCourseDetail   = "https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyCourseDetailNewProt"
	urlOrderList      = "https://www.kuke99.com/prod-api/kukeonlineorder/pc/order/myOrderListProt"
	urlCourseList     = "https://www.kuke99.com/prod-api/kukesearch/pc/kssUserBuyUnitGoods/v1/listMyOrderGoodsProt"
	urlPolyvNodeInfo  = "https://www.kuke99.com/prod-api/kukestudentservice/userBroadcast/getPolyvNodeInfoProt"
	kukeReferer       = "https://www.kuke99.com/learn-center"
	kukeAppID         = "c9379359685"
	kukeAppKey        = "awo6ureum8bn"
	kukeUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"
	kukePolyvOrgID    = "927455000785735680"
)

var patterns = []string{`(?:[\w-]+\.)?kuke99\.com/`, `#小程序://库课`}

func init() {
	extractor.Register(&Kuke{}, extractor.SiteInfo{Name: "Kuke", URL: "kuke99.com", NeedAuth: true})
}

type Kuke struct{}

func (s *Kuke) Patterns() []string { return patterns }

var kukeCourseRe = regexp.MustCompile(`(?i)(?:goodsMasterId=([0-9]+)|/course/([0-9]+)|courseId=([0-9]+)|/learn-center/(?:live-detail|myClass).*?[?&]id=([0-9]+).*?[?&]userBuyUnitGoodsId=([0-9]+))`)

func (s *Kuke) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("kuke requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	cookie := kukeCookieString(opts.Cookies)
	token := kukeCookieValue(cookie, "TOKEN")
	if token == "" {
		return nil, fmt.Errorf("kuke requires TOKEN cookie")
	}
	headers := kukeHeaders(cookie, token)
	if err := kukeCheckCookie(c, headers, token); err != nil {
		return nil, err
	}

	cid, userBuyID := kukeParseIDs(rawURL)
	courses, _ := kukeFetchCourseList(c, headers, token)
	course := kukePickCourse(courses, cid, userBuyID)
	if len(course) == 0 && cid != "" && userBuyID != "" {
		if direct, err := kukeBuildDirectCourse(c, headers, token, cid, userBuyID); err == nil {
			course = direct
		}
	}
	if len(course) == 0 && len(courses) > 0 {
		course = courses[0]
	}
	if len(course) == 0 {
		return nil, fmt.Errorf("kuke: no purchased course matched URL: %s", rawURL)
	}
	content := mapAny(course["content"])
	cid = firstText(content["goodsMasterId"], cid)
	userBuyID = firstText(userBuyID, course["businessId"])
	if cid == "" {
		return nil, fmt.Errorf("kuke: empty goodsMasterId")
	}
	title := firstText(course["title"], content["goodsName"], "库课课程_"+cid)

	var details []kukeDetailWithTitle
	if kukeIsSvip(course) {
		for _, sub := range kukeFetchSvipSubcourses(c, headers, token, cid, userBuyID) {
			if intOf(sub["courseType"]) != 1 {
				continue
			}
			subID := firstText(sub["goodsMasterId"])
			subBuyID := firstText(sub["id"], sub["userBuyUnitGoodsId"])
			if d, err := kukeFetchCourseDetail(c, headers, token, subID, subBuyID); err == nil {
				details = append(details, kukeDetailWithTitle{Detail: d, Title: firstText(sub["courseName"])})
			}
		}
	}
	if len(details) == 0 {
		d, err := kukeFetchCourseDetail(c, headers, token, cid, userBuyID)
		if err != nil {
			return nil, err
		}
		details = append(details, kukeDetailWithTitle{Detail: d})
		if dt := kukeTitleFromDetail(d); dt != "" && strings.HasPrefix(title, "库课课程_") {
			title = dt
		}
	}

	var entries []*extractor.MediaInfo
	for _, detail := range details {
		items := kukeBuildItems(detail.Detail, cid, detail.Title)
		for _, item := range items {
			var entry *extractor.MediaInfo
			var err error
			if item.Kind == "video" {
				entry, err = kukeBuildVideoEntry(c, headers, item)
			} else {
				entry = kukeBuildFileEntry(item)
			}
			if err == nil && entry != nil {
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("kuke: no playable video/file entries for goodsMasterId=%s", cid)
	}
	return &extractor.MediaInfo{Site: "kuke", Title: title, Entries: entries, Extra: map[string]any{"goodsMasterId": cid, "userBuyUnitGoodsId": userBuyID, "purchased": true}}, nil
}

type kukeEnvelope struct {
	Code    any             `json:"code"`
	Msg     string          `json:"msg"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}
type kukeCourseListData struct {
	OrderGoodsList []map[string]any `json:"orderGoodsList"`
	Count          any              `json:"count"`
}
type kukeSvipListData struct {
	CourseList []map[string]any `json:"courseList"`
}
type kukeNodeInfoData struct {
	PlaySafe    string `json:"playSafe"`
	VideoID     string `json:"videoId"`
	Token       string `json:"token"`
	KKAes       string `json:"kkAes"`
	KKSdkString string `json:"kkSdkString"`
}
type kukeDetailWithTitle struct {
	Detail map[string]any
	Title  string
}
type kukeItem struct {
	Kind, Name, Chapter, NodeID, GoodsMasterID, PolyvVideoID, FileURL, FileFmt string
	Duration                                                                   int
}

func kukeHeaders(cookie, token string) map[string]string {
	return map[string]string{"user-agent": kukeUserAgent, "referer": kukeReferer, "origin": "https://www.kuke99.com", "kk-version": "3.9.16", "kk-terminal-type": "pc", "kk-platform": "1", "kk-from": "web", "content-type": "application/json", "accept": "application/json", "cookie": cookie, "kk-token": token}
}

func kukeCheckCookie(c *util.Client, headers map[string]string, token string) error {
	_, err := kukeSignedPost(c, urlCourseList, map[string]any{"cateId": "0", "type": "0", "status": "1", "page": 1, "pageSize": 1}, headers, token)
	if err != nil {
		return fmt.Errorf("kuke cookie check: %w", err)
	}
	return nil
}

func kukeSignedPost(c *util.Client, apiURL string, biz map[string]any, headers map[string]string, token string) (json.RawMessage, error) {
	ts := time.Now().Unix()
	reqBody := map[string]any{"bizContent": biz, "time": ts, "sign": kukeSign(biz, ts, token), "appId": kukeAppID}
	b, _ := json.Marshal(reqBody)
	h := map[string]string{"content-type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	h["kk-request-id"] = strconv.FormatInt(time.Now().UnixMilli(), 10) + kukeRandHex(16)
	resp, err := c.Post(apiURL, bytes.NewReader(b), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, apiURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var env kukeEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse %s: %w", apiURL, err)
	}
	if code := firstText(env.Code); code != "10000" {
		return nil, fmt.Errorf("API code=%s msg=%s", code, firstText(env.Msg, env.Message))
	}
	return env.Data, nil
}

func kukeSign(biz map[string]any, ts int64, token string) string {
	keys := make([]string, 0, len(biz))
	for k, v := range biz {
		if s, ok := scalarString(v); ok && s != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		s, _ := scalarString(biz[k])
		parts = append(parts, k+"="+s)
	}
	base := strings.Join(parts, "&")
	if base != "" {
		base += "&"
	}
	base += fmt.Sprintf("appId=%s&appKey=%s&time=%d", kukeAppID, kukeAppKey, ts)
	if token != "" {
		base += "&token=" + token
	}
	sum := md5.Sum([]byte(base))
	return hex.EncodeToString(sum[:])
}

func kukeFetchCourseList(c *util.Client, headers map[string]string, token string) ([]map[string]any, error) {
	seen, out := map[string]bool{}, []map[string]any{}
	for _, status := range []string{"1", "3"} {
		for page := 1; page <= 50; page++ {
			data, err := kukeSignedPost(c, urlCourseList, map[string]any{"cateId": "0", "type": "0", "status": status, "page": page, "pageSize": 100}, headers, token)
			if err != nil {
				return out, err
			}
			var list kukeCourseListData
			if err := json.Unmarshal(data, &list); err != nil {
				return out, err
			}
			if len(list.OrderGoodsList) == 0 {
				break
			}
			for _, course := range list.OrderGoodsList {
				gid := firstText(mapAny(course["content"])["goodsMasterId"])
				if gid == "" || seen[gid] {
					continue
				}
				seen[gid] = true
				out = append(out, course)
			}
			if count := intOf(list.Count); count == 0 || page*100 >= count {
				break
			}
		}
	}
	filtered := make([]map[string]any, 0, len(out))
	for _, course := range out {
		content := mapAny(course["content"])
		if intOf(content["totalCourseCount"]) != 0 && intOf(content["goodsType"]) == 5 {
			filtered = append(filtered, course)
		}
	}
	if len(filtered) > 0 {
		return filtered, nil
	}
	return out, nil
}

func kukePickCourse(courses []map[string]any, cid, userBuyID string) map[string]any {
	if userBuyID != "" {
		for _, c := range courses {
			if firstText(c["businessId"]) == userBuyID {
				return c
			}
		}
	}
	if cid != "" {
		for _, c := range courses {
			if firstText(mapAny(c["content"])["goodsMasterId"]) == cid {
				return c
			}
		}
	}
	return nil
}

func kukeBuildDirectCourse(c *util.Client, headers map[string]string, token, gid, buyID string) (map[string]any, error) {
	detail, err := kukeFetchCourseDetail(c, headers, token, gid, buyID)
	if err != nil {
		return nil, err
	}
	content := map[string]any{"goodsMasterId": firstText(deepText(detail, "goodsMasterId"), gid), "goodsType": intOf(deepText(detail, "goodsType")), "totalCourseCount": len(records(detail["goodsCourseNodeList"]))}
	return map[string]any{"goods": detail, "title": firstText(kukeTitleFromDetail(detail), "库课课程_"+gid), "type": 1, "businessId": buyID, "content": content}, nil
}

func kukeFetchCourseDetail(c *util.Client, headers map[string]string, token, gid, buyID string) (map[string]any, error) {
	data, err := kukeSignedPost(c, urlCourseDetail, map[string]any{"goodsMasterId": gid, "userBuyUnitGoodsId": buyID}, headers, token)
	if err != nil {
		return nil, fmt.Errorf("kuke course detail: %w", err)
	}
	var detail map[string]any
	if err := json.Unmarshal(data, &detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func kukeFetchSvipSubcourses(c *util.Client, headers map[string]string, token, gid, buyID string) []map[string]any {
	data, err := kukeSignedPost(c, urlSvipCourseList, map[string]any{"goodsType": "", "subjectId": "", "userBuyUnitGoodsId": buyID, "goodsMasterId": gid, "specificationItemId": "", "page": 1, "pageSize": 100}, headers, token)
	if err != nil {
		return nil
	}
	var out kukeSvipListData
	_ = json.Unmarshal(data, &out)
	return out.CourseList
}
