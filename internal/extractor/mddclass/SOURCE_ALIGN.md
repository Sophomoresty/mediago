# mddclass source alignment self-review

Source files read:

- `Mooc/Courses/Mddclass/Mddclass_Config.pyc.1shot.cdc.py`
- `Mooc/Courses/Mddclass/Mddclass_Base.pyc.1shot.cdc.py`
- `Mooc/Courses/Mddclass/Mddclass_Base.pyc.1shot.das`
- `Mooc/Courses/Mddclass/Mddclass_Course.pyc.1shot.cdc.py`
- `Mooc/Courses/Mddclass/Mddclass_Course.pyc.1shot.das`

## URL / HTTP / JSON alignment

| Source location | Source behavior | Go alignment |
|---|---|---|
| `Mddclass_Config` constants | Defines `MDDCLASS_PASS_API_HOST = https://pass-api.sksight.com`, `MDDCLASS_LEXUE_HOST = https://lexue.mddclass.com`, `MDDCLASS_ACCESS_HOST = https://access.mddclass.com`, `MDDCLASS_GLOBAL_WEBAPI_HOST = https://webapi.sksight.com`, `MDDCLASS_API_V11 = /webapi/content/v1.1`, `MDDCLASS_API_V12 = /webapi/content/v1.2`, `MDDCLASS_PCWEB_KEY = pccembed`, and the Qt user-agent string. | Constants `url0`, `url1`, `url2`, `mddclassGlobalWebAPIHost`, `mddclassAPIV11`, `mddclassAPIV12`, `mddclassPCWebKey`, `mddclassUserAgent` are copied verbatim. |
| `Mooc_Config` / `_get_cid` | Accepts `mddclass` / `墨督督` and `*.mddclass.com`; parses company subdomain, `/v/<videoId>`, `/series/<seriesId>`, `/m/series/<seriesId>`, `/group/<groupId>/series/<seriesId>`, group paths, and query keys `sid`, `seriesId`, `series_id`, `groupId`, `group_id`, `gid`, `contentId`, `videoId`, `vid`. | `mddclassParseTarget` implements those host, path, and query parsers. |
| `Mddclass_Base._web_headers` | Builds web headers: `User-Agent`, `Accept`, `Referer`, `Origin`, `Hujiang-App-Key`, `SKsight-App-Key`, `X-CC-COMPANY`, `Cookie`. | `(*mddclassSession).webHeaders` builds the same header family, with auth-context extras only when env/cookie values are supplied. |
| `Mddclass_Base._pc_content_headers` | Builds PC content headers with `HJClient ... /qt/mddclass` UA, `Accept: */*`, blank `Origin`, PC web keys, `X-CC-COMPANY`, and cookie. | `(*mddclassSession).pcContentHeaders` mirrors this; `pcClientUserAgent` uses `HJUserAgent` or device/tracert context like the source. |
| `_api_url` + `_request_json_get` | Relative API paths resolve to `https://<company>.mddclass.com` + `/webapi/content/v1.1` or `/webapi/content/v1.2`; absolute paths pass through. GET uses `_web_headers`. | `mddclassAPIURL` and `mddclassAPIGet` implement the same URL selection and GET+JSON parse. |
| `_pc_content_url` + `_request_pc_content_get` | Relative PC content paths resolve to `https://webapi.sksight.com/content/<version><path>`. GET uses `_pc_content_headers`. | `mddclassPCContentURL` and `mddclassPCContentGet` implement that global webapi route. |
| `_request_pc_content_post` | POSTs JSON to `https://webapi.sksight.com/content/<version><path>` with `Content-Type: application/json`. | `mddclassPCContentPost` POSTs JSON to the same URL family and parses JSON. |
| `_payload_success` | Success if `status in (0, "0", None)` and `code not in (-1, "-1")`. | `mddclassPayloadSuccess` matches this predicate. |
| `_payload_data` | Uses `payload["data"]`; falls back to `payload["data:"]`; only dict/list payload data is accepted. | `mddclassPayloadData` matches those keys and accepted types. |
| `_extract_list` | Recursively checks `items`, `list`, `records`, `rows`, `data`, `result`. | `mddclassExtractList` uses the same key order and recursion. |
| `_get_group_list` | For company domains, GET `https://<domain>-m.mddclass.com/webapi/content/v1.1/user/my_group_list` with `start`, `limit`, `sortType=1`, `keyword=""`; parses group ids/titles. | `mddclassFetchGroups` requests the same mobile API for `lexue`, `meixiang`, and the active domain, parsing `groupId/contentId/id/group_id` and group title keys. |
| `_get_group_series` | For a group, POST `/series/group/<groupId>/series_subscribe` on PC content API with `groupId`, `limit`, `start`, optional `userId`; fallbacks: PC content GET `/series/group/<groupId>/series_list`, then company API GET `/series/group/<groupId>/series` v1.2. | `mddclassFetchGroupSeries` implements the same primary route and both fallbacks, parsing `seriesId/id/series_id` and title keys. |
| `_get_courses` / `_load_direct_course` | Direct `seriesId` becomes `direct_series`; direct `videoId` becomes one selected video. Otherwise groups are expanded to series. | `mddclassLoadCourses` and `mddclassDirectVideo` mirror those branches. |
| `_get_infos` | GET PC content `/series/all_lesson_list`, params `showStudyTime=true`, `seriesId`, version `v1.2`; parses `data.seriesName`, `data.groupId`, `data.items[]`, `videoInfo`, `contentId`, `contentTitle`, `showIndex`, `totalSize`, `contentDuration`, `videoUrl`. | `mddclassFetchSeriesVideos` uses the same endpoint, params, version, and JSON fields to build entry metadata. |
| `_fetch_video_detail` | GET company API `/video/detail`, params `videoId`, `seriesId`, version `v1.1`; referer is `https://lexue.mddclass.com/v/<videoId>?sid=<seriesId>`; adds `TracetNo/NewTracetNo` headers; parses `data`, `companyInfo`, `coursewareInfo`, `videoUrl`, `playPermission`. | `mddclassFetchVideoDetail`, `mddclassLessonReferer`, and `tracertHeaders` align with this request and parsing behavior. |
| `_extract_courseware_info_from_detail` | Searches nested `coursewareInfo`, `courseWareInfo`, `courseware_info`, `ocsInfo`, `videoInfo`, `mediaInfo`, `contentInfo`, `resourceInfo`, `playInfo`, `detail`, `raw`; maps `coursewareId`, `tenantId`, `userSign`, `videoId`, `videoUrl/playUrl/m3u8Url/hlsUrl/mediaUrl/downloadUrl/url`, company/seller fields. | `mddclassExtractCoursewareInfo` recursively maps the same field families into normalized `coursewareId`, `tenantId`, `userSign`, `videoId`, `videoUrl`, and company fields. |
| `_normalize_media_url` / `_is_placeholder_url` | Trims media URLs, replaces `\u0026` with `&`, and rejects blank/null/404/placeholder `51b106759c84acade91a81ef83cf2eea.mp4`. | `mddclassNormalizeMediaURL` and `mddclassIsPlaceholderURL` implement those checks before producing streams. |

## Notes

- The extractor performs real HTTP requests and `json.Unmarshal` parsing for group, series, lesson, and video-detail flows.
- The source also contains a device/gateway OCS recovery path after the direct `videoUrl` branch. The Go path extracts all direct and nested media URL candidates plus raw `coursewareInfo`; if an account response only returns encrypted OCS metadata without any URL candidate, the extractor returns a concrete `no media URL in API payload` error rather than fabricating a stream.
