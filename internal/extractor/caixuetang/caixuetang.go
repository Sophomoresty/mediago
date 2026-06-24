// Package caixuetang implements an extractor for caixuetang.cn.
package caixuetang

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	refererURL         = "https://www.caixuetang.cn/"
	originURL          = "https://www.caixuetang.cn"
	agentURL           = "https://service.agent.pro.caixuetang.cn"
	appcode            = "5ecbbee7c8259jmj7etln6jf"
	userInfoAPI        = "?c=user&a=memberinfo&v=user&site=user&serviceversion=v1"
	mycourseAPI        = "?c=Webmembercourse&a=mycourse&v=user&site=course&serviceversion=v3610"
	myvipcourseAPI     = "?c=Webmembercourse&a=myvipcourse&v=user&site=course&serviceversion=v3610"
	webplayInfoAPI     = "/course/v4207/App/play/webplayinfo/?"
	playinfoAPI        = "?c=Play&a=playinfo&v=app&site=course"
	webplayAPI         = "?c=video&a=getwebplayinfo&v=user&site=material"
	videoPlayAPI       = "?c=video&v=app&a=getvideoplay&site=material"
	downloadChapterAPI = "/course/v4114/user/download/chapter"
	downloadTaskAPI    = "/course/v4114/user/download/initiatetask"
	downloadInfoAPI    = "/material/v4119/user/download/info"
)

var (
	patterns     = []string{`(?:[\w-]+\.)?caixuetang\.cn/`}
	fallbackIDRe = regexp.MustCompile(`(?i)(?:course|play|detail|webplayinfo)[^\d]*(\d{3,})`)
	titleCleanRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
)

func init() {
	extractor.Register(&Caixuetang{}, extractor.SiteInfo{Name: "Caixuetang", URL: "caixuetang.cn", NeedAuth: true})
}

type Caixuetang struct{}

func (s *Caixuetang) Patterns() []string { return patterns }

type authState struct{ token, memberID, cookie string }
type cxContext struct {
	c                        *util.Client
	auth                     authState
	cid, videoID, courseType string
}

func (s *Caixuetang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("caixuetang requires login cookies")
	}
	cid, videoID, courseType := parseIDs(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("caixuetang: cannot parse course id from URL %q", rawURL)
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	auth := authFromJar(opts.Cookies)
	if auth.memberID == "" {
		return nil, fmt.Errorf("caixuetang: MEMBER_ID/member_id cookie is required")
	}
	ctx := &cxContext{c: c, auth: auth, cid: cid, videoID: videoID, courseType: courseType}
	if err := ctx.checkCookie(); err != nil {
		return nil, err
	}
	courseInfo := ctx.findCourse(cid)
	if courseType == "" {
		courseType = firstString(courseInfo, "course_type", "courseType", "course_type_new", "courseTypeNew")
		ctx.courseType = courseType
	}
	playinfo, err := ctx.getPlayinfo()
	if err != nil {
		return nil, err
	}
	title := cleanTitle(firstNonEmpty(firstString(courseInfo, "title", "course_name", "courseName", "name"), titleFromPlayinfo(playinfo), "caixuetang_"+cid))
	entries := ctx.buildEntries(playinfo)
	if len(entries) == 0 {
		return nil, fmt.Errorf("caixuetang: no playable entries found for course %s", cid)
	}
	return &extractor.MediaInfo{Site: "caixuetang", Title: title, Entries: entries}, nil
}

func parseIDs(raw string) (cid, videoID, courseType string) {
	u, _ := url.Parse(raw)
	qs := url.Values{}
	if u != nil {
		for k, vs := range u.Query() {
			for _, v := range vs {
				qs.Add(k, v)
			}
		}
		if strings.Contains(u.Fragment, "?") {
			if f, err := url.ParseQuery(strings.SplitN(u.Fragment, "?", 2)[1]); err == nil {
				for k, vs := range f {
					for _, v := range vs {
						qs.Add(k, v)
					}
				}
			}
		}
	}
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(qs.Get(k)); v != "" {
				return v
			}
		}
		return ""
	}
	cid = get("course_id", "courseId", "cid", "id")
	videoID = get("video_id", "videoId", "vid", "item_id", "itemId")
	courseType = get("course_type", "courseType", "course_type_new", "courseTypeNew")
	if cid == "" {
		if m := fallbackIDRe.FindStringSubmatch(raw); len(m) > 1 {
			cid = m[1]
		}
	}
	return
}

func authFromJar(jar http.CookieJar) authState {
	var cookies []*http.Cookie
	for _, raw := range []string{"https://www.caixuetang.cn/", "https://service.agent.pro.caixuetang.cn/"} {
		u, _ := url.Parse(raw)
		cookies = append(cookies, jar.Cookies(u)...)
	}
	parts, a := []string{}, authState{}
	for _, ck := range cookies {
		parts = append(parts, ck.Name+"="+ck.Value)
		switch ck.Name {
		case "TOKEN", "token", "key":
			if a.token == "" {
				a.token = ck.Value
			}
		case "MEMBER_ID", "member_id", "memberId":
			if a.memberID == "" {
				a.memberID = ck.Value
			}
		}
	}
	a.cookie = strings.Join(parts, "; ")
	return a
}

func (x *cxContext) apiURL(api string) string {
	if strings.HasPrefix(api, "http") {
		return api
	}
	if strings.HasPrefix(api, "/") {
		return agentURL + api
	}
	return agentURL + api
}
func (x *cxContext) apiData(data map[string]string) map[string]string {
	out := map[string]string{"client_type": "pc"}
	for k, v := range data {
		out[k] = v
	}
	if x.auth.memberID != "" {
		out["member_id"] = x.auth.memberID
	}
	if x.auth.token != "" {
		out["key"] = x.auth.token
	}
	out["appcode"] = appcode
	return out
}
func (x *cxContext) headers() map[string]string {
	h := map[string]string{"content-type": "application/x-www-form-urlencoded", "accept": "application/json, text/plain, */*", "origin": originURL, "referer": refererURL}
	if x.auth.cookie != "" {
		h["cookie"] = x.auth.cookie
	}
	return h
}
func (x *cxContext) postJSON(api string, data map[string]string) (map[string]any, error) {
	body, err := x.c.PostForm(x.apiURL(api), x.apiData(data), x.headers())
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (x *cxContext) checkCookie() error {
	resp, err := x.postJSON(userInfoAPI, nil)
	if err != nil {
		return fmt.Errorf("caixuetang memberinfo: %w", err)
	}
	data := asMap(resp["data"])
	if x.auth.memberID == "" {
		x.auth.memberID = firstString(data, "id", "member_id", "memberId")
	}
	if isSuccess(resp) || len(data) > 0 {
		return nil
	}
	return fmt.Errorf("caixuetang memberinfo failed: code=%s status=%s", firstString(resp, "code", "errcode"), firstString(resp, "status"))
}

func (x *cxContext) findCourse(cid string) map[string]any {
	for _, item := range x.getCourseList() {
		if firstString(item, "course_id", "courseId", "id", "cid") == cid {
			return item
		}
	}
	return nil
}
func (x *cxContext) getCourseList() []map[string]any {
	jobs := []struct {
		api  string
		data map[string]string
		cat  string
	}{{mycourseAPI, map[string]string{"course_type_new": "1"}, "普通课"}, {mycourseAPI, map[string]string{"course_type_new": "14"}, "讲义课"}, {myvipcourseAPI, nil, "VIP课"}}
	seen, out := map[string]bool{}, []map[string]any{}
	for _, j := range jobs {
		resp, err := x.postJSON(j.api, j.data)
		if err != nil {
			continue
		}
		for _, item := range extractItems(resp["data"]) {
			id := firstString(item, "course_id", "courseId", "id", "cid", "goods_id", "obj_id", "product_id")
			title := firstString(item, "course_name", "courseName", "title", "name", "goods_name", "product_name", "chapter_name")
			if id == "" || title == "" || seen[id+":"+j.cat] {
				continue
			}
			seen[id+":"+j.cat] = true
			item["course_id"] = id
			item["title"] = title
			item["course_category"] = j.cat
			item["list_api"] = j.api
			out = append(out, item)
		}
	}
	return out
}

func (x *cxContext) getPlayinfo() (map[string]any, error) {
	params := map[string]string{"course_id": x.cid}
	if x.videoID != "" {
		params["video_id"] = x.videoID
	}
	if x.courseType != "" {
		params["course_type"] = x.courseType
	}
	tries := []struct {
		api  string
		data map[string]string
	}{{webplayInfoAPI, map[string]string{"course_id": x.cid}}, {playinfoAPI, params}}
	if x.videoID != "" {
		tries = append([]struct {
			api  string
			data map[string]string
		}{{webplayInfoAPI, params}}, tries...)
	}
	for _, t := range tries {
		resp, err := x.postJSON(t.api, t.data)
		if err != nil {
			continue
		}
		data := asMap(resp["data"])
		if len(data) > 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("caixuetang: empty playinfo for course %s", x.cid)
}

func titleFromPlayinfo(p map[string]any) string {
	for _, k := range []string{"course_info", "course", "play_video"} {
		if t := firstString(asMap(p[k]), "course_name", "courseName", "course_title", "title", "name"); t != "" {
			return t
		}
	}
	return firstString(p, "course_name", "courseName", "title", "name")
}

func (x *cxContext) buildEntries(playinfo map[string]any) []*extractor.MediaInfo {
	roots := findChapterRoots(playinfo)
	if len(roots) == 0 {
		roots = []map[string]any{playinfo}
	}
	seen := map[string]bool{}
	var entries []*extractor.MediaInfo
	for i, root := range roots {
		x.parseNode(root, []int{i + 1}, seen, &entries)
	}
	return entries
}

func (x *cxContext) parseNode(node map[string]any, index []int, seen map[string]bool, out *[]*extractor.MediaInfo) {
	children := iterChildren(node)
	if len(children) > 0 {
		for i, child := range children {
			x.parseNode(child, append(index, i+1), seen, out)
		}
		return
	}
	if looksVideoNode(node) {
		vi := parseVideoInfo(node, index, x.cid, x.courseType)
		if id := firstString(vi, "video_id", "direct_url"); id != "" && !seen["v:"+id] {
			if entry := x.videoEntry(vi); entry != nil {
				seen["v:"+id] = true
				*out = append(*out, entry)
			}
		}
	}
	if looksFileNode(node) {
		fi := parseFileInfo(node, index)
		if u := firstString(fi, "file_url", "item_id"); u != "" && !seen["f:"+u] {
			if entry := x.fileEntry(fi); entry != nil {
				seen["f:"+u] = true
				*out = append(*out, entry)
			}
		}
	}
}

func (x *cxContext) videoEntry(vi map[string]any) *extractor.MediaInfo {
	u := x.getVideoURL(vi)
	if u == "" {
		return nil
	}
	return &extractor.MediaInfo{Site: "caixuetang", Title: firstString(vi, "video_name"), Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{u}, Format: pickFormat(u), Headers: map[string]string{"Referer": refererURL, "cookie": x.auth.cookie}}}, Extra: map[string]any{"video_id": firstString(vi, "video_id")}}
}
func (x *cxContext) fileEntry(fi map[string]any) *extractor.MediaInfo {
	u := firstString(fi, "file_url")
	if u == "" {
		u = x.generatedDownloadURL(fi)
	}
	if u == "" {
		return nil
	}
	return &extractor.MediaInfo{Site: "caixuetang", Title: firstString(fi, "file_name"), Streams: map[string]extractor.Stream{"file": {Quality: "source", URLs: []string{u}, Format: firstNonEmpty(firstString(fi, "file_fmt"), fileExt(u), "bin"), Headers: map[string]string{"Referer": refererURL, "cookie": x.auth.cookie}}}, Extra: map[string]any{"type": "file", "item_id": firstString(fi, "item_id")}}
}

func (x *cxContext) getVideoURL(vi map[string]any) string {
	if u := firstString(vi, "direct_url"); strings.HasPrefix(u, "http") {
		return u
	}
	vid := firstString(vi, "video_id")
	if vid == "" {
		return ""
	}
	data := map[string]string{"video_id": vid, "course_id": firstNonEmpty(firstString(vi, "course_id"), x.cid)}
	if ct := firstNonEmpty(firstString(vi, "course_type"), x.courseType); ct != "" {
		data["course_type"] = ct
	}
	for _, api := range []string{webplayAPI, videoPlayAPI} {
		if u := x.getVideoURLFromAPI(api, data); u != "" {
			return u
		}
	}
	return ""
}
func (x *cxContext) getVideoURLFromAPI(api string, data map[string]string) string {
	resp, err := x.postJSON(api, data)
	if err != nil {
		return ""
	}
	return extractPlayURL(resp["data"])
}
func (x *cxContext) generatedDownloadURL(fi map[string]any) string {
	id := firstString(fi, "item_id", "download_id")
	if id == "" {
		return ""
	}
	resp, err := x.postJSON(downloadTaskAPI, map[string]string{"item_id": id})
	if err != nil {
		return ""
	}
	dl := firstNonEmpty(firstString(asMap(resp["data"]), "downloadId", "download_id"), id)
	info, err := x.postJSON(downloadInfoAPI, map[string]string{"downloadId": dl})
	if err != nil {
		return ""
	}
	data := asMap(info["data"])
	if firstString(data, "status") == "" || firstString(data, "status") == "1" {
		return firstString(data, "url", "download_url")
	}
	return ""
}
