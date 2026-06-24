# meeting 源码对齐对照

## URL 常量

| .cdc.py 行 | meeting.go 行/名 | 一致? |
|---|---|---|
| Meeting_Course.py:36 referer = 'https://meeting.tencent.com' | meeting.go:18 referer | ✓ |
| Course_Others decrypted _get_meeting_url_list: record-info?c_instance_id=5 | meeting.go:19 recordInfoURL | ✓ |
| Course_Others decrypted _get_meeting_url_list: common-record-info?c_instance_id=5 | meeting.go:20 commonRecordInfoURL | ✓ |
| Course_Others decrypted _get_meeting_url_list: wemeet-cloudrecording-webapi/v1/sign?...need_multi_stream=1 | meeting.go:21 shareSignURL | ✓ |
| Course_Others decrypted _get_meeting_url_list: tapi/v2/wemeet-cloudrecording-webapi/v1/sign?c_instance_id=5 | meeting.go:22 shareSignPostURL | ✓ |
| Course_Others decrypted _get_meeting_live_url_list: liveportal/v2/query_live_stream | meeting.go:23 liveStreamURL | ✓ |
| Course_Others decrypted _get_meeting_live_url_list: liveportal/v2/query_meeting_room_live_replay_info | meeting.go:24 liveReplayURL | ✓ |

## HTTP 调用

| 源码方法 (line) | Go 函数 (line) | method | 一致? |
|---|---|---|---|
| Meeting_Course._build_downloader_for_item line 419 delegates Course_Others.prepare | Extract/resolveMeeting lines 48, 85 | GET raw share URL | ✓ |
| Course_Others._get_meeting_live_url_list decrypted: query_live_stream/query_meeting_room_live_replay_info | resolveMeeting lines 95-103 | POST form | ✓ |
| Course_Others._get_meeting_url_list decrypted: record-info/common-record-info/sign | resolveMeeting lines 105-123 | POST/GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go struct/tag or parser | 一致? |
|---|---|---|
| result.get('data') | firstJSONText(body, "data.*") lines 98, 103, 112, 124 | ✓ |
| info.get('origin_video_url') / info.get('video_url') | firstText(... "origin_video_url", "video_url") line 169 | ✓ |
| live_replay_info.get('replay_url_long') | firstText(... "replay_url_long") line 169 | ✓ |
| info.get('title') / info.get('filename') | firstText(... "title", "filename") line 171 | ✓ |
| recordings[].get('id') / get('recording_id') | recordingIDs lines 177-190 | ✓ |

## 阻塞步骤

无. 腾讯会议的实际下载地址由页面 JSON 或 record-info/common-record-info/sign/liveportal 接口返回, Go 侧均发起真实 HTTP 并解析 JSON/regex.
