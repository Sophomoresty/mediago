# PRD: medigo-extractors-v2

## Task Goal

Convert all ~80 site extractors under `~/code/medigo/internal/extractor/<site>/`
from STUB to PASS or BLOCKED status (per `scripts/verify_full_alignment.py`).

A PASS extractor implements the real source-aligned HTTP+parse chain. A BLOCKED
extractor performs the non-blocking steps and then explicitly returns a
"blocked: needs X" error for the impossible step (e.g. JS sandbox, LWP protocol).

A STUB extractor — Extract() that returns "not yet implemented" without any
HTTP call — is unacceptable. The previous v1 task closed with 75 stubs; v2 must
eliminate them all.

## Scope

10 sites already PASS (carried over from v1):
  bilibili, bilibili-cheese, cctv, chaoxing, dingtalk, douyin, feishu,
  icourse163, imooc, xuetang, zhihuishu

75 sites are currently STUB and must be converted.

## Acceptance Criteria

Per site:
- `go build ./...` clean
- `go vet ./...` clean
- `python3 scripts/verify_full_alignment.py` reports PASS or BLOCKED for the site
- Spot-check: a 50-line `_test.go` with `httptest` mock + fixture JSON drives
  Extract() and asserts the returned MediaInfo structure matches the API
  response shape

Overall:
- 0 STUB after all phases, 0 NO_EXTRACT
- `scripts/verify_api_alignment.py` still passes
- Total LOC across all extractors stays under 25,000

## Realistic Scope Estimate

Median ~3 hours/site, 75 sites = ~225 hours = 5-7 weeks of focused work.

## Phase Plan

Each phase is one codex-long-task issue. Issues run in this order.

### Phase 0: Shared platform helpers (must finish first)

- `shared/csslcloud.go`: full login → vod → m3u8-rewrite chain
- `shared/polyv.go`: secure/{vid}.json → manifest → AES key fetch chain
- `shared/bokecc.go`: getvideofile + segment list parser
- `shared/baijiayun.go`: web/playback/getPlayInfo + jsonp callback unwrap

Each helper needs its own _test.go with mock server and fixture based on a
real source method.

### Phase 1: Tier-A single-API sites (15 sites, ~30 hours)

ahu, caixuetang, cctalk, cnmooc, enetedu, haiyangknow, houdu, htknow,
icourses, koolearn, lexueyun, nmkjxy, open163, renrenjiang, sanjieke,
smartedu, unipus, wendao, yikaobang (already blocked), yizhiknow, zhengbao

### Phase 2: Tier-B multi-API tree-traversal sites (15 sites, ~50 hours)

cto51, dongao, duanshu, eoffcn, fenbi, gaodun, hqwx, huatu, huke88, icve,
itbaizhan, kaoyanvip, sier, speiyou, xueersi, xuelang, youdao, youzan, zlketang

### Phase 3: csslcloud sites (7 sites, ~35 hours)

aishangke, chaoge, houda, jianshe99, med66, qihang, shanxiang

### Phase 4: polyv sites (16 sites, ~50 hours)

gongxuanwang, jinbangshidai, kuke, luffycity, magedu, mashibing (DRM blocked),
minshi, orangevip, plaso, wangxiao233, wangxiao, youyuan, zhaozhao

### Phase 5: bokecc/baijiayun/misc (10 sites, ~25 hours)

baijiayunxiao, ckjr, classin, jingtongxue, kaimingzhixue, mddclass,
haozaixian, ledu, qlchat (websocket), tmooc, wallstreets, xiwang, xsteach,
meeting, xiaoetech, xiaoeapp, yangcong, yixiaoerguo, keqq (dead), gaotu,
lizhiweike, wowtiku

### Phase 6: code review + e2e

- verify_full_alignment.py: 0 STUB
- verify_api_alignment.py: 0 MISMATCH
- 5 real-cookie e2e downloads
- Code review

## Working Method (mandatory per site)

1. **Read source** (~15 min): list .cdc.py files in
   `~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/<Site>/`,
   read main `*_Course.pyc.1shot.cdc.py`, extract URL constants and main
   workflow methods.

2. **Look up encrypted methods** when .cdc.py shows `b'\x81...'` blobs in
   `~/code/xwz-downloader-source-release/decrypted_full/all_decrypted.json`.

3. **Find URL regex** in `Mooc_Config.pyc.1shot.cdc.py`.

4. **Write Go code** (30-90 min): one file under 400 lines that calls real
   APIs, parses real responses, returns real *MediaInfo.

5. **Write unit test**: 50-line _test.go with httptest mock.

6. **Verify**: build + vet + test + verify_full_alignment.py.

7. **Commit**: one site = one commit.

## Hard Rules (violate any = redo that site)

R1: Extract() must call HTTP and parse the response. No "not yet implemented".
R2: URL constants match source byte-for-byte.
R3: JSON parsing uses struct tags matching source `.get(...)` keys.
R4: Auth flow matches source (cookie name, referer, headers).
R5: Multi-video courses return Entries. Single-video sites return Streams.
R6: Use shared/csslcloud.go etc. when source uses the platform.
R7: Build/vet/test clean before commit.

## Anti-patterns

- ❌ `return nil, fmt.Errorf("X chain not yet implemented")`
- ❌ MediaInfo with empty Streams + "api_response_received" marker
- ❌ Fabricated endpoints not in source
- ❌ Helper that probes API but returns fake-success
- ❌ Calling URL constant but no actual HTTP call
- ❌ Only first step of multi-step chain
