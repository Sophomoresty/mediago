# qihang 源码对齐对照

## URL 常量

| .cdc.py 行 | qihang.go 行/名 | 一致? |
|---|---|---|
| Qihang_Base.py:33 referer = 'https://www.iqihang.com' | qihang.go:18 referer | ✓ |
| Qihang_Course.py:37 course_url = 'https://www.iqihang.com/api/ark/web/v1/user/course/course-list?isMarketingCourse=&status=&type=1' | qihang.go:19 course_url | ✓ |
| Qihang_Course.py:38 info_url = 'https://www.iqihang.com/api/ark/web/v1/course/catalog/{cid}' | qihang.go:20 info_url | ✓, {cid}→%s |
| Qihang_Course.py:39 video_play_url = 'https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid=A183AC83A2983CCC' | qihang.go:21 video_play_url | ✓, helper shared.BokeCCResolve 使用同 siteid |
| Qihang_Course.py:40 live_url = 'https://www.iqihang.com/api/ark/web/v1/user/course/live/replay?liveId={live_id}' | qihang.go:22 live_url | ✓, {live_id}→%s |
| Qihang_Course.py:41 live_login_url = 'https://view.csslcloud.net/api/room/replay/login?roomid={room_id}&userid={user_id}&recordid={record_id}&viewertoken={uid}%3A{lid}' | qihang.go:23 live_login_url | ✓, 实际请求走 shared.CssLcloudResolvePlayInfo |
| Qihang_Course.py:42 live_play_url = 'https://view.csslcloud.net/api/record/vod?accountId={user_id}&recordId={record_id}&terminal=3&token={token}' | qihang.go:24 live_play_url | ✓, 实际请求走 shared.CssLcloudResolvePlayInfo |
| Qihang_Course.py:43 source_url = 'https://www.iqihang.com/api/ark/web/v1/lecture/curriculum/node?curriculumId={cid}' | qihang.go:25 source_url | ✓, {cid}→%s |
| Qihang_Course.py:44 price_url = 'https://iqihang.com/api/ark/web/v1/product/{product_id}' | qihang.go:26 price_url | ✓, {product_id}→%s |
| Qihang_Base.py:299 user info URL | qihang.go:27 user_info_url | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Qihang_Base._check_cookie line 299-303 | fetchUID line 284 | GET user_info_url | ✓ |
| Qihang_Course._get_course_list line 58-91 | fetchCourseList line 86 | GET course_url | ✓ |
| Qihang_Course._get_price line 95-120 | titleFromCourse line 109 | GET price_url | ✓ |
| Qihang_Course._get_infos line 175-201 | fetchNodes line 144 | GET info_url | ✓ |
| Qihang_Course._get_source_info line 381-407 | fetchNodes line 144 | GET source_url | ✓ |
| Qihang_Course._get_video_url line 466-486 | resolveVideo line 192 | GET via shared.BokeCCResolve | ✓, 按硬规则使用 shared helper |
| Qihang_Course._get_live_url line 491-522 | resolveLive line 213 | GET live_url + CSSLcloud helper | ✓, 按硬规则使用 shared.CssLcloudResolvePlayInfo |

## JSON 字段映射

| 源码 key 链 | Go struct tag | 一致? |
|---|---|---|
| result.get('data') course list | Data `json:"data"` | ✓ |
| item.get('id') | ID `json:"id"` | ✓ |
| item.get('productId') | ProductID `json:"productId"` | ✓ |
| item.get('productCurriculumId') | ProductCurriculumID `json:"productCurriculumId"` | ✓ |
| item.get('productName') | ProductName `json:"productName"` | ✓ |
| result.get('data',{}).get('name') | Data.Name `json:"name"` | ✓ |
| result.get('data',{}).get('sellPrice') | Data.SellPrice `json:"sellPrice"` | ✓ |
| result.get('data',{}).get('courseNodes',[]) | Data.CourseNodes `json:"courseNodes"` | ✓ |
| result.get('data',[]) | Data []qNode `json:"data"` | ✓ |
| node.get('name') | Name `json:"name"` | ✓ |
| node.get('children',[]) | Children `json:"children"` | ✓ |
| node.get('studyResourceType') | StudyResourceType `json:"studyResourceType"` | ✓ |
| node.get('resourceList',[]) | ResourceList `json:"resourceList"` | ✓ |
| resource.get('vid') | Vid `json:"vid"` | ✓ |
| resource.get('resourceId') | ResourceID `json:"resourceId"` | ✓ |
| resource.get('lectureUrl') | LectureURL `json:"lectureUrl"` | ✓ |
| live result.get('data',{}).get('replayUrl','') | Data.ReplayURL `json:"replayUrl"` | ✓ |

## studyResourceType 处理

| 源码 type | 源码方法 | Go 函数 | 一致? |
|---|---|---|---|
| 2, 3 (video/live) | _parse_video_info | collectEntries → resolveVideo | ✓ |
| 4 (file) | _parse_file_info | collectEntries → resolveFile | ✓ |

## 阻塞步骤 (如果有)

无。
