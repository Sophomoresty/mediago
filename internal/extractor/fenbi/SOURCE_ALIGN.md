# fenbi 源码对齐对照

## URL 常量

| .cdc.py 行 | fenbi.go 行/名 | 一致? |
|---|---|---|
| Fenbi_Base.py:29-32 referer/origin/login check URLs | `referer`, `origin`, `login_check_url`, `ke_check_url` | ✓ |
| Fenbi_Course.py:34 `course_list_url = 'https://ke.fenbi.com/win/v3/courses'` | `course_list_url` | ✓ |
| Fenbi_Course.py:455-510 course prefix + visible lecture enumeration | `resolveVisibleLectures` / `fetchCoursePrefixes` / `fetchVisibleLectures` | ✓ |
| Fenbi_Course.py:35-39 visible/lecture/detail/summary/lectureset URLs | corresponding constants with `%s` | ✓ |
| Fenbi_Course.py:40-44 episode nodes/detail/media meta URLs | corresponding constants with `%s` | ✓ |
| Fenbi_Course.py:45-46 material URLs | `material_url`, `vertical_material_url` | ✓ |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
|---|---|---|---|
| `_check_cookie` line 199 | `checkLogin` | GET login probes | ✓ |
| `_get_lecture_detail` / `_get_title` | `resolveLecture` | GET win/api lecture detail | ✓ |
| `_get_infos` decrypted t1405 | `resolveLecture` + `collectEpisodes` | GET detail/summary/episode_nodes | ✓ |
| `_get_episode_detail` | `resolveEpisode` | GET win/api episode detail | ✓ |
| `_get_video_url` decrypted t1612 | `resolveEpisode` | GET `mediafile/meta` | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 映射 | 一致? |
|---|---|---|
| `episodes`, `episodeList`, `items`, `list`, `nodes`, `children` | recursive `collectEpisodes` | ✓ |
| `episodeId`, `episode_id`, `videoId`, `video_id`, `contentId`, `id` | `episodeNode.ID` | ✓ |
| `title`, `name`, `episodeTitle`, `videoName`, `courseTitle`, `lectureTitle` | `pickTitle` / `collectEpisodes` | ✓ |
| media meta `url`, `mediaUrl`, `path`, `files/list/streams/data` | `findMediaURL` | ✓ |
| route `prefix`, `lecture_id`, `episode_id` | `parseIDs` | ✓ |
| material candidates, url extraction, media meta fallback | `collectMaterialCandidates` / `pickURLFromResponse` / `resolveMaterialURL` / `resolveEpisode` | ✓ |

## 树遍历补全

| 源码方法 | Go 函数 | 一致? |
|---|---|---|
| `_get_lecture_set_contents` line 1087 paginated lecturesets/{id}/contents | `fetchLectureSetContents` | ✓ |
| `_summary_episode_set_entries` line 1135 discover root episode sets from summary | `fetchSummaryEpisodeSetNodes` / `extractSummaryEpisodeSetIDs` | ✓ |
| `_get_episode_nodes(prefix, lecture_id, episode_set_id)` line 1107 via my_lecture_episode_set_nodes_url | `fetchEpisodeSetNodes` | ✓ |

## 阻塞步骤

无. 媒体元数据接口没有返回可下载 URL 时返回明确错误, 不返回空 Streams.
