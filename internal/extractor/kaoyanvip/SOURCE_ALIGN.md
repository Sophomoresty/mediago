# kaoyanvip 源码对齐对照

## URL 常量

| .cdc.py/.das 行 | kaoyanvip.go 行/名 | 一致? |
|---|---|---|
| Kaoyanvip_Base.py:32-33 `referer/order_url` | kaoyanvip.go:35,37 `urlReferer/urlOrder` | ✓ |
| Kaoyanvip_Base.py:295 user-info URL | kaoyanvip.go:36 `urlUserInfo` | ✓ |
| Kaoyanvip_Course.py:38 `course_url` | kaoyanvip.go:38 `urlCourse` | ✓ |
| Kaoyanvip_Course.py:39-42 delivery/uuid info + outline URL | kaoyanvip.go:39-42 `urlInfo/urlInfoUUID/urlOutline/urlOutlineUUID` | ✓, `{...}` -> `%s` |
| Kaoyanvip_Course.py:43-47 live/VOD/token URLs | kaoyanvip.go:43-47 `urlLiveRecords/urlVideoM3U8/urlVideoMP4/urlLivePlay/urlKeyToken` | ✓ |
| Kaoyanvip_Course.py:51 timestamp URL | kaoyanvip.go:48 `urlTimestamp` | ✓ |
| Kaoyanvip_Course.py:48 `source_url` (my_outline_resource, resource_type=material) | kaoyanvip.go `urlSource` | ✓ |
| Kaoyanvip_Course.py:49 `file_url` (pc/material) | kaoyanvip.go `urlFile` | ✓ |
| Kaoyanvip_Course.das `_get_live_url` constants `fjd1n2k14a` and sign template | kaoyanvip.go:49-50 `polyvLiveAppID/polyvLiveSignTmpl` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Base `_check_cookie` lines 287-306: cookie `Pcsite-Token`, GET user info, code `20000`, parse `unionuuid` | kaoyanvip.go:71-79 and 138-158 | GET | ✓ |
| Course `_get_course_list` das: `course_url`, parse `data.course`, `my_delivery_id/uuid/name/product_id` | kaoyanvip.go:189-204 `fetchKaoyanCourses` | GET | ✓ |
| Course `_get_infos` das: delivery `info_url` -> `data.outlines` -> `delivery_outline_id` -> `_get_lesson_dict` | kaoyanvip.go:223-239 `fetchKaoyanOutlineRoots` | GET | ✓ |
| Course `_get_infos` das: uuid `info_uuid_url` -> `data.outline` -> `subject_id` -> `_get_uuid_lesson_dict` | kaoyanvip.go:240-254 | GET | ✓ |
| Course `_get_video_list` das constants `video/living/video_id/classroom/record/is_live` | kaoyanvip.go:277-316 `walkKaoyan` / `parseKaoyanSection` | local parse | ✓ |
| Course `_get_video_url` das: build `hls.videocc.net/{first10}/{last}/{vid}.m3u8`, parse `#EXT-X-STREAM-INF`, rewrite `URI` with `_get_key_token`; fallback `dpv.videocc.net` MP4 | kaoyanvip.go:345-379 and 381-394 | GET + regex | ✓ |
| Course `_get_live_url` das: GET `living/{room_id}/records`, regex `plv_channel`, timestamp, MD5 sign, GET polyv inner API, parse `data.fileUrl/url` | kaoyanvip.go:396-423 and 425-433 | GET + regex + JSON | ✓ |
| Course `_download_files` das: delivery `source_url` -> `data[].course_sections[].materials[]` -> `title/download_link`; `file_url` -> `data[].material_list[]` -> `name/link`; ext from URL (`split('?')` then `rsplit('.')`) | `fetchKaoyanMaterials/fetchKaoyanSourceFiles/fetchKaoyanFileList/buildKaoyanFileEntry/kaoyanFileExt` | GET + parse | ✓ (delivery only, mirrors `_is_delivery` gate) |

## JSON 字段映射

| 源码 key 链 | Go struct tag / parse | 一致? |
|---|---|---|
| user info `code`, `data.unionuuid/user_id/id` | anonymous struct tags in kaoyanvip.go:143-150 | ✓ |
| API `code/msg/data` | `kaoyanEnvelope` tags in kaoyanvip.go:160-164 | ✓ |
| mycourse `data.course[]`, `my_delivery_id/uuid/title/name/course_id/product_id` | `fetchKaoyanCourses` lines 194-203 | ✓ |
| delivery info `data.outlines[].delivery_outline_id` | `fetchKaoyanOutlineRoots` lines 226-239 | ✓ |
| uuid info `data.outline[].subject_id` | `fetchKaoyanOutlineRoots` lines 241-254 | ✓ |
| outline tree `children/course_sections`, section `video.video_id`, `living.classroom`, `living.record.video_id` | `walkKaoyan` / `parseKaoyanSection` lines 277-316 | ✓ |
| VOD token response regex `"data":"([\w-]+)"` | `fetchKaoyanKeyToken` lines 381-394 | ✓ |
| live response `data.fileUrl/url` | `resolveKaoyanLiveURL` lines 414-422 | ✓ |

## 阻塞步骤

无.

## 备注

源码 `Kaoyanvip_Base.download_audio` 已定义但从未被调用; `_download_video` 对 video 与 live 一律解析 polyv VOD/live URL, 返回 mp3 时即作为音频下载, Go 端由 `mediaExt` 识别 `.mp3`. 因此无独立 audio 接口需要移植.
