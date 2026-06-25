# med66 源码对齐对照

## URL 常量

| .cdc.py 行 | med66.go 行/名 | 一致? |
|---|---|---|
| `Med66_Config.py:36 LOGIN_URL = 'https://www.med66.com/OtherItem/loginAgain/index.shtml'` | `med66.go:19 LOGIN_URL` | ✓ |
| `Med66_Config.py:37 MEMBER_HOME_URL = 'https://member.med66.com/homes/mycourse'` | `med66.go:20 MEMBER_HOME_URL` | ✓ |
| `Med66_Config.py:38 COURSE_INFO_URL = 'https://member.med66.com/homes/mycourse/courseInfo'` | `med66.go:21 COURSE_INFO_URL` | ✓ |
| `Med66_Config.py:39 COURSEWARE_INFO_URL = 'https://member.med66.com/homes/course/courseClassWareInfo'` | `med66.go:22 COURSEWARE_INFO_URL` | ✓ |
| `Med66_Config.py:41 ELEARNING_HOME_URL = 'https://elearning.med66.com/'` | `med66.go:23 ELEARNING_HOME_URL` | ✓ |
| `Med66_Course.py:41 live_replay_referer = 'https://live.cdeledu.com/'` | `med66.go:25 LIVE_REFERER_URL` | ✓ |
| `Med66_Course.py:42 live_replay_info_url = 'https://live.cdeledu.com/liveapi/entry/getReplayInfo'` | `med66.go:24 LIVE_REPLAY_INFO_URL` | ✓ |
| `Med66_Config.py:42 MATERIALS_URL = 'https://elearning.med66.com/xcware/download/teachingMaterials.shtm?cwareID={cware_id}&iskcjy=1&identity={identity}'` | `med66.go:26 MATERIALS_URL` | ✓ |
| `Med66_Config.py:43 MATERIAL_DOWNLOAD_URL = 'https://elearning.med66.com/data2file/downloadFile/getWordVipFile?cwareID=&fileUrl={file_url}&fileReName={file_name}'` | `med66.go:27 MATERIAL_DOWNLOAD_URL` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---:|---|
| `Med66_Course._get_course_list` line 137: `_post_form_json(COURSE_INFO_URL,{})` | `fetchCourse` line 172: `PostForm(COURSE_INFO_URL,{})` | POST form | ✓ |
| `Med66_Course._get_coursewares` line 446: `_post_form_json(COURSEWARE_INFO_URL,payload)` | `fetchCoursewares` line 209: `PostForm(COURSEWARE_INFO_URL,form)` | POST form | ✓ |
| `Med66_Course._resolve_live_replay_payload` lines 1337,1347: GET play URL then `getReplayInfo` JSON | `resolveReplayPayload` lines 634,648 | GET + GET JSON | ✓ |
| `Med66_Course._login_live_replay_cc` lines 1399-1411 and `_get_live_replay_context` line 1132 | `resolveReplayEntry` lines 574-586 via `shared.CssLcloudResolvePlayInfo` | CSSL helper | ✓ |
| `Med66_Course._prepare_live_replay_m3u8_text` line 1622 | `resolveReplayEntry` lines 610-614 via `shared.CssLcloudRewriteM3U8Keys` | GET m3u8 + key rewrite | ✓ |
| `Zhengbao_Course._resolve_video_play_info` line 672: GET play page + h5Vars | `resolveRegularVideo` line 346: `GetString(playURL)` + `parseH5Vars` | GET + parse JSON | ✓ |
| `Zhengbao_Course._parse_material_tree` line 531: GET materials_url HTML | `parseMaterialTree` line 434: `GetString(MATERIALS_URL)` + `extractAttrs` | GET HTML + parse attrs | ✓ |
| `Zhengbao_Course._build_material_url` line 749: build download URL | `buildMaterialDownloadURL` line 504 | URL template | ✓ |
| `Med66_Course._is_direct_material` line 461: check showType=5 / file ext | `isDirectMaterial` line 238 | check logic | ✓ |
| `Med66_Course._build_direct_material_tree` line 479: direct file download | `buildDirectMaterialEntry` line 257 | build entry | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct/tag 或解析 | 一致? |
|---|---|---|
| `COURSE_INFO_URL -> data/result list -> courseId, eduSubjectId, classType, classId, linkedCourseIds` | `collectMaps` + `med66Course` fields in `fetchCourse` | ✓ |
| `COURSEWARE_INFO_URL -> homeCwareList/homeWareList/courseWareList/wareList` | `fetchCoursewares` keys list | ✓ |
| `html onclick -> goToLive('...')` | `goToLiveRe` same regex body | ✓ |
| `html onclick -> continueStudyVideo / window.open('...')` | `openURLRe` + `parseRegularVideoTree` | ✓ |
| `play page -> window.cdelmedia.h5Vars -> videoPath, srtPath` | `h5VarsRe` + `parseH5Vars` | ✓ |
| `payload['data']['replay'] or payload['replay']` | `liveReplayPayload.Data.Replay` / `Replay` | ✓ |
| `replay.liveRoomId/liveId/accessid/recordId/accesskey, token` | `liveReplayReplay` tags + `Token` | ✓ |
| `csslcloud datas.sessionId, data.vod_info.video/audio` | `shared.CssLcloudResolvePlayInfo` | ✓ |
| `materials HTML: data-fileurl, data-pdfurl, data-sepurl, data-seppdfurl` | `extractAttrs` + `parseMaterialTree` specs | ✓ |
| `MATERIAL_DOWNLOAD_URL template: {file_url}, {file_name}` | `buildMaterialDownloadURL` | ✓ |
| `showType=5, cwDirURL/cwURL direct file ext` | `isDirectMaterial` + `buildDirectMaterialEntry` | ✓ |

## 阻塞步骤

无。CSSL replay 登录, vod 解析和 m3u8 key 处理复用 `internal/extractor/shared/csslcloud.go`。
普通录播视频通过 h5Vars 解析, 资料文件通过 MATERIALS_URL HTML 解析+MATERIAL_DOWNLOAD_URL 模板构建。
直接资料文件通过 showType=5 或文件扩展名识别后直接用 cwDirURL/cwURL。
