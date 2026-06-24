# caixuetang source alignment

Decompiler sources:

- `~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/Caixuetang/Caixuetang_Base.pyc.1shot.cdc.py`
- `~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/Caixuetang/Caixuetang_Course.pyc.1shot.cdc.py`
- bytecode listing used for truncated course tail: `~/code/xwz-downloader-source-release/python-bytecode-listings/Caixuetang_Course.pyc.dis`

| Source construct | Decompiled line(s) | Go implementation | Status |
| --- | ---: | --- | --- |
| Referer/origin/agent/appcode constants | Base 31-36 | `refererURL`, `originURL`, `agentURL`, `appcode` in `caixuetang.go:17-21` | Aligned |
| `_api_url` and `_api_data` prepend agent host, add `client_type`, `member_id`, `key`, `appcode` | Base 200-233 | `apiURL`, `apiData` in `caixuetang.go:151-172` | Aligned |
| `_post_json` form POST plus JSON parse | Base 238-264 | `postJSON` uses `PostForm` and `json.Unmarshal` in `caixuetang.go:181-191` | Aligned |
| Cookie/member check `memberinfo` | Base 294-321 | `checkCookie` posts `userInfoAPI` in `caixuetang.go:193-206` | Aligned |
| Course list APIs `mycourse`, `myvipcourse`, `course_type_new=1/14` | Course 31-32, 185-187 | `getCourseList` jobs in `caixuetang.go:216-243` | Aligned |
| Play info APIs `webplayinfo` and `playinfo` | Course 33-34, 370-402 | `getPlayinfo` tries `webplayInfoAPI` and `playinfoAPI` in `caixuetang.go:245-274` | Aligned |
| Chapter root keys | bytecode `_find_chapter_roots` | `findChapterRoots` in `helpers.go:10-16` | Aligned |
| Child traversal keys | bytecode `_iter_children` | `iterChildren` in `helpers.go:17-23` | Aligned |
| Video/file node recognition and metadata keys | bytecode `_looks_video_node`, `_parse_video_info`, `_parse_file_info` | `looksVideoNode`, `looksFileNode`, `parseVideoInfo`, `parseFileInfo` in `helpers.go:24-43` | Aligned |
| Play URL extraction and quality selection | bytecode `_extract_play_url` | `extractPlayURL`, `pickByDefinition`, `candidateURL` in `helpers.go:54-124` | Aligned |
| Material play APIs `getwebplayinfo` and `getvideoplay` | Course 35-36, bytecode `_get_video_url` | `getVideoURL` / `getVideoURLFromAPI` in `caixuetang.go:344-369` | Aligned |
| Download task/info APIs | Course 37-39 | `generatedDownloadURL` in `caixuetang.go:370-389` | Aligned |
| Output entries | course parse chain | `Extract` returns top-level `MediaInfo{Entries: entries}` in `caixuetang.go:54-87` | Aligned |

## Self-review

| Check | Result |
| --- | --- |
| Real HTTP calls present | `PostForm` for memberinfo, course list, playinfo, material play, and download APIs. |
| Real parsing present | `json.Unmarshal` for API JSON, plus recursive JSON tree parsing helpers. |
| Stub text absent | No stub sentinel phrases remain. |
| Single-api rule | Implements the agent-host form API chain directly; no csslcloud helper is used. |
| File layout | Helper functions are split into `helpers.go` to keep each Go file below the 400-line worker limit. |
