# wowtiku 源码对齐对照

## URL 常量

| .cdc.py 行 | wowtiku.go 行/名 | 一致? |
|---|---|---|
| Wowtiku_Base.py:44 `referer = 'https://www.wowtiku.com/'` | wowtiku.go:21 `refererURL` | ✓ |
| Wowtiku_Base.py:45 `origin = 'https://www.wowtiku.com'` | wowtiku.go:22 `originURL` | ✓ |
| Wowtiku_Base.py:46 `api_host = 'https://new.wowtiku.net'` | wowtiku.go:23 `apiHost` | ✓ |
| Wowtiku_Base.py:47 `www_api_host = 'https://www.wowtiku.net'` | wowtiku.go:24 `wwwAPIHost` | ✓ |
| Wowtiku_Course.py:38 `buy_lists_api = '/goods/buy_lists'` | wowtiku.go:25 `buyListsAPI` | ✓ |
| Wowtiku_Course.py:39 `detail_api = '/goods/sg_detail'` | wowtiku.go:26 `detailAPI` | ✓ |
| Wowtiku_Course.py:40 `subset_api = '/goods/subset'` | wowtiku.go:27 `subsetAPI` | ✓ |
| Wowtiku_Course.py:41 `document_api = '/goods/class_document_lists'` | wowtiku.go:28 `documentAPI` | ✓ |
| Wowtiku_Course.py:42 `platform_lists_api = '/config/platform_lists'` | wowtiku.go:29 `platformListsAPI` | ✓ |
| Wowtiku_Course.py:43 `sts_api = '/alibaba/get_sts'` | wowtiku.go:30 `stsAPI` | ✓ |
| Wowtiku_Course.py:44 `play_token_api = '/alibaba/get_play_token'` | wowtiku.go:31 `playTokenAPI` | ✓ |
| Wowtiku_Course.py:45 `vod_region = 'cn-shanghai'` | wowtiku.go:32 `vodRegion` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Wowtiku_Base._check_cookie line 406 `/question_bank/user/user_info`, host `www`, version `v2` | wowtiku.go:58 `requestData(..., "GET", "www", "v2")` | GET | ✓ |
| Wowtiku_Base._request_json lines 311-338 | wowtiku.go:140 `requestJSON` | GET/POST | ✓ |
| Wowtiku_Course._get_platform_id_list line 525 `platform_lists_api` | wowtiku.go:100 `requestData(platformListsAPI...)` | GET | ✓ |
| Wowtiku_Course._get_course_list lines 604-605 `buy_lists_api` with `platform_id` | wowtiku.go:113 `requestData(buyListsAPI...)` | GET | ✓ |
| Wowtiku_Course._load_detail lines 685-686 `detail_api` with `id` | wowtiku.go:69 `requestData(detailAPI...)` | GET | ✓ |
| Wowtiku_Course._get_infos lines 936-938 `subset_api` with `stage_goods_id`, `class_id` | wowtiku.go:75 `requestData(subsetAPI...)` | GET | ✓ |
| Wowtiku_Course._get_sts_info line 1003 `sts_api`, host `www` | wowtiku.go:205 `requestData(stsAPI..., "POST", "www", "v1")` | POST | ✓ |
| Wowtiku_Course._get_play_info lines 1155-1165 signed Aliyun VOD `GetPlayInfo` | wowtiku.go:221-229 `aliyunPlayURL` | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 解析 | 一致? |
|---|---|---|
| `result.get('code') in (1,0,200,'1','0','200',None)` | wowtiku.go:131-136 `root["code"]`, `root["data"]` | ✓ |
| `platform_lists_api` `lists` / `list` / list data, `id` / `platform_id` | wowtiku.go:100-104 `mapsUnder`, `id`, `platform_id` | ✓ |
| `buy_lists_api` `id` / `stage_goods_id` | wowtiku.go:117-119 | ✓ |
| `_get_cid` query keys `id`, `stage_goods_id`, `course_id` | wowtiku.go:280-289 | ✓ |
| `_parse_video_info` `name/title/subject_name`, `vid/video_id` | wowtiku.go:175-184 | ✓ |
| `_get_infos` `class_id`, `id` | wowtiku.go:187-196 | ✓ |
| `_get_play_info` STS keys `ky`, `sc`, `tk` | wowtiku.go:216-221 | ✓ |
| `_extract_aliyun_play_response` `PlayURL`, `PlayUrl`, nested play list | wowtiku.go:237 and `firstURLByKeys` lines 321-329 | ✓ |

## 阻塞步骤

无.
