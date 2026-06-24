# worker-5 cross-audit of work/v2-batch1-w6

随机抽样站点:
- `zhengbao`
- `yikaobang`

审计材料:
- w6 Go: `/home/sophomores/code/medigo-w6/internal/extractor/zhengbao/zhengbao.go`
- w6 Go: `/home/sophomores/code/medigo-w6/internal/extractor/yikaobang/yikaobang.go`
- w6 对齐文档: `/home/sophomores/code/medigo-w6/internal/extractor/{zhengbao,yikaobang}/SOURCE_ALIGN.md`
- Python 源: `/home/sophomores/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/Zhengbao/`
- Python 源: `/home/sophomores/code/xwz-downloader-source-release/decompiled_full/Mooc/Courses/Yikaobang/`

## Findings

### 1. `zhengbao`: `courseWareInfo` 请求参数缺少源码中的课程上下文字段

- Go 位置: `/home/sophomores/code/medigo-w6/internal/extractor/zhengbao/zhengbao.go:232-233`
- 源码依据: `Zhengbao_Course.pyc.1shot.cdc.py:348-365`
- 源码行为: `_get_coursewares()` 调 `courseware_info_path` 时, 参数包含 `eduSubjectId`, `courseType`, `courseIds`, `courseId`, `classId`, `uid`.
- Go 当前行为: `cwParams` 只包含 `courseIds`, `courseId`, `uid`.
- 影响: 对需要 `eduSubjectId/courseType/classId` 限定的课程, w6 实现可能拿不到完整课件树或拿错课件列表; `SOURCE_ALIGN.md` 只说明调用了 `courseWareInfo`, 没覆盖参数级差异.
- 建议: 在 `loadCoursewares()` 里保留从 `getUserHomeCourse`/course detail 得到的 `eduSubjectId`, `courseType`, `classId` 等字段, 并按源码构造 `coursewareInfo` 参数.

### 2. `zhengbao`: 录播课件过滤比源码更宽

- Go 位置: `/home/sophomores/code/medigo-w6/internal/extractor/zhengbao/zhengbao.go:311-315`
- 源码依据: `Zhengbao_Course.pyc.1shot.cdc.py:381-400`
- 源码行为: `_is_recorded_cware()` 先要求 `cwDirURL/dirURL` 归一化后非空, 再接受 `courseFormName` 含 `录播`, 或 `courseForm == 2`, 或 URL 包含 `videoList/courseView`.
- Go 当前行为: 除了上述条件外, 只要有 `cwareId/cwareID/cwId` 就返回 true.
- 影响: 可能把非录播或没有可解析播放目录的课件加入下载流程, 后续产生无效 `DirURL` 或误报解析失败.
- 建议: 去掉 `cwareId` 单独放行条件, 并在 `dir` 为空时按源码直接返回 false.

### 3. `yikaobang`: 未见阻塞性差异

- Go 位置: `/home/sophomores/code/medigo-w6/internal/extractor/yikaobang/yikaobang.go:26-35`
- 源码依据: `Yikaobang_Course.pyc.1shot.cdc.py:42-49`
- 结论: 源码明确打印“缺少可靠的课程/播放接口样本，暂不提供伪实现”后返回 false; Go 版 probe home URL 后返回 `blocked: needs upstream API samples`, 与 blocked 源码状态一致.

## Summary

- 抽样 2 站.
- 发现 `zhengbao` 2 个参数/过滤条件层面的源码对齐问题.
- `yikaobang` blocked 状态与源码一致.
