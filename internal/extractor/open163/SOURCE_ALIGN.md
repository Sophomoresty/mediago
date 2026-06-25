# open163 源码对齐对照

## URL 常量

| .cdc.py 行 | open163.go 行/名 | 一致? |
|---|---|---|
| Open163_App.py:33 `order_url = 'https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do'` | open163.go:27 `urlMyOrders` | ✓ |
| Open163_App.py:34 `course_info_url = 'https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do'` | open163.go:28 `urlCourseInfo` | ✓ |
| Open163_App.py:35 `login_check_url = 'https://c.open.163.com/member/loginStatus.do'` | open163.go:29 `urlLoginStatus` | ✓ |
| Open163_App.py:36 `detail_url = 'https://vip.open.163.com/courses/{}'` | open163.go:30 `urlCoursePage = "https://vip.open.163.com/courses/%s"` | ✓ |
| Open163_Free.py:31 `url_course = 'https://open.163.com/newview/movie/free?pid={}'` | open163.go:31 `urlFreePage` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Open163_App._check_cookie lines 78-98, `loginStatus.do` JSON `code == 200` | open163.go:131 `checkOpen163Cookie` | GET | ✓ |
| Open163_App._get_course_list lines 186-239, POST `myOrders.do` with `page/size`, paginate ≤9 pages, filter status==2, dedup (courseUid, productId) | open163.go:492 `extractMyOrders` | POST form | ✓ |
| Open163_App._load_course_data lines 288-319, POST `courseInfo.do` with `courseId/courseUid/version` | open163.go:186 `loadOpen163Course` | POST form | ✓ |
| Open163_App._get_infos lines 350-385, iterate `movieChapterList/audioChapterList/contentList` | open163.go:77-99 `Extract` entries loop | local parse after POST | ✓ |
| Open163_Free._get_infos lines 53-64, fetch free page and regex mp4 links + `codecs.decode(parse.unquote(url), 'unicode_escape')` | open163.go:102 `extractFree` + `normalizeFreeURL` | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct tag | 一致? |
|---|---|---|
| info.get('code') == 200, info.get('data') | `Code` `json:"code"`, `Data` `json:"data"` in open163.go:148-162 | ✓ |
| data.get('courseInfo', {}) | `CourseInfo` `json:"courseInfo"` in open163.go:151-158 | ✓ |
| data.get('movieChapterList') / `audioChapterList` | `MovieChapterList` / `AudioChapterList` tags in open163.go:159-160 | ✓ |
| chapter.get('contentList', []) | `ContentList` `json:"contentList"` in open163.go:164-168 | ✓ |
| content.get('mediaInfoList', []) | `MediaInfoList` `json:"mediaInfoList"` in open163.go:170-174 | ✓ |
| media.get('type'/'encryptUrl'/'mediaUrl'/'url'/'mediaSize') | `Type/EncryptURL/MediaURL/URL/MediaSize` tags in open163.go:177-183 | ✓ |
| order items: status/courseUid/productId/productName/contentType/discountPrice/productPrice | `open163OrderItem` fields in open163.go:479-487 | ✓ |

## 标题/URL 规范化

| 源码功能 | Go 函数 | 一致? |
|---|---|---|
| winre_dir_sub / winre_sub: 去非法字符, 去emoji, 折叠空白, 截断WIN_LEN=32 | `sanitizeTitle` (open163.go:389-403) | ✓ |
| Free URL: `codecs.decode(parse.unquote(url), 'unicode_escape')` | `normalizeFreeURL` → `url.QueryUnescape` + `decodeUnicodeEscape` (open163.go:409-463) | ✓ |
| VIP media URL: base64 decode if not http | `decodeOpen163MediaURL` (open163.go:278-295) | ✓ |

## 购课列表回退

| 源码功能 | Go 函数 | 一致? |
|---|---|---|
| prepare: 无 cid/course_uid 时调 _select_my_course | Extract: cid==""&&courseUID=="" 时调 extractMyOrders | ✓ |
| _get_course_list: POST myOrders.do, page=1..9, size=99999, status==2, dedup | extractMyOrders 分页逻辑 | ✓ |
| _normalize_cent_price: price>=100 → price/100 | normalizeCentPrice (open163.go:658-678) | ✓ |

## 阻塞步骤

无.
