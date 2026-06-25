// Package ledu implements an extractor for ledupeiyou.com courses.
//
// API endpoints from decompiled Mooc/Courses/Ledu/:
//
//	https://passport.vdyoo.com
//	https://app.ledupeiyou.com
//	https://classroom-api.ledupeiyou.com
//	https://classroom-api-online.saasp.vdyoo.com
//	https://course-api-online.saasp.vdyoo.com
//	https://cloudlearn.ledupeiyou.com
package ledu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	talHost               = "https://passport.vdyoo.com"
	appHost               = "https://app.ledupeiyou.com"
	apiHost               = "https://classroom-api.ledupeiyou.com"
	onlineAPIHost         = "https://classroom-api-online.saasp.vdyoo.com"
	courseAPIHost         = "https://course-api-online.saasp.vdyoo.com"
	cloudlearnHost        = "https://cloudlearn.ledupeiyou.com"
	h5StudyHost           = "https://app.ledupeiyou.com"
	userInfoPath          = "/backstage/user/tallogin/code"
	h5GetClassListPath    = "/backend-service/m/backend/study/getClassList"
	h5CurriculumListPath  = "/wx-aggregation/cs/backend-service/m/backend/study/getCurriculumList"
	h5LessonDetailPath    = "/wx-aggregation/cs/backend-service/m/backend/study/lessonDetail"
	h5CourseMaterialsPath = "/wx-aggregation/cs/backend-service/m/backend/study/queryCourseMaterials"
	getClassListPath      = "/backstage/xes/study/v1/classroom/getClassList"
	queryLessonsPath      = "/homepage/lessonDetailV0812/queryLessons"
	lessonDetailPath      = "/homepage/lessonDetailV0812/queryLessonDetail"
	courseMaterialsPath   = "/homepage/lessonDetail/queryCourseMaterialListV0303"
	handoutPDFPath        = "/homepage/lessonDetail/share/handout"
	videoInfoPath         = "/playback/v4/video/init?from=YUNXUEXI"
	recordResourcesPath   = "/classroom-ai/record/v3/resources"
	courseSubjectListPath = "/course/v1/student/course/subject-list"
	courseListPath        = "/course/v1/student/course/list"
	courseDetailListPath  = "/course/v1/student/course/user-live-list"
	classroomInitAuthPath = "/classroom/basic/v2/init/auth"
	classroomInitStuPath  = "/classroom/basic/v2/init/student"
	browserUA             = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
	leduReferer           = "https://app.ledupeiyou.com/"
)

var patterns = []string{`(?:[\w-]+\.)?ledupeiyou\.com/`, `classroom-api(?:-online)?\.(?:ledupeiyou|saasp\.vdyoo)\.com/`}

func init() {
	extractor.Register(&Ledu{}, extractor.SiteInfo{Name: "Ledu", URL: "ledupeiyou.com", NeedAuth: true})
}

type Ledu struct{}

func (s *Ledu) Patterns() []string { return patterns }

var classIDRe = regexp.MustCompile(`(?i)(?:classId|class_id|id)=([A-Za-z0-9_-]+)|/class(?:room)?/([A-Za-z0-9_-]+)`)

func (s *Ledu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("ledu requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	cookie := leduCookieString(opts.Cookies)
	studentID := firstText(cookieValue(cookie, "stuId"), cookieValue(cookie, "stuIdStr"), cookieValue(cookie, "user_id"), cookieValue(cookie, "uid"), cookieValue(cookie, "puid"), cookieValue(cookie, "pu_uid"))
	if studentID == "" {
		return nil, fmt.Errorf("ledu requires stuId/user_id cookie")
	}
	headers := leduHeaders(cookie, studentID, "", "", "", "", "")
	if _, err := leduGetJSON(c, courseAPIHost, courseSubjectListPath, map[string]string{"stuId": studentID}, headers); err != nil {
		return nil, fmt.Errorf("ledu validate pc cookie: %w", err)
	}

	cid := parseClassID(rawURL)
	classes := fetchClasses(c, headers, studentID)
	classInfo := chooseClass(classes, cid)
	if classInfo == nil && len(classes) > 0 {
		classInfo = classes[0]
	}
	if classInfo == nil {
		return nil, fmt.Errorf("ledu: no class found for %s", rawURL)
	}
	cid = firstText(classInfo["classId"], classInfo["id"], classInfo["class_id"], cid)
	title := firstText(classInfo["clientCourseName"], classInfo["clientClassName"], classInfo["className"], classInfo["courseName"], classInfo["name"], "ledu_"+cid)
	courseID := firstText(classInfo["pcStdCourseId"], classInfo["stdCourseId"], classInfo["stdCourseIdForDetail"], classInfo["courseId"], cid)
	grade := firstText(classInfo["gradeId"], classInfo["stdGrade"])

	details := fetchCourseDetails(c, headers, studentID, courseID)
	if len(details) == 0 {
		details = append(details, classInfo)
	}
	entries := buildEntries(c, details, leduHeaders(cookie, studentID, cid, courseID, grade, "", ""))
	if len(entries) == 0 {
		return nil, fmt.Errorf("ledu: no playable video/material entries for classId=%s", cid)
	}
	return &extractor.MediaInfo{Site: "ledu", Title: title, Entries: entries, Extra: map[string]any{"classId": cid, "stdCourseId": courseID, "stuId": studentID}}, nil
}

func leduHeaders(cookie, stuID, classID, courseID, grade, liveID, tutorID string) map[string]string {
	h := map[string]string{"Accept": "application/json, text/plain, */*", "User-Agent": browserUA, "Referer": leduReferer, "Origin": strings.TrimRight(leduReferer, "/"), "terminal": "pc", "version": "7.76.91", "branchId": "1111", "stuId": stuID, "stdClassId": classID, "stdCourseId": courseID, "stdGrade": grade, "liveId": liveID, "tutorId": tutorID, "reqTime": strconv.FormatInt(time.Now().UnixMilli(), 10), "lang": "ch", "businessType": "saasp"}
	if cookie != "" {
		h["Cookie"] = cookie
	}
	if tok := firstText(cookieValue(cookie, "token"), cookieValue(cookie, "hb_token"), cookieValue(cookie, "classroom_token")); tok != "" {
		h["token"] = tok
	}
	return h
}

func fetchClasses(c *util.Client, headers map[string]string, stuID string) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	for _, status := range []string{"1", "2", "3"} {
		for page := 1; page <= 3; page++ {
			payload, err := leduGetJSON(c, courseAPIHost, courseListPath, map[string]string{"order": "asc", "perPage": "50", "page": strconv.Itoa(page), "stdSubject": "", "courseStatus": status, "stuId": stuID}, headers)
			if err != nil {
				break
			}
			recs := extractRecords(extractPayload(payload))
			if len(recs) == 0 {
				break
			}
			for _, rec := range recs {
				id := firstText(rec["classId"], rec["id"], rec["class_id"], rec["stdClassId"])
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, rec)
			}
			if len(recs) < 50 {
				break
			}
		}
	}
	return out
}

func fetchCourseDetails(c *util.Client, headers map[string]string, stuID, courseID string) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	for _, typ := range []string{"1", "2", "3", "4"} {
		payload, err := leduGetJSON(c, courseAPIHost, courseDetailListPath, map[string]string{"order": orderForType(typ), "version": "", "perPage": "50", "page": "1", "needPage": "1", "type": typ, "stdCourseId": courseID, "stuId": stuID}, headers)
		if err != nil {
			continue
		}
		for _, rec := range extractRecords(extractPayload(payload)) {
			key := firstText(rec["liveId"], rec["taskId"], rec["noteId"], rec["paperId"], rec["coursewareId"], rec["liveName"]) + ":" + typ
			if key == ":" || seen[key] {
				continue
			}
			seen[key] = true
			rec["detailType"] = typ
			out = append(out, rec)
		}
	}
	return out
}

func buildEntries(c *util.Client, details []map[string]any, headers map[string]string) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	classID := headers["stdClassId"]
	stuID := headers["stuId"]
	puid := firstText(cookieValue(headers["Cookie"], "puid"), cookieValue(headers["Cookie"], "pu_uid"), cookieValue(headers["Cookie"], "uid"))

	for i, detail := range details {
		roots := []map[string]any{detail}
		liveID := firstText(detail["liveId"], detail["live_id"])

		// 1. classroomInitAuth -- critical precondition for video playback
		if liveID != "" {
			ctx := cloneHeaders(headers)
			ctx["liveId"] = liveID
			ctx["tutorId"] = firstText(detail["tutorId"], detail["tutor_id"])
			if authPayload, err := classroomInitAuth(c, ctx, liveID); err == nil {
				authToken, initData := initAuthTokens(authPayload)
				_ = authToken // token is set via response headers in real flow
				if initData != nil {
					roots = append(roots, nestedMaps(initData)...)
				}
			}
		}

		// 2. Video init (existing path)
		if liveID != "" {
			ctx := cloneHeaders(headers)
			ctx["liveId"] = liveID
			ctx["tutorId"] = firstText(detail["tutorId"], detail["tutor_id"])
			if payload, err := leduGetJSON(c, onlineAPIHost, videoInfoPath, nil, ctx); err == nil {
				roots = append(roots, nestedMaps(extractPayload(payload))...)
			}
		}

		// 3. Handout PDF (existing path)
		if paperID := firstText(detail["paperId"], detail["paper_id"]); paperID != "" {
			if payload, err := leduGetJSON(c, cloudlearnHost, handoutPDFPath, map[string]string{"paperId": paperID}, headers); err == nil {
				roots = append(roots, nestedMaps(extractPayload(payload))...)
			}
		}

		// 4. queryLessons + queryLessonDetail -- structured lesson info with video IDs
		curriculumID := firstText(detail["curriculumId"], detail["curriculum_id"])
		curriculumNo := firstText(detail["curriculumNo"], detail["curriculum_no"])
		registID := firstText(detail["registId"], detail["regist_id"])
		if classID != "" && (curriculumID != "" || liveID != "") {
			lessons := fetchQueryLessons(c, headers, classID, curriculumID, curriculumNo, registID, stuID, puid)
			for _, lesson := range lessons {
				roots = append(roots, lesson)
				// Extract scene objects from each lesson
				if scene, ok := lesson["sceneObject"].(map[string]any); ok {
					roots = append(roots, nestedMaps(scene)...)
				}
			}
			// Also fetch detailed lesson info
			if curriculumID != "" {
				if detailPayload := fetchLessonDetail(c, headers, classID, curriculumID, curriculumNo, registID, stuID, puid); detailPayload != nil {
					roots = append(roots, nestedMaps(detailPayload)...)
				}
			}
		}

		// 5. recordResources -- for recorded video URLs (encUrl/m3u8Url with encKey/encIv)
		resourceID := firstText(detail["resourceId"], detail["resource_id"], detail["cloudLearnVideoResourceId"])
		if resourceID != "" {
			if recPayload := fetchRecordResources(c, headers, resourceID); recPayload != nil {
				roots = append(roots, nestedMaps(recPayload)...)
			}
		}

		// 6. courseMaterials -- downloadable files (PDFs, docs, etc.)
		if classID != "" && (curriculumID != "" || liveID != "") {
			materials := fetchCourseMaterials(c, headers, classID, curriculumID, curriculumNo, registID, stuID, puid)
			for _, mat := range materials {
				murl := firstText(mat["itemUrl"], mat["fileUrl"], mat["url"], mat["downloadUrl"], mat["resourceUrl"], mat["attachmentUrl"])
				if murl == "" || seen[murl] {
					continue
				}
				if strings.HasPrefix(murl, "//") {
					murl = "https:" + murl
				}
				if !strings.HasPrefix(murl, "http") {
					continue
				}
				seen[murl] = true
				name := firstText(mat["itemName"], mat["name"], mat["title"], mat["fileName"], fmt.Sprintf("material_%03d", len(entries)+1))
				format := mediaFormat(murl, mat)
				stream := extractor.Stream{Quality: "best", URLs: []string{murl}, Format: format, Headers: map[string]string{"Referer": leduReferer}}
				extra := map[string]any{"type": "material"}
				if pid := firstText(mat["paperId"], mat["paper_id"]); pid != "" {
					extra["paperId"] = pid
				}
				entries = append(entries, &extractor.MediaInfo{Site: "ledu", Title: fmt.Sprintf("(%d.%d)--%s", i+1, len(entries)+1, name), Streams: map[string]extractor.Stream{"best": stream}, Extra: extra})
			}
		}

		// Collect video entries from all roots
		for _, node := range nestedMaps(roots) {
			murl := mediaURL(node)
			if murl == "" || seen[murl] {
				continue
			}
			seen[murl] = true
			name := firstText(node["video_name"], node["videoTitle"], node["video_title"], node["liveName"], node["taskName"], node["itemName"], node["title"], node["name"], fmt.Sprintf("item_%03d", len(entries)+1))
			format := mediaFormat(murl, node)
			stream := extractor.Stream{Quality: "best", URLs: []string{murl}, Format: format, Headers: map[string]string{"Referer": leduReferer}}
			if format == "m3u8" {
				stream.NeedMerge = true
			}
			extra := map[string]any{"source": firstText(node["liveId"], node["taskId"], node["paperId"], node["noteId"])}
			// Propagate encryption info if present
			if encKey := firstText(node["encKey"]); encKey != "" {
				extra["encKey"] = encKey
			}
			if encIv := firstText(node["encIv"]); encIv != "" {
				extra["encIv"] = encIv
			}
			entries = append(entries, &extractor.MediaInfo{Site: "ledu", Title: fmt.Sprintf("[%d.%d]--%s", i+1, len(entries)+1, name), Streams: map[string]extractor.Stream{"best": stream}, Extra: extra})
		}
	}
	return entries
}

func leduGetJSON(c *util.Client, host, path string, params map[string]string, headers map[string]string) (any, error) {
	u, err := url.Parse(host + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	body, err := c.GetString(u.String(), headers)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("ledu parse %s: %w", u.String(), err)
	}
	return payload, nil
}

// leduPostJSON sends a JSON POST request to host+path with the given body map.
func leduPostJSON(c *util.Client, host, path string, body map[string]any, headers map[string]string) (any, error) {
	u := host + path
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	h := map[string]string{"Content-Type": "application/json; charset=UTF-8"}
	for k, v := range headers {
		h[k] = v
	}
	resp, err := c.Post(u, bytes.NewReader(raw), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ledu POST %s: HTTP %d", u, resp.StatusCode)
	}
	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("ledu parse POST %s: %w", u, err)
	}
	return payload, nil
}

// ---------- structured API calls ----------

// classroomInitAuth is the critical precondition for video playback. It sends
// GET onlineAPIHost/classroom/basic/v2/init/auth?classroomMode=playback&resVer=1.1
// and returns the initData payload containing auth tokens, course/live context.
func classroomInitAuth(c *util.Client, headers map[string]string, liveID string) (any, error) {
	ctx := cloneHeaders(headers)
	if liveID != "" {
		ctx["liveId"] = liveID
	}
	return leduGetJSON(c, onlineAPIHost, classroomInitAuthPath, map[string]string{
		"classroomMode": "playback",
		"resVer":        "1.1",
	}, ctx)
}

// initAuthTokens extracts the token from classroomInitAuth response and returns
// updated headers with it. Also returns the initData map for context extraction.
func initAuthTokens(payload any) (token string, initData map[string]any) {
	m, ok := payload.(map[string]any)
	if !ok {
		return "", nil
	}
	// The response may have {"data": {"initData": {...}}} or {"initData": {...}}
	root := m
	if d, ok := m["data"].(map[string]any); ok {
		root = d
	}
	initData, _ = root["initData"].(map[string]any)
	if initData == nil {
		initData = root
	}
	// Extract token from response headers field or initData
	token = firstText(m["token"], root["token"])
	return token, initData
}

// fetchCourseMaterials calls POST cloudlearnHost/homepage/lessonDetail/queryCourseMaterialListV0303.
// Returns material items with itemUrl/fileUrl + itemName + paperId.
func fetchCourseMaterials(c *util.Client, headers map[string]string, classID, curriculumID, curriculumNo, registID, studentID, studentUID string) []map[string]any {
	body := map[string]any{
		"classId":      classID,
		"curriculumId": curriculumID,
		"curriculumNo": curriculumNo,
		"registId":     registID,
		"studentId":    studentID,
		"studentUid":   studentUID,
	}
	payload, err := leduPostJSON(c, cloudlearnHost, courseMaterialsPath, body, headers)
	if err != nil {
		return nil
	}
	return extractMaterialItems(extractPayload(payload))
}

// extractMaterialItems walks the response tree to find material entries that have
// a downloadable URL (itemUrl/fileUrl/url) and a name (itemName/paperId).
func extractMaterialItems(v any) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	for _, node := range nestedMaps(v) {
		murl := firstText(node["itemUrl"], node["fileUrl"], node["url"], node["downloadUrl"], node["resourceUrl"], node["attachmentUrl"])
		if murl == "" || !(strings.HasPrefix(murl, "http") || strings.HasPrefix(murl, "//")) {
			continue
		}
		name := firstText(node["itemName"], node["name"], node["title"], node["fileName"])
		pid := firstText(node["paperId"], node["paper_id"])
		if name == "" && pid == "" {
			continue
		}
		key := murl + "|" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, node)
	}
	return out
}

// fetchQueryLessons calls POST cloudlearnHost/homepage/lessonDetailV0812/queryLessons.
// Returns structured lesson list with video IDs and scene objects.
func fetchQueryLessons(c *util.Client, headers map[string]string, classID, curriculumID, curriculumNo, registID, studentID, studentUID string) []map[string]any {
	body := map[string]any{
		"classId":      classID,
		"curriculumId": curriculumID,
		"curriculumNo": curriculumNo,
		"registId":     registID,
		"registType":   "1",
		"lessonType":   "",
		"studentId":    studentID,
		"studentUid":   studentUID,
	}
	payload, err := leduPostJSON(c, cloudlearnHost, queryLessonsPath, body, headers)
	if err != nil {
		return nil
	}
	return extractLessonList(extractPayload(payload))
}

// fetchLessonDetail calls POST cloudlearnHost/homepage/lessonDetailV0812/queryLessonDetail.
// Returns detailed lesson info with scene objects containing video resources.
func fetchLessonDetail(c *util.Client, headers map[string]string, classID, curriculumID, curriculumNo, registID, studentID, studentUID string) any {
	body := map[string]any{
		"classId":        classID,
		"registClassId":  classID,
		"curriculumId":   curriculumID,
		"curriculumNo":   curriculumNo,
		"registId":       registID,
		"studentId":      studentID,
		"studentUid":     studentUID,
	}
	payload, err := leduPostJSON(c, cloudlearnHost, lessonDetailPath, body, headers)
	if err != nil {
		return nil
	}
	return extractPayload(payload)
}

// extractLessonList pulls lesson dicts from the queryLessons response.
func extractLessonList(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"lessonList", "lessons", "list", "curriculumList"} {
			if r := extractLessonList(m[k]); len(r) > 0 {
				return r
			}
		}
		// Single lesson object with sceneObject
		if m["sceneObject"] != nil || m["chapterId"] != nil || m["liveType"] != nil {
			return []map[string]any{m}
		}
	}
	return nil
}

// fetchRecordResources calls GET onlineAPIHost/classroom-ai/record/v3/resources
// to get recorded video URLs (encUrl/m3u8Url with encKey/encIv).
func fetchRecordResources(c *util.Client, headers map[string]string, resourceID string) any {
	params := map[string]string{}
	if resourceID != "" {
		params["cloudLearnVideoResourceId"] = resourceID
	}
	payload, err := leduGetJSON(c, onlineAPIHost, recordResourcesPath, params, headers)
	if err != nil {
		return nil
	}
	return extractPayload(payload)
}

func chooseClass(classes []map[string]any, cid string) map[string]any {
	if cid == "" {
		return nil
	}
	for _, c := range classes {
		if firstText(c["classId"], c["id"], c["class_id"], c["stdClassId"]) == cid {
			return c
		}
	}
	return nil
}

func parseClassID(raw string) string {
	if m := classIDRe.FindStringSubmatch(raw); len(m) > 0 {
		return firstText(m[1], m[2])
	}
	return ""
}

func orderForType(t string) string {
	if t == "2" || t == "4" {
		return "desc"
	}
	return "asc"
}

func mediaURL(m map[string]any) string {
	for _, k := range []string{"m3u8Url", "videoM3u8Url", "m3u8", "m3u8_url", "mp4Url", "trVideoUrl", "videoUrl", "encUrl", "fileUrl", "itemUrl", "downloadUrl", "resourceUrl", "attachmentUrl", "pdfUrl", "url", "src"} {
		if s := firstText(m[k]); s != "" && (strings.HasPrefix(s, "http") || strings.HasPrefix(s, "//")) {
			if strings.HasPrefix(s, "//") {
				s = "https:" + s
			}
			if looksMedia(s) || isMaterial(m) {
				return s
			}
		}
	}
	return ""
}

func looksMedia(s string) bool {
	ls := strings.ToLower(s)
	return strings.Contains(ls, ".m3u8") || strings.Contains(ls, ".mp4") || strings.Contains(ls, ".pdf") || strings.Contains(ls, ".ppt") || strings.Contains(ls, ".doc") || strings.Contains(ls, ".zip")
}

func isMaterial(m map[string]any) bool {
	return firstText(m["paperId"], m["paper_id"], m["noteId"], m["itemName"], m["fileName"]) != ""
}

func mediaFormat(s string, m map[string]any) string {
	ls := strings.ToLower(strings.SplitN(strings.SplitN(s, "?", 2)[0], "#", 2)[0])
	for _, ext := range []string{"m3u8", "mp4", "pdf", "pptx", "ppt", "docx", "doc", "zip", "rar"} {
		if strings.HasSuffix(ls, "."+ext) {
			return ext
		}
	}
	if ft := strings.TrimPrefix(strings.ToLower(firstText(m["fileType"], m["type"], m["contentType"])), "."); ft != "" {
		return ft
	}
	return "bin"
}

func extractPayload(v any) any {
	for {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		advanced := false
		for _, k := range []string{"data", "result", "content", "payload"} {
			if x, ok := m[k]; ok && x != nil {
				v, advanced = x, true
				break
			}
		}
		if !advanced {
			return m
		}
	}
}

func extractRecords(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, it := range x {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, k := range []string{"classInfo", "classInfos", "classList", "list", "rows", "records", "lessonList", "lessons", "curriculumList", "items"} {
			if r := extractRecords(x[k]); len(r) > 0 {
				return r
			}
		}
	}
	return nil
}

func nestedMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch y := x.(type) {
		case []map[string]any:
			for _, m := range y {
				walk(m)
			}
		case []any:
			for _, it := range y {
				walk(it)
			}
		case map[string]any:
			out = append(out, y)
			for _, it := range y {
				walk(it)
			}
		}
	}
	walk(v)
	return out
}

func cloneHeaders(h map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = v
	}
	return out
}

func leduCookieString(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	seen, parts := map[string]bool{}, []string{}
	for _, raw := range []string{appHost, apiHost, onlineAPIHost, courseAPIHost, cloudlearnHost, talHost, "https://stu.ledupeiyou.com"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			if !seen[ck.Name] {
				seen[ck.Name] = true
				parts = append(parts, ck.Name+"="+ck.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func cookieValue(cookie, name string) string {
	for _, p := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 && strings.EqualFold(kv[0], name) {
			return kv[1]
		}
	}
	return ""
}

func firstText(vals ...any) string {
	for _, v := range vals {
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
