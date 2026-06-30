# bilibili 源码对齐对照

Python 参考:

- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Bilibili/Bilibili_Base.py`
- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Bilibili/Bilibili_Course.py`
- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Bilibili/Bilibili_Gongfang.py`

## 普通视频 / 分 P / 合集

| Python 逻辑 | Go 实现 | 覆盖点 |
|---|---|---|
| `Bilibili_Base.get_redirect_url` 处理 b23.tv 短链 | `resolveShortURL`, `normalizeURL` | 短链与 BV/av URL 归一化 |
| `Bilibili_Base._check_cookie` 请求 `https://api.bilibili.com/x/web-interface/nav` | `ensureBilibiliLogin`, `validateBilibiliLogin` | Cookie 登录态校验 |
| 普通视频信息 API | `getVideoInfo` -> `https://api.bilibili.com/x/web-interface/view` | bvid/aid, cid, pages, title |
| 播放地址 API | `getPlayURL` -> `https://api.bilibili.com/x/player/playurl?fnval=4048&fourk=1&qn=127` | dash video/audio, durl fallback, mp4/m4s |
| 多 P 下载 | `extractMultiP` | pages[].cid/title 逐项解析 |
| 字幕 | `fetchSubtitles` -> `x/player/v2` | subtitle_url 输出 `Subtitles` |
| 合集/系列/收藏夹 | `extractCollection`, `extractSeries`, `extractMediaList` | `x/polymer/web-space/seasons_archives_list`, `x/series/archives`, `x/v2/medialist/resource/list` |

## Bilibili 课堂 / Cheese

| Python 源码 | Go 实现 | 覆盖点 |
|---|---|---|
| `Bilibili_Course.course_list_url = https://api.bilibili.com/pugv/pay/web/my/paid?ps=10&pn={page}` | `fetchCheesePaidCourses` | 已购课程分页列表 |
| `course_url = https://api.bilibili.com/pugv/view/web/season/v2?season_id={cid}` | `fetchCheeseSeason`, `extractCheeseSeason` | ss 课程详情 |
| `section_url = https://api.bilibili.com/pugv/view/web/season?ep_id={sid}` | `extractCheeseSeason` ep 分支 | ep 入口反查 season |
| `video_url = https://api.bilibili.com/pugv/player/web/playurl?fnval=16&fourk=1&ep_id={vid}` | `fetchCheesePlayURL`, `buildCheeseEntries` | dash video/audio 与 mp4 fallback |
| `_get_video_url` 按 width/bandwidth 排序选清晰度 | `buildCheeseEntries` / stream 选择 | 输出 best/audio stream, m3u8/merge 标记 |

## 工房 / Gongfang

| Python 源码 | Go 实现 | 覆盖点 |
|---|---|---|
| `course_url = https://mall.bilibili.com/mall-c/order/detail?orderId={cid}` | `fetchGongfangOrderTitle` | 订单标题/价格 HTML 解析 |
| `info_url = https://mall.bilibili.com/mall-c/ship/orderdetails/query` | `fetchGongfangItems` | `shipOrderDetails[]`, `fileContentType`, `fileName`, `shipOrderDetailsId` |
| `source_url = https://mall.bilibili.com/mall-c/ship/orderdetails/querydownloadurl` | `fetchGongfangDownloadURL` | `data.url`/裸字符串下载地址 |
| `_download_source` 按 `fileContentType` 调用 video/attach 下载 | `BilibiliGongfang.Extract` entries | `format` 由 URL/content-type 推断, headers 保留 Bilibili referer |

## 静态审计

| 检查 | 当前证据 |
|---|---|
| `url.Parse` 错误处理 | 目标包内 parse 均检查 err 或位于测试辅助中 |
| `json.Unmarshal` 错误处理 | API 响应解析均返回错误或显式 fallback |
| 死代码/不可达分支 | AST 扫描无 return/panic 后不可达语句 |
| stub | 未发现 `not implemented` / stub sentinel |

## 阻塞步骤

无。
