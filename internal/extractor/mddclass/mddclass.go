// Package mddclass implements an extractor for mddclass.com (墨督督).
//
// API endpoints from decompiled Mooc/Courses/Mddclass/:
//
//	https://pass-api.sksight.com
//	https://lexue.mddclass.com
//	https://access.mddclass.com
//	https://webapi.sksight.com
package mddclass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	url0 = "https://pass-api.sksight.com"
	url1 = "https://lexue.mddclass.com"
	url2 = "https://access.mddclass.com"
)

const (
	mddclassPassAPIHost       = url0
	mddclassLexueHost         = url1
	mddclassAccessHost        = url2
	mddclassGlobalWebAPIHost  = "https://webapi.sksight.com"
	mddclassAPIV11            = "/webapi/content/v1.1"
	mddclassAPIV12            = "/webapi/content/v1.2"
	mddclassPCWebKey          = "pccembed"
	mddclassCompanyDomain     = "lexue"
	mddclassPlaceholderMP4    = "51b106759c84acade91a81ef83cf2eea.mp4"
	mddclassUserAgent         = "Mozilla/5.0 (Windows NT 6.2; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) QtWebEngine/5.15.2 Chrome/83.0.4103.122 Safari/537.36 CTPC/1.3.0.8/mddclass"
	mddclassPCClientAgentTmpl = "HJClient 1.0/pc/6.2.9200/1.3.0.8/qt/mddclass%s"
)

var (
	patterns                    = []string{`(?:[\w-]+\.)?mddclass\.com/`, `mddclass`, `墨督督`}
	mddclassVideoRe             = regexp.MustCompile(`/v/(\d+)`)
	mddclassGroupSeriesRe       = regexp.MustCompile(`/group/(\d+)/series/(\d+)`)
	mddclassSeriesRe            = regexp.MustCompile(`/(?:m/)?series/(\d+)`)
	mddclassGroupRe             = regexp.MustCompile(`/(?:m/|web/)?group/(\d+)`)
	mddclassWindowsBadTitleChar = regexp.MustCompile(`[\\/:*?"<>|]+`)
)

func init() {
	extractor.Register(&Mddclass{}, extractor.SiteInfo{Name: "Mddclass", URL: "mddclass.com", NeedAuth: true})
}

type Mddclass struct{}

func (s *Mddclass) Patterns() []string { return patterns }

type mddclassTarget struct {
	Raw           string
	CompanyDomain string
	GroupID       string
	SeriesID      string
	VideoID       string
}

type mddclassSession struct {
	Cookie        string
	CookieMap     map[string]string
	Auth          map[string]string
	CompanyDomain string
	CompanyID     string
}

type mddclassCourse struct {
	SeriesID      string
	Title         string
	GroupID       string
	GroupName     string
	CompanyDomain string
	CompanyID     string
	Raw           map[string]any
}

type mddclassGroup struct {
	ID            string
	Title         string
	CompanyDomain string
	CompanyID     string
	Raw           map[string]any
}

type mddclassVideo struct {
	VideoID        string
	SeriesID       string
	Title          string
	RawTitle       string
	Index          int
	ContentType    string
	GroupID        string
	CompanyDomain  string
	CompanyID      string
	VideoURL       string
	Size           int64
	Duration       int64
	Raw            map[string]any
	Detail         map[string]any
	CoursewareInfo map[string]any
}

func (s *Mddclass) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("mddclass requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	target := mddclassParseTarget(rawURL)
	sess, err := mddclassBuildSession(opts.Cookies, target)
	if err != nil {
		return nil, err
	}

	if target.VideoID != "" {
		video, err := mddclassDirectVideo(c, sess, target)
		if err != nil {
			return nil, err
		}
		return mddclassBuildVideoEntry(c, sess, video, opts.ListOnly)
	}

	courses, err := mddclassLoadCourses(c, sess, target)
	if err != nil {
		return nil, err
	}
	course := mddclassPickCourse(courses, target.SeriesID)
	if course.SeriesID == "" {
		return nil, fmt.Errorf("mddclass: no course series found for URL %s", rawURL)
	}
	if course.CompanyDomain != "" {
		sess.CompanyDomain = course.CompanyDomain
	}
	if course.CompanyID != "" {
		sess.CompanyID = course.CompanyID
	}

	videos, courseTitle, err := mddclassFetchSeriesVideos(c, sess, course)
	if err != nil {
		return nil, err
	}
	entries := make([]*extractor.MediaInfo, 0, len(videos))
	skipped := []string{}
	for _, video := range videos {
		entry, err := mddclassBuildVideoEntry(c, sess, video, opts.ListOnly)
		if err != nil {
			skipped = append(skipped, err.Error())
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("mddclass: no playable media entries for series=%s: %s", course.SeriesID, strings.Join(skipped, "; "))
		}
		return nil, fmt.Errorf("mddclass: empty lesson list for series=%s", course.SeriesID)
	}
	return &extractor.MediaInfo{
		Site:    "mddclass",
		Title:   mddclassFirstText(courseTitle, course.Title, "墨督督课程"+course.SeriesID),
		Entries: entries,
		Extra: map[string]any{
			"series_id":      course.SeriesID,
			"group_id":       course.GroupID,
			"company_domain": sess.CompanyDomain,
			"company_id":     sess.CompanyID,
			"raw":            course.Raw,
		},
	}, nil
}

func mddclassParseTarget(raw string) mddclassTarget {
	t := mddclassTarget{Raw: raw}
	text := strings.TrimSpace(raw)
	if text == "" {
		return t
	}
	if strings.EqualFold(text, "a") || strings.EqualFold(text, "app") || strings.EqualFold(text, "mddclass") || text == "墨督督" {
		return t
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" {
		parsed, _ = url.Parse("https://" + strings.TrimPrefix(text, "//"))
	}
	if parsed != nil {
		host := strings.ToLower(parsed.Hostname())
		if strings.HasSuffix(host, ".mddclass.com") {
			label := strings.SplitN(host, ".", 2)[0]
			label = strings.TrimSuffix(label, "-m")
			if label != "" && label != "www" && label != "pass" && label != "access" && label != "service" {
				t.CompanyDomain = label
			}
		}
		pathValue := parsed.EscapedPath()
		if pathValue == "" {
			pathValue = parsed.Path
		}
		if m := mddclassVideoRe.FindStringSubmatch(pathValue); len(m) == 2 {
			t.VideoID = m[1]
		}
		if m := mddclassGroupSeriesRe.FindStringSubmatch(pathValue); len(m) == 3 {
			t.GroupID, t.SeriesID = m[1], m[2]
		} else if m := mddclassSeriesRe.FindStringSubmatch(pathValue); len(m) == 2 {
			t.SeriesID = m[1]
		}
		if t.GroupID == "" {
			if m := mddclassGroupRe.FindStringSubmatch(pathValue); len(m) == 2 {
				t.GroupID = m[1]
			}
		}
		q := parsed.Query()
		if t.SeriesID == "" {
			t.SeriesID = mddclassFirstText(q.Get("sid"), q.Get("seriesId"), q.Get("series_id"))
		}
		if t.GroupID == "" {
			t.GroupID = mddclassFirstText(q.Get("groupId"), q.Get("group_id"), q.Get("gid"))
		}
		if t.VideoID == "" {
			t.VideoID = mddclassFirstText(q.Get("contentId"), q.Get("videoId"), q.Get("vid"))
		}
	}
	return t
}

func mddclassBuildSession(jar http.CookieJar, target mddclassTarget) (*mddclassSession, error) {
	cookie := mddclassCookieString(jar, target)
	if cookie == "" {
		return nil, fmt.Errorf("mddclass requires cookie header")
	}
	cookieMap := mddclassCookieMap(cookie)
	auth := mddclassAuthContext(cookieMap)
	if target.CompanyDomain != "" {
		auth["companyDomain"] = target.CompanyDomain
	}
	company := mddclassFirstText(auth["companyDomain"], auth["company_domain"], mddclassCompanyDomain)
	return &mddclassSession{Cookie: cookie, CookieMap: cookieMap, Auth: auth, CompanyDomain: strings.ToLower(company), CompanyID: mddclassFirstText(auth["companyId"], auth["company_id"])}, nil
}

func mddclassCookieString(jar http.CookieJar, target mddclassTarget) string {
	seen, parts := map[string]bool{}, []string{}
	hosts := []string{
		mddclassLexueHost + "/",
		mddclassAccessHost + "/",
		mddclassPassAPIHost + "/",
		mddclassGlobalWebAPIHost + "/",
		"https://mddclass.com/",
		"https://www.mddclass.com/",
		"https://lexue-m.mddclass.com/",
		"https://meixiang.mddclass.com/",
		"https://meixiang-m.mddclass.com/",
	}
	if target.CompanyDomain != "" {
		hosts = append(hosts, "https://"+target.CompanyDomain+".mddclass.com/", "https://"+target.CompanyDomain+"-m.mddclass.com/")
	}
	if u, err := url.Parse(target.Raw); err == nil && u.Host != "" {
		hosts = append(hosts, u.Scheme+"://"+u.Host+"/")
	}
	for _, raw := range hosts {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		for _, ck := range jar.Cookies(u) {
			if ck.Value == "" || seen[strings.ToLower(ck.Name)] {
				continue
			}
			seen[strings.ToLower(ck.Name)] = true
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func mddclassCookieMap(cookie string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" {
			continue
		}
		name := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if decoded, err := url.QueryUnescape(value); err == nil {
			value = decoded
		}
		out[name] = value
		out[strings.ToLower(name)] = value
	}
	return out
}

func mddclassAuthContext(cookieMap map[string]string) map[string]string {
	ctx := map[string]string{}
	for _, key := range []string{"uid", "userId", "user_id", "accessToken", "access_token", "hj_token", "ocsAccessToken", "ocs_access_token", "ocsPlayerAccessToken", "ocs_player_access_token", "playerAccessToken", "player_access_token", "tracertNo", "tracert_no", "TracetNo", "newTracetNo", "newTracertNo", "deviceId", "device_id", "clientDeviceId", "companyDomain", "company_domain", "companyId", "company_id", "userSign", "user_sign", "feAuth", "fe_auth", "keysEncrypt", "keys_encrypt", "tenantId", "tenant_id", "HJUserAgent"} {
		if value := mddclassFirstText(cookieMap[key], cookieMap[strings.ToLower(key)]); value != "" {
			ctx[key] = value
		}
	}
	envMap := map[string][]string{
		"MDDCLASS_GATEWAY_LOGIN_FLAGS": {"gatewayLoginFlags"},
		"MDDCLASS_GATEWAY_VERSION":     {"gatewayVersion", "timeFlag"},
		"MDDCLASS_GATEWAY_ENDPOINT":    {"gatewayEndpoint", "gateway_endpoint"},
		"MDDCLASS_GATEWAY_TOKEN":       {"gatewayToken", "gateway_token"},
		"MDDCLASS_REFRESH_TOKEN":       {"refreshToken", "refresh_token"},
		"MDDCLASS_OCS_ACCESS_TOKEN":    {"ocsAccessToken", "ocs_access_token"},
		"MDDCLASS_ACCESS_TOKEN":        {"accessToken", "access_token"},
		"MDDCLASS_COMPANY_DOMAIN":      {"companyDomain"},
		"MDDCLASS_X_KEYS_ENCRYPT":      {"keysEncrypt"},
		"MDDCLASS_KEYS_ENCRYPT":        {"keysEncrypt"},
		"MDDCLASS_FE_AUTH":             {"feAuth"},
		"MDDCLASS_X_FE_AUTH":           {"feAuth"},
		"MDDCLASS_USER_SIGN":           {"userSign"},
		"MDDCLASS_X_USER_SIGN":         {"userSign"},
		"MDDCLASS_NEW_TRACERT_NO":      {"newTracetNo", "newTracertNo"},
		"MDDCLASS_TRACERT_NO":          {"tracertNo", "TracetNo", "deviceId"},
		"MDDCLASS_DEVICE_ID":           {"deviceId", "tracertNo", "TracetNo"},
	}
	for env, keys := range envMap {
		value := strings.TrimSpace(os.Getenv(env))
		if value == "" {
			continue
		}
		for _, key := range keys {
			ctx[key] = value
		}
	}
	if ctx["access_token"] == "" && ctx["hj_token"] != "" {
		ctx["access_token"] = ctx["hj_token"]
	}
	return ctx
}

func mddclassLoadCourses(c *util.Client, sess *mddclassSession, target mddclassTarget) ([]mddclassCourse, error) {
	if target.SeriesID != "" {
		return []mddclassCourse{{SeriesID: target.SeriesID, GroupID: target.GroupID, Title: "墨督督课程" + target.SeriesID, CompanyDomain: sess.CompanyDomain, CompanyID: sess.CompanyID, Raw: map[string]any{"_source": "direct_series", "seriesId": target.SeriesID}}}, nil
	}
	if target.GroupID != "" {
		group := mddclassGroup{ID: target.GroupID, Title: "墨督督班级" + target.GroupID, CompanyDomain: sess.CompanyDomain, CompanyID: sess.CompanyID, Raw: map[string]any{"_source": "direct_group", "groupId": target.GroupID}}
		courses := mddclassFetchGroupSeries(c, sess, group)
		if len(courses) == 0 {
			return nil, fmt.Errorf("mddclass: no series found for group=%s", target.GroupID)
		}
		return courses, nil
	}
	groups := mddclassFetchGroups(c, sess)
	courses := []mddclassCourse{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, course := range mddclassFetchGroupSeries(c, sess, group) {
			key := course.CompanyDomain + ":" + course.GroupID + ":" + course.SeriesID
			if course.SeriesID == "" || seen[key] {
				continue
			}
			seen[key] = true
			courses = append(courses, course)
		}
	}
	if len(courses) == 0 {
		return nil, fmt.Errorf("mddclass: no purchased course series found")
	}
	return courses, nil
}

func mddclassFetchGroups(c *util.Client, sess *mddclassSession) []mddclassGroup {
	domains := mddclassUniqueStrings([]string{sess.CompanyDomain, mddclassCompanyDomain, "meixiang"})
	out := []mddclassGroup{}
	seen := map[string]bool{}
	for _, domain := range domains {
		if domain == "" {
			continue
		}
		mobileHost := fmt.Sprintf("https://%s-m.mddclass.com", domain)
		for start := 0; start < 2000; start += 50 {
			headers := sess.webHeaders(domain, mobileHost+"/mycourse", "application/json, text/plain, */*")
			headers["Origin"] = mobileHost
			headers["X-CC-COMPANY"] = domain
			resp, err := mddclassGetJSON(c, mobileHost+"/webapi/content/v1.1/user/my_group_list", map[string]string{"start": strconv.Itoa(start), "limit": "50", "sortType": "1", "keyword": ""}, headers)
			if err != nil || !mddclassPayloadSuccess(resp) {
				break
			}
			data := mddclassPayloadData(resp)
			items := mddclassRecords(mddclassExtractList(data))
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				gid := mddclassFirstText(item["groupId"], item["contentId"], item["id"], item["group_id"])
				if gid == "" {
					continue
				}
				key := domain + ":" + gid
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, mddclassGroup{ID: gid, Title: mddclassFirstText(item["groupName"], item["contentName"], item["name"], item["title"]), CompanyDomain: domain, CompanyID: mddclassFirstText(item["companyId"], item["sellerId"]), Raw: item})
			}
			if !mddclassHasNextPage(mddclassMap(data), len(items), start, 50) {
				break
			}
		}
	}
	return out
}

func mddclassFetchGroupSeries(c *util.Client, sess *mddclassSession, group mddclassGroup) []mddclassCourse {
	if group.ID == "" {
		return nil
	}
	domain := mddclassFirstText(group.CompanyDomain, sess.CompanyDomain, mddclassCompanyDomain)
	oldDomain, oldCompanyID := sess.CompanyDomain, sess.CompanyID
	sess.CompanyDomain = domain
	if group.CompanyID != "" {
		sess.CompanyID = group.CompanyID
	}
	defer func() { sess.CompanyDomain, sess.CompanyID = oldDomain, oldCompanyID }()

	out := []mddclassCourse{}
	seen := map[string]bool{}
	appendItems := func(items []map[string]any) {
		for _, item := range items {
			seriesID := mddclassFirstText(item["seriesId"], item["id"], item["series_id"])
			if seriesID == "" || seen[seriesID] {
				continue
			}
			seen[seriesID] = true
			out = append(out, mddclassCourse{SeriesID: seriesID, Title: mddclassFirstText(item["seriesName"], item["title"], item["name"]), GroupID: group.ID, GroupName: group.Title, CompanyDomain: domain, CompanyID: mddclassFirstText(group.CompanyID, item["companyId"], item["sellerId"]), Raw: item})
		}
	}

	postWorked := false
	for start := 0; start < 2000; start += 30 {
		payload := map[string]any{"groupId": group.ID, "limit": 30, "start": start}
		if uid := mddclassFirstText(sess.Auth["uid"], sess.Auth["userId"], sess.Auth["user_id"]); uid != "" {
			if n, err := strconv.Atoi(uid); err == nil {
				payload["userId"] = n
			} else {
				payload["userId"] = uid
			}
		}
		resp, err := mddclassPCContentPost(c, sess, fmt.Sprintf("/series/group/%s/series_subscribe", url.PathEscape(group.ID)), payload, "v1.1")
		if err != nil || !mddclassPayloadSuccess(resp) {
			break
		}
		postWorked = true
		data := mddclassPayloadData(resp)
		items := mddclassRecords(mddclassExtractList(data))
		appendItems(items)
		if !mddclassHasNextPage(mddclassMap(data), len(items), start, 30) {
			break
		}
	}
	if postWorked && len(out) > 0 {
		return out
	}
	for start := 0; start < 2000; start += 30 {
		resp, err := mddclassPCContentGet(c, sess, fmt.Sprintf("/series/group/%s/series_list", url.PathEscape(group.ID)), map[string]string{"limit": "30", "start": strconv.Itoa(start)}, "v1.1")
		if err != nil || !mddclassPayloadSuccess(resp) {
			break
		}
		data := mddclassPayloadData(resp)
		items := mddclassRecords(mddclassExtractList(data))
		appendItems(items)
		if !mddclassHasNextPage(mddclassMap(data), len(items), start, 30) {
			break
		}
	}
	if len(out) > 0 {
		return out
	}
	for start := 0; start < 2000; start += 30 {
		resp, err := mddclassAPIGet(c, sess, fmt.Sprintf("/series/group/%s/series", url.PathEscape(group.ID)), map[string]string{"limit": "30", "start": strconv.Itoa(start)}, "v1.2", "")
		if err != nil || !mddclassPayloadSuccess(resp) {
			break
		}
		data := mddclassPayloadData(resp)
		items := mddclassRecords(mddclassExtractList(data))
		appendItems(items)
		if !mddclassHasNextPage(mddclassMap(data), len(items), start, 30) {
			break
		}
	}
	return out
}

func mddclassPickCourse(courses []mddclassCourse, seriesID string) mddclassCourse {
	for _, course := range courses {
		if seriesID != "" && course.SeriesID == seriesID {
			return course
		}
	}
	if len(courses) > 0 {
		return courses[0]
	}
	return mddclassCourse{}
}

func mddclassFetchSeriesVideos(c *util.Client, sess *mddclassSession, course mddclassCourse) ([]mddclassVideo, string, error) {
	if course.CompanyDomain != "" {
		sess.CompanyDomain = course.CompanyDomain
	}
	if course.CompanyID != "" {
		sess.CompanyID = course.CompanyID
	}
	resp, err := mddclassPCContentGet(c, sess, "/series/all_lesson_list", map[string]string{"showStudyTime": "true", "seriesId": course.SeriesID}, "v1.2")
	if err != nil {
		return nil, "", fmt.Errorf("mddclass all_lesson_list: %w", err)
	}
	if !mddclassPayloadSuccess(resp) {
		return nil, "", fmt.Errorf("mddclass all_lesson_list rejected for series=%s", course.SeriesID)
	}
	data := mddclassPayloadData(resp)
	dataMap := mddclassMap(data)
	items := mddclassRecords(dataMap["items"])
	if len(items) == 0 {
		items = mddclassRecords(mddclassExtractList(data))
	}
	videos := make([]mddclassVideo, 0, len(items))
	for i, item := range items {
		videoInfo := mddclassMap(item["videoInfo"])
		vid := mddclassFirstText(videoInfo["videoId"], item["contentId"], item["videoId"], item["id"])
		if vid == "" {
			continue
		}
		idx := i + 1
		if showIndex, ok := mddclassInt(item["showIndex"]); ok {
			idx = showIndex + 1
		}
		rawTitle := mddclassFirstText(videoInfo["videoName"], item["contentTitle"], item["title"], item["name"], vid)
		coursewareInfo := mddclassMergeMaps(mddclassExtractCoursewareInfo(item), mddclassExtractCoursewareInfo(videoInfo))
		videos = append(videos, mddclassVideo{
			VideoID:        vid,
			SeriesID:       course.SeriesID,
			Title:          mddclassFormatVideoTitle(idx, rawTitle),
			RawTitle:       rawTitle,
			Index:          idx,
			ContentType:    mddclassFirstText(item["contentType"], videoInfo["contentType"]),
			GroupID:        mddclassFirstText(dataMap["groupId"], course.GroupID),
			CompanyDomain:  mddclassFirstText(course.CompanyDomain, sess.CompanyDomain),
			CompanyID:      mddclassFirstText(course.CompanyID, sess.CompanyID),
			VideoURL:       mddclassNormalizeMediaURL(mddclassFirstText(videoInfo["videoUrl"], item["videoUrl"], coursewareInfo["videoUrl"])),
			Size:           mddclassInt64(videoInfo["totalSize"], item["totalSize"], videoInfo["size"], item["size"]),
			Duration:       mddclassInt64(videoInfo["contentDuration"], item["contentDuration"], videoInfo["duration"], item["duration"]),
			Raw:            item,
			Detail:         videoInfo,
			CoursewareInfo: coursewareInfo,
		})
	}
	return videos, mddclassFirstText(dataMap["seriesName"], course.Title), nil
}

func mddclassDirectVideo(c *util.Client, sess *mddclassSession, target mddclassTarget) (mddclassVideo, error) {
	if target.CompanyDomain != "" {
		sess.CompanyDomain = target.CompanyDomain
	}
	detail, err := mddclassFetchVideoDetail(c, sess, target.VideoID, target.SeriesID)
	if err != nil {
		return mddclassVideo{}, err
	}
	seriesInfo := mddclassMap(detail["seriesInfo"])
	seriesID := mddclassFirstText(target.SeriesID, seriesInfo["seriesId"])
	rawTitle := mddclassFirstText(detail["videoName"], detail["title"], "墨督督视频"+target.VideoID)
	coursewareInfo := mddclassExtractCoursewareInfo(detail)
	return mddclassVideo{VideoID: target.VideoID, SeriesID: seriesID, Title: mddclassFormatVideoTitle(1, rawTitle), RawTitle: rawTitle, Index: 1, ContentType: mddclassFirstText(detail["contentType"]), GroupID: mddclassFirstText(detail["groupId"], seriesInfo["groupId"]), CompanyDomain: sess.CompanyDomain, CompanyID: sess.CompanyID, VideoURL: mddclassNormalizeMediaURL(mddclassFirstText(detail["videoUrl"], coursewareInfo["videoUrl"])), Size: mddclassInt64(detail["totalSize"], detail["size"]), Duration: mddclassInt64(detail["contentDuration"], detail["duration"]), Detail: detail, Raw: detail, CoursewareInfo: coursewareInfo}, nil
}

func mddclassBuildVideoEntry(c *util.Client, sess *mddclassSession, video mddclassVideo, listOnly bool) (*extractor.MediaInfo, error) {
	title := mddclassFirstText(video.Title, mddclassFormatVideoTitle(video.Index, video.RawTitle), video.VideoID)
	extra := map[string]any{"video_id": video.VideoID, "series_id": video.SeriesID, "group_id": video.GroupID, "company_domain": mddclassFirstText(video.CompanyDomain, sess.CompanyDomain), "content_type": video.ContentType, "raw": video.Raw}
	if listOnly {
		return &extractor.MediaInfo{Site: "mddclass", Title: title, Extra: extra}, nil
	}
	detail := video.Detail
	if video.VideoID != "" && (len(detail) == 0 || mddclassIsPlaceholderURL(mddclassFirstText(video.VideoURL, detail["videoUrl"]))) {
		if fetched, err := mddclassFetchVideoDetail(c, sess, video.VideoID, video.SeriesID); err == nil && len(fetched) > 0 {
			detail = mddclassMergeMaps(detail, fetched)
		}
	}
	coursewareInfo := mddclassMergeMaps(video.CoursewareInfo, mddclassExtractCoursewareInfo(detail))
	mediaURL := mddclassNormalizeMediaURL(mddclassFirstText(video.VideoURL, detail["videoUrl"], coursewareInfo["videoUrl"], mddclassFindMediaURL(detail), mddclassFindMediaURL(video.Raw), mddclassFindMediaURL(coursewareInfo)))
	if mddclassIsPlaceholderURL(mediaURL) {
		mediaURL = ""
	}
	if mediaURL == "" {
		extra["detail"] = detail
		extra["courseware_info"] = coursewareInfo
		return nil, fmt.Errorf("mddclass video %s: no media URL in API payload", video.VideoID)
	}
	format := mddclassStreamFormat(mediaURL)
	extra["detail"] = detail
	extra["courseware_info"] = coursewareInfo
	return &extractor.MediaInfo{
		Site:  "mddclass",
		Title: title,
		Streams: map[string]extractor.Stream{"best": {
			Quality:   "best",
			URLs:      []string{mediaURL},
			Format:    format,
			Size:      video.Size,
			NeedMerge: format == "m3u8",
			Headers:   sess.mediaHeaders(video),
		}},
		Extra: extra,
	}, nil
}

func mddclassFetchVideoDetail(c *util.Client, sess *mddclassSession, videoID, seriesID string) (map[string]any, error) {
	if videoID == "" {
		return nil, fmt.Errorf("mddclass: empty video id")
	}
	withSeriesFirst := strings.EqualFold(os.Getenv("MDDCLASS_DETAIL_WITH_SERIES_ID"), "1") || strings.EqualFold(os.Getenv("MDDCLASS_DETAIL_WITH_SERIES_ID"), "true") || strings.EqualFold(os.Getenv("MDDCLASS_DETAIL_WITH_SERIES_ID"), "yes") || strings.EqualFold(os.Getenv("MDDCLASS_DETAIL_WITH_SERIES_ID"), "y")
	candidates := []string{""}
	if seriesID != "" && withSeriesFirst {
		candidates = []string{seriesID, ""}
	} else if seriesID != "" {
		candidates = []string{"", seriesID}
	}
	var lastErr error
	for _, sid := range mddclassUniqueStrings(candidates) {
		headers := sess.tracertHeaders()
		resp, err := mddclassAPIGet(c, sess, "/video/detail", map[string]string{"videoId": videoID, "seriesId": sid}, "v1.1", mddclassLessonReferer(videoID, seriesID), headers)
		if err != nil {
			lastErr = err
			continue
		}
		if !mddclassPayloadSuccess(resp) {
			lastErr = fmt.Errorf("mddclass video/detail rejected for video=%s series=%s", videoID, sid)
			continue
		}
		data := mddclassMap(mddclassPayloadData(resp))
		if len(data) == 0 {
			lastErr = fmt.Errorf("mddclass video/detail empty for video=%s", videoID)
			continue
		}
		mddclassUpdateSessionFromDetail(sess, data)
		return data, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("mddclass video/detail failed for video=%s", videoID)
}

func mddclassUpdateSessionFromDetail(sess *mddclassSession, data map[string]any) {
	companyInfo := mddclassMap(data["companyInfo"])
	if domain := mddclassFirstText(companyInfo["domain"]); domain != "" {
		sess.CompanyDomain = domain
		sess.Auth["companyDomain"] = domain
	}
	if companyID := mddclassFirstText(companyInfo["sellerId"], data["companyId"]); companyID != "" {
		sess.CompanyID = companyID
		sess.Auth["companyId"] = companyID
		sess.Auth["company_id"] = companyID
		sess.Auth["gatewayCompanyId"] = companyID
		sess.Auth["gateway_company_id"] = companyID
	}
	coursewareInfo := mddclassExtractCoursewareInfo(data)
	for _, pair := range [][2]string{{"userSign", "userSign"}, {"user_sign", "userSign"}, {"tenantId", "tenantId"}, {"tenant_id", "tenantId"}} {
		if value := mddclassFirstText(coursewareInfo[pair[1]]); value != "" {
			sess.Auth[pair[0]] = value
		}
	}
}

func (sess *mddclassSession) companyHost(domain string) string {
	d := strings.TrimSpace(domain)
	if d == "" {
		d = mddclassFirstText(sess.CompanyDomain, mddclassCompanyDomain)
	}
	return fmt.Sprintf("https://%s.mddclass.com", d)
}

func mddclassAPIURL(sess *mddclassSession, apiPath, version string) string {
	if strings.HasPrefix(apiPath, "http") {
		return apiPath
	}
	prefix := mddclassAPIV11
	if version == "v1.2" {
		prefix = mddclassAPIV12
	}
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	return sess.companyHost("") + prefix + apiPath
}

func mddclassPCContentURL(apiPath, version string) string {
	if strings.HasPrefix(apiPath, "http") {
		return apiPath
	}
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	return strings.TrimRight(mddclassGlobalWebAPIHost, "/") + fmt.Sprintf("/content/%s%s", version, apiPath)
}

func mddclassAPIGet(c *util.Client, sess *mddclassSession, apiPath string, params map[string]string, version, referer string, extraHeaders ...map[string]string) (map[string]any, error) {
	headers := sess.webHeaders(sess.CompanyDomain, referer, "application/json, text/plain, */*")
	for _, extra := range extraHeaders {
		for k, v := range extra {
			headers[k] = v
		}
	}
	return mddclassGetJSON(c, mddclassAPIURL(sess, apiPath, version), params, headers)
}

func mddclassPCContentGet(c *util.Client, sess *mddclassSession, apiPath string, params map[string]string, version string) (map[string]any, error) {
	return mddclassGetJSON(c, mddclassPCContentURL(apiPath, version), params, sess.pcContentHeaders(""))
}

func mddclassPCContentPost(c *util.Client, sess *mddclassSession, apiPath string, payload map[string]any, version string) (map[string]any, error) {
	headers := sess.pcContentHeaders("")
	headers["Content-Type"] = "application/json"
	return mddclassPostJSON(c, mddclassPCContentURL(apiPath, version), payload, headers)
}

func mddclassGetJSON(c *util.Client, apiURL string, params map[string]string, headers map[string]string) (map[string]any, error) {
	body, err := c.GetString(mddclassURLWithParams(apiURL, params), headers)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("parse JSON from %s: %w", apiURL, err)
	}
	return payload, nil
}

func mddclassPostJSON(c *util.Client, apiURL string, payload map[string]any, headers map[string]string) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.Post(apiURL, bytes.NewReader(body), headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse JSON from %s: %w", apiURL, err)
	}
	return out, nil
}
