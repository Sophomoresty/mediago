# Context: medigo-extractors

## Current State
- 项目骨架完成: Go CLI + 下载引擎 (direct/HLS/DASH) + Cookie 模块
- 4 站对齐源码可用: Bilibili, Douyin, CCTV, Chaoxing
- 6 站有 extractor 但 API 错误: icourse163, Xuetang, Zhihuishu, imooc, DingTalk, Feishu
- 63 站用 generic HTML 提取, 未对齐源码 API
- CLI 已对齐 yt-dlp 风格: -f, -F, -j, -o template, --cookies, --cookies-from-browser
- Code review R1+R2 的 HIGH 问题已修 (HTTP status check, .part atomic write, ABE cookie decrypt, browser whitelist)

## Checked Entrypoints
- cmd/medigo/main.go: CLI 入口, 导入所有 extractor 包
- internal/extractor/sites/registry.go: generic 站点注册 (需逐步移除已有专用 extractor 的)
- internal/extractor/sites/generic.go: 通用 HTML 提取逻辑

## Reusable Assets
- internal/util/http.go: HTTP client with retry + UA pool
- internal/util/crypto.go: AES CBC/ECB, RSA, Base64 (部分站点需要)
- internal/extractor/extractor.go: Extractor 接口 + MediaInfo/Stream 类型
- internal/extractor/registry.go: URL → Extractor 路由
- scripts/verify_api_alignment.py: 自动化 API 对齐验证脚本

## Critical Reference: all_decrypted.json

**部分站点 .cdc.py 中的 API URL 是加密 blob (形如 `b'\x81M7\xfc...'`).**
明文 URL 必须从 `~/code/xwz-downloader-source-release/decrypted_full/all_decrypted.json` 查找.
- Key 格式: `Courses/<Site>__t<N>_<func>.pyc`
- 如果 .cdc.py 中找不到明文 URL, 必须查 all_decrypted.json
- 解不出的 URL 禁止猜测, 标 blocked 并汇报

## Risks and Unknowns
- 部分站点原版用 WebSocket (DingTalk) — Go 需要 gorilla/websocket
- 部分站点有 JS 加密 (imooc_decode) — 需要 Go 重写或调外部 JS
- 部分站点 API 可能已变更 (源码是 2024 年逆向的)
- 有些站点共用第三方视频平台 (csslcloud, polyv, bokecc) — 可抽公共模块
- Cookie domain 带前导点 (.bilibili.com) 不能 strip, 否则 subdomain 匹配失败

## Corrections (所有对话必读)
1. 不允许用 generic extractor 凑数标 passed — `go build` 通过不构成对齐证据
2. 每站必须先读源码再写, 不编造 API — .cdc.py 中加密的 URL 从 all_decrypted.json 查
3. 发现偏差必须立即汇报, 不带假设推进
4. 每站 verify 必须 grep 命中至少一个来自该站 .cdc.py 的真实 API token
5. batch issue 关闭前, 必须对 batch 内每一站执行 API grep 验证, 任何一站缺失则 batch 不得关闭
6. Cookie domain 保留原始格式 (含前导点), 不 strip

## Code Review R2 残留问题 (新任务中修)
- H1: HLS no-ffmpeg 路径 ts→mp4 rename 逻辑错误
- H3: DASH tmpEngine self-rename 无效
- H5: douyin 全部用 http.DefaultClient 无超时
- M1: douyin NewRequest 未检查 error
- M2: main.go 修改 info.Title 影响 --write-info-json
- M5: SelectBestStream map 遍历不确定性
- M7: parseM3U8 不处理 master playlist / AES-128 key
