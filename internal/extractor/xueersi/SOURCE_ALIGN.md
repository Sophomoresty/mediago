# xueersi 源码对齐对照

## URL 常量

| .cdc.py / .das 行 | xueersi.go 行/名 | 一致? |
|---|---|---|
| Xueersi_Base.py:30 `referer = 'https://www.xueersi.com/'` | xueersi.go:18 `refererURL` | ✓ |
| Xueersi_Base.py:31 `login_check_url = 'https://api.xueersi.com/login/V1/Web/checkLogin?X-Businessline-Id=10'` | xueersi.go:19 `loginCheckURL` | ✓ |
| Xueersi_Course.py:33 `course_list_api = 'https://i.xueersi.com/janus/App/StudyCenter/v2/courseList'` | xueersi.go:20 `courseListAPI` | ✓ |
| Xueersi_Course.py:34 `backup_course_api = 'http://i.xueersi.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList'` | xueersi.go:21 `backupCourseAPI` | ✓ |
| Xueersi_Course.py:35 `plan_list_api = 'http://i.xueersi.com/icenter-go/App/StudyCenter/MyPlans/planListV2'` | xueersi.go:22 `planListAPI` | ✓ |
| Xueersi_Course.py:36 `playback_api = 'http://studentlive.xueersi.com/v1/student/classroom/playback/enter'` | xueersi.go:23 `playbackAPI` | ✓ |
| Xueersi_Course.py:37 `price_detail_urls = ('.../1/{cid}', '.../10/{cid}', '.../2/{cid}')` | xueersi.go:24-26 `priceDetailURL*` | ✓ |
| Xueersi_Course.das `_get_recording_m3u8` `https://studentlive.xueersi.com/v1/student/classroom/drama/get` | xueersi.go:27 `dramaGetURL` | ✓ |
| Xueersi_Course.das `_get_live_m3u8` `https://gslbsaturn.xescdn.com/v2/vodshow?...bid=7&uri={}` | xueersi.go:28 `liveVodshowURL` | ✓ |
| Xueersi_Course.das `_get_recording_m3u8` `https://gslbsaturn.xescdn.com/v1/player/vodshow?...bid=68&uri={}` | xueersi.go:29 `recordingVodshowURL` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| `Xueersi_Base._check_login_cookie` t138 / `request_get(login_check_url)` | xueersi.go:52-65 `Extract` | GET + regex | ✓ |
| `Xueersi_Course._get_course_list` t342 | xueersi.go:101-121 `fetchCourses` | POST JSON + POST form | ✓ |
| `Xueersi_Course._get_plan_list` t448 | xueersi.go:123-149 `fetchPlans` | POST form + JSON | ✓ |
| `Xueersi_Course._get_video_m3u8` t621 | xueersi.go:151-169 `getVideoM3U8` | POST JSON + JSON | ✓ |
| `Xueersi_Course._get_live_m3u8` das:4941 | xueersi.go:171-178 `liveM3U8` + `vodshow` | GET + JSON | ✓ |
| `Xueersi_Course._get_recording_m3u8` das:5142 | xueersi.go:180-214 `recordingM3U8` + `vodshow` | POST JSON, GET + JSON | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 解析 | 一致? |
|---|---|---|
| `_check_login_cookie`: regex `"stat"\s*:\s*1` | xueersi.go:35, 60-65 | ✓ |
| `_extract_v2_courses`: `data.learningCourses.courseList`, `pendingCourses.courseList`, `endedCourses.courseList` | xueersi.go:257-264 | ✓ |
| `_extract_backup_courses`: `result.data.learningCourses`, `endedCourses`, `pendingCourses` | xueersi.go:265-272 | ✓ |
| course keys `courseId`, `stuCouId`, `courseName`, `type/courseType/couType`, `gradeId/grade` | xueersi.go:273-282 | ✓ |
| `_extract_plan_list`: `result.data.list` | xueersi.go:300-315 | ✓ |
| `_build_video_list`: `planId`, `planName` | xueersi.go:306-312 | ✓ |
| `_get_video_m3u8` payload: `acceptPlanVersion`, `bizId`, `planId`, `stuCouId` | xueersi.go:151-159 | ✓ |
| live: `data.configs.videoFile` / `beforeClassFileId` → `content.addrs[0].addr` | xueersi.go:171-178, 216-230 | ✓ |
| recording: `data.dramaInfo.chapters[0].chapterLogicId`, `dramaInfo.dramaId`, `planInfo.id/planId`, `data.chapters[0].sections[0].sectionResource.fid` → `content.addrs[0].addr` | xueersi.go:180-214, 216-230 | ✓ |

## 阻塞步骤

无. `.cdc.py` 在 `_find_course_by_target` 后截断, 播放链按同目录 `.das` 与 `decrypted_full/all_decrypted.json` 的 t448/t621 常量和字节码路径对齐.
