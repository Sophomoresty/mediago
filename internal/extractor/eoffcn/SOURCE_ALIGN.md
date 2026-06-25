# eoffcn 源码对齐对照

## URL 常量

| .cdc.py 行 | eoffcn.go 行/名 | 一致? |
|---|---|---|
| Eoffcn_Course.py:41 `order_url = 'https://xue.eoffcn.com/api/order/complete'` | `order_url` | ✓ |
| Eoffcn_Course.py:42 `new_order_url = 'https://xue.eoffcn.com/api/new/goods/list'` | `new_order_url` | ✓ |
| Eoffcn_Course.py:43 `package_url = 'https://xue.eoffcn.com/api/package/list?system_order={system_order:}&coding={coding:}'` | `package_url` with `%s` | ✓ |
| Eoffcn_Course.py:44 `catagory_url = 'https://xue.eoffcn.com/api/lesson/catagory?package_id={package_id:}&system_order={system_order:}'` | `catagory_url` with `%s` | ✓ |
| Eoffcn_Course.py:45 `course_list_url = 'https://xue.eoffcn.com/api/new/course/list?system_order={system_order:}'` | `course_list_url` with `%s` | ✓ |
| Eoffcn_Course.py:46 `lesson_url = 'https://xue.eoffcn.com/api/lesson/detail?lesson_id={lid:}&package_id={cid:}&module_type={m_type:}&system_order={system_order:}'` | `lesson_url` with `%s` | ✓ |
| Eoffcn_Base.py:118 `https://xue.eoffcn.com/api/check/member` | `check_member_url` | ✓ |
| Eoffcn_Course.py:47-48 public key / watch demand APIs | `pub_key_url`, `encrypt_url` | ✓ |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
|---|---|---|---|
| `_check_cookie` line 118 | `Extract` (cookie validation) | GET | ✓ |
| `_get_order_list` / `_get_new_order_list` / `_get_cid` lines 72-214 | `fetchOldOrders`, `selectOrder`, `fetchSpuIDFromPage`, `mergeOrderParams` | GET | ✓ |
| `_get_courses_list` line 175 | `resolveCourse` | GET | ✓ |
| `_get_package_list` line 155 | `resolveCourse` | GET | ✓ |
| `_get_catagory_info` line 199 | `resolveCourse` | GET | ✓ |
| `_get_lesson_info` line 265 | `resolveLesson` | GET | ✓ |
| `_get_m3u8_info` line 330 (AES decrypt live_url) | `findMediaURL` + `aesDecryptLiveURL` | - | ✓ |
| `_get_pub_key` / `_request_watch_demand_data` | `requestWatchDemand` | GET + POST form | ✓ (plain POST; RSA crypto not implemented) |

## AES Decryption

| 源码 | Go 映射 | 一致? |
|---|---|---|
| `_get_m3u8_info` line 330: AES key `1234567898882222`, IV `8NONwyJtHesysWpM` | `aesDecryptLiveURL` with `aesKey`/`aesIV` | ✓ |
| `_get_file` line 697: same AES key/IV for download_path | `aesDecryptLiveURL` (same function) | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 映射 | 一致? |
|---|---|---|
| `data.list[]` | recursive `collectLessonNodes` over `data`, `list`, `items` | ✓ |
| `level_name`, `module_type`, `id`, `room_id`, `file_id`, `child` | `lessonNode` fields | ✓ |
| lesson `data.video_url` / `data.live_url` | `findMediaURL` keys `video_url`, `live_url` | ✓ |
| lesson `data.live_url` (AES-encrypted) | `findMediaURL` → `aesDecryptLiveURL` | ✓ |
| watch demand `data.live_url` | `requestWatchDemand` + `findMediaURL` | ✓ |

## 阻塞步骤

| 源码方法 | 状态 | 说明 |
|---|---|---|
| `_decrypt_video_key` / `_request_watch_demand_data` RSA crypto | blocked | requires RSA public key encryption (`rsa_encrypt`) to POST to watch_demand; DRM flow, fail closed |
| `_download_eoffcn_board_playback` board rendering | blocked | requires Eoffcn_Local whiteboard SDK + Edge/Chrome renderer; local tool, not HTTP extraction |

未解析到 `live_url` / `video_url` 时返回明确错误 (包含 RSA 提示), 不返回空 Streams.
