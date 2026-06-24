# xiwang 源码对齐对照

## URL 常量

| .cdc.py / .das 行 | xiwang.go 行/名 | 一致? |
|---|---|---|
| Xiwang_Base.py:32 `referer = 'https://www.xiwang.com'` | xiwang.go `refererURL` | ✓ |
| Xiwang_Base.py:262 `https://api.xue.xiwang.com/login/V1/Web/checkLogin?X-Businessline-Id=30` | xiwang.go `checkLoginURL` | ✓ |
| Xiwang_Course.py:58 `course_url = 'https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList'` | xiwang.go `courseURL` | ✓ |
| Xiwang_Course.py:59 `info_url = 'https://i.bcc.xiwang.com/icenter-go/App/StudyCenter/MyPlans/planListV2'` | xiwang.go `infoURL` | ✓ |
| Xiwang_Course.py:60 `video_play_url = 'https://studentlive.bcc.xiwang.com/v1/student/classroom/playback/enter'` | xiwang.go `videoPlayURL` | ✓ |
| Xiwang_Course.py:61 `live_play_url = 'https://lecturepie.bcc.xiwang.com/v1/student/classroom/playback/enter'` | xiwang.go `livePlayURL` | ✓ |
| Xiwang_Course.py:62 `m3u8_play_url = 'https://gslbsaturnbcc.saasw.vdyoo.com/v1/player/vodshow?appid={app_id}&fid={fid}&bid={bid}'` | xiwang.go `m3u8PlayURL` | ✓ |
| Xiwang_Course.py:63 `ppt_list_url = 'https://studentlive.bcc.xiwang.com/v1/student/note/getTeacherNoteListV2?bizId=3&planId={plan_id}'` | xiwang.go `pptListURL` | ✓ |
| Xiwang_Course.py:64 `file_url = 'https://i.bcc.xiwang.com/icenter/App/StudyCenter/MyPlans/getDatumListByType'` | xiwang.go `fileURL` | ✓ |
| Xiwang_Course.py:65 `price_url = 'https://api.xue.xiwang.com/mall/detail/1/{cid}'` | xiwang.go `priceURL` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 | method | 一致? |
|---|---|---|---|
| Xiwang_Base._check_cookie lines 255-274 | xiwang.go `Extract` checkLogin | GET | ✓ |
| Xiwang_Course._get_course_list lines 435-490 | xiwang.go `fetchCourses` | POST form + JSON | ✓ |
| Xiwang_Course._get_infos lines 566-604 | xiwang.go `fetchLessons` | POST form + JSON | ✓ |
| Xiwang_Course._get_video_url from .das | xiwang.go `resolveLesson` | POST form + JSON | ✓ |
| Xiwang_Course._get_m3u8_urls from .das | xiwang.go `m3u8URLs` | GET + JSON | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 解析 | 一致? |
|---|---|---|
| `_check_cookie` regex `"stat"\s*:\s*1` | xiwang.go `loginOKRe` | ✓ |
| `_get_course_list`: `result.data.learningCourses`, `endedCourses` | xiwang.go `listUnder(root, "learningCourses")`, `endedCourses` | ✓ |
| course keys `stuCouId`, `type`, `courseId`, `courseName` | xiwang.go `fetchCourses` | ✓ |
| `_get_infos`: `result.data.list[].planName`, `planId` | xiwang.go `fetchLessons` | ✓ |
| `_get_video_url`: `data.configs.beforeClassFileId`, `afterClassFileId`, `videoFile`, `appId`; `data.planInfo.liveTypeId` | xiwang.go `resolveLesson` | ✓ |
| `_get_m3u8_urls`: `content.addrs[].addr` | xiwang.go `m3u8URLs` | ✓ |

## 阻塞步骤

无. `_get_video_url` 的 .cdc.py 反编译在函数头截断, 已按同目录 `.das` 常量和字节码控制流对齐.
