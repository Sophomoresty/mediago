package hqwx

import "fmt"

func (x *hqwxCtx) loadItems() ([]hqwxItem, error) {
	switch x.courseType {
	case TYPE_STAGE_TASK:
		return x.loadStageTaskItems()
	case TYPE_SCHEDULE_LESSON:
		return x.loadScheduleLessonItems()
	case TYPE_OPEN_COURSE:
		return x.loadOpenCourseItems()
	case TYPE_STUDY_PLAN:
		return x.loadStudyPlanItems()
	default:
		return nil, nil
	}
}

func (x *hqwxCtx) loadStageTaskItems() ([]hqwxItem, error) {
	stages, err := x.requestStages()
	if err != nil {
		return nil, err
	}
	var out []hqwxItem
	for si, stage := range stages {
		stageName := firstNonEmpty(str(stage["stageName"]), fmt.Sprintf("阶段%d", si+1))
		stageKey := cleanName(fmt.Sprintf("{%d}--%s", si+1, stageName))
		tasks, err := x.requestStageTasks(str(stage["stage"]))
		if err != nil {
			return nil, err
		}
		_ = stageKey
		for _, group := range tasks {
			for ci, result := range listMaps(group["result"]) {
				chapterName := firstNonEmpty(str(result["chapterName"]), "未分类")
				chapterKey := cleanName(fmt.Sprintf("{%d}--%s", ci+1, chapterName))
				_ = chapterKey
				videoNo := 0
				seen := map[string]bool{}
				for _, item := range listMaps(result["list"]) {
					objName := str(item["objName"])
					prefix := fmt.Sprintf("(%d.%d.%d)--%s", si+1, ci+1, videoNo+1, objName)
					appendMaterialItem(&out, item, prefix, seen)
					if !isVideoTask(item) {
						continue
					}
					videoNo++
					name := fmt.Sprintf("[%d.%d.%d]--%s", si+1, ci+1, videoNo, objName)
					live := asMap(item["resourceLive"])
					video := hqwxItem{
						Kind:          "video",
						Name:          cleanName(name),
						ResourceID:    str(item["resourceId"]),
						PlaybackID:    str(live["playbackResIds"]),
						SubtitleResID: pickSubtitleResID(item),
						Raw:           item,
					}
					out = append(out, video)
				}
			}
		}
	}
	return out, nil
}

func (x *hqwxCtx) loadScheduleLessonItems() ([]hqwxItem, error) {
	schedules, err := x.requestSchedules()
	if err != nil {
		return nil, err
	}
	var out []hqwxItem
	for si, schedule := range schedules {
		scheduleName := firstNonEmpty(str(schedule["name"]), "未分类目录")
		_ = cleanName(fmt.Sprintf("{%d}--%s", si+1, scheduleName))
		stages := collectScheduleStages(schedule)
		for gi, stage := range stages {
			stageName := firstNonEmpty(str(stage["name"]), "未分类阶段")
			_ = cleanName(fmt.Sprintf("{%d}--%s", gi+1, stageName))
			lessons, err := x.requestLessons(str(stage["stageId"]), str(schedule["scheduleId"]))
			if err != nil {
				return nil, err
			}
			videoNo := 0
			seen := map[string]bool{}
			for _, lesson := range lessons {
				if !isVideoLesson(lesson) {
					continue
				}
				videoNo++
				lessonName := firstNonEmpty(str(lesson["name"]), str(lesson["objName"]), fmt.Sprintf("lesson%d", videoNo))
				videoName := cleanName(fmt.Sprintf("[%d.%d.%d]--%s", si+1, gi+1, videoNo, lessonName))
				prefix := fmt.Sprintf("(%d.%d.%d)--%s", si+1, gi+1, videoNo, lessonName)
				appendMaterialItem(&out, lesson, prefix, seen)
				videoURL := str(lesson["hdUrl"])
				resource := lesson
				if videoURL == "" {
					infos := listMaps(asMap(lesson["liveDetail"])["videoInfos"])
					if len(infos) > 0 {
						resource = infos[0]
						videoURL = pickVideoURL(resource)
					}
				}
				appendVideoItem(&out, videoName, videoURL, lesson, resource)
			}
		}
	}
	return out, nil
}

func collectScheduleStages(schedule map[string]any) []map[string]any {
	seen := map[string]bool{}
	var stages []map[string]any
	add := func(stage map[string]any) {
		id := str(stage["stageId"])
		if id != "" && !seen[id] {
			seen[id] = true
			stages = append(stages, stage)
		}
	}
	for _, stage := range listMaps(schedule["stages"]) {
		add(stage)
	}
	for _, group := range listMaps(schedule["stageGroups"]) {
		for _, stage := range listMaps(group["stages"]) {
			add(stage)
		}
	}
	return stages
}

func (x *hqwxCtx) loadOpenCourseItems() ([]hqwxItem, error) {
	lastLog, err := x.requestLastVideoLog()
	if err != nil || len(lastLog) == 0 {
		return nil, err
	}
	categoryID := str(lastLog["category_id"])
	orderID := firstNonEmpty(str(lastLog["order_id"]), str(x.course["buyOrderId"]))
	buyType := firstNonEmpty(str(lastLog["buy_type"]), str(x.course["buyType"]), "3")
	products, err := x.requestOpenProducts(categoryID, orderID, buyType)
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		products = []map[string]any{{"objId": lastLog["course_id"], "name": lastLog["product_name"]}}
	}
	return x.loadOpenLikeProducts(products)
}

func (x *hqwxCtx) loadStudyPlanItems() ([]hqwxItem, error) {
	categories, err := x.requestPlanCategories()
	if err != nil {
		return nil, err
	}
	var out []hqwxItem
	for ci, category := range categories {
		categoryID := firstNonEmpty(str(category["category"]), str(category["categoryId"]))
		categoryName := firstNonEmpty(str(category["categoryName"]), str(category["categoryFullName"]), fmt.Sprintf("category%d", ci+1))
		groups, err := x.requestPlanGroups(category)
		if err != nil {
			return nil, err
		}
		if len(groups) == 0 {
			groups = []map[string]any{{"groupName": "", "productList": []any{map[string]any{"productId": x.productID, "name": categoryName}}}}
		}
		for gi, group := range groups {
			groupName := str(group["groupName"])
			products := listMaps(group["productList"])
			if len(products) == 0 {
				products = []map[string]any{{"productId": x.productID, "name": groupName}}
			}
			items, err := x.loadPlanLikeProducts(products, categoryID)
			if err != nil {
				return nil, err
			}
			prefix := cleanName(fmt.Sprintf("{%d}--%s", ci+1, categoryName))
			if groupName != "" {
				prefix = cleanName(fmt.Sprintf("{%d}--%s--%s", gi+1, groupName, categoryName))
			}
			_ = prefix
			out = append(out, items...)
		}
	}
	return out, nil
}

func (x *hqwxCtx) loadPlanLikeProducts(products []map[string]any, categoryID string) ([]hqwxItem, error) {
	var out []hqwxItem
	for pi, product := range products {
		productID := firstNonEmpty(str(product["objId"]), str(product["productId"]), str(product["id"]))
		productName := firstNonEmpty(str(product["name"]), str(product["objName"]), productID)
		if productID == "" {
			continue
		}
		lessons, err := x.requestPlanLessons(productID, categoryID)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		videoNo := 0
		for _, row := range lessons {
			lesson := asMap(row["lesson"])
			if len(lesson) == 0 {
				lesson = row
			}
			lessonName := firstNonEmpty(str(lesson["name"]), str(lesson["objName"]), fmt.Sprintf("lesson%d", videoNo+1))
			typ := str(lesson["type"])
			if typ == "0" || typ == "1" {
				videoNo++
				prefix := fmt.Sprintf("(%d.%d)--%s", pi+1, videoNo, lessonName)
				videoName := cleanName(fmt.Sprintf("[%d.%d]--%s", pi+1, videoNo, lessonName))
				x.appendPlanLessonItems(&out, lesson, prefix, videoName, seen)
				continue
			}
			prefix := fmt.Sprintf("(%d.%d)--%s", pi+1, len(out)+1, lessonName)
			x.appendPlanLessonItems(&out, lesson, prefix, cleanName(fmt.Sprintf("[%d.%d]--%s", pi+1, len(out)+1, productName)), seen)
		}
	}
	return out, nil
}

func (x *hqwxCtx) loadOpenLikeProducts(products []map[string]any) ([]hqwxItem, error) {
	var out []hqwxItem
	for pi, product := range products {
		productID := firstNonEmpty(str(product["objId"]), str(product["productId"]), str(product["id"]))
		productName := firstNonEmpty(str(product["name"]), str(product["objName"]), productID)
		if productID == "" {
			continue
		}
		lessons, err := x.requestOpenLessons(productID)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		videoNo := 0
		for _, row := range lessons {
			lesson := asMap(row["lesson"])
			if len(lesson) == 0 {
				lesson = row
			}
			lessonName := firstNonEmpty(str(lesson["name"]), str(lesson["objName"]), fmt.Sprintf("lesson%d", videoNo+1))
			typ := str(lesson["type"])
			if typ == "0" || typ == "1" {
				videoNo++
				prefix := fmt.Sprintf("(%d.%d)--%s", pi+1, videoNo, lessonName)
				videoName := cleanName(fmt.Sprintf("[%d.%d]--%s", pi+1, videoNo, lessonName))
				x.appendPlanLessonItems(&out, lesson, prefix, videoName, seen)
				continue
			}
			prefix := fmt.Sprintf("(%d.%d)--%s", pi+1, len(out)+1, lessonName)
			x.appendPlanLessonItems(&out, lesson, prefix, cleanName(fmt.Sprintf("[%d.%d]--%s", pi+1, len(out)+1, productName)), seen)
		}
	}
	return out, nil
}

func (x *hqwxCtx) appendPlanLessonItems(out *[]hqwxItem, lesson map[string]any, prefix, videoName string, seen map[string]bool) {
	appendMaterialItem(out, lesson, prefix, seen)
	switch str(lesson["type"]) {
	case "0":
		videoInfo := asMap(lesson["videoInfo"])
		appendMaterialItem(out, videoInfo, prefix, seen)
		videoURL := pickVideoURL(videoInfo)
		if videoURL != "" {
			appendVideoItem(out, videoName, videoURL, lesson, videoInfo)
		}
	case "1":
		liveInfo := asMap(lesson["liveInfo"])
		appendMaterialItem(out, liveInfo, prefix, seen)
		playbacks := listMaps(liveInfo["playbackResList"])
		for i, playback := range playbacks {
			appendMaterialItem(out, playback, prefix, seen)
			url := pickVideoURL(playback)
			if url == "" {
				continue
			}
			name := videoName
			if len(playbacks) > 1 {
				name = cleanName(fmt.Sprintf("%s-%d", videoName, i+1))
			}
			appendVideoItem(out, name, url, lesson, playback)
		}
	}
}
