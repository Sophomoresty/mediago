// Icve_Course – mooc-old.icve.com.cn / course.icve.com.cn / sso.icve.com.cn / user.icve.com.cn extraction.
//
// Source: Icve_Course.pyc.1shot.cdc.py and Icve_Course.pyc.1shot.das.
// API: sso/user/mooc-old/course/zjy2 login + course tree + resource resolution.
// Auth: requires ICVE cookie with token + UNTYXLCOOKIE (NeedAuth: true).
package icve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	courseURLDetail       = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_selectCourseDetails.action"
	courseURLCourseID     = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_getClassAndCourseIdByCode.action"
	courseURLCIDClass     = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_selectCourseId.action"
	courseURLMyCourses    = "https://mooc-old.icve.com.cn/patch/zhzj/studentMooc_selectMoocCourse.action"
	courseURLUserCourses  = "https://user.icve.com.cn/learning/u/userDefinedSql/getBySqlCode.json"
	courseURLLogin        = "https://mooc-old.icve.com.cn/learning/o/student/training/islogin.action"
	courseURLToken        = "https://mooc-old.icve.com.cn/patch/zhzj/teacherMooc_simulationLogin.action?token=%s&role=teacher&source=teacher"
	courseURLJoin         = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_addCourseFormStudent.action"
	courseURLDataCheck    = "https://mooc-old.icve.com.cn/patch/zhzj/dataCheck.action"
	courseURLInfos        = "https://course.icve.com.cn/learnspace/learn/learn/templateeight/courseware_index.action?params.courseId=%s"
	courseURLM3U8         = "https://course.icve.com.cn/learnspace/learn/learn/templateeight/content_video.action?params.courseId=%s&params.itemId=%s"
	courseURLResource     = "https://course.icve.com.cn/learnspace/learn/learnCourseItem/getItemResourceDownloadUrl.json"
	courseURLAccessToken  = "https://zjy2.icve.com.cn/prod-api/auth/passLogin?token=%s"
	courseURLCheckLogin   = "https://zjy2.icve.com.cn/prod-api/system/user/getInfo"
	courseURLZJY2Chapter  = "https://zjy2.icve.com.cn/prod-api/spoc/courseDesignMooc/getModuleList/%s"
	courseURLZJY2Topic    = "https://zjy2.icve.com.cn/prod-api/spoc/courseDesignMooc/getTopicList"
	courseURLZJY2Cell     = "https://zjy2.icve.com.cn/prod-api/spoc/courseDesignMooc/getCellList"
	courseURLOutline      = "https://mooc-old.icve.com.cn/patch/zhzj/portalMooc_getCourseOutline.action"
	courseURLYunpan       = "https://spoc-yunpan-sdk.icve.com.cn/api/downloadbyte?token=%s/%s&isView=true&metaId=%s"
	courseURLSSOUserInfo  = "https://sso.icve.com.cn/api/user/userInfo?token=%s"
	courseFileHostPrefix  = "https://file.icve.com.cn/"
	courseDefaultPageSize = "99"
	courseMaxCoursePages  = 999
)

// Source: Mooc_Config courses_re['Icve_Course']
var coursePatterns = []string{
	`\s*https?://course\.icve\.com\.cn/learnspace/learn/learn/templateeight/.*?course[Ii]d=(?P<cid>[-\w]+)`,
	`\s*https?://mooc-old\.icve\.com\.cn/cms/courseDetails/index\.htm\?cid=(?P<class_code>[-\w]+)`,
	`\s*https?://mooc-old\.icve\.com\.cn/cms/courseDetails/index\.htm\?class[Ii]d=(?P<class_id>[-\w]+)`,
	`\s*https?://mooc-old\.icve\.com\.cn`,
	`\s*https?://sso\.icve\.com\.cn(?:[/?#][^\s]*)?`,
	`\s*https?://user\.icve\.com\.cn(?:[/?#][^\s]*)?`,
}

var (
	courseCIDRe = regexp.MustCompile(
		`(?i)(?:course\.icve\.com\.cn/.*?course[Ii]d=([-\w]+))|(?:mooc-old\.icve\.com\.cn/cms/courseDetails/index\.htm\?(cid|class[Ii]d)=([-\w]+))`,
	)
	courseTokenRe        = regexp.MustCompile(`token:\'(.*?)\'`)
	courseSiteCodeRe     = regexp.MustCompile(`siteCode:\'(.*?)\'`)
	courseLoginIDRe      = regexp.MustCompile(`loginId:\'(.*?)\'`)
	courseTeacherRoleRe  = regexp.MustCompile(`roleType:\'TEACHER\'`)
	courseLoginIDTokenRe = regexp.MustCompile(`loginIdToken\s*=\s*\'(.*?)\'`)
	courseLoginIDVarRe   = regexp.MustCompile(`_LOGINID_\s*=\s*\'(.*?)\'`)
	courseOKCodeRe       = regexp.MustCompile(`"code"\s*:\s*200`)
	courseSSOOKMsgRe     = regexp.MustCompile(`"msg"\s*:\s*"ok"`)
	courseJoinOKRe       = regexp.MustCompile(`"errorCode"\s*:\s*"200"`)
	courseVideoQualRe    = map[string]*regexp.Regexp{
		"FHD": regexp.MustCompile(`"FHD"\s*:\s*"(.*?)"`),
		"HD":  regexp.MustCompile(`"HD"\s*:\s*"(.*?)"`),
		"SD":  regexp.MustCompile(`"SD"\s*:\s*"(.*?)"`),
		"LD":  regexp.MustCompile(`"LD"\s*:\s*"(.*?)"`),
	}
	courseResourceRe      = regexp.MustCompile(`resource\s*=\s*\'(?P<url>.*?)\'`)
	courseTitleFmtRe      = regexp.MustCompile(`<title>.*?(?P<fmt>\.[a-zA-Z]+[0-9]?)</title>`)
	courseOpenLearnItemRe = regexp.MustCompile(`openLearnResItem\('([-\w]+)'.*?\)`)
)

func init() {
	extractor.Register(&IcveCourse{}, extractor.SiteInfo{Name: "IcveCourse", URL: "mooc-old.icve.com.cn,sso.icve.com.cn,user.icve.com.cn", NeedAuth: true})
}

type IcveCourse struct{}

func (i *IcveCourse) Patterns() []string { return coursePatterns }

type courseCtx struct {
	c            *util.Client
	jar          http.CookieJar
	headers      map[string]string
	mode         int
	cid          string
	title        string
	token        string
	sessionToken string
	siteCode     string
	loginID      string
	accessToken  string
	logined      bool
	joined       bool
	purchased    bool
	subscribe    bool
	infos        courseInfoNode
	rootURL      string
}

type courseListItem struct {
	CourseID string
	Title    string
}

type courseResourceItem struct {
	ID          string
	Name        string
	Kind        string
	ResourceURL string
	MetaID      string
	MediaURL    string
	Fmt         string
	FileInfo    map[string]any
}

type courseInfoNode map[string]any

func (i *IcveCourse) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil {
		opts = &extractor.ExtractOpts{}
	}
	jar := opts.Cookies
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}

	resolved, err := resolveSmartEduURL(rawURL, jar)
	if err == nil && resolved != "" {
		rawURL = resolved
	}

	x := newCourseCtx(jar, modeFromQuality(opts.Quality))
	x.rootURL = rawURL
	if err := x.prepare(rawURL, true); err != nil {
		return nil, err
	}
	if opts.ListOnly {
		return x.buildMediaFromInfos()
	}
	return x.download()
}

func newCourseCtx(jar http.CookieJar, mode int) *courseCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Platform": `"Windows"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Google Chrome";v="115", "Chromium";v="115"`,
		"Referer":            referer,
		"cookie":             cookieHeader(jar, courseCookieOrigins()),
		"User-Agent":         util.RandomUA(),
	}
	return &courseCtx{c: c, jar: jar, headers: headers, mode: mode, infos: courseInfoNode{}}
}

func courseCookieOrigins() []string {
	return icveCookieOrigins()
}

func (x *courseCtx) refreshCookieHeader() {
	cookie := cookieHeader(x.jar, courseCookieOrigins())
	if strings.TrimSpace(cookie) != "" {
		x.headers["cookie"] = cookie
	}
}

func parseCourseCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := courseCIDRe.FindStringSubmatch(raw); len(m) >= 4 {
		if strings.TrimSpace(m[1]) != "" {
			return strings.TrimRight(strings.TrimSpace(m[1]), "_")
		}
		if strings.TrimSpace(m[3]) != "" {
			return strings.TrimSpace(m[3])
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, key := range []string{"params.courseId", "courseId", "courseid", "cid", "classId", "class_id"} {
		if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
			return v
		}
	}
	return ""
}

func parseCourseURLIDs(raw string) (cid, classCode, classID string, isMoocRoot bool) {
	raw = strings.TrimSpace(raw)
	if m := courseCIDRe.FindStringSubmatch(raw); len(m) >= 4 {
		if strings.TrimSpace(m[1]) != "" {
			cid = strings.TrimRight(strings.TrimSpace(m[1]), "_")
			return
		}
		if strings.EqualFold(m[2], "cid") {
			classCode = strings.TrimSpace(m[3])
		} else {
			classID = strings.TrimSpace(m[3])
		}
		return
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", false
	}
	cid = firstNonEmpty(u.Query().Get("params.courseId"), u.Query().Get("courseId"), u.Query().Get("courseid"))
	classCode = firstNonEmpty(u.Query().Get("cid"), u.Query().Get("classCode"))
	classID = firstNonEmpty(u.Query().Get("classId"), u.Query().Get("class_id"))
	if isCourseLoginRootHost(u.Hostname()) && cid == "" && classCode == "" && classID == "" {
		isMoocRoot = true
	}
	return
}

func isCourseLoginRootHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "mooc-old.icve.com.cn", "sso.icve.com.cn", "user.icve.com.cn":
		return true
	default:
		return false
	}
}

func (x *courseCtx) prepare(rawURL string, parseInfos bool) error {
	if cookie := strings.TrimSpace(x.headers["cookie"]); cookie != "" {
		_ = x.checkCookie("icve", cookie)
	}
	if err := x.getToken(); err != nil && x.token == "" {
		return err
	}
	if err := x.resolveCID(rawURL); err != nil {
		return err
	}
	if x.cid == "" {
		return fmt.Errorf("icve_course: cannot parse course id from URL")
	}
	if err := x.loadCourseInfo(); err != nil {
		return err
	}
	if parseInfos {
		if err := x.loadInfos(); err != nil {
			return err
		}
	}
	return nil
}

func (x *courseCtx) resolveCID(rawURL string) error {
	cid, classCode, classID, moocRoot := parseCourseURLIDs(rawURL)
	if cid != "" {
		x.cid = cid
	}
	if x.cid == "" && classCode != "" {
		x.cid = x.getCourseID(classCode)
	}
	if x.cid == "" && classID != "" {
		x.cid = x.getCourseIDByClass(classID)
	}
	if moocRoot && x.logined {
		courses := x.getCourseList()
		if len(courses) > 0 {
			x.cid = courses[0].CourseID
			if x.title == "" {
				x.title = cleanTitle(courses[0].Title)
			}
			x.purchased = true
		}
	}
	if x.cid == "" {
		return fmt.Errorf("icve_course: cannot parse course id from URL")
	}
	return nil
}

func (x *courseCtx) getZJY2AccessToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	body, err := x.c.GetString(fmt.Sprintf(courseURLAccessToken, url.QueryEscape(token)), x.headers)
	if err != nil {
		return ""
	}
	return str(mapAt(parseJSONMap(body), "data")["access_token"])
}

func (x *courseCtx) checkZJY2Cookie(courseName, cookie string) bool {
	if courseName != "icve" {
		return false
	}
	cookies := parseRawCookieHeader(cookie)
	access := strings.TrimSpace(cookies["Token"])
	if access == "" {
		access = x.getZJY2AccessToken(cookies["token"])
	}
	if access == "" {
		return false
	}
	x.accessToken = access
	withToken := mergeCookieHeaders(cookie, "Token="+access)
	auth := "Bearer " + access
	headers := cloneHeaders(x.headers)
	headers["cookie"] = withToken
	headers["authorization"] = auth
	body, err := x.c.GetString(courseURLCheckLogin, headers)
	if err != nil || !courseOKCodeRe.MatchString(body) {
		return false
	}
	x.headers["cookie"] = withToken
	x.headers["Authorization"] = auth
	return true
}

func (x *courseCtx) checkCookie(courseName, cookie string) bool {
	if courseName != "icve" {
		return false
	}
	cookies := parseRawCookieHeader(cookie)
	token := strings.TrimSpace(cookies["token"])
	if token == "" {
		return false
	}
	headers := cloneHeaders(x.headers)
	headers["cookie"] = cookie
	body, err := x.c.PostForm(fmt.Sprintf(courseURLSSOUserInfo, url.QueryEscape(token)), map[string]string{}, headers)
	if err != nil || !courseOKCodeRe.MatchString(body) || !courseSSOOKMsgRe.MatchString(body) {
		return false
	}
	x.logined = true
	x.headers["cookie"] = cookie
	_ = x.checkZJY2Cookie(courseName, cookie)
	return true
}

func (x *courseCtx) getToken() error {
	body, err := x.c.PostForm(courseURLLogin, map[string]string{}, x.headers)
	if err != nil {
		return fmt.Errorf("icve_course: get token: %w", err)
	}
	x.token = regexExtract(courseTokenRe.String(), body)
	x.siteCode = regexExtract(courseSiteCodeRe.String(), body)
	x.loginID = regexExtract(courseLoginIDRe.String(), body)
	if !courseTeacherRoleRe.MatchString(body) || x.token == "" {
		return nil
	}
	return x.simulationTeacherLogin()
}

func (x *courseCtx) simulationTeacherLogin() error {
	for attempt := 0; attempt < 2; attempt++ {
		body, cookies, err := x.getWithCookieCapture(fmt.Sprintf(courseURLToken, url.QueryEscape(x.token)), x.headers)
		if err != nil {
			if attempt == 1 {
				return fmt.Errorf("icve_course: simulation login: %w", err)
			}
			continue
		}
		loginToken := regexExtract(courseLoginIDTokenRe.String(), body)
		loginID := regexExtract(courseLoginIDVarRe.String(), body)
		cookieString := cookieMapHeader(cookies)
		if loginToken != "" && loginID != "" {
			x.token = loginToken
			x.loginID = loginID
			x.sessionToken = cookies["token"]
			if cookieString != "" {
				x.headers["cookie"] = cookieString
			}
			return nil
		}
		if cookieString != "" {
			x.headers["cookie"] = mergeCookieHeaders(x.headers["cookie"], cookieString)
		}
	}
	return nil
}

func (x *courseCtx) getWithCookieCapture(rawURL string, headers map[string]string) (string, map[string]string, error) {
	resp, err := x.c.Get(rawURL, headers)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	cookies := map[string]string{}
	for _, ck := range resp.Cookies() {
		if ck.Name != "" {
			cookies[ck.Name] = ck.Value
		}
	}
	x.refreshCookieHeader()
	for k, v := range parseRawCookieHeader(x.headers["cookie"]) {
		if _, ok := cookies[k]; !ok {
			cookies[k] = v
		}
	}
	return string(b), cookies, nil
}

func (x *courseCtx) getCourseID(classCode string) string {
	body, err := x.c.PostForm(courseURLCourseID, map[string]string{"classCode": classCode}, x.headers)
	if err != nil {
		return ""
	}
	return str(mapAt(parseJSONMap(body), "data")["courseId"])
}

func (x *courseCtx) getCourseIDByClass(classID string) string {
	body, err := x.c.PostForm(courseURLCIDClass, map[string]string{"classId": classID}, x.headers)
	if err != nil {
		return ""
	}
	return str(parseJSONMap(body)["data"])
}

func (x *courseCtx) joinCourse() bool {
	body, err := x.c.PostForm(courseURLJoin, map[string]string{"courseId": x.cid, "token": x.token}, x.headers)
	if err != nil {
		return false
	}
	x.joined = courseJoinOKRe.MatchString(body)
	return x.joined
}

func (x *courseCtx) getCourseList() []courseListItem {
	var out []courseListItem
	for _, row := range x.getMyCourses() {
		if len(row) >= 9 {
			courseID := str(row[6])
			if courseID != "" {
				out = append(out, courseListItem{CourseID: courseID, Title: fmt.Sprintf("%s_%s", str(row[0]), str(row[8]))})
			}
		}
	}
	for _, row := range x.getUserCourses() {
		courseID := str(row["ext9"])
		title := str(row["ext1"])
		if courseID != "" {
			out = append(out, courseListItem{CourseID: courseID, Title: title})
		}
	}
	return out
}

func (x *courseCtx) getMyCourses() [][]any {
	var out [][]any
	for page := 1; page <= courseMaxCoursePages; page++ {
		rows := x.getMyCoursesByPage(fmt.Sprintf("%d", page))
		if len(rows) == 0 {
			break
		}
		out = append(out, rows...)
	}
	return out
}

func (x *courseCtx) getMyCoursesByPage(page string) [][]any {
	body, err := x.c.PostForm(courseURLMyCourses, map[string]string{
		"token":      x.token,
		"curPage":    page,
		"pageSize":   courseDefaultPageSize,
		"selectType": "0",
	}, x.headers)
	if err != nil {
		return nil
	}
	root := parseJSONMap(body)
	return anyRows(root["data"])
}

func (x *courseCtx) getUserCourses() []map[string]any {
	body, err := x.c.PostForm(courseURLUserCourses, map[string]string{
		"page.searchItem.queryId": "getNewStuCourseInfoById",
		"page.searchItem.keyname": "",
		"page.curPage":            "1",
		"page.pageSize":           courseDefaultPageSize,
	}, x.headers)
	if err != nil {
		return nil
	}
	root := parseJSONMap(body)
	page := mapAt(root, "page")
	items := listAt(page, "items")
	if len(items) == 0 {
		return nil
	}
	return listAt(items[0], "info")
}

// loadCourseInfo gets course detail/title and late-bound course id.
// Source: Icve_Course._get_title via portalMooc_selectCourseDetails.action.
func (x *courseCtx) loadCourseInfo() error {
	if x.cid == "" {
		return fmt.Errorf("icve_course: missing course id")
	}
	if x.token == "" {
		return nil
	}
	_ = x.joinCourse()
	body, err := x.c.PostForm(courseURLDetail, map[string]string{"courseId": x.cid}, x.headers)
	if err != nil {
		return fmt.Errorf("icve_course: load course info: %w", err)
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")
	className := str(data["className"])
	schoolName := str(data["schoolName"])
	if className != "" || schoolName != "" {
		x.title = cleanTitle(fmt.Sprintf("%s_%s", className, schoolName))
	}
	courseData := anyRows(root["courseData"])
	startTime := str(data["startTime"])
	if startTime != "" && len(courseData) >= 2 && time.Now().Format("2006-01-02 15:04:05") < startTime {
		lastButOne := courseData[len(courseData)-2]
		if len(lastButOne) >= 2 {
			if nextID := str(lastButOne[1]); nextID != "" {
				x.cid = nextID
			}
		}
	}
	if x.title == "" {
		x.loadCourseOutlineTitle()
	}
	return nil
}

func (x *courseCtx) loadCourseOutlineTitle() {
	body, err := x.c.PostForm(courseURLOutline, map[string]string{"courseId": x.cid}, x.headers)
	if err != nil {
		return
	}
	data := parseJSONMap(body)
	courseName := firstNonEmpty(str(data["courseName"]), str(mapAt(data, "data")["courseName"]))
	if courseName != "" {
		x.title = cleanTitle(courseName)
	}
}

func (x *courseCtx) getLearnURL() string {
	body, err := x.c.PostForm(courseURLDataCheck, map[string]string{
		"courseId":  x.cid,
		"checkType": "1",
		"sign":      "0",
		"template":  "blue",
	}, x.headers)
	if err != nil {
		return ""
	}
	return str(parseJSONMap(body)["data"])
}

func (x *courseCtx) loadInfos() error {
	if x.cid == "" {
		return fmt.Errorf("icve_course: missing course id")
	}
	learnURL := x.getLearnURL()
	if learnURL == "" {
		return fmt.Errorf("icve_course: dataCheck returned empty learn url")
	}
	x.infos = courseInfoNode{}
	x.loadJSONOutlineInfos()
	x.loadCoursewareHTMLInfos()
	if len(x.infos) == 0 {
		x.loadZJY2Infos()
	}
	if len(x.infos) == 0 {
		return fmt.Errorf("icve_course: no courseware entries")
	}
	return nil
}

func (x *courseCtx) loadCourseTree() ([]courseItem, error) {
	if err := x.loadInfos(); err != nil {
		return nil, err
	}
	return x.flattenInfoItems(), nil
}

func (x *courseCtx) loadJSONOutlineInfos() {
	body, err := x.c.PostForm(courseURLOutline, map[string]string{"courseId": x.cid}, x.headers)
	if err != nil {
		return
	}
	root := parseJSONMap(body)
	data := listAt(root, "data")
	if len(data) == 0 {
		dataMap := mapAt(root, "data")
		data = firstMapList(dataMap, "chapterList", "chapters", "children", "childItem")
	}
	if len(data) == 0 {
		return
	}
	tree := courseInfoNode{}
	for chapterIdx, chapter := range data {
		chapterName := fmt.Sprintf("{%d}--%s", chapterIdx+1, cleanTitle(str(chapter["title"])))
		chapterNode := courseInfoNode{}
		for sectionIdx, section := range listAt(chapter, "childItem") {
			sectionName := fmt.Sprintf("{%d}--%s", sectionIdx+1, cleanTitle(str(section["title"])))
			sectionNode := courseInfoNode{}
			items := mapsFromAny(section["childItem"])
			if len(items) == 0 && (str(section["id"]) != "" || str(section["metaId"]) != "") {
				items = []map[string]any{section}
			}
			videoList, fileList := x.itemsFromOutlineCells(items, []int{chapterIdx + 1, sectionIdx + 1})
			if len(videoList) > 0 {
				sectionNode["video_list"] = videoList
			}
			if len(fileList) > 0 {
				sectionNode["file_list"] = fileList
			}
			if len(sectionNode) > 0 {
				chapterNode[sectionName] = sectionNode
			}
		}
		if len(chapterNode) > 0 {
			tree[chapterName] = chapterNode
		}
	}
	if len(tree) > 0 {
		x.infos = tree
		x.purchased = true
	}
}

func (x *courseCtx) itemsFromOutlineCells(cells []map[string]any, prefix []int) ([]courseResourceItem, []courseResourceItem) {
	var videoList []courseResourceItem
	var fileList []courseResourceItem
	videoCounter := 1
	fileCounter := 1
	for _, cell := range cells {
		id := firstNonEmpty(str(cell["id"]), str(cell["Id"]), str(cell["itemId"]))
		name := cleanTitle(firstNonEmpty(str(cell["title"]), str(cell["name"]), str(cell["cellName"])))
		resourceURL := str(cell["resource"])
		fileInfo := courseMapFromAny(cell["cloudFileInfo"])
		if len(fileInfo) == 0 {
			fileInfo = courseMapFromAny(cell["fileInfo"])
		}
		kind := strings.ToLower(firstNonEmpty(str(cell["type"]), str(cell["CellType"]), str(cell["cellType"]), str(cell["categoryName"])))
		if id == "" && str(cell["metaId"]) == "" {
			continue
		}
		if kind == "video" || kind == "视频" || isVideoType(pickExt(firstNonEmpty(str(cell["videoUrl"]), resourceURL))) {
			idxs := append(append([]int{}, prefix...), videoCounter)
			videoCounter++
			videoList = append(videoList, courseResourceItem{ID: id, Name: fmt.Sprintf("[%s]--%s", joinInts(idxs, "."), trimRStripMP4(name)), Kind: "video", ResourceURL: resourceURL, MetaID: str(cell["metaId"]), MediaURL: mediaURLFromAny(cell["videoUrl"], cell["url"], cell["downloadUrl"]), Fmt: normalizeDotExt(firstNonEmpty(str(cell["fmt"]), str(cell["suffix"]), pickExt(resourceURL))), FileInfo: fileInfo})
		} else {
			idxs := append(append([]int{}, prefix...), fileCounter)
			fileCounter++
			fileList = append(fileList, courseResourceItem{ID: id, Name: fmt.Sprintf("(%s)--%s", joinInts(idxs, "."), name), Kind: "file", ResourceURL: resourceURL, MetaID: str(cell["metaId"]), Fmt: normalizeDotExt(firstNonEmpty(str(cell["fmt"]), str(cell["suffix"]), pickExt(resourceURL))), FileInfo: fileInfo})
		}
	}
	return videoList, fileList
}

func (x *courseCtx) loadZJY2Infos() {
	if x.cid == "" {
		return
	}
	chapters, ok := x.postJSONMaps(fmt.Sprintf(courseURLZJY2Chapter, url.PathEscape(x.cid)), map[string]any{})
	if !ok || len(chapters) == 0 {
		return
	}
	tree := courseInfoNode{}
	for chapterIdx, chapter := range chapters {
		chapterName := fmt.Sprintf("{%d}--%s", chapterIdx+1, cleanTitle(str(chapter["moduleName"])))
		chapterNode := courseInfoNode{}
		moduleID := str(chapter["id"])
		if moduleID == "" {
			continue
		}
		topics, ok := x.postJSONMaps(courseURLZJY2Topic, map[string]any{"moduleId": moduleID, "courseOpenId": x.cid})
		if !ok {
			continue
		}
		for topicIdx, topic := range topics {
			topicName := fmt.Sprintf("{%d}--%s", topicIdx+1, cleanTitle(str(topic["topicName"])))
			topicNode := courseInfoNode{}
			topicID := firstNonEmpty(str(topic["topicId"]), str(topic["id"]))
			cells, ok := x.postJSONMaps(courseURLZJY2Cell, map[string]any{"moduleId": moduleID, "topicId": topicID, "courseOpenId": x.cid})
			if !ok {
				continue
			}
			videoList, fileList := x.zjy2ItemsFromCells(cells, []int{chapterIdx + 1, topicIdx + 1})
			if len(videoList) > 0 {
				topicNode["video_list"] = videoList
			}
			if len(fileList) > 0 {
				topicNode["file_list"] = fileList
			}
			if len(topicNode) > 0 {
				chapterNode[topicName] = topicNode
			}
		}
		if len(chapterNode) > 0 {
			tree[chapterName] = chapterNode
		}
	}
	if len(tree) > 0 {
		x.infos = tree
		x.purchased = true
	}
}

func (x *courseCtx) zjy2ItemsFromCells(cells []map[string]any, prefix []int) ([]courseResourceItem, []courseResourceItem) {
	var videoList []courseResourceItem
	var fileList []courseResourceItem
	videoCounter := 1
	fileCounter := 1
	var walk func([]map[string]any)
	walk = func(nodes []map[string]any) {
		for _, cell := range nodes {
			if children := listAt(cell, "childNodeList"); len(children) > 0 {
				walk(children)
				continue
			}
			id := firstNonEmpty(str(cell["id"]), str(cell["cellId"]), str(cell["resourceId"]))
			name := cleanTitle(firstNonEmpty(str(cell["cellName"]), str(cell["name"]), str(cell["categoryName"])))
			resourceURL := str(cell["resourceUrl"])
			kind := firstNonEmpty(str(cell["categoryName"]), str(cell["type"]), str(cell["fileType"]))
			if id == "" && resourceURL == "" {
				continue
			}
			if kind == "视频" || isVideoType(strings.ToLower(kind)) || isVideoType(pickExt(resourceURL)) {
				idxs := append(append([]int{}, prefix...), videoCounter)
				videoCounter++
				videoList = append(videoList, courseResourceItem{ID: id, Name: fmt.Sprintf("[%s]--%s", joinInts(idxs, "."), trimRStripMP4(name)), Kind: "video", ResourceURL: resourceURL})
			} else {
				idxs := append(append([]int{}, prefix...), fileCounter)
				fileCounter++
				fileList = append(fileList, courseResourceItem{ID: id, Name: fmt.Sprintf("(%s)--%s", joinInts(idxs, "."), name), Kind: "file", ResourceURL: resourceURL, Fmt: normalizeDotExt(pickExt(resourceURL))})
			}
		}
	}
	walk(cells)
	return videoList, fileList
}

func (x *courseCtx) postJSONMaps(rawURL string, payload map[string]any) ([]map[string]any, bool) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	headers := cloneHeaders(x.headers)
	headers["Content-Type"] = "application/json"
	resp, err := x.c.Post(rawURL, bytes.NewReader(b), headers)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	return parseJSONMapsPayload(string(body)), true
}

func (x *courseCtx) loadCoursewareHTMLInfos() {
	body, err := x.c.GetString(fmt.Sprintf(courseURLInfos, url.QueryEscape(x.cid)), x.headers)
	if err != nil || strings.TrimSpace(body) == "" {
		return
	}
	if root := parseJSONMap(body); len(root) > 0 {
		items := listAt(root, "data")
		if len(items) == 0 {
			items = listAt(root, "chapterList")
		}
		if len(items) > 0 {
			flat := x.flattenCourseItems(items, nil)
			if len(flat) > 0 {
				x.mergeFlatItems(flat)
				return
			}
		}
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return
	}
	learnLists := htmlFindAll(doc, func(n *html.Node) bool { return htmlNodeHasClass(n, "s_learnlist") })
	if len(learnLists) == 0 {
		return
	}
	tree := courseInfoNode{}
	for listIdx, learnList := range learnLists {
		chapters := htmlFindAll(learnList, func(n *html.Node) bool { return htmlNodeHasClass(n, "s_chapter") })
		sectionLists := htmlFindAll(learnList, func(n *html.Node) bool { return htmlNodeHasClass(n, "s_sectionlist") })
		maxGroups := len(chapters)
		if len(sectionLists) > maxGroups {
			maxGroups = len(sectionLists)
		}
		if maxGroups == 0 {
			points := htmlFindAll(learnList, htmlIsCoursePoint)
			videoList, fileList := x.innerInfosFromHTML(points, []int{listIdx + 1})
			node := courseInfoNode{}
			if len(videoList) > 0 {
				node["video_list"] = videoList
			}
			if len(fileList) > 0 {
				node["file_list"] = fileList
			}
			if len(node) > 0 {
				tree[fmt.Sprintf("{%d}--课程资源", len(tree)+1)] = node
			}
			continue
		}
		for chapterIdx := 0; chapterIdx < maxGroups; chapterIdx++ {
			chapterTitle := fmt.Sprintf("章节%d", chapterIdx+1)
			if chapterIdx < len(chapters) {
				chapterTitle = cleanTitle(firstNonEmpty(htmlAttr(chapters[chapterIdx], "title"), htmlNodeText(chapters[chapterIdx]), chapterTitle))
			}
			chapterName := fmt.Sprintf("{%d}--%s", chapterIdx+1, chapterTitle)
			chapterNode := courseInfoNode{}
			if chapterIdx < len(sectionLists) {
				x.mergeHTMLSections(chapterNode, sectionLists[chapterIdx], []int{chapterIdx + 1})
			}
			if len(chapterNode) == 0 && chapterIdx < len(chapters) {
				points := htmlFindAll(chapters[chapterIdx], htmlIsCoursePoint)
				videoList, fileList := x.innerInfosFromHTML(points, []int{chapterIdx + 1})
				if len(videoList) > 0 {
					chapterNode["video_list"] = videoList
				}
				if len(fileList) > 0 {
					chapterNode["file_list"] = fileList
				}
			}
			if len(chapterNode) > 0 {
				tree[chapterName] = chapterNode
			}
		}
	}
	if len(tree) > 0 {
		x.infos = tree
		x.purchased = true
	}
}

func (x *courseCtx) mergeHTMLSections(chapterNode courseInfoNode, sectionList *html.Node, prefix []int) {
	sections := htmlFindAll(sectionList, func(n *html.Node) bool {
		return htmlNodeHasClass(n, "s_section") || htmlNodeHasClass(n, "s_sectionwrap")
	})
	sectionEntries := 0
	for sectionIdx, section := range sections {
		points := htmlFindAll(section, htmlIsCoursePoint)
		if len(points) == 0 {
			continue
		}
		title := cleanTitle(firstNonEmpty(htmlAttr(section, "title"), htmlNodeText(section), fmt.Sprintf("小节%d", sectionIdx+1)))
		sectionNode := courseInfoNode{}
		videoList, fileList := x.innerInfosFromHTML(points, append(append([]int{}, prefix...), sectionIdx+1))
		if len(videoList) > 0 {
			sectionNode["video_list"] = videoList
		}
		if len(fileList) > 0 {
			sectionNode["file_list"] = fileList
		}
		if len(sectionNode) > 0 {
			chapterNode[fmt.Sprintf("{%d}--%s", sectionIdx+1, title)] = sectionNode
			sectionEntries++
		}
	}
	if sectionEntries > 0 {
		return
	}
	points := htmlFindAll(sectionList, htmlIsCoursePoint)
	videoList, fileList := x.innerInfosFromHTML(points, prefix)
	if len(videoList) > 0 {
		chapterNode["video_list"] = videoList
	}
	if len(fileList) > 0 {
		chapterNode["file_list"] = fileList
	}
}

func (x *courseCtx) mergeFlatItems(items []courseItem) {
	var videoList []courseResourceItem
	var fileList []courseResourceItem
	for _, item := range items {
		if item.Kind == "video" {
			videoList = append(videoList, courseResourceItem{ID: item.ItemID, Name: item.Name, Kind: "video", ResourceURL: item.ResourceURL, MetaID: item.MetaID, MediaURL: item.MediaURL, Fmt: item.Fmt, FileInfo: item.FileInfo})
		} else {
			fileList = append(fileList, courseResourceItem{ID: item.ItemID, Name: item.Name, Kind: "file", ResourceURL: item.ResourceURL, MetaID: item.MetaID, MediaURL: item.MediaURL, Fmt: item.Fmt, FileInfo: item.FileInfo})
		}
	}
	if len(videoList) > 0 {
		x.infos["video_list"] = videoList
	}
	if len(fileList) > 0 {
		x.infos["file_list"] = fileList
	}
}

func (x *courseCtx) innerInfosFromHTML(nodes []*html.Node, indexTup []int) ([]courseResourceItem, []courseResourceItem) {
	var videoList []courseResourceItem
	var fileList []courseResourceItem
	videoCounter := 1
	fileCounter := 1
	for _, node := range nodes {
		itemType := firstNonEmpty(htmlAttr(node, "itemtype"), htmlFindAttr(node, "itemtype"))
		if itemType == "" {
			continue
		}
		titleNode := htmlFindFirst(node, func(n *html.Node) bool { return htmlNodeHasClass(n, "s_pointti") })
		name := cleanTitle(firstNonEmpty(htmlAttr(titleNode, "title"), htmlNodeText(titleNode), htmlAttr(node, "title"), htmlNodeText(node)))
		onclick := firstNonEmpty(htmlAttr(node, "onclick"), htmlFindMatchingAttr(node, "onclick", courseOpenLearnItemRe))
		itemID := regexExtract(courseOpenLearnItemRe.String(), onclick)
		if itemID == "" {
			continue
		}
		if strings.EqualFold(itemType, "video") || strings.Contains(itemType, "视频") {
			idxs := append(append([]int{}, indexTup...), videoCounter)
			videoCounter++
			videoList = append(videoList, courseResourceItem{ID: itemID, Name: fmt.Sprintf("[%s]--%s", joinInts(idxs, "."), trimRStripMP4(name)), Kind: "video"})
		} else {
			idxs := append(append([]int{}, indexTup...), fileCounter)
			fileCounter++
			fileList = append(fileList, courseResourceItem{ID: itemID, Name: fmt.Sprintf("(%s)--%s", joinInts(idxs, "."), name), Kind: "file"})
		}
	}
	return videoList, fileList
}

type courseItem struct {
	Name        string
	ItemID      string
	Kind        string // "video" or "file"
	ResourceURL string
	MetaID      string
	MediaURL    string
	Fmt         string
	FileInfo    map[string]any
}

func (x *courseCtx) flattenCourseItems(nodes []map[string]any, prefix []int) []courseItem {
	var out []courseItem
	videoCounter := 1
	fileCounter := 1
	for idx, node := range nodes {
		pos := idx + 1
		nextPrefix := append(append([]int{}, prefix...), pos)
		name := cleanTitle(firstNonEmpty(str(node["Title"]), str(node["title"]), str(node["name"]), str(node["cellName"]), str(node["categoryName"])))

		for _, childKey := range []string{"chapters", "knowleges", "cells", "children", "childItem", "childNodeList"} {
			children := listAt(node, childKey)
			if len(children) > 0 {
				out = append(out, x.flattenCourseItems(children, nextPrefix)...)
			}
		}

		itemID := firstNonEmpty(str(node["Id"]), str(node["id"]), str(node["itemId"]), str(node["cellId"]), str(node["resourceId"]))
		metaID := str(node["metaId"])
		if itemID == "" && metaID == "" {
			continue
		}
		cellType := strings.ToLower(firstNonEmpty(str(node["CellType"]), str(node["cellType"]), str(node["fileType"]), str(node["type"]), str(node["categoryName"])))
		resourceURL := firstNonEmpty(str(node["resourceUrl"]), str(node["resource"]), str(node["url"]), str(node["downloadUrl"]))
		fileInfo := courseMapFromAny(node["cloudFileInfo"])
		if len(fileInfo) == 0 {
			fileInfo = courseMapFromAny(node["fileInfo"])
		}
		switch {
		case cellType == "video" || cellType == "视频" || isVideoType(cellType) || isVideoType(pickExt(resourceURL)):
			idxs := append(append([]int{}, prefix...), videoCounter)
			videoCounter++
			out = append(out, courseItem{Name: fmt.Sprintf("[%s]--%s", joinInts(idxs, "."), trimRStripMP4(name)), ItemID: itemID, Kind: "video", ResourceURL: resourceURL, MetaID: metaID, MediaURL: mediaURLFromAny(node["videoUrl"], node["url"], node["downloadUrl"]), Fmt: normalizeDotExt(firstNonEmpty(str(node["fmt"]), str(node["suffix"]), pickExt(resourceURL))), FileInfo: fileInfo})
		case cellType != "" || resourceURL != "" || metaID != "":
			idxs := append(append([]int{}, prefix...), fileCounter)
			fileCounter++
			out = append(out, courseItem{Name: fmt.Sprintf("(%s)--%s", joinInts(idxs, "."), name), ItemID: itemID, Kind: "file", ResourceURL: resourceURL, MetaID: metaID, MediaURL: mediaURLFromAny(node["fileUrl"], node["url"], node["downloadUrl"]), Fmt: normalizeDotExt(firstNonEmpty(str(node["fmt"]), str(node["suffix"]), pickExt(resourceURL))), FileInfo: fileInfo})
		}
	}
	return out
}

func (x *courseCtx) getResourceURL(itemID string) string {
	u, _ := x.getFileURL(itemID, "")
	return u
}

func (x *courseCtx) getFileURL(fileID, resourceURL string) (string, string) {
	body, err := x.c.PostForm(courseURLResource, map[string]string{"params.itemId": fileID}, x.headers)
	if err != nil {
		return fallbackResourceURL(resourceURL), normalizeDotExt(pickExt(resourceURL))
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")
	downloadURL := firstNonEmpty(str(data["downloadUrl"]), str(data["downloadurl"]), str(data["url"]), str(root["downloadUrl"]), str(root["url"]))
	if downloadURL == "null" {
		downloadURL = ""
	}
	if downloadURL == "" {
		downloadURL = fallbackResourceURL(resourceURL)
	}
	fmtExt := normalizeDotExt(pickExt(downloadURL))
	if fmtExt == "." {
		fmtExt = normalizeDotExt(pickExt(resourceURL))
	}
	return strings.TrimSpace(downloadURL), fmtExt
}

func fallbackResourceURL(resourceURL string) string {
	resourceURL = strings.TrimSpace(resourceURL)
	if resourceURL == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(resourceURL), "http") {
		return resourceURL
	}
	return courseFileHostPrefix + strings.TrimLeft(resourceURL, "/")
}

func (x *courseCtx) getVideoM3U8(itemID string) string {
	u, _ := x.getVideoURL(itemID, "")
	return u
}

func (x *courseCtx) getVideoURL(videoID, resourceURL string) (string, string) {
	body, err := x.c.GetString(fmt.Sprintf(courseURLM3U8, url.QueryEscape(x.cid), url.QueryEscape(videoID)), x.headers)
	if err != nil {
		return x.getFileURL(videoID, resourceURL)
	}
	quality := map[string]string{}
	for q, re := range courseVideoQualRe {
		quality[q] = regexExtract(re.String(), body)
	}
	resource := regexExtract(courseResourceRe.String(), body)
	selected := ""
	if x.mode == IS_HD {
		selected = firstNonEmpty(quality["FHD"], quality["HD"], quality["SD"], quality["LD"], resource)
	} else {
		selected = firstNonEmpty(quality["LD"], quality["SD"], quality["HD"], quality["FHD"], resource)
	}
	fmtExt := ".mp4"
	lower := strings.ToLower(selected)
	if !strings.Contains(lower, ".mp4") && !strings.Contains(lower, ".m3u8") {
		if m := courseTitleFmtRe.FindStringSubmatch(body); len(m) >= 2 && strings.Contains(selected, m[1]) {
			fmtExt = normalizeDotExt(m[1])
		} else if ext := normalizeDotExt(pickExt(resource)); ext != "." {
			fmtExt = ext
		}
	} else if strings.Contains(lower, ".m3u8") {
		fmtExt = ".m3u8"
	}
	if selected == "" {
		return x.getFileURL(videoID, resourceURL)
	}
	return selected, fmtExt
}

func (x *courseCtx) getYunpanFileURL(metaID string, fileInfo map[string]any) string {
	if strings.TrimSpace(metaID) == "" || len(fileInfo) == 0 {
		return ""
	}
	userName := str(fileInfo["cloudUserName"])
	siteCode := str(fileInfo["cloudSiteCode"])
	if userName == "" || siteCode == "" {
		return ""
	}
	return fmt.Sprintf(courseURLYunpan, url.QueryEscape(userName), url.QueryEscape(siteCode), url.QueryEscape(metaID))
}

func (x *courseCtx) download() (*extractor.MediaInfo, error) {
	if x.token == "" || x.title == "" || len(x.infos) == 0 {
		return nil, fmt.Errorf("icve_course: missing token/title/infos")
	}
	return x.downloadCourse()
}

func (x *courseCtx) downloadCourse() (*extractor.MediaInfo, error) {
	return x.buildMediaFromInfos()
}

func (x *courseCtx) buildMedia(items []courseItem) (*extractor.MediaInfo, error) {
	x.infos = courseInfoNode{}
	x.mergeFlatItems(items)
	return x.buildMediaFromInfos()
}

func (x *courseCtx) buildMediaFromInfos() (*extractor.MediaInfo, error) {
	entries := x.entriesFromInfoNode(x.infos)
	if len(entries) == 0 {
		return nil, fmt.Errorf("icve_course: no playable entries")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "icve", Title: firstNonEmpty(x.title, x.cid, "icve_course"), Entries: entries, Extra: map[string]any{"course_id": x.cid, "module": "course", "purchased": x.purchased}}, nil
}

func (x *courseCtx) entriesFromInfoNode(node courseInfoNode) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	if videos := courseResourceItems(node["video_list"]); len(videos) > 0 {
		entries = append(entries, x.downloadVideoList(videos)...)
	}
	if files := courseResourceItems(node["file_list"]); len(files) > 0 {
		entries = append(entries, x.downloadFileList(files)...)
	}
	for _, key := range sortedNodeKeys(node) {
		if key == "video_list" || key == "file_list" {
			continue
		}
		child, ok := node[key].(courseInfoNode)
		if !ok {
			if m, ok := node[key].(map[string]any); ok {
				child = courseInfoNode(m)
			}
		}
		if len(child) == 0 {
			continue
		}
		childEntries := x.entriesFromInfoNode(child)
		if len(childEntries) == 0 {
			continue
		}
		entries = append(entries, &extractor.MediaInfo{Site: "icve", Title: cleanTitle(key), Entries: childEntries, Extra: map[string]any{"module": "course_section"}})
	}
	return entries
}

func (x *courseCtx) downloadVideoList(videoList []courseResourceItem) []*extractor.MediaInfo {
	if x.mode == ONLY_PDF {
		return nil
	}
	var entries []*extractor.MediaInfo
	for _, item := range videoList {
		u := item.MediaURL
		fmtExt := normalizeDotExt(item.Fmt)
		if item.MetaID != "" && len(item.FileInfo) > 0 && (u == "" || !strings.HasPrefix(strings.ToLower(u), "http")) {
			u = x.getYunpanFileURL(item.MetaID, item.FileInfo)
		}
		if u == "" || !strings.HasPrefix(strings.ToLower(u), "http") {
			u, fmtExt = x.getVideoURL(item.ID, item.ResourceURL)
		}
		if u == "" {
			continue
		}
		if fmtExt == "." {
			fmtExt = normalizeDotExt(firstNonEmpty(pickExt(u), "mp4"))
		}
		entries = append(entries, x.mediaEntry(item.Name, item.Kind, strings.TrimPrefix(fmtExt, "."), u))
	}
	return entries
}

func (x *courseCtx) downloadFileList(fileList []courseResourceItem) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	for _, item := range fileList {
		u := ""
		fmtExt := normalizeDotExt(item.Fmt)
		if item.MetaID != "" && len(item.FileInfo) > 0 {
			u = x.getYunpanFileURL(item.MetaID, item.FileInfo)
		}
		if u == "" {
			u, fmtExt = x.getFileURL(item.ID, item.ResourceURL)
		}
		if u == "" {
			continue
		}
		if fmtExt == "." {
			fmtExt = normalizeDotExt(firstNonEmpty(pickExt(u), "html"))
		}
		entry := x.downloadFile(u, fmtExt, item.Name)
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (x *courseCtx) downloadFile(fileURL, fileFmt, fileName string) *extractor.MediaInfo {
	fileURLLower := strings.ToLower(firstNonEmpty(fileURL, ""))
	fileFmt = normalizeDotExt(fileFmt)
	if fileFmt == "." {
		fileFmt = normalizeDotExt(pickExt(fileURL))
	}
	if fileFmt == "." {
		fileFmt = ".html"
	}
	ext := strings.TrimPrefix(fileFmt, ".")
	isVideo := false
	for _, videoExt := range []string{".mp4", ".m3u8", ".flv", ".mpg", ".avi", ".mov"} {
		if fileFmt == videoExt || strings.Contains(fileURLLower, videoExt) {
			isVideo = true
			break
		}
	}
	if isVideo && x.mode == ONLY_PDF {
		return nil
	}
	if !isVideo {
		switch {
		case fileFmt == ".pdf", strings.Contains(fileURLLower, ".pdf"):
			ext = "pdf"
		case strings.Contains(fileFmt, ".ppt"), strings.Contains(fileURLLower, ".ppt"):
			ext = strings.TrimPrefix(fileFmt, ".")
		case strings.Contains(fileFmt, ".doc"), strings.Contains(fileURLLower, ".doc"):
			ext = strings.TrimPrefix(fileFmt, ".")
		}
	}
	return x.mediaEntry(fileName, map[bool]string{true: "video", false: "file"}[isVideo], ext, fileURL)
}

func (x *courseCtx) mediaEntry(name, kind, ext, mediaURL string) *extractor.MediaInfo {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		ext = "html"
	}
	headers := cloneHeaders(x.headers)
	if strings.TrimSpace(headers["cookie"]) == "" {
		x.refreshCookieHeader()
		headers = cloneHeaders(x.headers)
	}
	return &extractor.MediaInfo{Site: "icve", Title: cleanTitle(name), Streams: map[string]extractor.Stream{ext: {Quality: ext, URLs: []string{mediaURL}, Format: ext, NeedMerge: ext == "m3u8", Headers: headers}}, Extra: map[string]any{"kind": kind, "module": "course"}}
}

func (x *courseCtx) flattenInfoItems() []courseItem {
	var out []courseItem
	var walk func(courseInfoNode)
	walk = func(node courseInfoNode) {
		for _, v := range courseResourceItems(node["video_list"]) {
			out = append(out, courseItem{Name: v.Name, ItemID: v.ID, Kind: "video", ResourceURL: v.ResourceURL, MetaID: v.MetaID, MediaURL: v.MediaURL, Fmt: v.Fmt, FileInfo: v.FileInfo})
		}
		for _, v := range courseResourceItems(node["file_list"]) {
			out = append(out, courseItem{Name: v.Name, ItemID: v.ID, Kind: "file", ResourceURL: v.ResourceURL, MetaID: v.MetaID, MediaURL: v.MediaURL, Fmt: v.Fmt, FileInfo: v.FileInfo})
		}
		for _, key := range sortedNodeKeys(node) {
			if key == "video_list" || key == "file_list" {
				continue
			}
			if child, ok := node[key].(courseInfoNode); ok {
				walk(child)
			} else if child, ok := node[key].(map[string]any); ok {
				walk(courseInfoNode(child))
			}
		}
	}
	walk(x.infos)
	return out
}

func courseResourceItems(v any) []courseResourceItem {
	switch t := v.(type) {
	case []courseResourceItem:
		return t
	case []any:
		out := make([]courseResourceItem, 0, len(t))
		for _, item := range t {
			if ri, ok := item.(courseResourceItem); ok {
				out = append(out, ri)
			}
		}
		return out
	default:
		return nil
	}
}

func sortedNodeKeys(node courseInfoNode) []string {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseRawCookieHeader(cookie string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, "=") {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return out
}

func mergeCookieHeaders(base, extra string) string {
	merged := parseRawCookieHeader(base)
	for k, v := range parseRawCookieHeader(extra) {
		merged[k] = v
	}
	return cookieMapHeader(merged)
}

func cookieMapHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}
	keys := make([]string, 0, len(cookies))
	for k := range cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+cookies[k])
	}
	return strings.Join(parts, ";")
}

func anyRows(v any) [][]any {
	switch t := v.(type) {
	case []any:
		out := make([][]any, 0, len(t))
		for _, item := range t {
			switch row := item.(type) {
			case []any:
				out = append(out, row)
			case []map[string]any:
				converted := make([]any, len(row))
				for i, v := range row {
					converted[i] = v
				}
				out = append(out, converted)
			}
		}
		return out
	default:
		return nil
	}
}

func parseJSONMapsPayload(text string) []map[string]any {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "[") {
		var arr []any
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&arr); err == nil {
			return mapsFromAny(arr)
		}
	}
	root := parseJSONMap(text)
	for _, key := range []string{"data", "rows", "list", "items"} {
		if arr := listAt(root, key); len(arr) > 0 {
			return arr
		}
	}
	return nil
}

func firstMapList(m map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		if rows := listAt(m, key); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func normalizeDotExt(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	if ext == "" {
		return "."
	}
	if strings.Contains(ext, "?") {
		ext = strings.SplitN(ext, "?", 2)[0]
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func mediaURLFromAny(values ...any) string {
	for _, value := range values {
		if u := mediaURLString(value); u != "" {
			return u
		}
	}
	return ""
}

func mediaURLString(value any) string {
	switch v := value.(type) {
	case string:
		s := strings.TrimSpace(v)
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			if parsed := mediaURLStringFromJSON(s); parsed != "" {
				return parsed
			}
		}
		return s
	case map[string]any:
		for _, key := range []string{"FHD", "HD", "SD", "LD", "url", "downloadUrl", "downloadurl", "ossOriUrl", "ossGenUrl", "fileUrl"} {
			if s := mediaURLString(v[key]); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range v {
			if s := mediaURLString(item); s != "" {
				return s
			}
		}
	}
	return ""
}

func mediaURLStringFromJSON(text string) string {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return ""
	}
	return mediaURLString(value)
}

func courseMapFromAny(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case string:
		return parseJSONMap(v)
	default:
		return map[string]any{}
	}
}

func htmlAttr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func htmlNodeHasClass(n *html.Node, class string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, c := range strings.Fields(htmlAttr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func htmlIsCoursePoint(n *html.Node) bool {
	return htmlNodeHasClass(n, "s_point") || htmlNodeHasClass(n, "s_pointwrap") || htmlNodeHasClass(n, "s_point_hassub")
}

func htmlFindAttr(n *html.Node, key string) string {
	found := htmlFindFirst(n, func(cur *html.Node) bool {
		return htmlAttr(cur, key) != ""
	})
	return htmlAttr(found, key)
}

func htmlFindMatchingAttr(n *html.Node, key string, re *regexp.Regexp) string {
	found := htmlFindFirst(n, func(cur *html.Node) bool {
		return re != nil && re.MatchString(htmlAttr(cur, key))
	})
	return htmlAttr(found, key)
}

func htmlFindFirst(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	if pred(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if got := htmlFindFirst(child, pred); got != nil {
			return got
		}
	}
	return nil
}

func htmlFindAll(n *html.Node, pred func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur == nil {
			return
		}
		if pred(cur) {
			out = append(out, cur)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return out
}

func htmlNodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur == nil {
			return
		}
		if cur.Type == html.TextNode {
			b.WriteString(cur.Data)
			b.WriteByte(' ')
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return cleanTitle(b.String())
}
