# koolearn 源码对齐对照

## URL 常量

| .cdc.py 行 | koolearn.go 行/名 | 一致? |
|---|---|---|
| Koolearn_Base.py:31-33 home/order/detail URL | koolearn.go:31-33 `urlHome` / `urlOrderIndex` / `urlOrderDetail` | ✓, `{}` -> `%s` |
| Koolearn_App.py:34-35 study/my-data URL | koolearn.go:34-35 `urlStudyHome` / `urlMyData` | ✓, `{type:}` -> `%s` |
| Koolearn_Base.py:291 `https://i.koolearn.com/logininfo` | koolearn.go:36 `urlLoginInfo` | ✓ |
| Koolearn_Base.py:304 `https://api.roombox.xdf.cn/api/login/fetchToken/{}` | koolearn.go:37 `urlFetchToken` | ✓, `{}` -> `%s` |
| Koolearn_Roombox.py:37-41 roombox course/schedule/lessons/playback/module URL | koolearn.go:38-42 `urlRoomCourse` / `urlRoomSchedule` / `urlRoomLessons` / `urlPlaybackInfo` / `urlPlaybackModule` | ✓, placeholders -> `%s` |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Koolearn_Base._check_cookie lines 303-314, read `XDF_H5_TOKEN` then fetchToken | koolearn.go:67 + 101 `cookieValue` / `fetchRoomboxToken` | GET | ✓ |
| Koolearn_Roombox._get_infos lines 346-365, `class/lessons?classId={cid}&token={token}` | koolearn.go:124 `fetchRoomboxLessons` | GET | ✓ |
| Koolearn_Roombox._get_video_url lines 443-460, `module/info?classroomId={room_id}&module=playback` | koolearn.go:177 `fetchRoomboxModuleURL` | GET | ✓ |
| Koolearn_Roombox._get_live_info lines 409-437, `module/info/playback?classroomId={live_id}` with `Token` | koolearn.go:194 `fetchRoomboxLiveEntry` | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct tag | 一致? |
|---|---|---|
| fetchToken response regex `"token"` | `Token` `json:"token"` and `Data.Token` in koolearn.go:106-110 | ✓ |
| result.get('data', {}).get('list', []) | `Data.List` `json:"list"` in koolearn.go:129-132 | ✓ |
| item.get('id'/'room_id'/'class_id'/'classroom_name') | `ID/RoomID/ClassID/ClassroomName` tags in koolearn.go:140-150 | ✓ |
| info.get('playback', {}).get('urls'/'videoUrl'/'recordedMedia') | `Playback.URLs/VideoURL/RecordedMedia` tags in koolearn.go:153-157 | ✓ |
| recordedMedia.get('url') | `URL` `json:"url"` in koolearn.go:159-160 | ✓ |

## 我的课程发现 (my-data discovery)

| 源码方法 (line) | Go 函数 | method | 一致? |
|---|---|---|---|
| Koolearn_App._get_course_list, `my-data?type={type:}` | `fetchCourseList` | GET | ✓ |
| Koolearn_App._select_my_course, `_get_course_list() + _get_course_list('hide')` | `discoverMyCourses` 遍历 `["", "hide"]` | - | ✓ |
| Koolearn_Base._check_cookie (`'"status"\s*:\s*0'` @ i.koolearn.com/logininfo) | `koolearnLogined` / `loginStatusRe` | GET | ✓ |

### 字段/过滤

| 源码 key 链 | Go | 一致? |
|---|---|---|
| `data` 顶层 list, item.aggregatedItems[].productDisplayName/url 否则 item.productName/url | `myDataCourse` + `appendCourseLink` | ✓ |
| 过滤正则 `((/tongyong/)|(/schedule/)|(/ky/)|(/fer/)|(/1v1/)|(/chuguo/)|(/tiny-class/)|(small-?class))` | `courseLinkRe` | ✓ |

## 阻塞步骤

无. order.koolearn.com/user_order/index/detail 仅用于价格校验 (data-id/data-money, 按已知 order_no), 非课程枚举源, 故不接入发现流程.
