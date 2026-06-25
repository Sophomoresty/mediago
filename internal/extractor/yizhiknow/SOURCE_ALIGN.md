# yizhiknow 源码对齐对照

Source: `~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/Yizhiknow/`.
Encrypted/truncated sections were cross-checked with `~/code/xwz-downloader-source-release/decrypted_full/all_decrypted.json`.

## URL 常量

| .cdc.py 行 | yizhiknow.go 行/名 | 一致? |
| --- | --- | --- |
| `Yizhiknow_Base.py:36 referer = 'https://user.yizhiknow.com'` | `yizhiknow.go refererURL` | ✓ |
| `Yizhiknow_Base.py:37 origin = 'https://user.yizhiknow.com'` | `yizhiknow.go originURL` | ✓ |
| `Yizhiknow_Base.py:38 api_host = 'https://curriculum-api.yizhiknow.com'` | `yizhiknow.go apiHost` | ✓ |
| `Yizhiknow_Base.py:39 api_secret = 'dcwsnmsb'` | `yizhiknow.go apiSecret` | ✓ |
| `_check_cookie`: `/curriculum/user/getMultiPlatformMyCurricums` | `yizhiknow.go listPath` | ✓ |
| `_request_live_courses`: `/curriculum/user/getMyselfLiveCurricumX` | `yizhiknow.go liveListPath` | ✓ |
| `_load_detail`: `/curriculum/newDetailX` | `yizhiknow.go detailPath` | ✓ |
| `_load_status`: `/curriculum/user/getCurriculumStatusV2` | `yizhiknow.go statusPath` | ✓ |
| `_request_lesson_resource_result`: `/curriculum/getLessonResourceV2` | `yizhiknow.go lessonResourcePath` | ✓ |
| `_request_live_resource`: `/curriculum/getPlayLiveSteamX` | `yizhiknow.go liveResourcePath` | ✓ |
| `platform_type = 'wxkt'` | `yizhiknow.go platformType = "wxkt"` | ✓ (was "web", fixed) |

## Sign 计算

| 源码行为 | Go 实现 | 一致? |
| --- | --- | --- |
| `_serialize_sign_value`: recursive dict/list/scalar serializer | `serializeSignValue(value, nested)`: recursive, matches source | ✓ (was flat, fixed) |
| `md5(api_secret + base64(serialized))` | `md5(apiSecret + b64)` | ✓ (was reversed b64+secret, fixed) |
| `int(ts) % 5 % 2` → conditional `md5hex + token` or `token + md5hex` | `tsInt%5%2 != 0` → same conditional | ✓ (was missing token concat, fixed) |
| Sign output → HTTP headers (`token`, `sign`, `time-stamp`, `nonce-str`) | `signParams` returns `map[string]string` merged into headers | ✓ (was mixed into query/body, fixed) |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
| --- | --- | --- | --- |
| `_request_json`: sign→headers, params→query, data→body | `requestJSON`: sign→headers, params→query, data→body | ✓ (was sign→query/body, fixed) |
| `_request_json` pops `Access-Token` header | `requestJSON` deletes `Access-Token` | ✓ (was kept, fixed) |
| `_check_cookie` GET `?platform=wxkt&page=1&page_size=1` | `checkCookie` `platform=wxkt, page=1, page_size=1` | ✓ (was platform=yizhiknow, page_size=10, fixed) |
| `_load_detail` GET `?curriculum_id={cid}` | `detail` | ✓ |
| `_load_status` GET `?platform=wxkt&curriculum_id={cid}` | `Extract` status call `platform=wxkt` | ✓ (was platform=yizhiknow+platform_type, fixed) |
| `_request_lesson_resource_result` GET `?platform=wxkt&source=web&...` | `resolveLesson` `platform=wxkt, source=web` | ✓ (was platform=yizhiknow+platform_type, fixed) |
| `_request_live_resource` GET `?vid_x={vid}` | `resolveLesson` live fallback | ✓ |

## 资源解析

| 源码行为 | Go 实现 | 一致? |
| --- | --- | --- |
| type 1,2 → getLessonResourceV2 API | `resolveLesson` switch on lessonType | ✓ (was unconditional, fixed) |
| type 8 → stream_vod.vid → getPlayLiveSteamX | `resolveLesson` reads `stream_vod.vid_x/vid` from Raw | ✓ (was top-level vid lookup, fixed) |
| other types → use raw lesson data | `resolveLesson` default case | ✓ (was unconditional API call, fixed) |
| `_collect_media_candidates`: key priority by lesson_type | `collectMediaCandidates`: same priority logic | ✓ |
| Audio extensions (.mp3/.m4a/.aac/.wav) → download_audio (.mp3) | `pickFormat` returns actual ext from URL | ✓ |

## 课件/资料下载

| 源码行为 | Go 实现 | 一致? |
| --- | --- | --- |
| `_iter_material_items`: walks study_material for url/name pairs | `collectMaterialItems`: BFS queue, same URL keys | ✓ (was missing, added) |
| `_download_material_item`: PDF/PPT/DOC/attach download | Material entries emitted in `resolveLesson` | ✓ (was missing, added) |
| URL keys: url, file_url, download_url, material_url, oss_url | Same in `collectMaterialItems` | ✓ |
| Name keys: name, title, file_name | Same in `collectMaterialItems` | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 解析 | 一致? |
| --- | --- | --- |
| token keys `token`, `Token`, `Access-Token`, `access_token`, `accessToken` | `tokenFromJar` accepts same names | ✓ |
| `_request_api_data`: response `code`, `data` | `requestData` validates `code` and returns `data` | ✓ |
| `_get_cid`: `/course/video/(\d+)`, query `curriculum_id/curriculumId/course_id/id` | `parseCID` | ✓ |
| `_get_title`: `curriculum_detail.title`, `title` | `Extract` title selection | ✓ |
| `_get_infos`: `lesson_list`, group `lesson`, chapter `name`, lesson fields | `collectLessons` | ✓ |
| `_normalize_media_url`: `//` to `https:`, media extensions | `normalizeMediaURL` | ✓ |

## 阻塞步骤

无. All source flows are now implemented.
