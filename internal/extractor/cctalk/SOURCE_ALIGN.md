# CCTalk 源码对齐

## URL 常量

| .cdc.py 行 | cctalk.go 行/名 | 一致? |
|---|---|---|
| `Cctalk_Config.pyc.1shot.cdc.py:55-62` `CCTALK_BASE_URL`, `CCTALK_CONTENT_API_V1/V11/V12`, `CCTALK_PCWEB_KEY`, `CCTALK_TENANT_ID`, `CCTALK_USER_AGENT` | `cctalk.go:15-21` | ✓ |
| `Cctalk_Course.pyc.1shot.cdc.py:1901-1903` `my_group_list`, `mycourse`, mobile origin | `cctalk.go:23-25` | ✓ |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
|---|---|---|---|
| `_api_url` / `_request_api` (`Cctalk_Course.pyc.1shot.cdc.py:984-1010`) | `apiURL` / `requestAPI` (`cctalk.go:104-125`) | GET/POST | ✓ |
| `_get_course_structs` (`Cctalk_Course.pyc.1shot.cdc.py:1616-1630`) | `getCourseStructs` (`cctalk.go:145-156`) | GET | ✓ |
| `_get_series_all_lesson_list` / `_get_series_content_list` (`Cctalk_Course.pyc.1shot.cdc.py:1313-1412`) | `getSeriesStructs` (`cctalk.go:158-177`) | GET | ✓ |
| `_get_group_video_list` (`Cctalk_Course.pyc.1shot.cdc.py:1226-1249`) | `getGroupVideoList` (`cctalk.go:179-184`) | GET | ✓ |
| `_get_video_play_info` (`Cctalk_Course.pyc.1shot.cdc.py:1782-1859`) | `getVideoPlayInfo` (`cctalk.go:186-197`) | GET + POST | ✓ |
| OCS `_resolve_ocs_v55` / `_build_ocs_v55_media_result` (`all_decrypted.json` `Cctalk_Base__t3414`, `Cctalk_Base__t3376`) | `resolveOCSStream` / `buildOCSStreamFromPayload` / `rewriteV55M3U8Text` (`ocs.go`) | GET + m3u8 data stream | ✓ |
| `_get_article_detail` (`Cctalk_Course.pyc.1shot.cdc.py:2544`) | `getArticleDetail` (`resources.go`) | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 解析点 | 一致? |
|---|---|---|
| `_extract_data`: `data` / `Data` / `result` / `Result` | `extractData` (`cctalk.go:254-263`) | ✓ |
| `_extract_list`: `items`, `list`, `lessonList`, `videoList`, `contentList` | `extractList` (`cctalk.go:265-277`) | ✓ |
| `_walk_nodes`: `children`, `lessons`, `lessonList`, `items`, `list`, `contentList`, `videoList`, `mediaList`, `playList` | `walkMaps` (`cctalk.go:279-297`) | ✓ |
| `_build_video_info`: `videoUrl`, `playUrl`, `m3u8Url`, `hlsUrl`, `mediaUrl`, `mediaURL`, `mp4URL`, `url` | `findMediaURL` (`cctalk.go:299-324`) | ✓ |
| `_node_title`: `lessonName`, `videoName`, `contentName`, `title`, `name`, `subject` | `mediaFromMap` (`cctalk.go:216-230`) | ✓ |
| `_extract_courseware_info`: `coursewareInfo/courseWareInfo/ocsInfo/videoInfo/mediaInfo/contentInfo/resourceInfo/playInfo/activityInfo/lessonInfo`, `coursewareId`, `tenantId`, `userSign`, `sourceType/contentType`, media/file URLs | `extractCoursewareInfo` / `collectCoursewareInfo` (`ocs.go`) | ✓ |
| v55 payload: `m3u8s[].content`, `resourceId/resourceID`, `key`, `iv`, `cdnHosts`, `_lel_cryptor_type=aes_cbc_segment` | `findV55M3U8Item`, `v55KeyLine`, `rewriteV55M3U8Text`; download fallback parses `EXT-X-KEY` AES-128 in `hls.go` | ✓ |
| board playback classification: `board`, `whiteboard`, `sourceType/contentType/type` | `isBoardPayload` / `playbackType` (`ocs.go`) | ✓ |
| `_iter_material_candidates`: `materials/materialList/coursewareList/resourceList/resources/attachments/files/docs` | `iterMaterialCandidates` (`resources.go`) | ✓ |
| `_build_file_info`: `fileUrl/resourceUrl/materialUrl/attachUrl/downloadUrl/url`, `fileName/resourceName/materialName/attachName`, `fileSize/size/totalSize` | `fileEntry`, `looksLikeFileInfo`, `guessFileExt` (`resources.go`) | ✓ |
| `_build_article_info` / `_build_article_html`: `articleInfo.articleId/articleName`, body/detail fields, `publishTime`, `viewCount` | `articleEntry`, `buildArticleHTML` (`resources.go`) | ✓ |

## 阻塞步骤

无。加密 OCS v55 的私有响应解密体在 protected Base 中未给出完整可移植源码; Go 路径覆盖可见 v55 payload 中的 `m3u8s/content/key/iv` 并在下载层执行 AES-128 segment 解密。
