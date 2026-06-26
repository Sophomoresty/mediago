package classin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

// classinEnvelope is the standard ClassIn JSON response wrapper. data is left as
// RawMessage because its shape varies per endpoint (course_list, unit list,
// activity list, homework, file download info).
type classinEnvelope struct {
	ErrorInfo struct {
		Errno int    `json:"errno"`
		Msg   string `json:"msg"`
	} `json:"error_info"`
	Data json.RawMessage `json:"data"`
}

func (e classinEnvelope) ok() bool { return e.ErrorInfo.Errno == 1 }

type courseItem struct {
	CourseID   string `json:"courseId"`
	CourseName string `json:"courseName"`
	SchoolUID  string `json:"schoolUid"`
}

type categoryItem struct {
	CategoryID   string `json:"categoryId"`
	Name         string `json:"name"`
	CategoryName string `json:"categoryName"`
	Title        string `json:"title"`
}

type unitItem struct {
	UnitID   string `json:"unitId"`
	UnitName string `json:"unitName"`
	Name     string `json:"name"`
	Title    string `json:"title"`
}

type activityItem struct {
	ActivityID   string `json:"activityId"`
	BizID        string `json:"bizId"`
	ClassID      string `json:"classId"`
	Type         int    `json:"type"`
	ActivityName string `json:"activityName"`
	Name         string `json:"name"`
	Title        string `json:"title"`
}

func (a activityItem) name() string {
	return firstNonEmpty(a.ActivityName, a.Name, a.Title, "未命名课时")
}

// extractCourseTree walks the full ClassIn course structure and returns a
// playlist MediaInfo whose Entries mirror the category/unit/activity hierarchy.
//
// Chain (host t0d-cdn.eeo.cn):
//
//	course_list -> category/list -> studentUnitList -> studentUnitActivityList
//	  type 4/5 (video) -> recordClass/get or getLessonRecordInfo -> m3u8 token
//	  type 1 (homework) -> homework/get -> file/getDownInfo
func (ci *Classin) extractCourseTree(c *util.Client, in ids, headers map[string]string) (*extractor.MediaInfo, error) {
	courses := listCourses(c)
	if len(courses) == 0 {
		return nil, fmt.Errorf("classin: course_list returned no courses")
	}

	// When the URL pins a specific course, only traverse that one; otherwise
	// emit every course the member can see.
	var selected []courseItem
	for _, course := range courses {
		if in.CourseID == "" || course.CourseID == in.CourseID {
			selected = append(selected, course)
		}
	}
	if len(selected) == 0 {
		selected = courses
	}

	var entries []*extractor.MediaInfo
	for _, course := range selected {
		sid := firstNonEmpty(course.SchoolUID, in.SID)
		node := ci.buildCourseEntry(c, sid, course, headers)
		if node != nil {
			entries = append(entries, node)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("classin: course tree produced no downloadable media")
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "classin", Title: "ClassIn课程", Entries: entries}, nil
}

func (ci *Classin) buildCourseEntry(c *util.Client, sid string, course courseItem, headers map[string]string) *extractor.MediaInfo {
	title := util.SanitizeFilename(firstNonEmpty(course.CourseName, "ClassIn课程"))
	categories := listCategories(c, sid, course.CourseID)

	var children []*extractor.MediaInfo
	if len(categories) == 0 {
		// No category layer: enumerate units with an empty categoryId.
		children = append(children, ci.buildUnitEntries(c, sid, course.CourseID, "", headers)...)
	} else {
		for _, cat := range categories {
			catName := firstNonEmpty(cat.Name, cat.CategoryName, cat.Title)
			units := ci.buildUnitEntries(c, sid, course.CourseID, cat.CategoryID, headers)
			if len(units) == 0 {
				continue
			}
			if isDefaultCategoryName(catName) {
				children = append(children, units...)
				continue
			}
			children = append(children, &extractor.MediaInfo{
				Site:    "classin",
				Title:   util.SanitizeFilename(firstNonEmpty(catName, "未命名章节")),
				Entries: units,
			})
		}
	}
	if len(children) == 0 {
		return nil
	}
	if len(children) == 1 {
		// Collapse a single category to avoid a redundant directory level.
		only := children[0]
		only.Title = title
		return only
	}
	return &extractor.MediaInfo{Site: "classin", Title: title, Entries: children}
}

func (ci *Classin) buildUnitEntries(c *util.Client, sid, courseID, categoryID string, headers map[string]string) []*extractor.MediaInfo {
	units, uuid := listUnits(c, sid, courseID, categoryID)
	var out []*extractor.MediaInfo
	for _, unit := range units {
		acts := listUnitActivities(c, sid, courseID, unit.UnitID, uuid)
		entries := ci.resolveActivities(c, sid, courseID, acts, headers)
		if len(entries) == 0 {
			continue
		}
		unitName := firstNonEmpty(unit.UnitName, unit.Name, unit.Title)
		if isDefaultUnitName(unitName) {
			out = append(out, entries...)
			continue
		}
		out = append(out, &extractor.MediaInfo{
			Site:    "classin",
			Title:   util.SanitizeFilename(firstNonEmpty(unitName, "未命名章节")),
			Entries: entries,
		})
	}
	return out
}

func (ci *Classin) resolveActivities(c *util.Client, sid, courseID string, acts []activityItem, headers map[string]string) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for _, act := range acts {
		switch act.Type {
		case 4, 5:
			out = append(out, resolveVideoActivity(c, sid, courseID, act, headers)...)
		case 1:
			out = append(out, resolveHomeworkActivity(c, sid, courseID, act, headers)...)
		}
	}
	return out
}

// resolveVideoActivity turns a video activity into playable entries. type 5 is a
// recorded class (recordClass/get returns a `video` JSON string list); type 4 is
// a live replay (getLessonRecordInfo, keyed by clientClassId/bizId). Both feed
// the same playable collector + m3u8 token exchange already in classin.go.
func resolveVideoActivity(c *util.Client, sid, courseID string, act activityItem, headers map[string]string) []*extractor.MediaInfo {
	title := util.SanitizeFilename(act.name())
	forms := []map[string]string{
		{"getStuStatistic": "1", "activityId": firstNonEmpty(act.ActivityID, act.BizID), "courseId": courseID, "classRole": "1", "clusterRole": "0", "SID": sid},
		{"flag": "1", "memberUid": classinUID, "clientClassId": firstNonEmpty(act.BizID, act.ClassID, act.ActivityID), "clientCourseId": courseID, "SID": sid},
	}
	var plays []playable
	for _, form := range forms {
		payload, err := postFormJSON(c, formAPIForVideo(form), form)
		if err != nil {
			continue
		}
		plays = append(plays, collectPlayables(c, payload)...)
		if len(plays) > 0 {
			break
		}
	}
	plays = dedupePlayables(plays)
	return playablesToEntries(plays, title, headers)
}

func formAPIForVideo(form map[string]string) string {
	if _, ok := form["getStuStatistic"]; ok {
		return urlRecordGet
	}
	return urlLessonInfo
}

// resolveHomeworkActivity downloads the file list attached to a homework/material
// activity. homework/get returns file arrays under docs/image/audio/video and
// their th* mirrors; each fileId is resolved to a CDN URL via file/getDownInfo.
func resolveHomeworkActivity(c *util.Client, sid, courseID string, act activityItem, headers map[string]string) []*extractor.MediaInfo {
	activityID := firstNonEmpty(act.ActivityID, act.BizID)
	if activityID == "" {
		return nil
	}
	env, err := postFormMap(c, urlHomeworkGet, map[string]string{
		"activityId": activityID,
		"courseId":   courseID,
		"SID":        sid,
	})
	if err != nil || !env.ok() || len(env.Data) == 0 {
		return nil
	}
	files := parseHomeworkFiles(env.Data)
	if len(files) == 0 {
		return nil
	}

	var out []*extractor.MediaInfo
	for _, f := range files {
		downURL := resolveFileURL(c, f)
		if downURL == "" {
			continue
		}
		out = append(out, fileMediaInfo(firstNonEmpty(f.FileName, act.name(), "资料"), downURL, headers))
	}
	return out
}

type homeworkFile struct {
	FileID   string
	FileName string
	FileURL  string
}

// parseHomeworkFiles reads every file bucket in a homework `data` object. Each
// bucket may be a JSON array, a single object, or a JSON-encoded string of
// either (the ClassIn API is inconsistent), so values are normalized first.
func parseHomeworkFiles(data json.RawMessage) []homeworkFile {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	buckets := []string{"docs", "image", "audio", "video", "thDocs", "thImage", "thAudio", "thVideo"}
	var out []homeworkFile
	for _, bucket := range buckets {
		raw, ok := obj[bucket]
		if !ok {
			continue
		}
		for _, item := range parseJSONList(raw) {
			fid := jsonField(item, "fileId", "fileId_source", "originId")
			furl := jsonField(item, "url", "downloadUrl", "filePath", "address")
			if fid == "" && furl == "" {
				continue
			}
			out = append(out, homeworkFile{
				FileID:   fid,
				FileName: jsonField(item, "fileName", "name", "title"),
				FileURL:  furl,
			})
		}
	}
	return out
}

// resolveFileURL turns a homework file into a downloadable URL. A direct http
// URL is used as-is; an `upload/`-rooted path is joined to the CDN base;
// otherwise file/getDownInfo is queried for data.src/filePath/url.
func resolveFileURL(c *util.Client, f homeworkFile) string {
	if u := strings.TrimSpace(f.FileURL); u != "" {
		if strings.HasPrefix(u, "http") {
			return u
		}
		if strings.HasPrefix(strings.TrimLeft(u, "/"), "upload/") {
			return classinCDNBase + "/" + strings.TrimLeft(u, "/")
		}
	}
	if f.FileID == "" {
		return ""
	}
	env, err := postFormMap(c, urlFileDownInfo, map[string]string{"fileId": f.FileID})
	if err != nil || !env.ok() || len(env.Data) == 0 {
		return ""
	}
	var dataObj map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &dataObj); err != nil {
		return ""
	}
	src := jsonField(dataObj, "src", "filePath", "url")
	if src == "" {
		return ""
	}
	if strings.HasPrefix(src, "http") {
		return src
	}
	return classinCDNBase + "/" + strings.TrimLeft(src, "/")
}

func listCourses(c *util.Client) []courseItem {
	var out []courseItem
	seen := map[string]bool{}
	for page := 1; page <= 50; page++ {
		env, err := postJSONMap(c, urlCourseList, map[string]string{
			"page":     strconv.Itoa(page),
			"pageSize": "40",
		})
		if err != nil || !env.ok() {
			break
		}
		items := decodeList[courseItem](env.Data)
		for _, it := range items {
			if it.CourseID != "" && seen[it.CourseID] {
				continue
			}
			if it.CourseID != "" {
				seen[it.CourseID] = true
			}
			out = append(out, it)
		}
		if len(items) < 40 {
			break
		}
	}
	return out
}

func listCategories(c *util.Client, sid, courseID string) []categoryItem {
	env, err := postFormMap(c, urlCategoryList, map[string]string{
		"SID":         sid,
		"classRole":   "0",
		"clusterRole": "0",
		"courseId":    courseID,
	})
	if err != nil || !env.ok() {
		return nil
	}
	return decodeList[categoryItem](env.Data)
}

func listUnits(c *util.Client, sid, courseID, categoryID string) ([]unitItem, string) {
	data := courseFilterPayload(sid, courseID, categoryID)
	env, err := postFormMap(c, urlUnitList, data)
	if err != nil || !env.ok() || len(env.Data) == 0 {
		return nil, ""
	}
	units := decodeList[unitItem](env.Data)
	var holder struct {
		UUID string `json:"uuid"`
	}
	_ = json.Unmarshal(env.Data, &holder)
	return units, holder.UUID
}

func listUnitActivities(c *util.Client, sid, courseID, unitID, uuid string) []activityItem {
	if unitID == "" {
		return nil
	}
	// unitIds is sent as a bracketed string ("[id]"), the form the ClassIn app
	// uses; uuid pins the studentUnitList response the ids came from.
	data := mergeMap(courseFilterPayload(sid, courseID, ""), map[string]string{
		"unitIds": "[" + unitID + "]",
		"uuid":    uuid,
	})
	env, err := postFormMap(c, urlUnitActivity, data)
	if err != nil || !env.ok() || len(env.Data) == 0 {
		return nil
	}
	return decodeList[activityItem](env.Data)
}

// courseFilterPayload mirrors _build_course_filter_payload: the constant student
// filter fields plus optional categoryId. unitIds/uuid are layered on by callers.
func courseFilterPayload(sid, courseID, categoryID string) map[string]string {
	data := map[string]string{
		"sort":        "asc",
		"isSearch":    "0",
		"isUpcoming":  "0",
		"studentId":   classinUID,
		"role":        "student",
		"courseId":    courseID,
		"classRole":   "1",
		"clusterRole": "0",
		"SID":         sid,
	}
	if categoryID != "" {
		data["categoryId"] = categoryID
	}
	return data
}

func playablesToEntries(plays []playable, fallbackTitle string, headers map[string]string) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for i, p := range plays {
		title := firstNonEmpty(p.Title, fallbackTitle, fmt.Sprintf("ClassIn-%02d", i+1))
		out = append(out, mediaInfo(title, p.URL, p.Format, headers))
	}
	return out
}

func fileMediaInfo(title, downURL string, headers map[string]string) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "classin", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{
		"best": {Quality: "best", URLs: []string{downURL}, Format: fileExt(downURL), Headers: headers},
	}}
}

func fileExt(downURL string) string {
	clean := downURL
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	if dot := strings.LastIndex(clean, "."); dot >= 0 && dot > strings.LastIndex(clean, "/") {
		ext := strings.ToLower(clean[dot+1:])
		if len(ext) >= 1 && len(ext) <= 8 {
			return ext
		}
	}
	return ""
}

func isDefaultCategoryName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "new course", "default", "default section", "default category", "course",
		"新课程", "默认章节", "默认分类", "课程", "未分类", "无分类":
		return true
	}
	return false
}

func isDefaultUnitName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "untitled unit", "untitled section", "default unit", "default section", "unit", "section",
		"无主题单元", "无主题章节", "默认单元", "默认章节", "未命名单元", "未命名章节", "未分类":
		return true
	}
	return false
}

// decodeList extracts data.list[] into a typed slice. data may itself be a bare
// array on some endpoints, so both shapes are handled.
func decodeList[T any](data json.RawMessage) []T {
	if len(data) == 0 {
		return nil
	}
	var holder struct {
		List []T `json:"list"`
	}
	if err := json.Unmarshal(data, &holder); err == nil && holder.List != nil {
		return holder.List
	}
	var arr []T
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr
	}
	return nil
}

// parseJSONList normalizes a homework bucket value into a list of objects. It may
// arrive as an array, a single object, or a JSON-encoded string of either.
func parseJSONList(raw json.RawMessage) []map[string]json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == `""` {
		return nil
	}
	if arr := decodeObjArray(raw); arr != nil {
		return arr
	}
	// String-wrapped JSON: unwrap the quotes, then retry as array/object.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return decodeObjArray(json.RawMessage(s))
	}
	return nil
}

func decodeObjArray(raw json.RawMessage) []map[string]json.RawMessage {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		return []map[string]json.RawMessage{obj}
	}
	return nil
}

func jsonField(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			if s := scalarString(raw); s != "" {
				return s
			}
		}
	}
	return ""
}

// scalarString reads a JSON scalar (string or number) as a trimmed string,
// ignoring null/objects/arrays.
func scalarString(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

func mergeMap(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// jsonBody converts a flat string map into a JSON object, promoting purely
// numeric values to numbers. course_list sends page/pageSize as integers while
// the signature still hashes their string form.
func jsonBody(data map[string]string) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		if n, err := strconv.Atoi(v); err == nil {
			out[k] = n
			continue
		}
		out[k] = v
	}
	return out
}
