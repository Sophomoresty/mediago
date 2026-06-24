# W1 extractor assessment

Scope: `ahu`, `aishangke`, `baijiayunxiao`, `bilibili`, `caixuetang`, `cctalk`, `cctv`, `chaoge`, `chaoxing`, `ckjr`, `classin`, `cnmooc`, `cto51`, `dingtalk`, `dongao`, `douyin`.

Rating definitions:

- `PASS`: fully aligned with the audited source flow for the supported site scope.
- `BLOCKED`: partial implementation with an explicit, documented blocker.
- `PARTIAL`: known source-coverage/alignment gap that is not marked as blocked.

| site | rating | one-line status |
|---|---|---|
| ahu | PASS | Source URLs, GET flow, referer/cookie handling, and Aliyun play-info parsing align; no remaining code-review issue. |
| aishangke | PASS | Loveshangke course/series/enter flow aligns and CSSLCloud playback uses the shared helper as required. |
| baijiayunxiao | PASS | Course/token/live-enter and Baijiayun playback flows align; owned raw responses are closed and read errors are checked. |
| bilibili | PARTIAL | Public video and PUGV cheese flows are covered, but Gongfang/mall purchased-course endpoints remain missing. |
| caixuetang | PASS | Agent-host form API, member/course/play/material/download parsing align with the source flow. |
| cctalk | PASS | Content API constants, GET/POST split, headers, and dynamic JSON traversal align with the audited CCTalk source path. |
| cctv | PARTIAL | Main page/API flow exists, but source headers and chapter candidate branches are still not fully mirrored. |
| chaoge | PASS | Chaoge course/file/series/room flow aligns and CSSLCloud playback/key rewrite uses the shared helper. |
| chaoxing | PARTIAL | Direct ananas status video flow works, but live, meet-review, Yun file, and attachment/material branches are missing. |
| ckjr | PASS | CKJR API/qcloud constants, headers, auth keys, and recursive media parsing align with source. |
| classin | PASS | Record/token/CDN flow and ClassIn signed headers/media parsing align with source. |
| cnmooc | PASS | Course/session/item-detail flow aligns; malformed relative URLs are now guarded before `ResolveReference`. |
| cto51 | PASS | Course/training/qcloud routes and dynamic media/auth parsing align with source. |
| dingtalk | BLOCKED | Alidocs preset path is implemented, but live replay requires the documented LWP WebSocket stack blocker. |
| dongao | PASS | Stage/detail/live/lecture APIs and media extraction align with source. |
| douyin | PASS | Course_Others/local douyin-dl based ttwid/share-page/probe flow is implemented; previous request/read error gaps are fixed. |
