package hqwx

import "fmt"

func (x *hqwxCtx) requestCourseList() ([]map[string]any, error) {
	if len(x.courseList) > 0 {
		return x.courseList, nil
	}
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["viewStatus"] = "1"
	params["rows"] = "200"
	params["goodsType"] = "0"
	params["from"] = "0"
	var resp listResp
	if _, err := x.loadJSONGet(url_course_list, params, nil, &resp); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, item := range resp.Data.DataList {
		id := str(item["goodsId"])
		if id != "" && !seen[id] {
			seen[id] = true
			x.courseList = append(x.courseList, item)
		}
	}
	return x.courseList, nil
}

func (x *hqwxCtx) findCourseByID(cid string) map[string]any {
	courses, _ := x.requestCourseList()
	for _, course := range courses {
		if str(course["goodsId"]) == cid || str(course["oneProductId"]) == cid || str(course["productId"]) == cid {
			return course
		}
	}
	return nil
}

func (x *hqwxCtx) detectCourseType() (string, error) {
	if x.productID != "" {
		stages, err := x.requestStages()
		if err != nil {
			return "", err
		}
		if len(stages) > 0 {
			return TYPE_STAGE_TASK, nil
		}
	}
	if x.goodsID != "" {
		schedules, err := x.requestSchedules()
		if err != nil {
			return "", err
		}
		if len(schedules) > 0 {
			return TYPE_SCHEDULE_LESSON, nil
		}
	}
	if x.productID != "" {
		x.stageCache = nil
		stages, err := x.requestStages()
		if err != nil {
			return "", err
		}
		if len(stages) > 0 {
			return TYPE_STAGE_TASK, nil
		}
	}
	if x.goodsID != "" {
		log, err := x.requestLastVideoLog()
		if err != nil {
			return "", err
		}
		if str(log["course_id"]) != "" {
			return TYPE_OPEN_COURSE, nil
		}
	}
	if x.goodsID != "" {
		cats, err := x.requestPlanCategories()
		if err != nil {
			return "", err
		}
		if len(cats) > 0 {
			return TYPE_STUDY_PLAN, nil
		}
	}
	return TYPE_UNKNOWN, nil
}

func (x *hqwxCtx) requestStages() ([]map[string]any, error) {
	if len(x.stageCache) > 0 {
		return x.stageCache, nil
	}
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["type"] = "0"
	params["productId"] = x.productID
	params["categoryId"] = DEFAULT_CATEGORY_ID
	var resp arrayResp
	if _, err := x.loadJSONGet(url_stages, params, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Success {
		x.stageCache = resp.Data
	}
	return x.stageCache, nil
}

func (x *hqwxCtx) requestStageTasks(stage string) ([]map[string]any, error) {
	params := x.baseParams()
	params["privateCampType"] = "0"
	params["stage"] = stage
	params["categoryId"] = DEFAULT_CATEGORY_ID
	params["productId"] = x.productID
	headers := cloneHeaders(x.headers)
	headers["Content-Type"] = "application/x-www-form-urlencoded"
	var resp arrayResp
	if _, err := x.loadJSONPost(url_stage_tasks, params, headers, &resp); err != nil {
		return nil, err
	}
	if resp.Success {
		return resp.Data, nil
	}
	return nil, nil
}

func (x *hqwxCtx) requestSchedules() ([]map[string]any, error) {
	if len(x.schedCache) > 0 {
		return x.schedCache, nil
	}
	params := x.baseParams()
	params["goodsId"] = x.goodsID
	params["_t"] = nowMillis()
	var resp arrayResp
	if _, err := x.loadJSONGet(url_schedules, params, x.adminAPIHeaders(), &resp); err != nil {
		return nil, err
	}
	if codeOK(resp.Code) {
		x.schedCache = resp.Data
	}
	return x.schedCache, nil
}

func (x *hqwxCtx) requestLessons(stageID, scheduleID string) ([]map[string]any, error) {
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["scheduleId"] = scheduleID
	params["stageId"] = stageID
	params["goodsId"] = x.goodsID
	var resp arrayResp
	if _, err := x.loadJSONGet(url_lessons, params, x.adminAPIHeaders(), &resp); err != nil {
		return nil, err
	}
	if codeOK(resp.Code) {
		return resp.Data, nil
	}
	return nil, nil
}

func (x *hqwxCtx) requestCourseDetail() (map[string]any, error) {
	if len(x.detail) > 0 {
		return x.detail, nil
	}
	if x.goodsID == "" {
		return map[string]any{}, nil
	}
	params := x.baseParams()
	params["goodsId"] = x.goodsID
	params["orderId"] = x.courseOrderID()
	params["_t"] = nowMillis()
	var resp objectResp
	if _, err := x.loadJSONGet(url_course_detail, params, nil, &resp); err != nil {
		return nil, err
	}
	if jsonSuccess(resp.Success, resp.Code, resp.Status) {
		x.detail = resp.Data
	}
	return x.detail, nil
}

func (x *hqwxCtx) requestPlanCategories() ([]map[string]any, error) {
	if len(x.planCats) > 0 {
		return x.planCats, nil
	}
	if x.goodsID == "" {
		return nil, nil
	}
	if _, err := x.requestCourseDetail(); err != nil {
		return nil, err
	}
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["withToken"] = "true"
	params["goodsBusinessType"] = x.courseGoodsBusinessType()
	params["buyType"] = x.courseBuyType()
	params["orderId"] = x.courseOrderID()
	params["goodsId"] = x.goodsID
	var resp arrayResp
	if _, err := x.loadJSONGet(url_goods_plan_categories, params, nil, &resp); err != nil {
		return nil, err
	}
	if jsonSuccess(resp.Success, resp.Code, resp.Status) {
		x.planCats = resp.Data
	}
	return x.planCats, nil
}

func (x *hqwxCtx) requestPlanGroups(category map[string]any) ([]map[string]any, error) {
	categoryID := firstNonEmpty(str(category["category"]), str(category["categoryId"]))
	if categoryID == "" {
		return nil, nil
	}
	if cached, ok := x.planGroups[categoryID]; ok {
		return cached, nil
	}
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["withToken"] = "true"
	if str(category["hideElective"]) != "" && str(category["hideElective"]) != "0" && str(category["hideElective"]) != "false" {
		params["electiveShowFlag"] = "0"
	} else {
		params["electiveShowFlag"] = "1"
	}
	params["buyType"] = x.courseBuyType()
	params["orderId"] = x.courseOrderID()
	params["goodsId"] = x.goodsID
	params["categoryId"] = categoryID
	var resp arrayResp
	if _, err := x.loadJSONGet(url_category_plan, params, nil, &resp); err != nil {
		return nil, err
	}
	if jsonSuccess(resp.Success, resp.Code, resp.Status) {
		x.planGroups[categoryID] = resp.Data
	} else {
		x.planGroups[categoryID] = nil
	}
	return x.planGroups[categoryID], nil
}

func (x *hqwxCtx) requestPlanLessons(productID, categoryID string) ([]map[string]any, error) {
	productID = firstNonEmpty(productID, x.productID)
	if productID == "" {
		return nil, nil
	}
	key := fmt.Sprintf("%s:%s", categoryID, productID)
	if cached, ok := x.planLessons[key]; ok {
		return cached, nil
	}
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["withToken"] = "true"
	params["productId"] = productID
	params["goodsId"] = x.goodsID
	if categoryID != "" {
		params["categoryId"] = categoryID
	}
	var resp arrayResp
	if _, err := x.loadJSONGet(url_lesson_list_v7, params, nil, &resp); err != nil {
		return nil, err
	}
	if jsonSuccess(resp.Success, resp.Code, resp.Status) {
		x.planLessons[key] = resp.Data
	} else {
		x.planLessons[key] = nil
	}
	return x.planLessons[key], nil
}

func (x *hqwxCtx) requestVideoResource(resourceID string) (map[string]any, error) {
	if resourceID == "" {
		return nil, nil
	}
	if cached, ok := x.resource[resourceID]; ok {
		return cached, nil
	}
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["type"] = "1"
	params["id"] = resourceID
	params["categoryId"] = DEFAULT_CATEGORY_ID
	params["productId"] = x.productID
	var resp objectResp
	if _, err := x.loadJSONGet(url_resource, params, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Success && len(resp.Data) > 0 {
		x.resource[resourceID] = resp.Data
	}
	return x.resource[resourceID], nil
}

func (x *hqwxCtx) requestLivePlaybackResource(playbackID, resourceID string) (map[string]any, error) {
	if playbackID == "" {
		return nil, nil
	}
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["resourceId"] = resourceID
	params["type"] = "1"
	params["id"] = playbackID
	params["categoryId"] = DEFAULT_CATEGORY_ID
	params["productId"] = x.productID
	var resp arrayResp
	if _, err := x.loadJSONGet(url_resource_batch, params, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Success && len(resp.Data) > 0 {
		return resp.Data[0], nil
	}
	return nil, nil
}

func (x *hqwxCtx) requestSubtitleURL(resID string) (string, error) {
	if resID == "" {
		return "", nil
	}
	if cached, ok := x.subtitles[resID]; ok {
		return cached, nil
	}
	params := x.baseParams()
	params["resId"] = resID
	params["_t"] = nowMillis()
	var resp objectResp
	if _, err := x.loadJSONGet(url_subtitle, params, nil, &resp); err != nil {
		return "", err
	}
	if jsonSuccess(resp.Success, resp.Code, resp.Status) {
		x.subtitles[resID] = str(resp.Data["subtitlesUrl"])
	}
	return x.subtitles[resID], nil
}

func (x *hqwxCtx) requestLastVideoLog() (map[string]any, error) {
	params := x.baseParams()
	params["goodsId"] = x.goodsID
	params["_t"] = nowMillis()
	var resp objectResp
	if _, err := x.loadJSONGet(url_last_video_log, params, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Success {
		return resp.Data, nil
	}
	return nil, nil
}

func (x *hqwxCtx) requestOpenProducts(categoryID, orderID, buyType string) ([]map[string]any, error) {
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["electiveShowFlag"] = "1"
	params["buyType"] = buyType
	params["orderId"] = orderID
	params["goodsId"] = x.goodsID
	params["categoryId"] = categoryID
	var resp arrayResp
	if _, err := x.loadJSONGet(url_category_plan, params, nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, nil
	}
	var out []map[string]any
	for _, group := range resp.Data {
		out = append(out, listMaps(group["productList"])...)
	}
	return out, nil
}

func (x *hqwxCtx) requestOpenLessons(productID string) ([]map[string]any, error) {
	params := x.baseParams()
	params["_t"] = nowMillis()
	params["productId"] = productID
	params["goodsId"] = x.goodsID
	var resp arrayResp
	if _, err := x.loadJSONGet(url_lesson_list_v2, params, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Success {
		return resp.Data, nil
	}
	return nil, nil
}

func (x *hqwxCtx) courseOrderID() string {
	if id := firstNonEmpty(str(x.detail["orderId"]), str(x.course["orderId"]), str(x.course["buyOrderId"]), str(x.course["buyOrderIdStr"])); id != "" {
		return id
	}
	return ""
}

func (x *hqwxCtx) courseBuyType() string {
	return firstNonEmpty(str(x.detail["buyType"]), str(x.course["buyType"]), "3")
}

func (x *hqwxCtx) courseGoodsBusinessType() string {
	return firstNonEmpty(str(x.detail["goodsBusinessType"]), str(x.course["goodsBusinessType"]), "0")
}

func (x *hqwxCtx) resolveResourceInfo(item hqwxItem) (map[string]any, error) {
	if item.PlaybackID != "" {
		info, err := x.requestLivePlaybackResource(item.PlaybackID, item.ResourceID)
		if err == nil && len(info) > 0 {
			return info, nil
		}
	}
	return x.requestVideoResource(item.ResourceID)
}
