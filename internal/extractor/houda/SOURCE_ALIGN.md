# houda source alignment

## URL constants

| .cdc.py line | houda.go const | match |
|---|---|---|
| Houda_Base.py:31-32 `referer`, `origin` | `urlOrigin`, `urlHome` | Y |
| Houda_Course.py:35-41 course/lesson/live/material/CC APIs | `urlCourseList` / `urlStageLaw` / `urlLearnCourse` / `urlLearnCoursePage` / `urlLiveDetail` / `urlMaterial` / `urlCCViewPlayback` | Y |
| Houda_Course.py:42-45 CSSL replay URLs | `urlCsslLogin` / `urlCsslPlay` / `urlCsslMeta` / `urlCsslOrigin` | Y |
| Houda_Course.py:46-50 CSSL device/tpl/terminal/material | `csslDeviceType` / `csslDeviceVersion` / `csslTpl` / `csslTerminal` / `materialServiceTyp` | Y |

## HTTP calls

| source method | Go function | method | match |
|---|---|---|---|
| Houda_Base._check_cookie (t147) | `checkHoudaCookie` | GET | Y |
| Houda_Course._request_houda (t116) | `requestHouda` | POST form + data=json fallback | Y |
| Houda_Course._get_course_list (t208) | `fetchHoudaCourseList` + `parseHoudaCourseList` + `chooseHoudaCourse` | POST form | Y |
| Houda_Course._get_stage_law_data (t279) | `fetchHoudaStageLaw` + `collectHoudaLawRefs` | POST form | Y |
| Houda_Course._get_lesson_list (t289) | `fetchHoudaLessons` + `parseHoudaLessons` | POST form | Y, extra fallback to `getLearnCoursePage` |
| Houda_Course._resolve_cc_callback_url (t510) | `resolveHoudaCCCallback` + `fetchCCCallbackLocation` | GET no-redirect | Y, source-aligned: allow_redirects=False + Location header + // and / prefix handling |
| Houda_Course._parse_cc_info (t542) | query param parsing in `resolveHoudaCCCallback` | parse | Y |
| Houda_Course._get_cc_session (t556) | `resolveHoudaCSSLNative` | POST JSON + GET | Y, uses source-specific endpoints /replay/user/login and /replay/video/play |
| Houda_Course._get_cc_video_url (t646) | integrated in `buildHoudaEntry` via `resolveHoudaCSSL` | - | Y |
| Houda_Course._pick_video_url (t492) | `sortHoudaVideosByQuality` | sort | Y, matches source primary/secondary URL selection + quality-key sort |
| Houda_Course._quality_key (t478) | `houdaQualityKey` | pure | Y, exact keyword matching: FHD/1080/4K=400, chaoqing=320, HD/720=240, SD/480/360=160 |
| Houda_Course._download_media_url (t655) | stream construction in `buildHoudaEntry` | - | Y, m3u8 rewriting + mp4 direct |
| Houda_Course._download_video (t676) | `buildHoudaEntry` | - | Y, direct URL + CC resolve + playback_mp3 exposed |
| Houda_Course._get_materials (t428) | `fetchHoudaMaterials` | POST form | Y |
| Houda_Course._make_file_info (t411) | `buildHoudaMaterialEntry` | - | Y |
| Houda_Course.live_detail (t39) | `hydrateHoudaLesson` + `fetchHoudaLiveDetail` | POST/GET fallback | Y |
| shared._prepare_live_replay_m3u8_text | `rewriteHoudaM3U8` -> `shared.CssLcloudRewriteM3U8Keys` | GET | Y |

## JSON field mapping

| source key chain | Go struct/function | match |
|---|---|---|
| result.data.liveList | `parseHoudaLessons` | Y |
| lesson fields: title/name/courseName/type/ccLiveId/roomId/mainRoomId/recordId/liveUrl/playbackUrl/playbackMp4/playbackMp3/stageId/stageName/lawId/lawName | `houdaLesson` struct json tags | Y |
| material: downLoadUrl/downloadUrl/fileUrl/url, title/name, fileType | `buildHoudaMaterialEntry` | Y |
| callback query: userId/roomId/recordId/viewerToken | `resolveHoudaCCCallback` query parsing | Y |
| CSSL login response: success + data.user.token | `resolveHoudaCSSLNative` login parse | Y |
| CSSL play response: data.video[].primary/secondary/desc/qualityDesc/code/quality | `houdaCsslVideoItem` struct | Y |

## Blocked steps

| source flow | reason | status |
|---|---|---|
| Houda_Local.download_board_video (t250) | Local rendering: requires OpenCV (cv2) for frame manipulation + ffmpeg for video encoding. Composites board annotations (draw_events) onto page images (page_events) with concurrent frame workers. Not reproducible via API extraction. | BLOCKED |
| Houda_Local._render_board_video (t313) | Inner renderer: cv2.imwrite, _fit_frame, _draw_strokes, _preload_page_image_cache, _encode_frames_by_copying_jpegs. All local pixel manipulation. | BLOCKED (part of board flow) |
| Houda_Local._encode_frames_by_copying_jpegs (t112) | ffmpeg concat demuxer + copy codec + audio mux. Local toolchain. | BLOCKED (part of board flow) |
| cssl_meta_url (/replay/data/meta) | Used by board video _load_resources to fetch page_events/draw_events for rendering. | BLOCKED (part of board flow) |

Board video blocked marker is emitted in entry extra as `board_video_blocked: true` + `board_video_reason`.
