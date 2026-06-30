# dingtalk 源码对齐对照

Python 参考:

- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Dingtalk/Dingtalk_Base.py`
- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Dingtalk/Dingtalk_Live_Client.py`
- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Dingtalk/Dingtalk_Video.py`

## 入口与 Cookie

| Python 逻辑 | Go 实现 | 覆盖点 |
|---|---|---|
| `Dingtalk_Base` cookie/header | `cookieString`, `extractTokenFromCookie`, `newLwpClient` | 聚合 dingtalk/live/shanji/alidocs/webalfa Cookie, 提取 token |
| `_get_cid` 识别 live-room/group-live-share/transcribe/preview/notable | `Extract`, `extractLiveIDs`, `extractTranscribeUUID`, `extractPreviewDentryMeta`, `extractNotableRecordMeta` | 多 URL 入口统一分发 |
| 文本中批量 URL | `extractDingtalkURLsFromText` | 一段文本内多个钉钉链接批量 Entries |

## LWP WebSocket 链路

| Python 源码 | Go 实现 | 覆盖点 |
|---|---|---|
| `Dingtalk_Live_Client` LWP `/reg` 和 JSON-over-WebSocket | `lwp.go` `connect`, `sendJSON`, `recvForMid`, `call` | `wss://webalfa-cm3.dingtalk.com/long`, mid/headers/token |
| `probe_live_replay` | `resolveLiveReplay` | `roomId + liveUuid` 回放解析 |
| `probe_public_live_share` | `resolvePublicLiveShare` | `encCid + liveUuid + pcCode` 公开分享 |
| `probe_ai_transcribe` | `resolveAITranscribe` | `shanji.dingtalk.com/app/transcribes/{uuid}` |
| `probe_download_permission_playlist` | `resolveDownloadPlaylist` | `hasDownloadPermission`, root playlist, direct m3u8 |
| `getH5PlayUrl` | `resolveH5Playlist`, `prepareDingTalkM3U8Text` | 相对 segment 绝对化与 `ding_token` 追加 |
| `LiveRecord/getRecentlyView`, `LiveEntry/getRecommendLivePlayback` | `resolveLiveRecordSummary` | roomInfo 失败时兜底 summary |

## 预览文件 / Notable

| Python 源码 | Go 实现 | 覆盖点 |
|---|---|---|
| `parse_preview_dentry_url`, `probe_preview_dentry` | `extractPreviewDentryMeta`, `hydratePreviewDentryMeta`, `previewDentry` | CSpace file id/space id, preview/download transcoding |
| Alidocs preset / document data / sheet record APIs | `notable.go` `notableRequests`, `fetchNotableDocumentPayloads`, `extractNotableRecord` | notable row/attachment 媒体提取 |
| legacy doc preset | `previewDoc` | `nt/api/docs/preset`, binary preset 下载 |

## 输出与下载

| Python 逻辑 | Go 实现 | 覆盖点 |
|---|---|---|
| `_build_client_replay_m3u8_text`, `absolutize_m3u8_content` | `absolutizeM3U8`, `prepareDingTalkM3U8Text`, `dingtalkM3U8DataURL` | m3u8 文本 data URL, segment 绝对化 |
| `make_ding_token`, `add_or_replace_query_param` | `makeDingToken`, `addOrReplaceQueryParam` | 下载 token 签名 |
| `_get_video_url_list`, `_get_infos` | `buildMediaInfo` | PlaybackURLs, M3U8Content, Extra root_playlist/source_type |

## 静态审计

| 检查 | 当前证据 |
|---|---|
| `url.Parse` 错误处理 | parse 调用均检查 err 或在受控 helper 中返回原值 |
| `json.Unmarshal` 错误处理 | LWP/REST JSON 解码均返回错误或显式跳过无效候选 |
| 死代码/不可达分支 | AST 扫描无 return/panic 后不可达语句 |
| stub | 未发现 `not implemented` / stub sentinel |

## 阻塞步骤

无。LWP live, public share, AI transcribe, CSpace preview 与 notable record 均有实际解析链路; 无法解析媒体时返回明确错误。
