package gongxuanwang

import (
	"fmt"
	"net/url"
)

func (x *gxCtx) loadInfos() ([]gxVideo, []gxFile, error) {
	if x.cid == "" {
		courses := x.getCourseList()
		if len(courses) == 0 {
			return nil, nil, fmt.Errorf("gongxuanwang: no course id and empty course list")
		}
		x.applySelectedCourse(courses[0])
	}
	if x.selected.CourseID == "" && x.cid != "" {
		if c := x.findCourse(x.cid, x.source); c.CourseID != "" {
			x.applySelectedCourse(c)
		}
	}

	loaders := []func() ([]gxVideo, []gxFile, error){}
	switch x.source {
	case "open":
		loaders = append(loaders, x.getOpenInfos)
	case "lms", "":
		loaders = append(loaders, x.getLMSInfos)
	case "system":
		loaders = append(loaders, x.getSystemInfos)
	case "sku":
		loaders = append(loaders, x.getSKUInfos)
	case "legacy":
		loaders = append(loaders, x.getLegacyInfos)
	}
	if x.source == "" {
		loaders = []func() ([]gxVideo, []gxFile, error){x.getLMSInfos, x.getOpenInfos, x.getSKUInfos, x.getLegacyInfos}
	}
	var last error
	for _, load := range loaders {
		videos, files, err := load()
		if err == nil && (len(videos) > 0 || len(files) > 0) {
			return videos, files, nil
		}
		last = err
	}
	if last != nil {
		return nil, nil, last
	}
	return nil, nil, fmt.Errorf("gongxuanwang: empty course infos")
}

func (x *gxCtx) getLMSInfos() ([]gxVideo, []gxFile, error) { return x.getLMSInfosForCID(x.cid) }

func (x *gxCtx) getLMSInfosForCID(cid string) ([]gxVideo, []gxFile, error) {
	root, err := x.getJSON(fmt.Sprintf(lms_course_detail_api, url.QueryEscape(cid)))
	if err != nil {
		return nil, nil, err
	}
	data := dataMap(root)
	if intVal(root["code"]) != 200 || len(data) == 0 {
		return nil, nil, fmt.Errorf("gongxuanwang lms detail: code=%v empty_data=%v", root["code"], len(data) == 0)
	}
	x.source = "lms"
	x.title = firstNonEmpty(firstString(data, "courseSkuName"), x.title, cid)
	vidMap := x.getLMSPeriodVidMap(cid)
	sections := listAt(data, "webCourseSectionVOS")
	var videos []gxVideo
	var files []gxFile
	for ci, section := range sections {
		periods := listAt(section, "webCoursePeriodVOS")
		for li, period := range periods {
			if v := x.parseLMSVideo(period, vidMap, ci+1, li+1); v.VideoID != "" {
				videos = append(videos, v)
			}
			fileIdx := 1
			if cw := mapAt(period, "webCourseWareVO"); len(cw) > 0 && !isVideoCourseware(cw) {
				if f := buildFileDict(cw, ci+1, li+1, fileIdx); f.URL != "" {
					files = append(files, f)
					fileIdx++
				}
			}
			for _, cw := range listAt(period, "webCourseWareVOS") {
				if f := buildFileDict(cw, ci+1, li+1, fileIdx); f.URL != "" {
					files = append(files, f)
					fileIdx++
				}
			}
		}
	}
	return videos, files, nil
}

func (x *gxCtx) getLMSPeriodVidMap(cid string) map[string]map[string]any {
	root, err := x.getJSON(fmt.Sprintf(lms_period_vid_api, url.QueryEscape(cid)))
	if err != nil {
		return map[string]map[string]any{}
	}
	out := map[string]map[string]any{}
	for _, item := range listAt(dataMap(root), "periodVids") {
		id := firstString(item, "periodId", "coursePeriodId", "id")
		if id != "" {
			out[id] = item
		}
	}
	return out
}

func (x *gxCtx) parseLMSVideo(period map[string]any, vidMap map[string]map[string]any, chapterIndex, lessonIndex int) gxVideo {
	periodID := firstString(period, "coursePeriodId")
	courseware := mapAt(period, "webCourseWareVO")
	mapped := vidMap[periodID]
	videoID := firstString(mapped, "vid")
	if videoID == "" && isVideoCourseware(courseware) {
		videoID = firstString(courseware, "coursewareUrl")
	}
	if videoID == "" {
		return gxVideo{}
	}
	name := firstNonEmpty(firstString(period, "coursePeriodName"), firstString(courseware, "coursewareName"), "未命名课时")
	return gxVideo{Name: fmt.Sprintf("[%d.%d]--%s", chapterIndex, lessonIndex, name), VideoID: videoID, Source: "lms", VID: videoID, ClassID: firstString(mapped, "classId"), TimeArrangeID: firstNonEmpty(firstString(mapped, "timeArrangeId"), firstString(period, "timeArrangeId")), CourseCoursewareID: firstNonEmpty(firstString(mapped, "courseCoursewareId"), firstString(courseware, "coursewareId")), PeriodID: periodID}
}

func (x *gxCtx) getOpenInfos() ([]gxVideo, []gxFile, error) {
	root, err := x.getJSON(fmt.Sprintf(open_course_detail_api, url.QueryEscape(x.cid)))
	if err != nil {
		return nil, nil, err
	}
	data := dataMap(root)
	if intVal(root["code"]) != 200 || len(data) == 0 {
		return nil, nil, fmt.Errorf("gongxuanwang open detail: code=%v empty_data=%v", root["code"], len(data) == 0)
	}
	x.source = "open"
	x.title = firstNonEmpty(firstString(data, "courseSkuName", "title"), x.title, x.cid)
	vid := firstString(data, "recordedVid", "vid")
	if vid == "" {
		return nil, nil, fmt.Errorf("gongxuanwang open detail: empty recordedVid")
	}
	return []gxVideo{{Name: fmt.Sprintf("[1.1]--%s", x.cid), VideoID: vid, VID: firstString(data, "vid"), Source: "open", ClassID: firstString(data, "classId", "classRoomId"), TimeArrangeID: firstString(data, "timeArrangeId")}}, nil, nil
}

func (x *gxCtx) getSystemInfos() ([]gxVideo, []gxFile, error) {
	course := x.selected
	if course.CourseID == "" {
		course = x.findCourse(x.cid, "system")
	}
	payload := compactPayload(map[string]any{"studentGoodId": course.StudentGoodsID, "classId": course.ClassID, "courseSkuId": firstNonEmpty(course.CourseSkuID, x.cid)})
	rows, _ := x.loadPagedPostRows(system_course_detail_api, payload, 100, 1000)
	if len(rows) == 0 && course.ClassID != "" {
		alt := compactPayload(map[string]any{"studentGoodId": course.StudentGoodsID, "classRoomId": course.ClassID, "courseSkuId": firstNonEmpty(course.CourseSkuID, x.cid)})
		rows, _ = x.loadPagedPostRows(system_course_detail_api, alt, 100, 1000)
	}
	x.source = "system"
	x.title = firstNonEmpty(course.Title, x.title, x.cid)
	var videos []gxVideo
	var files []gxFile
	for ci, section := range iterSystemSections(rows) {
		periods := systemPeriodList(section)
		for li, period := range periods {
			if v := extractSystemVideo(period, ci+1, li+1); v.VideoID != "" {
				videos = append(videos, v)
			}
			for fi, cw := range listAt(period, "webCourseWareVOS") {
				if f := buildFileDict(cw, ci+1, li+1, fi+1); f.URL != "" {
					files = append(files, f)
				}
			}
		}
	}
	if root, err := x.postJSON(system_class_course_detail_api, payload); err == nil {
		for fi, cw := range listAt(dataMap(root), "courseCoursewares") {
			if f := buildFileDict(cw, len(files)+1, 1, fi+1); f.URL != "" {
				files = append(files, f)
			}
		}
	}
	if len(videos) == 0 && len(files) == 0 {
		return nil, nil, fmt.Errorf("gongxuanwang system detail: empty rows")
	}
	return videos, files, nil
}

func (x *gxCtx) getSKUInfos() ([]gxVideo, []gxFile, error) {
	detail := x.getSKUDetail(x.cid)
	if len(detail) == 0 {
		return nil, nil, fmt.Errorf("gongxuanwang sku detail: empty")
	}
	x.source = "sku"
	x.title = firstNonEmpty(firstString(detail, "goodsName", "title"), x.title, x.cid)
	children := extractSKUChildCourses(detail)
	if len(children) == 0 {
		children = []gxCourse{{CourseID: firstString(detail, "courseSkuId", "courseId", "id"), Title: x.title}}
	}
	var videos []gxVideo
	var files []gxFile
	for _, child := range children {
		cid := firstNonEmpty(child.CourseID, child.CourseSkuID)
		if cid == "" {
			continue
		}
		oldCID, oldTitle, oldSource := x.cid, x.title, x.source
		x.cid, x.title, x.source = cid, firstNonEmpty(child.Title, x.title), "lms"
		vs, fs, err := x.getLMSInfosForCID(cid)
		x.cid, x.title, x.source = oldCID, oldTitle, oldSource
		if err == nil {
			for i := range vs {
				vs[i].Name = fmt.Sprintf("{%d}--%s--%s", len(videos)+1, firstNonEmpty(child.Title, cid), vs[i].Name)
			}
			videos = append(videos, vs...)
			files = append(files, fs...)
		}
	}
	if len(videos) == 0 && len(files) == 0 {
		return nil, nil, fmt.Errorf("gongxuanwang sku: no child lms infos")
	}
	return videos, files, nil
}

func (x *gxCtx) getLegacyInfos() ([]gxVideo, []gxFile, error) {
	root, err := x.getJSON(fmt.Sprintf(legacy_course_detail_api, url.QueryEscape(x.cid)))
	if err != nil {
		return nil, nil, err
	}
	data := dataMap(root)
	if intVal(root["code"]) != 200 || len(data) == 0 {
		return nil, nil, fmt.Errorf("gongxuanwang legacy detail: code=%v empty_data=%v", root["code"], len(data) == 0)
	}
	x.source = "legacy"
	x.title = firstNonEmpty(firstString(mapAt(data, "course_info"), "title"), x.title, x.cid)
	lessons := legacyLessonList(data)
	var videos []gxVideo
	for i, lesson := range lessons {
		lessonID := firstString(lesson, "lessonId", "id")
		mediaID := firstString(lesson, "mediaId", "vid")
		if mediaID == "" {
			continue
		}
		name := firstNonEmpty(firstString(lesson, "title", "name"), "未命名课时")
		videos = append(videos, gxVideo{Name: fmt.Sprintf("[%d.%d]--%s", 1, i+1, name), VideoID: mediaID, Source: "legacy", MediaID: mediaID, LessonID: lessonID})
	}
	if len(videos) == 0 {
		return nil, nil, fmt.Errorf("gongxuanwang legacy detail: no lessons")
	}
	return videos, nil, nil
}
