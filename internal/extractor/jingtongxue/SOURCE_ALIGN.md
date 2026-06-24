# jingtongxue 源码对齐对照

## URL 常量

| .cdc.py 行 | jingtongxue.go 行/名 | 一致? |
|---|---|---|
| Jingtongxue_Base.py:35-38 `referer/origin/domain/api_base` | jingtongxue.go:28-31 `urlReferer/urlOrigin/urlDomain/urlAPIBase` | ✓ |
| Jingtongxue_Course.py:32 `course_list_api = '/course/v1/user/get/courses'` | jingtongxue.go:32 `pathCourseList` | ✓ |
| Jingtongxue_Course.py:33-37 detail/chapter/lecture/video/play-param API | jingtongxue.go:33-37 `pathDetail/pathChapter/pathLecture/pathVideoInfo/pathPlayParam` | ✓, `{...}` -> `%s` |
| Jingtongxue_Course.py:41 `https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}` | jingtongxue.go:38 `urlBokeCCVideoAPI` and shared `BokeCCResolve` | ✓, `{vid}/{siteid}` -> `%s/%s` |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Base `_request_json` lines 332-354: relative path uses `api_base`, query adds `domain`, default GET | jingtongxue.go:139 `jtxGetJSON` | GET | ✓ |
| Base `_check_cookie` lines 421-427: `get/courses` status/pageSize/offset validates login | jingtongxue.go:205 `fetchJingtongxueCourses` | GET | ✓ |
| Course `_get_course_list` lines 288-309: status 1/2/3, `pageSize=50`, `offset` pagination | jingtongxue.go:208-241 | GET | ✓ |
| Course `_get_detail` lines 340-354: `getDetail/{commodity_id}` + `liveSet=1` | jingtongxue.go:272 `fetchJingtongxueDetail` | GET | ✓ |
| Course `_get_chapters` lines 510-522 and `_get_lectures` lines 528-543 | jingtongxue.go:292 / 339 | GET | ✓ |
| Course `_get_play_param` lines 734-746: `broswer=pc`, `lectureId`, `classTypeId`, `moduleId` | jingtongxue.go:405-413 | GET | ✓ |
| Course `_get_bokecc_video_url` lines 842-858 | jingtongxue.go:420-432 -> `shared.BokeCCResolve` | GET helper | ✓, no inline BokeCC resolver |

## JSON 字段映射

| 源码 key 链 | Go struct tag / parse | 一致? |
|---|---|---|
| response `code/success/msg/message/data` | `jtxEnvelope` tags in jingtongxue.go:166-172 | ✓ |
| course list `data.records/rows/list/items/data` | `jtxExtractRecords` keys in jingtongxue.go:476-488 | ✓ |
| course `commodityId/comId/id`, `courseId/classTypeId/classTypePo.id`, `name/courseName/title` | `jtxCourse` tags in jingtongxue.go:179-189, normalized lines 198-202 | ✓ |
| detail `classTypePo.id/name`, `buyFlag/userVipFlag/priceFlag` | `jtxDetail` tags in jingtongxue.go:262-270 | ✓ |
| chapter `id/chapterId/chapterName/name/moduleId` | `jtxChapter` tags in jingtongxue.go:284-289 | ✓ |
| lecture `id/lectureId/lecId/name/title/videoId/videoCcId/webVideoId/video{...}` | `jtxLecture` / `jtxVideo` tags in jingtongxue.go:312-337 | ✓ |
| play-param direct keys `videoSrc/webVideoDomain/url/playUrl/m3u8/m3u8Url/filePath/path` | `findDirectJingtongxueURL` keys in jingtongxue.go:490-517 | ✓ |
| BokeCC `siteid` / payConfig `ccUserId` | `findStringKey` + `fetchJingtongxueSiteIDFromVideoInfo` lines 421-447 | ✓ |

## 阻塞步骤

无.
