# jingtongxue 源码对齐对照

## URL 常量

| .cdc.py 行 | jingtongxue.go 行/名 | 一致? |
|---|---|---|
| Jingtongxue_Base.py:35-38 `referer/origin/domain/api_base` | jingtongxue.go:28-31 `urlReferer/urlOrigin/urlDomain/urlAPIBase` | ✓ |
| Jingtongxue_Course.py:32 `course_list_api = '/course/v1/user/get/courses'` | jingtongxue.go:32 `pathCourseList` | ✓ |
| Jingtongxue_Course.py:33-37 detail/chapter/lecture/video/play-param API | jingtongxue.go:33-37 `pathDetail/pathChapter/pathLecture/pathVideoInfo/pathPlayParam` | ✓, `{...}` -> `%s` |
| Jingtongxue_Course.py:38 `resource_menu_api = '/saas-business/front/commodity/findClassResourceMenu'` | jingtongxue.go:38 `pathResourceMenu` | ✓ |
| Jingtongxue_Course.py:39 `resource_api = '/saas-business/front/commodity/findResource/{commodity_id}/{class_type_id}'` | jingtongxue.go:39 `pathResource` | ✓, `{...}` -> `%s` |
| Jingtongxue_Course.py:40 `download_link_api = '/saas-business/front/commodity/getDownloadLink/{resource_id}'` | jingtongxue.go:40 `pathDownloadLink` | ✓, `{...}` -> `%s` |
| Jingtongxue_Course.py:41 `https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}` | jingtongxue.go:41 `urlBokeCCVideoAPI` and shared `BokeCCResolve` | ✓, `{vid}/{siteid}` -> `%s/%s` |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Base `_request_json` lines 332-354: relative path uses `api_base`, query adds `domain`, default GET | jingtongxue.go `jtxGetJSON` | GET | ✓ |
| Base `_check_cookie` lines 421-427: `get/courses` status/pageSize/offset validates login | jingtongxue.go `fetchJingtongxueCourses` | GET | ✓ |
| Course `_get_course_list` lines 288-309: status 1/2/3, `pageSize=50`, `offset` pagination | jingtongxue.go `fetchJingtongxueCourses` | GET | ✓ |
| Course `_get_detail` lines 340-354: `getDetail/{commodity_id}` + `liveSet=1` | jingtongxue.go `fetchJingtongxueDetail` | GET | ✓ |
| Course `_get_chapters` lines 510-522 and `_get_lectures` lines 528-543 | jingtongxue.go `fetchJingtongxueChapters` / `fetchJingtongxueLectures` | GET | ✓ |
| Course `_get_play_param` lines 734-746: `broswer=pc`, `lectureId`, `classTypeId`, `moduleId` | jingtongxue.go `resolveJingtongxuePlayURL` | GET | ✓ |
| Course `_get_bokecc_video_url` lines 842-858 | jingtongxue.go -> `shared.BokeCCResolve` | GET helper | ✓ |
| Course `_get_source_info` lines 641-680: resource_menu_api + resource_api | jingtongxue.go `fetchJingtongxueResourceMenu` + `fetchJingtongxueResourceList` | GET | ✓ |
| Course `_get_file_url` lines 1046-1066: direct URL or download_link_api | jingtongxue.go `resourceFileURL` + `fetchJingtongxueDownloadLink` | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct tag / parse | 一致? |
|---|---|---|
| response `code/success/msg/message/data` | `jtxEnvelope` tags | ✓ |
| course list `data.records/rows/list/items/data` | `jtxExtractRecords` keys | ✓ |
| course `commodityId/comId/id`, `courseId/classTypeId/classTypePo.id`, `name/courseName/title` | `jtxCourse` tags + `normalized()` | ✓ |
| detail `classTypePo.id/name`, `buyFlag/userVipFlag/priceFlag` | `jtxDetail` tags | ✓ |
| chapter `id/chapterId/chapterName/name/moduleId` | `jtxChapter` tags | ✓ |
| lecture `id/lectureId/lecId/name/title/videoId/videoCcId/webVideoId/video{...}` | `jtxLecture` / `jtxVideo` tags | ✓ |
| play-param direct keys `videoSrc/webVideoDomain/url/playUrl/m3u8/m3u8Url/filePath/path` | `findDirectJingtongxueURL` keys | ✓ |
| BokeCC `siteid` / payConfig `ccUserId` | `findStringKey` + `fetchJingtongxueSiteIDFromVideoInfo` | ✓ |
| resource menu `id/name` | `jtxResourceMenuItem` tags | ✓ |
| resource file `id/resourceId/name/fileName/title/download/downloadUrl/filePath/path/url/format/fileType/suffix/ext` | `jtxResourceFile` tags | ✓ |
| download link response: string or dict with `url/downloadUrl/path/filePath` | `fetchJingtongxueDownloadLink` switch | ✓ |

## Live replay flow

jingtongxue 源码中无独立的 live replay API. 唯一 live 相关的是 `_get_detail` 的 `liveSet=1` 查询参数, 已在 `fetchJingtongxueDetail` 中实现.

## 阻塞步骤

无.
