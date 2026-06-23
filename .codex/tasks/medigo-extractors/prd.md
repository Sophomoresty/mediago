# PRD: medigo-extractors

## Task Goal
将所有 69 个有专用 API 的站点 extractor 对齐反编译源码. 当前只有 4 站真正对齐, 其余用 generic HTML 提取.

## Background
- 项目: ~/code/medigo/ (Go CLI, yt-dlp 风格)
- 反编译源码: ~/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/<Site>/
- 解密字符串: ~/code/xwz-downloader-source-release/decrypted_full/all_decrypted.json
- 抖音源码: ~/code/clis/douyin-dl/src/

## In-Scope
- 6 站重写 (API 错误/stub): icourse163, Xuetang, Zhihuishu, imooc, DingTalk, Feishu
- 63 站新写专用 extractor (当前用 generic)
- 移除 generic extractor 中已有专用实现的站点
- 源码对齐验证 (自动化脚本)
- 全量 code review

## Out-of-Scope
- 已完成的 4 站 (Bilibili/Douyin/CCTV/Chaoxing) 不重做
- 无专用 API 的 ~16 个简单站保留 generic
- 腾讯课堂已关停, 移除

## Acceptance Criteria
- 每站 API URL 与源码完全一致
- JSON 解析路径与源码一致
- Cookie/认证检查与源码一致
- `go build ./...` 通过
- 每站写完后标注: verified (API 对齐) / needs_auth (需登录无法E2E)

## Deliverables
- internal/extractor/<site>/<site>.go 专用 extractor (69 个)
- 更新 cmd/medigo/main.go 导入
- 更新 sites/registry.go 移除已有专用实现的站点
- scripts/verify_api_alignment.py 自动化验证脚本

## Constraints
- 每站必须先读反编译 .cdc.py 源码再写 Go 代码
- 不允许用 generic HTML 提取代替专用 API
- 不允许编造 API 端点 — 必须来自源码
- 多站共用的第三方视频平台 (csslcloud, polyv, bokecc) 抽公共模块复用

## 硬性验证流程 (每批 issue 关闭前必须执行)

### 1. 单站验证 (每写完一个站)
- `go build ./...` 编译通过
- 从反编译源码中提取该站至少 1 个核心 API URL
- `grep` 确认该 API URL 出现在 Go 代码中
- 需登录站点: 无 cookie 时返回明确 auth 错误, 不 panic 不返回假数据

### 2. 源码对齐审计 (source-verify issue)
- 运行 `scripts/verify_api_alignment.py`
- 该脚本自动: 遍历反编译源码每站的 API URL → grep Go extractor → 报告缺失/不匹配
- 任何站点 API 不匹配 → issue 不得关闭

### 3. Code Review (code-review-final issue)
- 逐文件审查: nil panic, 资源泄漏, 安全 (browser.go Python 注入), 死代码, 未检查错误
- `go vet ./...` 零 warning
- `gofmt -l .` 零未格式化文件
- 找到 HIGH 以上问题必须修复后才能关闭

### 4. E2E 验证 (final-verify issue)
- `go build ./...` + `go test ./...` + `bash scripts/e2e_test.sh`
- Bilibili/Douyin 真实 URL 下载验证
- 全部 auth-required 站点无 cookie 时错误信息正确

## 工作方法
1. 读 <Site>_Course.pyc.1shot.cdc.py (主逻辑)
2. 读 <Site>_Config.pyc.1shot.cdc.py (常量/URL)
3. 读 <Site>_Base.pyc.1shot.cdc.py (基类/认证)
4. 提取: API URL, 请求参数, 认证方式, JSON 路径, 视频格式
5. Go 重写到 internal/extractor/<site>/<site>.go
6. 执行单站验证 (上述步骤 1)
7. 批次完成后执行源码对齐审计 (上述步骤 2)

## 站点分批

### 第一批: 重写错误的 6 站
icourse163, Xuetang, Zhihuishu, imooc, DingTalk, Feishu

### 第二批: 考试类 12 站
Fenbi, Huatu, Gaodun, Jianshe99, Med66, Hqwx, Wangxiao, Wangxiao233, Dongao, Eoffcn, Kaoyanvip, Yikaobang

### 第三批: K12/辅导 6 站
Xueersi, Yangcong, Yixiaoerguo, Speiyou, Gaotu, Koolearn

### 第四批: 在线课程 7 站
Cto51, Huke88, Magedu, Itbaizhan, Luffycity, Tmooc, Mashibing

### 第五批: 知识付费 9 站
Xiaoetech, Xiaoeapp, Youzan, Qlchat, Lizhiweike, Renrenjiang, Sanjieke, Duanshu, Lexueyun

### 第六批: 企业/会议 5 站
Meeting, Classin, CCTalk, Baijiayunxiao, Keqq (已关停待确认)

### 第七批: 教育门户 8 站
Smartedu, Icourses, Icve, Cnmooc, Open163, Unipus, Ahu, Nmkjxy

### 第八批: 剩余 22 站
Haozaixian, Shanxiang, Ledu, Xiwang, Xsteach, Chaoge, Ckjr, Enetedu, Wowtiku, Haiyangknow, Houda, Houdu, Htknow, Jinbangshidai, Jingtongxue, Kaimingzhixue, Kuke, Mddclass, Minshi, Orangevip, Plaso, Qihang, Sier, Wallstreets, Wendao, Xuelang, Yizhiknow, Youdao, Youyuan, Zhaozhao, Zhengbao, Zlketang, Aishangke, Caixuetang, Gongxuanwang

## Issue 执行顺序
fix-* (6站重写) → batch-* (7批新写) → cleanup-generic → source-verify (API对齐审计) → code-review-final → final-verify (E2E)
