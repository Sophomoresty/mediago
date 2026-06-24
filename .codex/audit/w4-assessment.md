# w4 final assessment

Scope: sites covered by `.codex/audit/w4-full-audit.md`: `lexueyun`, `lizhiweike`, `luffycity`, `magedu`, `mashibing`, `mddclass`, `med66`, `meeting`, `minshi`, `nmkjxy`, `open163`, `orangevip`, `plaso`, `qihang`, `qlchat`.

Status basis: current `work/v2-batch1-w4` tree after final repair. `go build ./...` and `go vet ./...` are required verification gates; `scripts/verify_full_alignment.py` is used only as a stub/HTTP+parse sanity check, not as full source-completeness proof.

| Site | Rating | One-line assessment |
|---|---|---|
| lexueyun | PARTIAL | Core course/lesson/Sunlands video flow is implemented and the unchecked `ReadAll`/marshal errors are fixed, but source datum/courseware files are still not emitted as downloadable entries. |
| lizhiweike | PARTIAL | Main purchased course media flow is implemented, but the source `buy_record` order-price path remains declared without an executed Go call site. |
| luffycity | PARTIAL | Degree/module media paths are implemented, but the source `/study/vip-card/` discovery branch is still absent. |
| magedu | PASS | Current Go extractor matches the inspected source URL/method/auth/JSON media flow and no repair-class code issue remains. |
| mashibing | PASS | Current Go extractor matches the inspected source flow, uses shared Polyv handling, and direct response reads are closed and checked. |
| mddclass | PARTIAL | Direct series/group flows are implemented, but seller-list, trade-order, and joined-company discovery branches from the source are still absent. |
| med66 | PASS | Current csslcloud replay flow uses `shared.CssLcloudResolvePlayInfo` and no source-alignment or repair-class issue remains in the inspected path. |
| meeting | PASS | Tencent Meeting recording/live replay probes perform real HTTP+parse, close bodies through shared helpers, and no blocking issue remains. |
| minshi | PARTIAL | Course video and Polyv playback are implemented, but source material/file artifacts are still only metadata, not first-class downloadable entries. |
| nmkjxy | PARTIAL | Course video playback is implemented, but source courseware files are returned only in `Extra`, not exposed as downloadable entries. |
| open163 | PARTIAL | VIP/free media extraction works, but source `myOrders.do` purchased-course fallback and stronger free URL/title normalization are still missing. |
| orangevip | PARTIAL | Course video/Baijiayun playback is implemented, but source cookie-validity check and file/courseware entries are still incomplete. |
| plaso | BLOCKED | Fabricated local playback URLs were removed; local STS/plist playback plus Aiwenyun/Jhpy variants now fail explicitly as blocked instead of silently guessing. |
| qihang | PARTIAL | BokeCC and csslcloud video/live paths are implemented with shared helpers, but source file nodes are still omitted from entries. |
| qlchat | PARTIAL | The CRITICAL dead Qianliao train flow is now wired to real train APIs and JSON parsing, but source file/doc/ppt/article branches are still not surfaced as entries. |

Repair summary:
- Fixed `lexueyun` parse-critical unchecked errors: `json.Marshal` and `io.ReadAll` are now checked.
- Fixed `qlchat` CRITICAL train routing: Qianliao/Xingqudao/Nicegoods URLs are matched and dispatched to real train APIs.
- Fixed `qlchat` unchecked JSON marshal in `postJSON`.
- Fixed `plaso` CRITICAL fabricated local media URLs by removing guessed CDN paths and returning explicit blocked errors for unresolved local/variant flows.
