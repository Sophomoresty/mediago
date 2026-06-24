# hqwx 源码对齐对照

## URL 常量

| .cdc.py 行 | hqwx.go 行/名 | 一致? |
|---|---|---|
| Hqwx_Base.py:32 `referer = 'https://user.hqwx.com/'` | hqwx.go:18 `referer` | ✓ |
| Hqwx_Base.py:33 `check_url = 'https://japi.hqwx.com/uc/study/v2/getList'` | hqwx.go:20 `check_url` | ✓ |
| Hqwx_Base.py:34 `url_course_list = 'https://japi.hqwx.com/uc/study/v2/getList'` | hqwx.go:21 `url_course_list` | ✓ |
| Hqwx_Base.py:35 `url_stages = 'https://japi.hqwx.com/al/v3/getStagesByProduct'` | hqwx.go:22 `url_stages` | ✓ |
| Hqwx_Base.py:36 `url_schedules = 'https://adminapi.hqwx.com/goods-siteapp/app/v1/course-schedules/list'` | hqwx.go:23 `url_schedules` | ✓ |
| Hqwx_Base.py:37 `url_lessons = 'https://adminapi.hqwx.com/goods-siteapp/app/v2/course-lessons/list'` | hqwx.go:24 `url_lessons` | ✓ |
| Hqwx_Base.py:38 `url_stage_tasks = 'https://japi.hqwx.com/al/v3/selfTask/getStageTasks'` | hqwx.go:25 `url_stage_tasks` | ✓ |
| Hqwx_Base.py:39 `url_resource = 'https://japi.hqwx.com/al/userKnowledge/resource'` | hqwx.go:26 `url_resource` | ✓ |
| Hqwx_Base.py:40 `url_resource_batch = 'https://japi.hqwx.com/al/userKnowledge/resourceBatch'` | hqwx.go:27 `url_resource_batch` | ✓ |
| Hqwx_Base.py:41 `url_subtitle = 'https://japi.hqwx.com/resourceVideo/getSubtitleUrl'` | hqwx.go:28 `url_subtitle` | ✓ |
| Hqwx_Base.py:42 `url_last_video_log = 'https://japi.hqwx.com/uc/study/getLastUserVideoLogByGoodsId'` | hqwx.go:29 `url_last_video_log` | ✓ |
| Hqwx_Base.py:43 `url_course_detail = 'https://japi.hqwx.com/uc/study/getCourseDetail'` | hqwx.go:30 `url_course_detail` | ✓ |
| Hqwx_Base.py:44 `url_goods_plan_categories = 'https://japi.hqwx.com/uc/study/listUserGoodsPlanTotalCategorySort'` | hqwx.go:31 `url_goods_plan_categories` | ✓ |
| Hqwx_Base.py:45 `url_category_plan = 'https://japi.hqwx.com/uc/v2/study/getCategoryStudyPlanGroupInfo'` | hqwx.go:32 `url_category_plan` | ✓ |
| Hqwx_Base.py:46 `url_lesson_list_v2 = 'https://japi.hqwx.com/uc/v2/study/getLessonList'` | hqwx.go:33 `url_lesson_list_v2` | ✓ |
| Hqwx_Base.py:47 `url_lesson_list_v7 = 'https://japi.hqwx.com/uc/v7/study/getLessonList'` | hqwx.go:34 `url_lesson_list_v7` | ✓ |
| Hqwx_Base.py:48 `url_order_list = 'https://japi.hqwx.com/buy/order/getUserOrderList?...'` | hqwx.go:35 `url_order_list` | ✓ |
| Hqwx_Base.py:49 `url_price = 'https://kjapi.98809.com/web/goods/getGoodsDetail?...'` | hqwx.go:36 `url_price` (`{}` → `%s`) | ✓ |

## 认证与基础参数

| 源码方法/行 | Go 函数/行 | 对齐点 | 一致? |
|---|---|---|---|
| Hqwx_Base.__init__ line 51-63 | `newCtx` hqwx.go:113-147 | `User-Agent`, `Accept`, `Origin`, `Referer`, `Cookie` | ✓ |
| Hqwx_Base._parse_cookie_meta line 114-130 | `newCtx` hqwx.go:125-126, `parseCookieHeader` helpers.go | `passport` / `passportCors` | ✓ |
| Hqwx_Base._base_params line 134-153 | `baseParams` hqwx.go:204-220 | `edu24ol_token`, `passport`, `platform`, `pschId`, `schId`, `v`, `_v`, `os`, `_os`, `org_id`, `_org_id`, `appid`, `_appid` | ✓ |
| Hqwx_Base._adminapi_header line 182-190 | `adminAPIHeaders` hqwx.go:269-272 | `edu24ol-token = passport` | ✓ |
| Hqwx_Config.py line 10-25 | hqwx.go:38-52 | default app/org/os/version/category/type constants | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Hqwx_Course._load_json_get line 57 | `loadJSONGet` hqwx.go:222 | GET + query encode + JSON parse | ✓ |
| Hqwx_Course._load_json_post line 77 | `loadJSONPost` hqwx.go:249 | POST form + JSON parse | ✓ |
| Hqwx_Course._request_course_list line 109 | `requestCourseList` api.go:5 | GET `url_course_list` | ✓ |
| Hqwx_Course._request_stages line 161 | `requestStages` api.go:90 | GET `url_stages` | ✓ |
| Hqwx_Course._request_stage_tasks line 176 | `requestStageTasks` api.go:109 | POST `url_stage_tasks` | ✓ |
| Hqwx_Course._request_schedules line 204 | `requestSchedules` api.go:127 | GET `url_schedules` with admin header | ✓ |
| Hqwx_Course._request_lessons line 230 | `requestLessons` api.go:144 | GET `url_lessons` with admin header | ✓ |
| Hqwx_Course._request_course_detail line 173 | `requestCourseDetail` api.go:160 | GET `url_course_detail` | ✓ |
| Hqwx_Course._request_plan_categories line 190 | `requestPlanCategories` api.go:181 | GET `url_goods_plan_categories` | ✓ |
| Hqwx_Course._request_plan_groups line 210 | `requestPlanGroups` api.go:208 | GET `url_category_plan` | ✓ |
| Hqwx_Course._request_plan_lessons line 231 | `requestPlanLessons` api.go:240 | GET `url_lesson_list_v7` | ✓ |
| Hqwx_Course._request_video_resource line 401 | `requestVideoResource` api.go:269 | GET `url_resource` | ✓ |
| Hqwx_Course._request_live_playback_resource line 425 | `requestLivePlaybackResource` api.go:292 | GET `url_resource_batch` | ✓ |
| Hqwx_Course._request_subtitle_url line 445 | `requestSubtitleURL` api.go:313 | GET `url_subtitle` | ✓ |
| Hqwx_Course._request_last_video_log line 475 | `requestLastVideoLog` api.go:333 | GET `url_last_video_log` | ✓ |
| Hqwx_Course._request_open_products line 490 | `requestOpenProducts` api.go:347 | GET `url_category_plan` | ✓ |
| Hqwx_Course._request_open_lessons line 516 | `requestOpenLessons` api.go:369 | GET `url_lesson_list_v2` | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct tag / map key | 一致? |
|---|---|---|
| `result.get('success')`, `result.get('code')`, `result.get('status',{}).get('code')` | `Success json:"success"`, `Code json:"code"`, `Status.Code json:"code"` in types.go:3-29 | ✓ |
| `result.get('data',{}).get('dataList',[])` | `Data.DataList json:"dataList"` in types.go:9-11 | ✓ |
| `_request_course_list`: `goodsId`, `goodsName`, `oneProductId` | api.go:20-24, hqwx.go:155-165 | ✓ |
| `_get_cid`: regex groups `goods_id`, `product_id`, `goods_id2`, `product_id2` | hqwx.go:55-61, 185-201 | ✓ |
| `_request_stages`: `data`, `stageName`, `stage` | api.go:99-105, flows.go:26-30 | ✓ |
| `_request_stage_tasks`: `data[].result[].chapterName`, `list[].objName`, `resourceId`, `resourceLive.playbackResIds` | flows.go:34-60 | ✓ |
| `_request_schedules`: `code == 0`, `data[].stages`, `data[].stageGroups[].stages`, `stageId`, `scheduleId` | api.go:127-157, flows.go:73-129 | ✓ |
| `_is_video_lesson`: `relationType`, `hdUrl`, `liveDetail.videoInfos` | media.go:69-77 | ✓ |
| `_pick_video_url`: `hdurl/mdurl/sdurl`, `hdUrl/mdUrl/sdUrl`, `hd_url/md_url/sd_url`, `mediaInfos`, `downloadUrl`, `download_url` | media.go:17-27 | ✓ |
| `_pick_media_url`: `fhd_m3u8`, `hd_m3u8`, `sd_m3u8`, `url` | media.go:8-15 | ✓ |
| `_make_material_item`: `materialDownloadUrl`, `materialUrl`, `materialFileName`, `materialName`, `materialFile`, `materialInfo`, `additionFile`, `htmlFileResourceDto` | media.go:79-100 | ✓ |
| `_request_plan_categories`: `goodsId`, `orderId`, `buyType`, `goodsBusinessType`, `withToken` | api.go:181-205 | ✓ |
| `_request_plan_groups`: `category/categoryId`, `hideElective`, `productList` | api.go:208-238, flows.go:156-181 | ✓ |
| `_request_plan_lessons`: `productId`, `goodsId`, `categoryId`, `data` | api.go:240-267 | ✓ |
| `_append_plan_lesson_items`: `lesson.type`, `videoInfo`, `liveInfo.playbackResList`, `id` | flows.go:222-249, media.go:112-125 | ✓ |
| `_request_video_resource`: `data.mediaInfos` and URL fields | api.go:269-289, media.go:17-27 | ✓ |
| `_request_live_playback_resource`: `data[0]` | api.go:292-310 | ✓ |
| `_request_subtitle_url`: `data.subtitlesUrl` | api.go:313-330 | ✓ |
| `_get_infos`: `TYPE_STAGE_TASK`, `TYPE_SCHEDULE_LESSON`, `TYPE_OPEN_COURSE`, `TYPE_STUDY_PLAN` | flows.go:5-17 | ✓ |

## 返回结构

| 源码行为 | Go 行 | 一致? |
|---|---|---|
| 多课节写入 `self.infos[...] = items` 后下载 video/file 列表 | hqwx.go:275-287 converts collected items to `MediaInfo.Entries` | ✓ |
| video 条目带 `video_url` 或 `resource_id`/`playback_id`, 下载时再解析资源 | hqwx.go:290-301, api.go:399-407 | ✓ |
| file/material 条目带 `type=file`, `name`, `url` | media.go:79-110, hqwx.go:309-318 | ✓ |

## 阻塞步骤

无。
