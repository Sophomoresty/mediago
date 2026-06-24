# haiyangknow 源码对齐对照

## URL 常量

| .cdc.py 行 | Go 行/名 | 一致? |
|---|---|---|
| `Haiyangknow_Base.pyc.1shot.cdc.py:33 referer = 'https://user.haiyangknow.com/'` | `haiyangknow.go:19 referer` | ✓ |
| `Haiyangknow_Base.pyc.1shot.cdc.py:34 origin = 'https://user.haiyangknow.com'` | `haiyangknow.go:20 origin` | ✓ |
| `Haiyangknow_Base.pyc.1shot.cdc.py:35 api_host = 'https://user.haiyangknow.com/prod-api'` | `haiyangknow.go:21 api_host` | ✓ |
| `Haiyangknow_Course.pyc.1shot.cdc.py:944 https://vod.{}.aliyuncs.com/?{}` | `aliyun.go:93` | ✓ |
| `Haiyangknow_Course.pyc.1shot.cdc.py:1152 https://mts.{}.aliyuncs.com/?` | `aliyun.go:228` | ✓ |

## 认证与 Cookie

| 源码方法 (line) | Go 函数 (line) | 一致? |
|---|---|---|
| `Haiyangknow_Base._extract_token` line 17019 | `extractToken` line 33 | ✓ |
| `Haiyangknow_Base._request_json` line 17022 | `requestJSON` line 136 | ✓ |
| `Haiyangknow_Base._request_api_data` line 17025 | `requestAPIData` line 189 | ✓ |
| `Haiyangknow_Base._check_cookie` line 17028 | `checkCookie` line 200 | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---:|---|
| `Haiyangknow_Course._get_course_list` line 17118 | `getCourseList` line 217 + `requestAPIData` | GET | ✓ |
| `Haiyangknow_Course._request_course_page` line 17115 | `requestAPIData` line 189 | GET | ✓ |
| `Haiyangknow_Course._extract_url_course_id` line 17124 | `extractURLCourseID` line 298 | URL parse + regex | ✓ |
| `Haiyangknow_Course._request_group_list` line 17148 | `requestGroupList` line 37 | GET | ✓ |
| `Haiyangknow_Course._request_chapter_list` line 17151 | `requestChapterList` line 50 | GET | ✓ |
| `Haiyangknow_Course._request_lesson_play_info` line 17178 | `requestLessonPlayInfo` line 126 | GET | ✓ |
| `Haiyangknow_Course._request_aliyun_play_info` line 17184 | `requestAliyunPlayInfo` line 76 | GET | ✓ |
| `Haiyangknow_Course._request_aliyun_license` line 17205 | `requestAliyunLicense` line 223 | POST form | ✓ |

## JSON / XML 字段映射

| 源码 key 链 | Go 解析 | 一致? |
|---|---|---|
| `code -> data` | `requestAPIData` / `checkCookie` | ✓ |
| `records/list/rows/data/result/groupList/chapterList/courseGroupList/courseChapterList` | `extractRecords` line 70 | ✓ |
| `courseId/course_id/draftId/draft_id/curriculumId` | `extractURLCourseID` line 298 + `normalizeCourseInfo` line 265 | ✓ |
| `dyCourseOrderId / ksCourseOrderId / wxCourseOrderId` | `resolvePlatformType` line 273 | ✓ |
| `playAuth/playauth, videoId/vid, regionId` | `decodeAliyunPlayAuth` line 49 + `aliyunSource` line 20 | ✓ |
| `PlayInfoList.PlayInfo.PlayURL/PlayUrl/Definition/Format/Bitrate/Encrypt/EncryptType` | `extractAliyunPlayResponse` line 115 | ✓ |
| `License/license/Data.data` | `requestAliyunLicense` line 223 | ✓ |

## 阻塞步骤

无。阿里云 VOD 解析与 GetLicense 已按源码签名链实现, 课程树与 lesson/material 解析也已落地。
