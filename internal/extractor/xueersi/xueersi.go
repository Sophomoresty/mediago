// Package xueersi implements an extractor for xueersi.com (好未来学而思) courses.
package xueersi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	refererURL          = "https://www.xueersi.com/"
	loginCheckURL       = "https://api.xueersi.com/login/V1/Web/checkLogin?X-Businessline-Id=10"
	courseListAPI       = "https://i.xueersi.com/janus/App/StudyCenter/v2/courseList"
	backupCourseAPI     = "http://i.xueersi.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList"
	planListAPI         = "http://i.xueersi.com/icenter-go/App/StudyCenter/MyPlans/planListV2"
	playbackAPI         = "http://studentlive.xueersi.com/v1/student/classroom/playback/enter"
	priceDetailURL1     = "https://api.xueersi.com/mall/detail/1/%s"
	priceDetailURL10    = "https://api.xueersi.com/mall/detail/10/%s"
	priceDetailURL2     = "https://api.xueersi.com/mall/detail/2/%s"
	dramaGetURL         = "https://studentlive.xueersi.com/v1/student/classroom/drama/get"
	liveVodshowURL      = "https://gslbsaturn.xescdn.com/v2/vodshow?appid=xes20001&support=4&proto=2&scheme=3&fid=%s&agentp=psplayer-win&agentv=3.0.0&bid=7&uri=%s"
	recordingVodshowURL = "https://gslbsaturn.xescdn.com/v1/player/vodshow?appid=xes20001&support=4&proto=2&scheme=3&fid=%s&agentp=psplayer-win&agentv=2.9.7&bid=68&uri=%s"
	defaultUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var patterns = []string{`(?:[\w-]+\.)?(?:xueersi\.com|speiyou\.com)/`}
var targetRe = regexp.MustCompile(`(?i)(?:courseId|course_id|cid|stuCouId|stu_cou_id|stucouid|planId|plan_id|couType|courseType|type)=([^&#/]+)|course-detail/(\d+)|course(?:_|)id[=:](\d+)|stu(?:_|)cou(?:_|)id[=:](\d+)`)
var loginOKRe = regexp.MustCompile(`"stat"\s*:\s*1`)

func init() {
	extractor.Register(&Xueersi{}, extractor.SiteInfo{Name: "Xueersi", URL: "xueersi.com", NeedAuth: true})
}

type Xueersi struct{}

func (x *Xueersi) Patterns() []string { return patterns }

type target struct{ courseID, stuCouID, courseType, planID string }
type course struct{ id, stuCouID, title, typ, gradeID string }
type plan struct {
	id, title string
	raw       map[string]any
}

func (x *Xueersi) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xueersi requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	cookie := cookieHeader(opts.Cookies)
	h := baseHeaders(cookie)
	body, err := c.GetString(loginCheckURL, h)
	if err != nil {
		return nil, fmt.Errorf("xueersi checkLogin: %w", err)
	}
	if loginOKRe.FindStringSubmatch(body) == nil {
		return nil, fmt.Errorf("xueersi checkLogin rejected cookie")
	}
	t := parseTarget(rawURL)
	courses := fetchCourses(c, cookie)
	co := selectCourse(courses, t)
	if co.stuCouID == "" && t.stuCouID != "" {
		co = course{id: t.courseID, stuCouID: t.stuCouID, typ: firstNonEmpty(t.courseType, "1"), gradeID: "0", title: "xueersi_" + firstNonEmpty(t.courseID, t.stuCouID)}
	}
	if co.stuCouID == "" {
		return nil, fmt.Errorf("xueersi course %q not found in account course list", firstNonEmpty(t.courseID, t.stuCouID))
	}
	plans := fetchPlans(c, cookie, co, t)
	if len(plans) == 0 && t.planID != "" {
		plans = []plan{{id: t.planID, title: "plan_" + t.planID}}
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("xueersi plan list is empty for course %s", firstNonEmpty(co.id, co.stuCouID))
	}
	entries, seen := []*extractor.MediaInfo{}, map[string]bool{}
	for _, p := range plans {
		if t.planID != "" && p.id != t.planID {
			continue
		}
		u := getVideoM3U8(c, cookie, co, p.id)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		entries = append(entries, media(firstNonEmpty(p.title, "plan_"+p.id), u, co, p))
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("xueersi: no playable m3u8 URL resolved")
	}
	return &extractor.MediaInfo{Site: "xueersi", Title: firstNonEmpty(co.title, "xueersi_"+firstNonEmpty(co.id, co.stuCouID)), Entries: entries}, nil
}

func fetchCourses(c *util.Client, cookie string) []course {
	out := []course{}
	for _, s := range []struct{ stuMode, courseStructID int }{{1, 1}, {1, 0}, {0, 1}, {0, 0}, {1, 2}, {1, 3}} {
		root, err := postJSON(c, courseListAPI, map[string]any{"version": 3, "systemName": "pc-win", "subjectId": 0, "stuMode": s.stuMode, "region": "1", "pageNum": 100, "page": 1, "identifierForClient": "1", "gradeId": 0, "courseStructId": s.courseStructID, "couStatus": 0, "businessLineId": 10, "appVersionNumber": 101602}, courseHeaders(cookie, true, "10.16.02", "101602"))
		if err == nil {
			out = extendCourses(out, extractV2Courses(root))
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, typ := range []string{"1", "2"} {
		for _, sid := range []string{"1", "0"} {
			root, err := postFormJSON(c, backupCourseAPI, map[string]string{"courseStructId": sid, "gradeId": "0", "systemName": "pc-win", "appVersionNumber": "99800", "position": "1", "subjectId": "0", "couStatus": "0", "couType": typ}, courseHeaders(cookie, false, "9.98.0", "99800"))
			if err == nil {
				out = extendCourses(out, extractBackupCourses(root))
			}
		}
	}
	return out
}

func fetchPlans(c *util.Client, cookie string, co course, t target) []plan {
	head := planHeaders(cookie)
	seenCombo := map[string]bool{}
	for _, typ := range candidates(co.typ, t.courseType, "1", "2", "0") {
		for _, grade := range candidates(co.gradeID, "0", "8") {
			key := typ + ":" + grade
			if seenCombo[key] {
				continue
			}
			seenCombo[key] = true
			for _, withCID := range []bool{true, false} {
				form := map[string]string{"stuCouId": co.stuCouID, "type": typ, "appVerison": "9.98.0", "appVersionNumber": "99800", "systemName": "pc-win", "gradeId": grade}
				if withCID && co.id != "" {
					form["courseId"] = co.id
				}
				root, err := postFormJSON(c, planListAPI, form, head)
				if err != nil {
					continue
				}
				if plans := extractPlans(root); len(plans) > 0 {
					return plans
				}
			}
		}
	}
	return nil
}

func getVideoM3U8(c *util.Client, cookie string, co course, planID string) string {
	pid, err1 := strconv.Atoi(planID)
	sid, err2 := strconv.Atoi(co.stuCouID)
	if err1 != nil || err2 != nil {
		return ""
	}
	h := playbackHeaders(cookie, planID, co.stuCouID)
	root, err := postJSON(c, playbackAPI, map[string]any{"acceptPlanVersion": 1015, "bizId": 3, "planId": pid, "stuCouId": sid}, h)
	if err != nil {
		return ""
	}
	if u := liveM3U8(c, root); u != "" {
		return u
	}
	if u := recordingM3U8(c, cookie, root); u != "" {
		return u
	}
	return firstMediaURL(root)
}

func liveM3U8(c *util.Client, root map[string]any) string {
	configs := mapAt(mapAt(root, "data"), "configs")
	fid := firstNonEmpty(val(configs, "videoFile"), val(configs, "beforeClassFileId"))
	if fid == "" {
		return ""
	}
	return vodshow(c, liveVodshowURL, fid, "psplayer-win 3.0.0")
}

func recordingM3U8(c *util.Client, cookie string, root map[string]any) string {
	data := mapAt(root, "data")
	drama := mapAt(data, "dramaInfo")
	chapters := listFrom(drama["chapters"])
	planInfo := mapAt(data, "planInfo")
	if len(chapters) == 0 {
		return ""
	}
	chapterLogicID, dramaID, pid := val(chapters[0], "chapterLogicId"), val(drama, "dramaId"), firstNonEmpty(val(planInfo, "id"), val(planInfo, "planId"))
	if chapterLogicID == "" || dramaID == "" || pid == "" {
		return ""
	}
	root2, err := postJSON(c, dramaGetURL, map[string]any{"chapterLogicId": chapterLogicID, "dramaId": dramaID, "planId": pid}, playbackHeaders(cookie, "", ""))
	if err != nil {
		return ""
	}
	chapters2 := listFrom(mapAt(root2, "data")["chapters"])
	if len(chapters2) == 0 {
		return ""
	}
	sections := listFrom(chapters2[0]["sections"])
	if len(sections) == 0 {
		return ""
	}
	res := sections[0]["sectionResource"]
	rm, _ := res.(map[string]any)
	if s, ok := res.(string); ok {
		_ = json.Unmarshal([]byte(s), &rm)
	}
	fid := val(rm, "fid")
	if fid == "" {
		return ""
	}
	return vodshow(c, recordingVodshowURL, fid, "psplayer-win 2.9.7")
}

func vodshow(c *util.Client, tpl, fid, ua string) string {
	body, err := c.GetString(fmt.Sprintf(tpl, url.QueryEscape(fid), url.QueryEscape(fid)), map[string]string{"User-Agent": ua})
	if err != nil {
		return ""
	}
	root, err := parseJSON(body)
	if err != nil {
		return ""
	}
	addrs := listFrom(mapAt(root, "content")["addrs"])
	if len(addrs) == 0 {
		return ""
	}
	return val(addrs[0], "addr")
}

func postJSON(c *util.Client, api string, payload map[string]any, h map[string]string) (map[string]any, error) {
	b, _ := json.Marshal(payload)
	resp, err := c.Post(api, bytes.NewReader(b), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return parseJSON(string(raw))
}
func postFormJSON(c *util.Client, api string, form map[string]string, h map[string]string) (map[string]any, error) {
	body, err := c.PostForm(api, form, h)
	if err != nil {
		return nil, err
	}
	return parseJSON(body)
}
func parseJSON(body string) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("xueersi parse JSON: %w", err)
	}
	return root, nil
}

func extractV2Courses(root map[string]any) []course {
	data := mapAt(root, "data")
	out := []course{}
	for _, key := range []string{"learningCourses", "pendingCourses", "endedCourses"} {
		out = append(out, coursesFrom(listFrom(mapAt(data, key)["courseList"]))...)
	}
	return out
}
func extractBackupCourses(root map[string]any) []course {
	data := mapAt(mapAt(root, "result"), "data")
	out := []course{}
	for _, key := range []string{"learningCourses", "endedCourses", "pendingCourses"} {
		out = append(out, coursesFrom(listFrom(data[key]))...)
	}
	return out
}
func coursesFrom(ms []map[string]any) []course {
	out := []course{}
	for _, m := range ms {
		co := course{id: val(m, "courseId"), stuCouID: val(m, "stuCouId"), title: val(m, "courseName"), typ: firstNonEmpty(val(m, "type"), val(m, "courseType"), val(m, "couType")), gradeID: firstNonEmpty(val(m, "gradeId"), val(m, "grade"))}
		if co.id != "" || co.stuCouID != "" {
			out = append(out, co)
		}
	}
	return out
}
func extendCourses(base, add []course) []course {
	seen := map[string]bool{}
	out := []course{}
	for _, c := range base {
		k := c.id + "|" + c.stuCouID + "|" + c.title
		seen[k] = true
		out = append(out, c)
	}
	for _, c := range add {
		k := c.id + "|" + c.stuCouID + "|" + c.title
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}
func extractPlans(root map[string]any) []plan {
	list := listFrom(mapAt(mapAt(mapAt(root, "result"), "data"), "list"))
	if len(list) == 0 {
		list = listUnder(root, "list")
	}
	out := []plan{}
	for i, m := range list {
		id := val(m, "planId")
		if id == "" {
			continue
		}
		title := fmt.Sprintf("[%d]--%s", i+1, firstNonEmpty(val(m, "planName"), "未命名课程"))
		out = append(out, plan{id: id, title: title, raw: m})
	}
	return out
}
