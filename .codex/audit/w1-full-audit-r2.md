# w1 full audit R2

Scope: second-round audit for the 16 sites from `.codex/audit/w1-full-audit.md`: `ahu`, `aishangke`, `baijiayunxiao`, `bilibili`, `caixuetang`, `cctalk`, `cctv`, `chaoge`, `chaoxing`, `ckjr`, `classin`, `cnmooc`, `cto51`, `dingtalk`, `dongao`, `douyin`.

Audit baseline before adding this R2 report: `3d9e49a`. Since the first W1 report commit `4c5c783`, the only W1-owned extractor file changed before this report is `internal/extractor/douyin/douyin.go`.

Second-round focus:

1. Re-check first-round `CRITICAL` findings against current code and `.codex/audit/CRITICAL_TRIAGE.md`.
2. Deep static review for nil panic, response-body leaks, unchecked material errors, dead imports, and remaining source-alignment regressions.
3. Record current status, including false-positive reclassification and newly found issues.

## Summary

| site | R2 status |
|---|---|
| ahu | NO ISSUE |
| aishangke | NO ISSUE |
| baijiayunxiao | NO ISSUE |
| bilibili | first-round CRITICAL reclassified false positive; one non-critical source-coverage issue remains |
| caixuetang | NO ISSUE |
| cctalk | NO ISSUE |
| cctv | existing source-alignment issues remain |
| chaoge | NO ISSUE |
| chaoxing | existing source-coverage issues remain |
| ckjr | NO ISSUE |
| classin | NO ISSUE |
| cnmooc | CRITICAL new nil-panic risk found |
| cto51 | NO ISSUE |
| dingtalk | expected blocked, not current MUST-FIX critical per triage |
| dongao | NO ISSUE |
| douyin | first-round nil-panic CRITICAL fixed; source-missing CRITICAL reclassified false positive; unchecked read issue remains |

## First-round CRITICAL disposition

- `bilibili`: first-round `fabricated URL` CRITICAL is reclassified as a false positive. Current evidence: `.codex/audit/CRITICAL_TRIAGE.md` marks it false-positive; `Course_Others.pyc.1shot.cdc.py:350-359,466-483,537-579` has a public Bilibili branch; `Course_Others.pyc.1shot.das:25390-26645` contains `_get_bili_bvid_list`, `_get_bangumi_list`, and `_get_bili_video_p_num`; `internal/extractor/bilibili/cheese.go:13-21` separately matches the PUGV/cheese source endpoints.
- `dingtalk`: first-round live replay CRITICAL is not fixed in code, but `.codex/audit/CRITICAL_TRIAGE.md` classifies it as `EXPECTED BLOCKED (not bugs, by design)`. The hard error at `internal/extractor/dingtalk/dingtalk.go:64-65` remains.
- `douyin`: first-round nil-panic CRITICAL from unchecked `http.NewRequest` is fixed in `3d9e49a`: current checks are at `internal/extractor/douyin/douyin.go:92-95`, `118-121`, `144-147`, and `300-303`.
- `douyin`: first-round `no source` CRITICAL is reclassified as a false positive. Source is not under `Courses/Douyin/`, but `Course_Others.pyc.1shot.cdc.py:430-443,677-713` contains the Douyin branch, and local `~/code/clis/douyin-dl/src/douyin_dl/` contains the maintained no-login resolver path that current Go mirrors.

## ahu

NO ISSUE

- Source alignment remains as in `internal/extractor/ahu/SOURCE_ALIGN.md`; no W1-current code change since R1.
- Deep code review: no raw response body ownership, no unchecked request construction, no nil-deref pattern. `hmac.Hash.Write` ignore at `ahu.go:265` remains benign because hash writes cannot fail.

## aishangke

NO ISSUE

- CSSLCloud flow still uses `shared.CssLcloudResolvePlayInfo`; source endpoint and header behavior unchanged.
- Deep code review: no raw response leak or nil-panic pattern. `url.QueryUnescape` errors are only best-effort parsing of source page `ccInfo` values and do not dereference nil or hide network failures.

## baijiayunxiao

NO ISSUE

- Source-aligned GET/POST JSON flow unchanged.
- Deep code review: the only owned raw response (`resolveLiveEnter`) closes `resp.Body` at `baijiayunxiao.go:230`; `io.ReadAll` error is checked at `:231-234`. Optional JSON fallback at `:181` is backed by regex fallback and is not a material unchecked network/parse failure.

## bilibili

- R2 RECLASSIFICATION: the first-round `CRITICAL: fabricated source URLs` is a false positive. The source tree has both generic public Bilibili handling in `Course_Others` and paid PUGV/cheese handling in `Bilibili_Course`; current Go splits these into `bilibili.go` and `cheese.go`.
- ISSUE: `Bilibili_Gongfang.pyc.1shot.cdc.py:35-37,228` still defines mall/gongfang purchased-course endpoints (`mall-c/order/detail`, `ship/orderdetails/query`, `querydownloadurl`, `gf.bilibili.com/order/hyg-download`). Current `internal/extractor/bilibili` has no mall/gongfang implementation. This is source coverage loss, but not the R1 fabricated-URL CRITICAL.
- Deep code review: no body leak; `resolveShortURL` closes the raw response at `bilibili.go:263`. Ignored `regexp.MatchString` error at `:258` is benign for a constant literal pattern.

## caixuetang

NO ISSUE

- Source-aligned form API chain unchanged.
- Deep code review: `url.Parse` results in `parseIDs` and `authFromJar` are nil-checked or constant-valid; no nil panic. HTTP bodies are handled by `util.Client.PostForm` / `GetString`, which close bodies.

## cctalk

NO ISSUE

- Source constants and GET/POST split remain aligned.
- Deep code review: ignored `url.Parse` error at `cctalk.go:234` is followed by `if u != nil` before use; no nil panic. No raw response body ownership or dead imports found.

## cctv

- ISSUE UNCHANGED: auth/header flow is still not source-aligned. Source builds `cookie`, `Accept`, `Origin`, `Referer`, and `User-Agent` at `Cctv_Course.pyc.1shot.cdc.py:53-58` and passes that header to page/API requests at `:122-149`; Go sends only `Referer` for page GET and nil headers for API GET at `internal/extractor/cctv/cctv.go:32-50`.
- ISSUE UNCHANGED: source candidate branches include `chapters4`, `chapters3`, `chapters2`, and `chapters` in addition to `hls_url`/`video_url`; Go decodes only top-level `title`, `hls_url`, `video_url`, and `chapters_url` at `cctv.go:55-60`.
- Deep code review: no raw response leak, nil panic, or dead import found.

## chaoge

NO ISSUE

- CSSLCloud flow still uses `shared.CssLcloudResolvePlayInfo` and `shared.CssLcloudRewriteM3U8Keys`.
- Deep code review: no raw response leak or nil-panic pattern. Best-effort `QueryUnescape` and fallback `json.Unmarshal` checks do not hide network errors.

## chaoxing

- ISSUE UNCHANGED: source coverage is still incomplete. Source defines `url_source`, `url_live`, `url_meet_review`, and `url_yun_file` in `Chaoxing_Course.pyc.1shot.cdc.py:47-51`; Go only resolves object IDs and calls `https://mooc1.chaoxing.com/ananas/status/%s` at `internal/extractor/chaoxing/chaoxing.go:37-55`.
- ISSUE UNCHANGED: Go parses only `filename`, `http`, and `hls` at `chaoxing.go:60-78`; source also has `attachments`, `liveId`, `statusUrl`, `downloadUrl`, and material/live branches.
- Deep code review: no raw response body ownership, nil-panic pattern, or dead import found.

## ckjr

NO ISSUE

- Source API/qcloud flow unchanged.
- Deep code review: ignored `url.ParseQuery` / `url.Parse` errors in `helpers.go:17,36` are followed by fallback ID extraction and nil checks; no nil panic. No raw response body ownership found.

## classin

NO ISSUE

- Source record/token/CDN flow unchanged.
- Deep code review: `url.Parse` uses are guarded by `err == nil`; no raw response leak, nil-panic pattern, or dead import found.

## cnmooc

- CRITICAL: newly found nil-panic risk in `normalizeURL`. `internal/extractor/cnmooc/cnmooc.go:318-324` checks the first `url.Parse(s)` only for the absolute-URL fast path, then ignores errors from `b, _ := url.Parse(base)` and `r, _ := url.Parse(s)` before calling `b.ResolveReference(r).String()`. If source data supplies a malformed relative URL such as `%`, Go's `url.Parse("%")` returns `nil, err`, and `ResolveReference(nil)` panics.
- Evidence: temporary harness confirmed `url.Parse("%")` returns `(*url.URL)(nil)` and `b.ResolveReference(r)` panics with `runtime error: invalid memory address or nil pointer dereference`.
- Deep code review otherwise: HTTP bodies are managed by `util.Client`; no raw response leak or dead import found.

## cto51

NO ISSUE

- Source course/training/qcloud flow unchanged.
- Deep code review: JSON fallbacks are explicit optional probes and no raw response body ownership, nil-panic pattern, or dead import was found.

## dingtalk

- EXPECTED BLOCKED: the live replay path still returns an explicit blocked error at `internal/extractor/dingtalk/dingtalk.go:64-65`. This is not fixed by other workers, but `.codex/audit/CRITICAL_TRIAGE.md` classifies the LWP WebSocket replay stack as expected blocked and not a current MUST-FIX bug.
- ISSUE UNCHANGED under strict source coverage: source document probing includes `api/doc/info`, `api/document/data`, and `nt/api/docs/preset/binary` (`Dingtalk_Live_Client.pyc.1shot.cdc.py:3610-3619`), while current Go only uses `nt/api/docs/preset`.
- Deep code review: no raw response leak, nil-panic pattern, or dead import found.

## dongao

NO ISSUE

- Source stage/detail/live/lecture flow unchanged.
- Deep code review: guarded `url.Parse` use at `dongao.go:196`; no raw response body ownership, nil-panic pattern, or dead import found.

## douyin

- RESOLVED: first-round nil-panic CRITICAL from unchecked `http.NewRequest` is fixed in current code: all four request constructions now check `err` before using `req` (`douyin.go:92-95`, `118-121`, `144-147`, `300-303`).
- RECLASSIFIED: first-round `no decompiled source` CRITICAL is false positive. The relevant xwz source is `Course_Others`, not `Courses/Douyin/`, and the maintained local `douyin-dl` resolver/quality code matches the current no-login `ttwid` + share page + `aweme.snssdk.com` probe strategy.
- ISSUE: parse-critical body reads still ignore `io.ReadAll` errors at `douyin.go:135` and `douyin.go:159`, which can convert a transport/body read failure into a later parse error. The drain read in `getTTWID` at `:104` is non-critical because cookie extraction does not depend on the body.
- ISSUE: `fmt.Sscanf` result is ignored at `douyin.go:319`; malformed `Content-Range` can silently return size zero for an otherwise reachable stream.
- Deep code review: no response body leak found; all raw responses are closed at `douyin.go:103`, `130`, `158`, and `313`.

## Static sweep notes

- Raw response ownership appears in only three W1 packages: `baijiayunxiao`, `bilibili`, and `douyin`; all observed paths close response bodies.
- Ignored `url.Parse` / `url.ParseQuery` errors were reviewed site-by-site. Most are nil-checked or constant-valid; `cnmooc.normalizeURL` is the only nil-panic class issue found in R2.
- Ignored JSON parse errors in `aishangke`, `baijiayunxiao`, `caixuetang`, `cctalk`, `chaoge`, `cnmooc`, `cto51`, and `dongao` are optional/fallback probes rather than unchecked mandatory parse results.
