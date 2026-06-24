# open163 源码对齐对照

## URL 常量

| .cdc.py 行 | open163.go 行/名 | 一致? |
|---|---|---|
| Open163_App.py:33 `order_url = 'https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do'` | open163.go:26 `urlMyOrders` | ✓ |
| Open163_App.py:34 `course_info_url = 'https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do'` | open163.go:27 `urlCourseInfo` | ✓ |
| Open163_App.py:35 `login_check_url = 'https://c.open.163.com/member/loginStatus.do'` | open163.go:28 `urlLoginStatus` | ✓ |
| Open163_App.py:36 `detail_url = 'https://vip.open.163.com/courses/{}'` | open163.go:29 `urlCoursePage = "https://vip.open.163.com/courses/%s"` | ✓ |
| Open163_Free.py:31 `url_course = 'https://open.163.com/newview/movie/free?pid={}'` | open163.go:30 `urlFreePage` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Open163_App._check_cookie lines 78-98, `loginStatus.do` JSON `code == 200` | open163.go:129 `checkOpen163Cookie` | GET | ✓ |
| Open163_App._load_course_data lines 288-319, POST `courseInfo.do` with `courseId/courseUid/version` | open163.go:184 `loadOpen163Course` | POST form | ✓ |
| Open163_App._get_infos lines 350-385, iterate `movieChapterList/audioChapterList/contentList` | open163.go:75-93 `Extract` entries loop | local parse after POST | ✓ |
| Open163_Free._get_infos lines 53-64, fetch free page and regex mp4 links | open163.go:100 `extractFree` | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct tag | 一致? |
|---|---|---|
| info.get('code') == 200, info.get('data') | `Code` `json:"code"`, `Data` `json:"data"` in open163.go:146-160 | ✓ |
| data.get('courseInfo', {}) | `CourseInfo` `json:"courseInfo"` in open163.go:149-156 | ✓ |
| data.get('movieChapterList') / `audioChapterList` | `MovieChapterList` / `AudioChapterList` tags in open163.go:157-158 | ✓ |
| chapter.get('contentList', []) | `ContentList` `json:"contentList"` in open163.go:162-166 | ✓ |
| content.get('mediaInfoList', []) | `MediaInfoList` `json:"mediaInfoList"` in open163.go:168-172 | ✓ |
| media.get('type'/'encryptUrl'/'mediaUrl'/'url'/'mediaSize') | `Type/EncryptURL/MediaURL/URL/MediaSize` tags in open163.go:175-181 | ✓ |

## 阻塞步骤

无.
