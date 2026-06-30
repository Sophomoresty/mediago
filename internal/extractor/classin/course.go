package classin

import (
	"encoding/json"
	"fmt"
	"sort"
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
	CourseID2  string `json:"clientCourseId"`
	CourseName string `json:"courseName"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	SchoolUID  string `json:"schoolUid"`
	SchoolUID2 string `json:"school_uid"`
}

func (c courseItem) id() string { return firstNonEmpty(c.CourseID, c.CourseID2) }
func (c courseItem) title() string {
	return firstNonEmpty(c.CourseName, c.Name, c.Title, "ClassIn课程")
}
func (c courseItem) sid() string { return firstNonEmpty(c.SchoolUID, c.SchoolUID2) }

type categoryItem struct {
	CategoryID   string `json:"categoryId"`
	CategoryID2  string `json:"id"`
	Name         string `json:"name"`
	CategoryName string `json:"categoryName"`
	Title        string `json:"title"`
}

func (c categoryItem) id() string { return firstNonEmpty(c.CategoryID, c.CategoryID2) }

type unitItem struct {
	UnitID   string `json:"unitId"`
	UnitID2  string `json:"id"`
	UnitName string `json:"unitName"`
	Name     string `json:"name"`
	Title    string `json:"title"`
}

func (u unitItem) id() string { return firstNonEmpty(u.UnitID, u.UnitID2) }

type activityItem struct {
	ActivityID   string `json:"activityId"`
	ActivityID2  string `json:"id"`
	BizID        string `json:"bizId"`
	ClassID      string `json:"classId"`
	ClassID2     string `json:"clientClassId"`
	Type         int    `json:"type"`
	ActivityType int    `json:"activityType"`
	ActivityName string `json:"activityName"`
	Name         string `json:"name"`
	Title        string `json:"title"`
}

func (a activityItem) id() string { return firstNonEmpty(a.ActivityID, a.ActivityID2, a.BizID) }
func (a activityItem) classID() string {
	return firstNonEmpty(a.BizID, a.ClassID, a.ClassID2, a.ActivityID, a.ActivityID2)
}
func (a activityItem) kind() int {
	if a.Type != 0 {
		return a.Type
	}
	return a.ActivityType
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
func (ci *Classin) extractCourseTree(c *util.Client, in ids, headers map[string]string, auth classinAuth) (*extractor.MediaInfo, error) {
	courses := listCourses(c, auth)
	if len(courses) == 0 {
		return nil, fmt.Errorf("classin: course_list returned no courses")
	}

	// When the URL pins a specific course, only traverse that one; otherwise
	// emit every course the member can see.
	var selected []courseItem
	for _, course := range courses {
		if in.CourseID == "" || course.id() == in.CourseID {
			selected = append(selected, course)
		}
	}
	if len(selected) == 0 {
		selected = courses
	}

	var entries []*extractor.MediaInfo
	for _, course := range selected {
		sid := firstNonEmpty(course.sid(), in.SID)
		node := ci.buildCourseEntry(c, sid, course, headers, auth)
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

func (ci *Classin) buildCourseEntry(c *util.Client, sid string, course courseItem, headers map[string]string, auth classinAuth) *extractor.MediaInfo {
	title := util.SanitizeFilename(course.title())
	courseID := course.id()
	categories := listCategories(c, sid, courseID, auth)

	var children []*extractor.MediaInfo
	if len(categories) == 0 {
		// No category layer: first enumerate units with an empty categoryId; if
		// the LMS has no unit tree (older ClassIn records), fall back to
		// getuserRecordclasses, matching _build_legacy_record_infos.
		children = append(children, ci.buildUnitEntries(c, sid, courseID, "", headers, auth)...)
		if len(children) == 0 {
			children = append(children, ci.buildLegacyRecordEntries(c, sid, courseID, title, headers, auth)...)
		}
	} else {
		for _, cat := range categories {
			catName := firstNonEmpty(cat.Name, cat.CategoryName, cat.Title)
			units := ci.buildUnitEntries(c, sid, courseID, cat.id(), headers, auth)
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
		children = append(children, ci.buildLegacyRecordEntries(c, sid, courseID, title, headers, auth)...)
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

func (ci *Classin) buildLegacyRecordEntries(c *util.Client, sid, courseID, title string, headers map[string]string, auth classinAuth) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	records := listUserRecordClasses(c, sid, courseID, auth)
	for i, record := range records {
		classID := firstNonEmpty(textValue(record, "client_class_id", "clientClassId", "classId"), textValue(record, "bizId", "recordId"))
		if classID == "" {
			continue
		}
		name := firstNonEmpty(
			textValue(record, "class_name", "className", "lessonName", "course_name", "courseName", "name", "title"),
			fmt.Sprintf("ClassIn record %02d", i+1),
		)
		act := activityItem{
			ActivityID:   classID,
			BizID:        classID,
			ClassID:      classID,
			ActivityName: name,
			Type:         4,
		}
		entries := resolveVideoActivity(c, firstNonEmpty(textValue(record, "school_uid", "schoolUid"), sid), firstNonEmpty(textValue(record, "client_course_id", "clientCourseId"), courseID), act, headers, auth)
		if len(entries) == 0 {
			continue
		}
		if len(entries) == 1 {
			out = append(out, entries[0])
			continue
		}
		out = append(out, &extractor.MediaInfo{Site: "classin", Title: util.SanitizeFilename(name), Entries: entries})
	}
	return out
}

func (ci *Classin) buildUnitEntries(c *util.Client, sid, courseID, categoryID string, headers map[string]string, auth classinAuth) []*extractor.MediaInfo {
	units, uuid := listUnits(c, sid, courseID, categoryID, auth)
	var out []*extractor.MediaInfo
	for _, unit := range units {
		unitID := unit.id()
		acts := listUnitActivities(c, sid, courseID, categoryID, unitID, uuid, auth)
		entries := ci.resolveActivities(c, sid, courseID, acts, headers, auth)
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

func (ci *Classin) resolveActivities(c *util.Client, sid, courseID string, acts []activityItem, headers map[string]string, auth classinAuth) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for _, act := range acts {
		switch act.kind() {
		case 4, 5:
			out = append(out, resolveVideoActivity(c, sid, courseID, act, headers, auth)...)
		case 1:
			out = append(out, resolveHomeworkActivity(c, sid, courseID, act, headers, auth)...)
		}
	}
	return out
}

// resolveVideoActivity turns a video activity into playable entries. type 5 is a
// recorded class (recordClass/get returns a `video` JSON string list); type 4 is
// a live replay (getLessonRecordInfo, keyed by clientClassId/bizId). Both feed
// the same playable collector + m3u8 token exchange already in classin.go.
func resolveVideoActivity(c *util.Client, sid, courseID string, act activityItem, headers map[string]string, auth classinAuth) []*extractor.MediaInfo {
	title := util.SanitizeFilename(act.name())
	forms := []map[string]string{
		{"getStuStatistic": "1", "activityId": act.id(), "courseId": courseID, "classRole": "1", "clusterRole": "0", "SID": sid},
		{"flag": "1", "memberUid": auth.normalized().UID, "clientClassId": act.classID(), "clientCourseId": courseID, "SID": sid},
	}
	var plays []playable
	for _, form := range forms {
		payload, err := postFormJSON(c, formAPIForVideo(form), form, auth)
		if err != nil {
			continue
		}
		plays = append(plays, collectPlayables(c, payload, auth)...)
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
func resolveHomeworkActivity(c *util.Client, sid, courseID string, act activityItem, headers map[string]string, auth classinAuth) []*extractor.MediaInfo {
	activityID := act.id()
	if activityID == "" {
		return nil
	}
	env, err := postFormMap(c, urlHomeworkGet, map[string]string{
		"activityId": activityID,
		"courseId":   courseID,
		"SID":        sid,
	}, auth)
	if err != nil || !env.ok() || len(env.Data) == 0 {
		return nil
	}
	files := parseHomeworkFiles(env.Data)
	if len(files) == 0 {
		return nil
	}

	var out []*extractor.MediaInfo
	for _, f := range files {
		downURL := resolveFileURL(c, f, auth)
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
func resolveFileURL(c *util.Client, f homeworkFile, auth classinAuth) string {
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
	env, err := postFormMap(c, urlFileDownInfo, map[string]string{"fileId": f.FileID}, auth)
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

func listCourses(c *util.Client, auth classinAuth) []courseItem {
	var out []courseItem
	seen := map[string]bool{}
	for page := 1; page <= 50; page++ {
		env, err := postJSONMap(c, urlCourseList, map[string]string{
			"page":     strconv.Itoa(page),
			"pageSize": "40",
		}, auth)
		if err != nil || !env.ok() {
			break
		}
		items := decodeList[courseItem](env.Data)
		for _, it := range items {
			id := it.id()
			if id != "" && seen[id] {
				continue
			}
			if id != "" {
				seen[id] = true
			}
			out = append(out, it)
		}
		if len(items) < 40 {
			break
		}
	}
	return out
}

func listCategories(c *util.Client, sid, courseID string, auth classinAuth) []categoryItem {
	env, err := postFormMap(c, urlCategoryList, map[string]string{
		"SID":         sid,
		"classRole":   "0",
		"clusterRole": "0",
		"courseId":    courseID,
	}, auth)
	if err != nil || !env.ok() {
		return nil
	}
	return decodeList[categoryItem](env.Data)
}

func listUnits(c *util.Client, sid, courseID, categoryID string, auth classinAuth) ([]unitItem, string) {
	data := courseFilterPayload(sid, courseID, categoryID, auth)
	env, err := postFormMap(c, urlUnitList, data, auth)
	if err != nil || !env.ok() || len(env.Data) == 0 {
		return nil, ""
	}
	units := decodeList[unitItem](env.Data)
	var holder struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(env.Data, &holder); err != nil {
		holder.UUID = ""
	}
	return units, holder.UUID
}

func listUnitActivities(c *util.Client, sid, courseID, categoryID, unitID, uuid string, auth classinAuth) []activityItem {
	if unitID == "" {
		return nil
	}
	// unitIds is sent as a bracketed string ("[id]"), the form the ClassIn app
	// uses; uuid pins the studentUnitList response the ids came from.
	payloads := []map[string]string{
		mergeMap(courseFilterPayload(sid, courseID, categoryID, auth), map[string]string{
			"unitIds": "[" + unitID + "]",
			"uuid":    uuid,
		}),
		mergeMap(courseFilterPayload(sid, courseID, categoryID, auth), map[string]string{
			"unitIds": unitID,
			"uuid":    uuid,
		}),
	}
	seen := map[string]bool{}
	for _, data := range payloads {
		key := stableFormKey(data)
		if seen[key] {
			continue
		}
		seen[key] = true
		env, err := postFormMap(c, urlUnitActivity, data, auth)
		if err != nil || !env.ok() || len(env.Data) == 0 {
			continue
		}
		if items := decodeList[activityItem](env.Data); len(items) > 0 {
			return items
		}
	}
	return nil
}

// courseFilterPayload mirrors _build_course_filter_payload: the constant student
// filter fields plus optional categoryId. unitIds/uuid are layered on by callers.
func courseFilterPayload(sid, courseID, categoryID string, auth classinAuth) map[string]string {
	data := map[string]string{
		"sort":        "asc",
		"isSearch":    "0",
		"isUpcoming":  "0",
		"studentId":   auth.normalized().UID,
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

func listUserRecordClasses(c *util.Client, sid, courseID string, auth classinAuth) []map[string]any {
	if courseID == "" {
		return nil
	}
	var out []map[string]any
	seen := map[string]bool{}
	lastClassID := ""
	for page := 0; page < 50; page++ {
		data := map[string]string{
			"clientCourseId": courseID,
			"UID":            auth.normalized().UID,
		}
		if sid != "" {
			data["schoolUid"] = sid
		}
		if lastClassID != "" {
			data["clientClassId"] = lastClassID
		}
		env, err := postFormMap(c, urlUserRecords, data, auth)
		if err != nil || !env.ok() || len(env.Data) == 0 {
			break
		}
		items := decodeList[map[string]any](env.Data)
		if len(items) == 0 {
			break
		}
		var added int
		nextClassID := ""
		for _, item := range items {
			if cid := firstNonEmpty(textValue(item, "client_course_id", "clientCourseId"), courseID); cid != "" && cid != courseID {
				continue
			}
			if itemSID := textValue(item, "school_uid", "schoolUid"); sid != "" && itemSID != "" && itemSID != sid {
				continue
			}
			classID := firstNonEmpty(textValue(item, "client_class_id", "clientClassId", "classId"), textValue(item, "bizId", "recordId"))
			if classID == "" {
				continue
			}
			nextClassID = classID
			if seen[classID] {
				continue
			}
			seen[classID] = true
			out = append(out, item)
			added++
		}
		if !hasMoreClassin(env.Data) || nextClassID == "" || nextClassID == lastClassID || added == 0 {
			break
		}
		lastClassID = nextClassID
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := classinIntValue(out[i], "class_btime", "classBtime"), classinIntValue(out[j], "class_btime", "classBtime")
		if bi != bj {
			return bi < bj
		}
		return classinIntValue(out[i], "classId", "client_class_id", "clientClassId") < classinIntValue(out[j], "classId", "client_class_id", "clientClassId")
	})
	return out
}

func hasMoreClassin(data json.RawMessage) bool {
	var holder map[string]any
	if err := json.Unmarshal(data, &holder); err != nil {
		return false
	}
	return classinBoolValue(holder, "has_more", "hasMore")
}

func classinBoolValue(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		switch v := m[k].(type) {
		case bool:
			return v
		case float64:
			return v != 0
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "1", "true", "yes", "ok":
				return true
			case "0", "false", "no", "":
				return false
			}
		}
	}
	return false
}

func classinIntValue(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch x := v.(type) {
			case float64:
				return int(x)
			case int:
				return x
			case int64:
				return int(x)
			case json.Number:
				n, _ := strconv.Atoi(x.String())
				return n
			case string:
				n, _ := strconv.Atoi(strings.TrimSpace(x))
				return n
			}
		}
	}
	return 0
}

func stableFormKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "&")
}
