package gongxuanwang

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

func (x *gxCtx) loadPagedPostRows(api string, payload map[string]any, pageSize, maxPages int) ([]map[string]any, error) {
	var out []map[string]any
	for page := 1; page <= maxPages; page++ {
		req := clonePayload(payload)
		req["page"] = page
		req["size"] = pageSize
		req["rows"] = pageSize
		req["pageSize"] = pageSize
		root, err := x.postJSON(api, req)
		if err != nil {
			return out, err
		}
		rows, total := extractRowsData(root)
		if len(rows) == 0 {
			break
		}
		out = append(out, rows...)
		if total > 0 && len(out) >= total {
			break
		}
		if len(rows) < pageSize {
			break
		}
	}
	return out, nil
}

func (x *gxCtx) getCourseList() []gxCourse {
	var out []gxCourse
	out = append(out, x.loadLMSCourseList()...)
	out = append(out, x.loadSystemCourseList()...)
	out = append(out, x.loadLegacyCourseList()...)
	out = append(out, x.loadOpenCourseList()...)
	return out
}

func (x *gxCtx) findCourse(courseID, source string) gxCourse {
	if courseID == "" {
		return gxCourse{}
	}
	for _, src := range []string{"open", "lms", "system", "legacy", "sku"} {
		if source != "" && source != src {
			continue
		}
		var list []gxCourse
		switch src {
		case "open":
			list = x.loadOpenCourseList()
		case "lms":
			list = x.loadLMSCourseList()
		case "system":
			list = x.loadSystemCourseList()
		case "legacy":
			list = x.loadLegacyCourseList()
		case "sku":
			list = x.loadSKUCourseList(false)
		}
		for _, c := range list {
			if stringSetHas(courseIdentityValues(c), courseID) {
				return c
			}
		}
	}
	return gxCourse{}
}

func (x *gxCtx) applySelectedCourse(c gxCourse) {
	x.selected = c
	if c.CourseID != "" {
		x.cid = c.CourseID
	}
	if c.Source != "" {
		x.source = c.Source
	}
	if c.Title != "" {
		x.title = c.Title
	}
}

func (x *gxCtx) loadLMSCourseList() []gxCourse {
	if v, ok := x.courseLists["lms"]; ok {
		return v
	}
	rows, _ := x.loadPagedPostRows(lms_course_list_api, map[string]any{}, 100, 1000)
	var out []gxCourse
	for _, row := range rows {
		out = append(out, gxCourse{Source: "lms", CourseID: firstString(row, "courseSkuId"), Title: firstString(row, "courseSkuName"), Course: row, ClassID: firstString(row, "classRoomId"), StudentGoodsID: firstString(row, "studentGoodsId")})
	}
	x.courseLists["lms"] = out
	return out
}

func (x *gxCtx) loadSystemCourseList() []gxCourse {
	if v, ok := x.courseLists["system"]; ok {
		return v
	}
	rows, _ := x.loadPagedPostRows(system_course_list_api, map[string]any{}, 100, 1000)
	var out []gxCourse
	for _, row := range rows {
		out = append(out, gxCourse{Source: "system", CourseID: firstString(row, "courseSkuId", "id"), CourseSkuID: firstString(row, "courseSkuId", "id"), GoodsID: firstString(row, "goodsId", "id"), Title: firstString(row, "goodsName", "courseSkuName", "title", "name"), Course: row, ClassID: firstString(row, "classRoomId", "classId"), StudentGoodsID: firstString(row, "studentGoodId", "studentGoodsId", "userGoodsId", "userGoodsID"), Accessible: true})
	}
	x.courseLists["system"] = out
	return out
}

func (x *gxCtx) loadOpenCourseList() []gxCourse {
	if v, ok := x.courseLists["open"]; ok {
		return v
	}
	var out []gxCourse
	for _, status := range []int{0, 100} {
		for page := 1; page <= 1000; page++ {
			root, err := x.postJSON(open_course_list_api, map[string]any{"sorting": 1, "timeArrangeStatus": status, "size": 50, "page": page})
			if err != nil {
				break
			}
			rows := listAt(dataMap(root), "rows")
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				out = append(out, gxCourse{Source: "open", CourseID: firstString(row, "courseSkuId"), Title: firstString(row, "title", "courseSkuName"), Course: row, ClassID: firstString(row, "classRoomId")})
			}
			if len(rows) < 50 {
				break
			}
		}
	}
	x.courseLists["open"] = out
	return out
}

func (x *gxCtx) loadLegacyCourseList() []gxCourse {
	if v, ok := x.courseLists["legacy"]; ok {
		return v
	}
	var out []gxCourse
	for page := 1; page <= 1000; page++ {
		root, err := x.getJSON(fmt.Sprintf(legacy_course_list_api, page, 100))
		if err != nil {
			break
		}
		rows := listAt(dataMap(root), "course")
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			out = append(out, gxCourse{Source: "legacy", CourseID: firstString(row, "id"), Title: firstString(row, "title"), Course: row})
		}
		if len(rows) < 100 {
			break
		}
	}
	x.courseLists["legacy"] = out
	return out
}

func (x *gxCtx) loadSKUCourseList(accessibleOnly bool) []gxCourse {
	if v, ok := x.courseLists["sku"]; ok {
		return filterAccessible(v, accessibleOnly)
	}
	rows, _ := x.loadPagedPostRows(sku_course_list_api, map[string]any{}, 200, 1000)
	var out []gxCourse
	for _, row := range rows {
		source := "sku"
		if firstString(row, "classType") == "G" {
			source = "open"
		}
		out = append(out, gxCourse{Source: source, CourseID: firstString(row, "id", "courseSkuId"), GoodsID: firstString(row, "goodsId", "id"), CourseSkuID: firstString(row, "id"), Title: firstString(row, "goodsName", "courseSkuName"), Course: row, Accessible: isAccessibleSKUCourse(row)})
	}
	x.courseLists["sku"] = out
	return filterAccessible(out, accessibleOnly)
}

func (x *gxCtx) getSKUDetail(courseID string) map[string]any {
	courseID = firstNonEmpty(courseID, x.cid)
	for _, key := range []string{"id", "goodsId", "courseSkuId"} {
		root, err := x.postJSON(lms_price_api, map[string]any{key: courseID})
		if err != nil {
			continue
		}
		data := dataMap(root)
		if intVal(root["code"]) == 200 && (firstString(data, "id", "goodsId", "goodsName") != "") {
			return data
		}
	}
	return map[string]any{}
}

func extractRowsData(root map[string]any) ([]map[string]any, int) {
	data := root["data"]
	if list, ok := data.([]any); ok {
		return listMaps(list), len(list)
	}
	m, _ := data.(map[string]any)
	for _, key := range []string{"rows", "records", "list", "data", "course"} {
		if list, ok := m[key].([]any); ok {
			return listMaps(list), firstPositiveInt(m["total"], m["count"], m["countNum"], m["totalCount"])
		}
	}
	return nil, firstPositiveInt(m["total"], m["count"], m["countNum"], m["totalCount"])
}

func extractSKUChildCourses(detail map[string]any) []gxCourse {
	seen := map[string]bool{}
	var out []gxCourse
	for _, m := range collectMaps(detail) {
		id := firstString(m, "courseSkuId", "courseId", "id", "giftId", "outlineId")
		if id == "" || seen[id] {
			continue
		}
		if !hasAnyKey(m, "courseContentStageDTOS", "courseOutlineVO", "courseContentDTOS", "strategyPackageDTOList", "skuGiftDTOS", "webClassPackageContentDTOList", "courseSkuId", "courseId", "giftId") {
			continue
		}
		seen[id] = true
		out = append(out, gxCourse{Source: "lms", CourseID: id, CourseSkuID: id, Title: firstString(m, "title", "courseName", "courseSkuName", "name", "giftName"), Course: m})
	}
	return out
}

func legacyLessonList(data map[string]any) []map[string]any {
	info := data["courseLesson_info"]
	var out []map[string]any
	for _, m := range collectMaps(info) {
		if firstString(m, "mediaId", "vid") != "" || firstString(m, "lessonId", "id") != "" && hasAnyKey(m, "vidsPlus", "lesson") {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		out = append(out, listAt(asMap(info), "vidsPlus")...)
		out = append(out, listAt(asMap(info), "lesson")...)
	}
	return out
}

func iterSystemSections(rows []map[string]any) []map[string]any {
	var out []map[string]any
	for _, row := range rows {
		found := false
		for _, key := range []string{"webCourseSectionVOS", "courseSectionList", "sectionList", "sections"} {
			sections := listAt(row, key)
			if len(sections) > 0 {
				out = append(out, sections...)
				found = true
				break
			}
		}
		if !found {
			out = append(out, row)
		}
	}
	return out
}

func systemPeriodList(section map[string]any) []map[string]any {
	for _, key := range []string{"webCoursePeriodVOS", "coursePeriodList", "periodList", "periods", "coursePeriods"} {
		if rows := listAt(section, key); len(rows) > 0 {
			return rows
		}
	}
	return []map[string]any{section}
}

func extractSystemVideo(period map[string]any, chapterIndex, lessonIndex int) gxVideo {
	vid := firstString(period, "vid", "recordedVid", "videoId", "video_id", "coursewareUrl")
	courseware := firstMap(mapAt(period, "webCourseWareVO"), mapAt(period, "courseware"))
	if vid == "" && isVideoCourseware(courseware) {
		vid = firstString(courseware, "coursewareUrl")
	}
	if vid == "" {
		return gxVideo{}
	}
	name := firstNonEmpty(firstString(period, "coursePeriodName", "periodName", "periodTitle", "title"), firstString(courseware, "coursewareName"), "Untitled")
	return gxVideo{Name: fmt.Sprintf("[%d.%d]--%s", chapterIndex, lessonIndex, name), VideoID: vid, VID: firstString(period, "vid"), Source: "lms", PeriodID: firstString(period, "coursePeriodId", "periodId", "id"), ClassID: firstString(period, "classId", "classRoomId"), TimeArrangeID: firstString(period, "timeArrangeId", "id")}
}

var vidLikeRe = regexp.MustCompile(`^[0-9a-zA-Z]+_[0-9a-zA-Z]+$`)

func looksLikeVID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.HasPrefix(value, "http") && vidLikeRe.MatchString(value)
}

func isVideoCourseware(cw map[string]any) bool {
	if len(cw) == 0 {
		return false
	}
	ft := strings.ToLower(firstString(cw, "fileType"))
	if ft == "video" || ft == "mp4" || ft == "m3u8" {
		return true
	}
	if intVal(cw["type"]) == 4 {
		return true
	}
	return looksLikeVID(firstString(cw, "coursewareUrl"))
}

func buildFileDict(cw map[string]any, parts ...int) gxFile {
	raw := firstString(cw, "coursewareUrl")
	if raw == "" || looksLikeVID(raw) {
		return gxFile{}
	}
	name := firstNonEmpty(firstString(cw, "coursewareName"), path.Base(parsedPath(raw)))
	if name == "" || name == "." || name == "/" {
		return gxFile{}
	}
	fmtv := strings.ToLower(firstString(cw, "fileType"))
	base := name
	if dot := strings.LastIndex(base, "."); dot > 0 {
		if fmtv == "" || fmtv == "video" {
			fmtv = strings.ToLower(base[dot+1:])
		}
		base = base[:dot]
	}
	if fmtv == "" {
		fmtv = strings.TrimPrefix(path.Ext(parsedPath(raw)), ".")
	}
	if fmtv == "video" {
		fmtv = "mp4"
	}
	if fmtv == "" {
		fmtv = "pdf"
	}
	return gxFile{Name: fmt.Sprintf("(%s)-%s", joinInts(parts, "."), base), URL: quoteFileURL(raw), Fmt: fmtv}
}
