# gaotu 源码对齐对照

## URL 常量

| .cdc.py 行 | gaotu.go 行/名 | 一致? |
|---|---|---|
| Gaotu_Course.py:37 `course_url = 'https://api.gaotu.cn/studyPlatform/v1/unit/clazz/list?isDebounce=true&os=h5-pc&p_client=1'` | `courseURLFormat` + `endpointsFor()` | ✓ |
| Gaotu_Tutu.py:41 / Gaotu_Gaozhong.py:41 / Gaotu_Suyang.py:41 brand-specific `course_url` | `endpointsFor()` selects `api.gaotu100.com` / `api.gtgz.cn` / `api.naiyouxuexi.com` and `p_client=2/8/18` | ✓ |
| Gaotu_Course.py:38 `info_url = 'https://interactive.gaotu.cn/live/api/studyCenter/v1/user/pc/clazz/detail'` | `infoURLFormat` + `endpointsFor()` | ✓ |
| Gaotu_Course.py:39 `video_url = 'https://api.gaotu.cn/live/zplan/login/videoLive'` | `videoURLFormat` + `endpointsFor()` | ✓ |
| Gaotu_Course.py:40 `live_url = 'https://interactive.gaotu.cn/live/api/live/zplan/playbackWeb'` | `liveURLFormat` + `endpointsFor()` | ✓ |
| Gaotu_Course.py:41-42 Wenzai `getPlayUrl` / `getPlaybackInfoV4` | `video_play_url`, `live_play_url` with `%s` | ✓ |
| Gaotu_Course.py:43-45 pan/file/price APIs | `sourceURLFormat`, `fileURLFormat`, `priceURLFormat` | ✓ |
| Gaotu_Base.py:29 and brand subclasses `order_url` | `orderURLFormat` + `endpointsFor()` selects `api.gaotu.cn` / `api.gaotu100.com` / `api.gtgz.cn` / `api.naiyouxuexi.com` | ✓ |
| Brand `User-Agent` strings | `gaotuUserAgent()` + `endpointsFor()` app/version mapping | ✓ |
| Python API/interactive hostnames | `patterns` matches `api.*` / `interactive.*` hosts for `gaotu.cn`, `gaotu100.com`, `gtgz.cn`, `naiyouxuexi.com`; `endpointsFor()` maps them back to the same brand endpoints | ✓ |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
|---|---|---|---|
| `_get_course_list` line 61 | `fetchGaotuCourseList` list-only/root URL flow + `resolveCourse` fallback, `gaotuCourseListRequestPayload()` pages 1..9 | POST JSON | ✓ |
| `_get_infos` line 158 | `resolveCourse` | POST JSON | ✓ |
| `_get_video_url` line 200 / bytecode payload `liveId,sid,sessionId=0,roleType=0` | `resolveLesson` + `gaotuVideoRequestPayload()` | POST JSON | ✓ |
| `_get_live_url` line 281 / bytecode payload `liveId,sessionId=0,roleType=0` | `resolveLesson` + `gaotuLiveRequestPayload()` + `postFormJSON()` | POST form, then JSON decode | ✓ |
| `_decode_video_url` / `_decode_inner_live_url`; direct `api.wenzaizhibo.com` pcUrl | `decodePcURL`, `decodeWenzaiPCURL`, `playbackURLVariants()`, `directGaotuPCURL()` | GET, V4 -> V3 -> legacy `getPlaybackInfo&end_type=4` fallback | ✓ |
| `_get_order_price` | `fetchGaotuOrderPrice` | POST JSON | ✓ |
| `_check_cookie` extracts `__user_token__` into `Sid`, `userId` into `Uid`, keeps Cookie header | `gaotuAuthFromCookies` | Cookie jar -> `Cookie`/`Sid`/`Uid` headers | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 映射 | 一致? |
|---|---|---|
| `data.clazzDetailChapterPcVO.chapterItemVOList[].lessonCardList[]` | `collectLessons` keys `chapterItemVOList`, `lessonCardList` | ✓ |
| `userClazzLessonBaseVO.clazzLessonName` | `lessonNode.Title` | ✓ |
| `userClazzLessonBaseVO.clazzLessonNumber` | `lessonNode.ID` | ✓ |
| `userClazzLessonBaseVO.bindType` | `lessonNode.Kind`, routes VOD vs live payload | ✓ |
| `data.videoLiveDTO.pcUrl` / `data.signinLivePlayback.pcUrl`, plus raw Wenzai pcUrl input | `mediaFromPayload` + `collectStrings("pcUrl")` + `directGaotuPCURL` | ✓ |
| Wenzai `data.play_info` / `data.signinLivePlayback` `cdn_list[].url/enc_url` | `gaotuMediaURLFromPayload`, `pickGaotuPlaybackURL`, `decodeBjcloudvod` | ✓ |
| `data.payOrderList[].orderBaseVO.course.courseId` + `paymentInfo.originalPrice` | `gaotuOrderPriceFromPayload` | ✓ |

## 阻塞步骤

无. 无法从 `pcUrl` 或 `cdn_list` 解出媒体时返回明确错误, 不返回空 Streams.
