# MediGo v2: 接力提示词 (本会话用)

## 当前状态 (2026-06-23)

`git log --oneline` 最新 6 个 commit:
```
ahu: convert STUB → PASS with HTML lesson parsing
v2 phase 0: shared/{csslcloud,polyv,bokecc,baijiayun}.go real impls + tests
final-verify+cleanup+code-review+shared-platforms+bilibili-cheese: all 19 issues done
batch-enterprise/edu-portal/misc: 45 source-aligned extractor stubs; NO_EXTRACTOR=0
batch-exam/k12/online/knowledge: 34 source-aligned extractor stubs with API URL constants
rewrite xuetang/zhihuishu/imooc/dingtalk/feishu extractors aligned with decompiled source
```

`python3 scripts/verify_full_alignment.py` baseline:
- PASS: 11 (bilibili, bilibili-cheese, cctv, chaoxing, dingtalk, douyin, feishu, icourse163, imooc, xuetang, zhihuishu, ahu)
- BLOCKED: 0
- PARTIAL: 1 (fenbi — parse without HTTP)
- STUB: 80 (剩余)

长任务 medigo-extractors-v2:
- phase0-shared: DONE (shared/ helpers + tests)
- phase1-tier-a: TODO (19 单 API 站, ahu 已转 1 个)
- phase2-tier-b: TODO (19 multi-API 站)
- phase3-csslcloud: TODO (7 站)
- phase4-polyv: TODO (13 站)
- phase5-misc: TODO (22 站)
- phase6-final: TODO (e2e + review)

## 接力规则 (硬约束)

R1: 每站 Extract() 必须真实调 HTTP + 真实解析响应, 不允许 "not yet implemented"。
R2: URL 常量原样照抄反编译源码 (`~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/<Site>/`)。
R3: JSON 解析路径照抄源码 .cdc.py 里 `dict.get('...')` 的 key 链。
R4: 认证流程照抄 (cookie 名/referer/X-Client 等 header)。
R5: 多视频课程返回 `*MediaInfo.Entries`, 单视频返回 `Streams`。
R6: csslcloud/polyv/bokecc/baijiayun 平台走 `internal/extractor/shared/`, 不重写。
R7: 每站完成必须: build clean + vet clean + `verify_full_alignment.py` 报 PASS 或 BLOCKED。

## 工作模板 (per site, 30-90 分钟)

```bash
# Step 1: 读源码
SITE=ahu  # 改成下一个目标
ls ~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/$SITE/
rg -on "https?://[^'\"]{15,}|url_[a-z_]+\s*=" \
  ~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/$SITE/*.cdc.py | head -30

# Step 2: 加密方法体查 all_decrypted.json
python3 -c "
import json
d=json.load(open('/home/sophomores/code/xwz-downloader-source-release/decrypted_full/all_decrypted.json'))
for k in d:
    if '$SITE' in k.title(): print(k)
" | head -10

# Step 3: 找 URL regex
rg -n "$SITE.*?_Course'|${SITE^}_Course'" \
  ~/code/xwz-downloader-source-release/decompiled_full/Mooc/Mooc_Config.pyc.1shot.cdc.py

# Step 4: 写 Go (Edit/Write internal/extractor/$SITE/$SITE.go)
# Step 5: 验证
cd ~/code/medigo
go build ./... && go vet ./internal/extractor/$SITE/...
python3 scripts/verify_full_alignment.py | grep $SITE

# Step 6: 提交
git add -A && git commit -m "$SITE: STUB → PASS with <chain description>"
```

## ahu.go 是模板参考

`internal/extractor/ahu/ahu.go` 是这一轮转换的范本:
- URL pattern + 正则提取 courseId
- 调真实 API: `https://www.ahuyikao.com/course/courseinfo.html?courseId=%s`
- 用 regex 解析 HTML 抽 lesson 列表
- 返回 `*MediaInfo.Entries` (多视频课程)
- 没有数据时返回明确错误而不是假成功

## 下一批目标 (按从易到难)

Phase 1 (tier-a, 单 API 站):
1. ✅ ahu (done)
2. caixuetang
3. cctalk
4. cnmooc (只有 home, 标 BLOCKED)
5. enetedu (只有 1 URL, 标 BLOCKED)
6. haiyangknow
7. houdu (`api.houduweilai.com/mini/student/...`)
8. htknow (saas.clientapi.htknow.com REST)
9. icourses (icourse-portal-api)
10. koolearn (roombox.xdf.cn token)
11. lexueyun (sunlands video CDN)
12. nmkjxy (RecentCourse API)
13. open163 (vip.open.163.com)
14. renrenjiang (Tencent VOD)
15. sanjieke (classroom.sanjieke.cn)
16. smartedu (basic.smartedu.cn)
17. unipus (moocs.unipus.cn)
18. wendao (pc.wendao101.com)
19. yikaobang (BLOCKED, 上游无样本)
20. yizhiknow / zhengbao (简单, 真实 API)

每个跑下来 ~30-90 分钟。一次会话 (max effort) 大致能转 4-8 个站。

## 提交格式

每站一个 commit:
```
<site>: STUB → PASS (<chain summary>)

Real implementation:
- API: <URL>
- Parses: <JSON path or HTML regex>
- Returns: <Streams|Entries>
- Source: Mooc/Courses/<Site>/<Site>_Course.pyc

Blocked steps (if any): <reason>
```

Phase 闭合用:
```bash
cd ~/code/medigo && codex-long-task issue-close \
  --task-id medigo-extractors-v2 \
  --issue-id phase1-tier-a \
  --result passed \
  --summary "<N sites converted STUB→PASS, <M> BLOCKED, all verify_full_alignment.py pass>"
codex-long-task advance --task-id medigo-extractors-v2
```

## 防再缩水检查 (每次提交前必跑)

```bash
cd ~/code/medigo && python3 scripts/verify_full_alignment.py 2>&1 | grep -E "PASS:|STUB:"
# STUB 数必须严格减少, 不允许涨
```

## 永远不要做的事

- ❌ 用 `return nil, fmt.Errorf("...not yet implemented")` 凑过门禁
- ❌ 写"probe API 然后返回空 Streams 但带 'api_response_received' 标记"的假成功
- ❌ 编造源码里没有的 endpoint
- ❌ 用 generic HTML 提取 (sites/ 包) 代替专用 extractor
- ❌ 单 commit 改 > 3 站 (难复查)
