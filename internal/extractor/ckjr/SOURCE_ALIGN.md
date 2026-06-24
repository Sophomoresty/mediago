# ckjr 源码对齐对照

## URL 常量

| .cdc.py 行 | ckjr.go 行/名 | 一致? |
|---|---|---|
| `Ckjr_Base.pyc.1shot.cdc.py:125` `api_host = 'https://kpapiop.ckjr001.com'` | `ckjr.go:20-21` `url0` | ✓ |
| `Ckjr_Base.pyc.1shot.cdc.py:127` `qcloud_play_api = 'https://playvideo.qcloud.com/getplayinfo/v4/{}/{}'` | `ckjr.go:22` `url1` | ✓ |
| `Ckjr_Config.pyc.1shot.cdc.py:14-15` `CKJR_WEBVERSION/CKJR_FROM_APP` | `ckjr.go:24-25` | ✓ |
| `Ckjr_Config.pyc.1shot.cdc.py:138-143` `CKJR_HEADERS` | `ckjr.go:26`, `helpers.go:123-131` | ✓ |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
|---|---|---|---|
| `Ckjr_Base._request_json` `2126-2147` | `requestAPI` `139-162` | GET + JSON | ✓ |
| `Ckjr_Base._request_course_detail` `2691-2706` | `fetchRoutePayloads` `108-137` | GET detail endpoints | ✓ |
| `Ckjr_Base._request_dirs_page` `2712-2732` | `fetchRoutePayloads` `108-137` | GET dirs endpoints | ✓ |
| `Ckjr_Base._request_qcloud_play_info` `1811-1880` | `requestQCloud` `197-208` | GET qcloud playinfo | ✓ |

## JSON 字段映射

| 源码 key 链 | Go parse | 一致? |
|---|---|---|
| route regex `/kpv2p/{company}#/homePage/...?...Id=` | `routeRe`, `parseRoute` | ✓ |
| `fromApp`, `webversion` params | `requestAPI` query defaults | ✓ |
| `fileID/fileId/file_id/fileid/vid`, `psign/pSign/p_sign/playAuth/sign/token`, `app_id/appId` | `qcloudAuth` | ✓ |
| `media.streamingInfo.*.url`, `url/path/src`, `playUrl/videoUrl/m3u8Url/audioUrl/fileUrl` | `findMediaURL` recursive parse | ✓ |
| lesson/course titles `lessonName/dirName/chapterName/title/name/courseName/prodName` | `entriesFromPayload` title fallback | ✓ |

## 阻塞步骤

无。
