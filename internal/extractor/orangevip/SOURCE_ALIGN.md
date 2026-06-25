# orangevip 源码对齐对照

## URL 常量

| .cdc.py 行 | orangevip.go 行/名 | 一致? |
|---|---|---|
| Orangevip_Base.py:32 referer = 'https://www.orangevip.com' | orangevip.go:17 referer | ✓ |
| Orangevip_Course.py:39 course_url = 'https://clapp.orangevip.com/otm/web/course/list' | orangevip.go:18 course_url | ✓ |
| Orangevip_Course.py:40 info_url = 'https://clapp.orangevip.com/otm/web/course/query/coursePeriod' | orangevip.go:19 info_url | ✓ |
| Orangevip_Course.py:41 video_play_url = 'https://api.baijiayun.com/web/playback/getPlayInfo?...' | orangevip.go:20 video_play_url + shared.BaijiayunResolvePlayback | ✓ |
| Orangevip_Course.py:42 live_play_url = 'https://www.baijiayun.com/vod/video/getPlayUrl?...' | orangevip.go:21 live_play_url + shared.BaijiayunResolveVOD | ✓ |
| Orangevip_Course.py:43 file_url = 'https://clapp.orangevip.com/otm/web/student/myCourseModelFile' | orangevip.go:22 file_url | ✓ |
| Orangevip_Course.py:44 price_url = 'https://www.orangevip.com/coursedetail/{cid:}.html' | orangevip.go:23 price_url | ✓ |
| Orangevip_Course.py:45 token_url = 'https://clapp.orangevip.com/otm/web/course/v2/reviewPlayInfo' | orangevip.go:24 token_url | ✓ |
| Orangevip_Base.py:33 order_url = 'https://clapp.orangevip.com/otm/web/order/orderList' | orangevip.go:25 order_url | ✓ |
| Orangevip_Base.py:233 _check_cookie url = 'https://u.api.orangevip.com/Api/Index/getUserInfo' | orangevip.go userinfo_url + checkCookie | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Orangevip_Course._get_course_list line 317 -> request_post(course_url) | fetchCourses lines 103-125 | POST | ✓ |
| Orangevip_Course._get_infos line 465 -> request_post(info_url, {'courseModelId': cid}) | fetchCourseInfo lines 127-137 | POST | ✓ |
| Orangevip_Course._get_token line 534 -> request_post(token_url, clientType/periodId/courseId) | fetchToken lines 159-169 | POST | ✓ |
| Orangevip_Course._get_source_url line 561 -> request_get(video_play_url) | resolveBaijiayun lines 171-190 plus shared.BaijiayunResolvePlayback | GET | ✓ |
| Orangevip_Course._get_live_url line 580 -> request_get(live_play_url) | resolveBaijiayun lines 178-181 via shared.BaijiayunResolveVOD | GET | ✓ |
| Orangevip_Base._check_cookie line 228 -> request_get(getUserInfo), re.search('"errno":0') | checkCookie + errnoRe | GET | ✓ |
| Orangevip_Course._get_file_list / _download_files line 775 -> request_post(file_url, courseModelId/pguid), folder = file_type=='1' recurse on file_id | fetchFiles (recursive) + fileFmt + fileEntries | POST | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct/tag or parser | 一致? |
|---|---|---|
| json.loads(...).get('courseList', []) | apiResp.CourseList `json:"courseList"` line 42 | ✓ |
| item.get('isExpire'), item.get('totalCount'), item.get('guid'), item.get('courseName') | fetchCourses lines 114-120 | ✓ |
| result.get('courseChapterList', []) | apiResp.CourseChapterList `json:"courseChapterList"` line 43 | ✓ |
| chapter.get('coursePeriodList', []) | parseLessons line 142 | ✓ |
| lesson.get('coursePeriodTitle'), get('guid'), get('roomId'), get('videoId') | parseLessons lines 148-153 | ✓ |
| result.get('data',{}).get('classInfo',{}).get('token') | fetchToken lines 164-168 dynamic walk on `token` | ✓ |
| files[].get('netUrl'), get('fileName'), get('isFolder'), get('guid'); file_fmt from name rsplit('.',1) else netUrl split('?')[0] | fetchFiles + fileFmt | ✓ |
| _download_one_file routes file_fmt (mp4/pdf/ppt/doc/attach) | fileEntries -> one MediaInfo entry per file with single Stream | ✓ |

## 阻塞步骤

无. Baijiayun 平台解析按硬规则调用 `shared.BaijiayunResolvePlayback` / `shared.BaijiayunResolveVOD`; 站包只负责橙啦业务 API 和字段拼接.
