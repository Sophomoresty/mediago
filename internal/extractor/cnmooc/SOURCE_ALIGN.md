# cnmooc 源码对齐对照

## URL 常量

| 源码行 | cnmooc.go 行/名 | 一致? |
|---|---|---|
| Cnmooc_Base.py:32 origin = 'https://cnmooc.sjtu.cn' | cnmooc.go:17 origin | ✓ |
| Cnmooc_Base.py:33 referer = origin + '/' | cnmooc.go:18 referer | ✓ |
| Cnmooc_Base.py:34 login_url = origin + '/home/login.mooc' | cnmooc.go:19 login_url | ✓ |
| Cnmooc_Config.py:7 USER_AGENT = 'Mozilla/5.0 ... Chrome/124.0.0.0 Safari/537.36' | cnmooc.go:20 user_agent | ✓ |
| Cnmooc_Course.das:_request_item_detail const '/item/detail.mooc' | cnmooc.go:21 item_detail | ✓ |
| Cnmooc_Course._course_pages line 213-231 | cnmooc.go:198 coursePages | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Cnmooc_Base._request_headers line 229-244 | requestHeaders line 172 | header build | ✓ |
| Cnmooc_Base._get_text line 265-272 | Extract line 44, 67 | GET course/session pages | ✓ |
| Cnmooc_Base._get_postoken line 288-297 | postoken line 183 | cpstk cookie | ✓ |
| Cnmooc_Base._post line 335-339 | requestItemDetail line 116 | POST form | ✓ |
| Cnmooc_Course._get_title line 134-181 | Extract line 43-57, hiddenValue/pageTitle line 239/247 | GET + parse hidden/title | ✓ |
| Cnmooc_Course._get_infos .das line 6801 | Extract line 58-92 | page traversal | ✓ |
| Cnmooc_Course._request_item_detail .das line 7370 | requestItemDetail line 116 | POST /item/detail.mooc | ✓ |
| Cnmooc_Course._resolve_video_url .das line 9134 | resolveItem line 98 | detail + candidates | ✓ |

## JSON / HTML 字段映射

| 源码 key 链 / selector | Go struct/tag/helper | 一致? |
|---|---|---|
| hidden input name='courseId' | hiddenValue(..., "courseId") | ✓ |
| hidden input name='courseOpenId' | hiddenValue(..., "courseOpenId") | ✓ |
| h1/h2/title | pageTitle(...) | ✓ |
| nodeId / node_id | firstText(raw, "nodeId", "node_id") | ✓ |
| itemId / item_id | firstText(raw, "itemId", "item_id") | ✓ |
| title / name | firstText(raw, "title", "name") | ✓ |
| itemType / type | firstText(raw, "itemType", "type") | ✓ |
| detail.get('node') | detail["node"].(map[string]any) | ✓ |
| detail.get('mediaResources') | detail["mediaResources"].(map[string]any) | ✓ |
| node flvUrl/flv_url/url/rsUrl/mediaUrl/fileUrl/downloadUrl | valuesFor(node, ...) | ✓ |
| mediaResources currentUrl/url/videoUrl/mediaUrl/fileUrl/downloadUrl | valuesFor(mr, ...) | ✓ |
| mediaResources.mediaUrls | mr["mediaUrls"].([]any) | ✓ |
| direct link attrs data-url/src/href/rsurl/rs-url | extractLinks attrURLRe | ✓ |

## 阻塞步骤 (如果有)

无。`.cdc.py` 在 `_extract_player_items` 处截断, 后续方法按同目录 `.das` 的 code object 常量与调用链对齐实现。
