# kuke 源码对齐对照

## URL 常量

| .cdc.py 行 | Go 行/名 | 一致? |
|---|---|---|
| `Kuke_Base.pyc.1shot.cdc.py:196-207` | `internal/extractor/kuke/kuke.go:33-45` | ✓ |
| `Kuke_Course.pyc.1shot.cdc.py:48-185` | `internal/extractor/kuke/kuke.go:60-141` | ✓ |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
|---|---|---|---|
| `Kuke_Base._check_cookie` (`Kuke_Base.pyc.1shot.cdc.py:376-400`) | `kukeCheckCookie` (`kuke.go:177-183`) | POST | ✓ |
| `Kuke_Base._signed_post` (`Kuke_Base.pyc.1shot.cdc.py:329-335`) | `kukeSignedPost` (`kuke.go:185-217`) | POST | ✓ |
| `Kuke_Course._request_course_list` (`Kuke_Course.pyc.1shot.cdc.py:48-91`) | `kukeFetchCourseList` (`kuke.go:241-280`) | POST | ✓ |
| `Kuke_Course._get_course_detail` (`Kuke_Course.pyc.1shot.das:2834-2860`) | `kukeFetchCourseDetail` (`kuke.go:309-318`) | POST | ✓ |
| `Kuke_Course._get_svip_subcourses` (`Kuke_Course.pyc.1shot.das:2934-2962`) | `kukeFetchSvipSubcourses` (`kuke.go:321-328`) | POST | ✓ |
| `Kuke_Base._get_polyv_secure_play_info` (`Kuke_Base.pyc.1shot.das:572-589`) | `kukeFetchPolyvNodeInfo` (`media.go:110-118`) | POST | ✓ |
| `Kuke_Base._get_polyv_m3u8_and_key` (`Kuke_Base.pyc.1shot.das:656-681`) | `kukeBuildVideoEntry` + `kukeFetchPolyvJS` (`media.go:80-137`) | GET+POST | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct/tag / 访问 | 一致? |
|---|---|---|
| `result.get('data',{}).get('orderGoodsList',[])` | `kukeCourseListData.OrderGoodsList` `json:"orderGoodsList"` | ✓ |
| `result.get('data',{}).get('count',0)` | `kukeCourseListData.Count` `json:"count"` | ✓ |
| `result.get('data',{}).get('courseList',[])` | `kukeSvipListData.CourseList` `json:"courseList"` | ✓ |
| `data.get('goodsCourseNodeList',[])` | `records(detail["goodsCourseNodeList"])` | ✓ |
| `data.get('videoId')` / `data.get('playSafe')` | `kukeNodeInfoData` tags `json:"videoId"`/`json:"playSafe"` | ✓ |
| `data.get('kkAes')` / `data.get('kkSdkString')` | `kukeNodeInfoData` tags `json:"kkAes"`/`json:"kkSdkString"` | ✓ |

## 阻塞步骤

无。
