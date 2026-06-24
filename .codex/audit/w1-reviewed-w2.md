# w1 reviewed w2

抽查站点: `htknow`, `haozaixian`.

## 结论

| site | result | 结论 |
|---|---|---|
| htknow | FAIL | Go 版丢了 answer 分支, 也会丢掉仅有 `html_content` 的源。 |
| haozaixian | PASS | 关键请求, 解析和媒体分流都和源码一致, 未发现阻断差异。 |

## htknow

### 1) answer 分支未落地

源码在 `Htknow_Course.pyc.1shot.das` 里明确存在:
- `answer_tag_url`, `answer_num_url`, `answer_list_url`, `answer_create_paper_url`.
- `_download_video_source` 在 `product_type == 9` 时直接转 `_download_answer_source`.
- `_download_answer_source` 里有 `(答题)`, `(答题HTML)`, `html_to_pdf`。

对应 Go 只有常规视频/专栏/系列/直播路径, `htknow.go` 里没有 `answer_*` 常量, 也没有 `_download_answer_source` 的等价实现。`SOURCE_ALIGN.md` 还写成了无阻塞, 这条自评不成立。

证据:
- 源码: `Htknow_Course.pyc.1shot.cdc.py:45-48`, `Htknow_Course.pyc.1shot.das:8956-8964`, `Htknow_Course.pyc.1shot.das:8479-8507`, `Htknow_Course.pyc.1shot.das:8714-8760`.
- Go: `internal/extractor/htknow/htknow.go:19-32`, `htknow.go:159-188`, `htknow.go:261-287`.

### 2) HTML-only 源会被 Go 丢弃

源码 `_download_video_source` 在 `pay_content` 存在时会走 HTML/PDF 路径; `source` 本身也保留 `html_content`。

Go 虽然把 `pay_content` 放进了 `source.html` 和 `Extra[html_content]`, 但最终 `mediaFromSources` 只接受 `src.url != ""` 的条目, 所以纯 HTML 源不会生成 entry, 这和源码的图文/PDF 行为不一致。

证据:
- 源码: `Htknow_Course.pyc.1shot.das:8942-8964`, `Htknow_Course.pyc.1shot.das:9149-9160`, `Htknow_Course.pyc.1shot.das:9176-9182`.
- Go: `internal/extractor/htknow/htknow.go:174-188`, `htknow.go:209-258`, `htknow.go:343-364`.

## haozaixian

### 结论

未发现阻断差异。

### 核对点

- `checkCookie` 对应 `_check_cookie`.
- `getTitle` 对应 `_get_title`.
- `getVideoAddress` 覆盖 `videoInfo`, `preloading.mixRoomVideoInfo.multiClarityPlaybackVideoData`, `lbk.lbpVideoAddress`.
- `getInfos` 覆盖 lesson, AI, materials, emphasis images, lecture images 的组合。
- `getAIRoundID` / `getAIVideoURLs` / `getCourseEmphasisImages` / `getLessonLectureImages` 都有对应实现。

证据:
- 源码: `Haozaixian_Course.pyc.1shot.das:1613-1847`, `Haozaixian_Course.pyc.1shot.das:2861-2921`, `Haozaixian_Course.pyc.1shot.das:3439-3557`, `Haozaixian_Course.pyc.1shot.das:4639-4677`, `Haozaixian_Course.pyc.1shot.das:4781-4900`, `Haozaixian_Course.pyc.1shot.das:4979-5055`, `Haozaixian_Course.pyc.1shot.das:5175-5597`, `Haozaixian_Course.pyc.1shot.das:5627-6180`, `Haozaixian_Course.pyc.1shot.das:6190-6419`.
- Go: `internal/extractor/haozaixian/haozaixian.go:88-235`, `course.go:120-257`, `ai.go:68-127`, `clarity.go:157-212`, `media.go:11-81`.

## 备注

`htknow/SOURCE_ALIGN.md` 目前自评为全绿, 但上面两点已经说明它至少有两处不对齐, 需要回修。
