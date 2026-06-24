# gongxuanwang 源码对齐对照

## URL 常量

| .cdc.py 行 | Go 行/名 | 一致? |
|---|---|---|
| `Gongxuanwang_Base.pyc.1shot.cdc.py:30 referer = 'https://www.gongxuanwang.com/'` | `gongxuanwang.go:21 referer` | ✓ |
| `Gongxuanwang_Base.pyc.1shot.cdc.py:31 user_info_api = 'https://newedu.gongxuanwang.com/api/v1/pc/getuserinfo'` | `gongxuanwang.go:22 user_info_api` | ✓ |
| `Gongxuanwang_Base.pyc.1shot.cdc.py:32 lms_course_list_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageMicroLessonCourse'` | `gongxuanwang.go:23 lms_course_list_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:35 lms_course_detail_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/microLessonCourseDetail?courseSkuId={}'` | `gongxuanwang.go:24 lms_course_detail_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:36 lms_period_vid_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/getWebSectionPeriodVidVO?courseSkuId={}'` | `gongxuanwang.go:25 lms_period_vid_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:37 lms_vid_auth_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webLive/getVidAuthorization?userId={user_id}&vid={vid}'` | `gongxuanwang.go:26 lms_vid_auth_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:38 lms_price_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/info'` | `gongxuanwang.go:27 lms_price_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:39 sku_course_list_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/page'` | `gongxuanwang.go:28 sku_course_list_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:40 system_course_list_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageSystem'` | `gongxuanwang.go:29 system_course_list_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:41 system_course_detail_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/systemCourseDetail'` | `gongxuanwang.go:30 system_course_detail_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:42 system_class_course_detail_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/systemClassCourseDetail'` | `gongxuanwang.go:31 system_class_course_detail_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:43 open_course_list_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageOpenCourse'` | `gongxuanwang.go:32 open_course_list_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:44 open_course_detail_api = 'https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/getOpenCourseDetail?courseSkuId={}'` | `gongxuanwang.go:33 open_course_detail_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:45 legacy_course_list_api = 'https://newedu.gongxuanwang.com/api/v1/pc/coursemember?page={page}&prePage={page_size}'` | `gongxuanwang.go:34 legacy_course_list_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:46 legacy_course_detail_api = 'https://newedu.gongxuanwang.com/api/v1/pc/courseDetails?course_id={}'` | `gongxuanwang.go:35 legacy_course_detail_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:47 legacy_play_api = 'https://newedu.gongxuanwang.com/api/v1/pc/courseshow?type=live&media_id={media_id}&id={lesson_id}'` | `gongxuanwang.go:36 legacy_play_api` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:49 polyv_secure_url = 'https://player.polyv.net/secure/{vid}.js'` | `gongxuanwang.go:37 polyv_secure_url` | ✓ |
| `Gongxuanwang_Course.pyc.1shot.cdc.py:50 polyv_key_url = 'https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}'` | `gongxuanwang.go:38 polyv_key_url` | ✓ |

## 认证与 Cookie

| 源码方法 (line) | Go 函数 (line) | 一致? |
|---|---|---|
| `Gongxuanwang_Base._get_cookie_token` line 193 | `newCtx` line 104 + `cookieValue(..., "edu_token")` | ✓ |
| `Gongxuanwang_Base._apply_token_headers` line 216 | `newCtx` line 112-120 | ✓ |
| `Gongxuanwang_Base._check_cookie` lines 258-325 | `newCtx` line 107-122 + `getUserID` line 306 | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| `Gongxuanwang_Course._get_cid` line 500 | `parseCourseRef` line 173 | URL parse + regex | ✓ |
| `Gongxuanwang_Course._get_infos` line 584 | `loadInfos` line 8 | 选择 open/lms/system/sku/legacy | ✓ |
| `Gongxuanwang_Course._get_lms_infos` line 843 | `getLMSInfosForCID` line 54 | GET JSON | ✓ |
| `Gongxuanwang_Course._get_open_infos` line 975 | `getOpenInfos` line 123 | GET JSON | ✓ |
| `Gongxuanwang_Course._get_system_infos` line 895 | `getSystemInfos` line 141 | POST JSON + GET JSON | ✓ |
| `Gongxuanwang_Course._get_sku_infos` line 792 | `getSKUInfos` line 182 | GET JSON + GET subcourse detail | ✓ |
| `Gongxuanwang_Course._get_legacy_infos` line 819 | `getLegacyInfos` line 218 | GET JSON | ✓ |
| `Gongxuanwang_Course._get_user_info` line 966 | `getUserID` line 306 | GET JSON | ✓ |
| `Gongxuanwang_Course._get_lms_play_info` line 1052 | `getLMSPlayInfo` line 278 | GET JSON | ✓ |
| `Gongxuanwang_Course._get_legacy_play_info` line 1066 | `getLegacyPlayInfo` line 294 | GET JSON | ✓ |
| `Gongxuanwang_Course._get_polyv_m3u8` line 1008 | `resolveVideo` line 238 + `shared.PolyvResolveSecure` / `shared.PolyvRewriteM3U8Keys` | GET JSON + GET m3u8 | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 解析 | 一致? |
|---|---|---|
| `data.courseSkuName` | `getLMSInfosForCID` line 64 | ✓ |
| `data.webCourseSectionVOS -> webCoursePeriodVOS -> webCourseWareVO/webCourseWareVOS` | `getLMSInfosForCID` line 66-87 | ✓ |
| `periodVids[].periodId / vid` | `getLMSPeriodVidMap` line 93-105 | ✓ |
| `courseSkuName/title/recordedVid/vid/classId/timeArrangeId` | `getOpenInfos` line 123-138 | ✓ |
| `data.rows / records / list / data / course` | `extractRowsData` line 199-210 | ✓ |
| `courseContentStageDTOS / courseOutlineVO / courseContentDTOS / strategyPackageDTOList / skuGiftDTOS / webClassPackageContentDTOList` | `extractSKUChildCourses` line 213-227 | ✓ |
| `course_info.title` | `getLegacyInfos` line 227-238 | ✓ |
| `courseLesson_info -> lesson/vidsPlus/mediaId/lessonId` | `legacyLessonList` line 230-242 | ✓ |
| `playUrl/url/m3u8/videoId/vid` | `resolveVideo` line 247-249 | ✓ |

## 阻塞步骤

无。`polyv_secure_url` / `polyv_key_url` 常量保留在站包中, 但实际 Polyv 取流按仓库规则委托 `internal/extractor/shared/polyv.go`。
