# renrenjiang 源码对齐对照

## URL 常量

| .cdc.py 行 | renrenjiang.go 行/名 | 一致? |
|---|---|---|
| Renrenjiang_Config.pyc:21-27 `API_HOST`, `REFERER`, `ORIGIN`, `QCLOUD_APP_ID`, `QCLOUD_PLAY_API` | `API_HOST`, `REFERER`, `ORIGIN`, `QCLOUD_APP_ID`, `QCLOUD_PLAY_API` | ✓ |
| Renrenjiang_Course.pyc:29-40 `columns_subscribed_api` 等 | `columns_subscribed_api`, `activities_subscribed_api`, `column_detail_api`, `activity_stream_api`, `activity_stream_url_api`, `column_docs_api`, `activity_docs_api` | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 | method | 一致? |
|---|---|---|---|
| `_get_course_list` 206-265 | `getCourseList` / `getPagedItems` | GET | ✓ |
| `_get_cid` 271-307 | `parseCourseID` | regex | ✓ |
| `_get_column_detail` 311-330 | `Extract` | GET | ✓ |
| `_get_activity_detail` 336-355 | `Extract` | GET | ✓ |
| `_get_column_infos` 655-734 | `getColumnActivities` | GET | ✓ |
| `_get_activity_infos` 738-758 | `resolveActivity` | GET | ✓ |
| `_get_activity_stream_info` / `_get_activity_stream_url_info` 886-903 | `resolveActivityStream` | GET | ✓ |
| `_get_qcloud_play_url` 840-856 | `getQCloudPlayURL` | GET | ✓ |
| `_get_column_documents` / `_get_activity_documents` 523-595 | `getDocuments` | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct / map 访问 | 一致? |
|---|---|---|
| `data.list` / `data.activities` / `data.documents` | `extractItems(..., "list", "activities", "documents")` | ✓ |
| `column.title` / `activity.title` | `textAt(..., "title", "name")` | ✓ |
| `column.price` / `activity.price` | `textAt(..., "price")` + `safe` 解析 | ✓ |
| `data.hls_url` / `stream_url` / `rtmp_url` | `extractStream` 候选键 | ✓ |
| `media.transcodeInfo.transcodeList[].url` / `masterPlayList.url` / `sourceVideo.url` | `pickQCloudURL` 递归收集 | ✓ |
| `fileId` / `pSign` | `getQCloudPlayURL` 入参 | ✓ |

## 阻塞步骤

无.
