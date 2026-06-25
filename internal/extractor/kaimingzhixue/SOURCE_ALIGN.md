# kaimingzhixue 源码对齐对照

## URL 常量

| .cdc.py 行 | kaimingzhixue.go 行/名 | 一致? |
|---|---|---|
| Kaimingzhixue_Base.py:29-30 `referer/api_base = 'https://www.lckmzx.com'` | kaimingzhixue.go:31 `urlReferer` | ✓ |
| Kaimingzhixue_Base.py:215 userInfo check | kaimingzhixue.go:32 `urlUserInfo` | ✓ |
| Kaimingzhixue_Course.py:32 `myStudy/{course_type}` | kaimingzhixue.go:33 `urlCourseList` | ✓, `{course_type}` -> `%s` |
| Kaimingzhixue_Course.py:33 `courseBasis` | kaimingzhixue.go:34 `urlPublicCourse` | ✓ |
| Kaimingzhixue_Course.py:34 `myStudy/course/{cid}` | kaimingzhixue.go:35 `urlDetail` | ✓, `{cid}` -> `%s` |
| Kaimingzhixue_Course.py:35 `getPlayToken/chapter_id={chapter_id}/course_id={cid}` | kaimingzhixue.go:36 `urlPlayToken` | ✓, placeholders -> `%s` |
| Kaimingzhixue_Course.py:36 `getPcRoomCode/course_id={cid}/chapter_id={chapter_id}` | kaimingzhixue.go:37 `urlLiveRoomCode` | ✓ |
| Kaimingzhixue_Course.py:37-38 Baijiayun VOD URL and referer | kaimingzhixue.go:38-39 and `shared.BaijiayunResolveVOD` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Base `_check_cookie` lines 196-239: extract `studentToken`, GET `/api/app/userInfo`, require `code==200` and `data.id`, set `SchoolID` | kaimingzhixue.go:60-69 and 136-155 | GET | ✓ |
| Course `_api_get/_api_data` lines 164-199: GET JSON and require `code == 200` | kaimingzhixue.go:163-188 `kzxAPIGet` | GET | ✓ |
| Course `_get_course_list` lines 285-318: course types `1,2,3,11`, `type=open` | kaimingzhixue.go:197-218 `fetchKaimingCourseList` | GET | ✓ |
| Course `_get_infos` lines 578-613: fetch detail, read `course`, `chapter` or `periods` tree | kaimingzhixue.go:232-270 and 273-295 | GET + local parse | ✓ |
| Course `_get_play_token` lines 619-630 | kaimingzhixue.go:340-355 `resolveKaimingVOD` | GET | ✓ |
| Course `_get_video_url_by_token` lines 698-712 | kaimingzhixue.go:350 -> `shared.BaijiayunResolveVOD` | GET helper | ✓ |
| Course `_get_live_room_info` lines 803-829 | kaimingzhixue.go:357-393 `resolveKaimingLivePlayback` | GET | ✓ |
| Course `_parse_baijiayun_playback_query` lines 872-888 | kaimingzhixue.go:396-412 `parseBaijiayunQuery` | local parse | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct tag / parse | 一致? |
|---|---|---|
| API `code/msg/data` | `kzxEnvelope` tags in kaimingzhixue.go:157-161 | ✓ |
| userInfo `data.id`, `data.school_id` | anonymous struct tags in kaimingzhixue.go:141-147 | ✓ |
| course list `course_id/id`, `title/name`, `course_type/type`, `price` | `fetchKaimingCourseList` lines 205-212 | ✓ |
| detail `course.title/course_type/type`, `chapter`, `periods` | `fetchKaimingDetail` + `collectKaimingItems` lines 232-270 | ✓ |
| node `video_id/id/course_chapter_id/periods_type/arrange_id/meeting_id/bjy_period_id/type/play_type/periods_id` | `walkKaimingNode` lines 280-288 | ✓ |
| play token `token`, `video_id` | `resolveKaimingVOD` lines 341-350 | ✓ |
| live room `chapterInfo`, `room_id/classid/roomId/bjy_room_id`, `token/playback_token`, `vid/video_id` | `resolveKaimingLivePlayback` lines 369-392 | ✓ |
| Baijiayun `data.play_info.*.cdn_list.url/enc_url` | shared/baijiayun.go:41-60 and 112-140 | ✓ |

## 阻塞步骤

无.

## 新增对齐 (file/material nodes + courseBasis price)

| 源码方法 (line) | Go 函数 (line) | 一致? |
|---|---|---|
| Course `_parse_node_sources` lines 533-541: file nodes from `datum`/`files` | `walkKaimingNode` lines 363-377, `parseKaimingFileInfo` | ✓ |
| Course `_parse_file_info` lines 417-439: extract `file_url/url`, `file_name/name`, ext | `parseKaimingFileInfo` lines 410-436, `fileExtFromInfo` lines 441-463 | ✓ |
| Course `_get_price` lines 226-269: paginate `courseBasis`, match `id`, extract `price/has_buy/title` | `fetchCourseBasisPrice` lines 261-312 | ✓ |
| Course `_download_one_file` line 679: download file by type (pdf/mp4/ppt/doc/attach) | `buildKaimingEntry` file branch lines 482-492 | ✓ (direct URL entry) |
