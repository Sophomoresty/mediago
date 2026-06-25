# icve 源码对齐对照

## URL 常量

| .cdc.py 行 | icve.go 行/名 | 一致? |
|---|---|---|
| Icve_Ai.pyc.1shot.cdc.py:39 `url_title = 'https://ai.icve.com.cn/prod-api/course/courseInfo/getLatestInfoByCourseId?courseId={cid}'` | icve.go `urlTitle = "https://ai.icve.com.cn/prod-api/course/courseInfo/getLatestInfoByCourseId?courseId=%s"` | ✓ |
| Icve_Ai.pyc.1shot.cdc.py:40 `url_info = 'https://ai.icve.com.cn/prod-api/course/courseDesign/getDesignList?courseInfoId={inf_id}&courseId={cid}'` | icve.go `urlInfo = "https://ai.icve.com.cn/prod-api/course/courseDesign/getDesignList?courseInfoId=%s&courseId=%s"` | ✓ |
| Icve_Ai.pyc.1shot.cdc.py:41 `url_cell = 'https://ai.icve.com.cn/prod-api/course/courseDesign/getCellList?courseInfoId={inf_id}&courseId={cid}&parentId={parent_id}'` | icve.go `urlCell = "https://ai.icve.com.cn/prod-api/course/courseDesign/getCellList?courseInfoId=%s&courseId=%s&parentId=%s"` | ✓ |
| Icve_Ai.pyc.1shot.cdc.py:42 `url_source_status = 'https://upload.icve.com.cn/{content:}/status'` | icve.go `urlSourceStatus = "https://upload.icve.com.cn/%s/status"` | ✓ |
| Icve_Base.pyc.1shot.cdc.py:38 `smartedu_query_url = 'https://vocational.smartedu.cn/gjzyjy/inco/ht/queryList'` | icve.go `smarteduQueryURL = "https://vocational.smartedu.cn/gjzyjy/inco/ht/queryList"` | ✓ |
| Icve_Base.pyc.1shot.cdc.py:39 `smartedu_detail_sqlid = '171695011763866a394676496125233763746e2fbd87ebc94'` | icve.go `smarteduDetailSQLID = "171695011763866a394676496125233763746e2fbd87ebc94"` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Icve_Ai._get_title lines 112-132 | icve.go `loadTitle` | GET | ✓ |
| Icve_Ai._get_infos lines 141-176 | icve.go `loadItems` | GET | ✓ |
| Icve_Ai._get_cell_info lines 182-260 | icve.go `loadCellItems`, helpers.go `collectAIItems` | GET | ✓ |
| Icve_Ai._get_inner_infos lines 266-292 | helpers.go `collectAIItems` | local JSON tree walk | ✓ |
| Icve_Ai._get_video_url lines 298-361 | icve.go `getVideoURL`, `selectTranscodedURL` | GET + HEAD-like check via GET | ✓ |
| Icve_Ai._get_file_url lines 367-386 | icve.go `getFileURL` | local JSON parse | ✓ |
| Icve_Base._video_quality_candidates lines 288-296 | icve.go `videoQualityCandidates` | local selection | ✓ |
| Icve_Base._select_video_quality lines 302-313 | icve.go `selectVideoQuality` | local selection | ✓ |
| Icve_Base._smartedu_encrypt_param lines 41-52 | icve.go `smarteduEncryptParam` | AES-CBC encrypt + base64 | ✓ |
| Icve_Base.get_redirect_url lines 57-94 | icve.go `resolveSmartEduURL` | POST JSON to queryList | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct tag / map access | 一致? |
|---|---|---|
| `json.loads(title).get('data',{}).get('id','')` | `aiTitleResp.Data.ID` `json:"id"` | ✓ |
| `json.loads(title).get('data',{}).get('courseName','')` | `aiTitleResp.Data.CourseName` `json:"courseName"` | ✓ |
| `json.loads(title).get('data',{}).get('schoolName','')` | `aiTitleResp.Data.SchoolName` `json:"schoolName"` | ✓ |
| `json.loads(info).get('data', [])` sorted by `sort` | `parseJSONMap` → `listAt(root,"data")` → `sortBySort` | ✓ |
| cell item `id`, `name`, `children`, `fileType`, `fileUrl` | `str(node["id"])`, `str(node["name"])`, `childList`, `node["fileType"]`, `fileInfoText(node["fileUrl"])` | ✓ |
| video info `ossOriUrl`, `ossGenUrl`, `content`, `url` | `data["ossOriUrl"]`, `data["ossGenUrl"]`, `data["content"]`, `data["url"]` | ✓ |
| source status `args`, `type`, quality booleans (`720p`, `480p`, `360p`, `1080p`) | `mapAt(status,"args")`, `status["type"]`, `args[q]` | ✓ |
| file info `ossOriUrl`, fallback `url` | `data["ossOriUrl"]`, `data["url"]` | ✓ |
| smartedu queryList response `content[0].fwdz` | `mapsFromAny(result["content"])[0]["fwdz"]` | ✓ |

## 认证与 header

| 源码位置 | Go 对齐 | 一致? |
|---|---|---|
| Icve_Ai.pyc.1shot.cdc.py:78-93 `_check_cookie` / `set_cookie` 均返回 `True` | `Extract` 允许空 CookieJar, 注册 `NeedAuth: false` | ✓ |
| Icve_Base.pyc.1shot.cdc.py:104-112 `Sec-Fetch-*`, `Sec-Ch-Ua-*`, `Referer`, `cookie` | `newCtx` 同名 header | ✓ |
| Icve_Base.get_redirect_url `Origin: smartedu_referer`, `Referer: url` | `resolveSmartEduURL` headers `Origin: smarteduReferer`, `Referer: rawURL` | ✓ |

## Crypto

| 源码位置 | Go 对齐 | 一致? |
|---|---|---|
| Icve_Base._smartedu_encrypt_param: AES.new(key=`inco12345678ocni`, MODE_CBC, iv=`ocni12345678inco`) + null-pad + base64 | `smarteduEncryptParam`: AES-CBC with same key/iv, null-byte pad, base64 | ✓ |

## 返回结构

| 源码行为 | Go 行为 | 一致? |
|---|---|---|
| `_download_course` 遍历 `video_list` / `file_list` 和嵌套章节 | `mediaFromItems` 返回 `MediaInfo.Entries`, 每个 entry 带一个 stream | ✓ |
| `mode == ONLY_PDF` 时 `_download_video_list` 跳过视频 | `mediaFromItems` 在 `ONLY_PDF` 下跳过 video entry | ✓ |

## URL Pattern 覆盖

| 源码 regex | Go pattern | 一致? |
|---|---|---|
| Mooc_Config `courses_re['Icve_Ai']` | patterns[0] `ai.icve.com.cn/.*?excellent.*?/` or `ai.icve.com.cn/.*?course.*?/` | ✓ |
| Mooc_Config `courses_redirect_re['Icve_Base']` (vocational.smartedu.cn/Details) | patterns[1] + `resolveSmartEduURL` with `net/url` param parse | ✓ |

## 阻塞步骤

无。
