# chaoxing 源码对齐对照

Python 参考:

- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Chaoxing/Chaoxing_Base.py`
- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Chaoxing/Chaoxing_Course.py`
- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Chaoxing/Chaoxing_Live.py`
- `/home/sophomores/code/xwz-downloader-source-release/restored_source/Mooc/Courses/Chaoxing/Chaoxing_Mooc.py`

## 入口与登录

| Python 逻辑 | Go 实现 | 覆盖点 |
|---|---|---|
| `Chaoxing_Base._check_cookie` 访问 `i.mooc.chaoxing.com/space/index` 与 `xueyinonline.com/portal/new-header` | `validateChaoxingLogin`, `shouldValidateChaoxingLogin` | 登录态校验, school/portal URL 宽松处理 |
| `_set_url_context`, `_extract_url_access_params`, portal 参数解析 | `newChaoxingContext`, `extractAccessFromText`, `extractPortalParams`, `resolvePortalAccess` | `courseId/clazzId/enc/cpi/openc`, school host, `mooc2-ans` |
| space/index 课程页 | `resolveSpaceIndex` | 从学习空间批量解析课程入口 |

## 课程与章节

| Python 源码 URL/方法 | Go 实现 | 覆盖点 |
|---|---|---|
| `url_course`, `url_new_course` | `buildCoursePageURL`, `abs` | 旧版 `/mycourse/studentcourse` 和新版 `mooc2-ans` |
| `_get_zid_enc`, `_joined_course`, `_apply_course` | `resolvePortalAccess`, `extractAccessFromText` | joined/apply/portal 兜底获取 `clazzId/enc` |
| `_get_infos` 解析 chapter timeline | `collectChaoxingChapters`, `resolveChapter` | `chapterId`, title, index |
| `url_nums = /mycourse/studentstudyAjax` | `resolveChapter` POST form | 卡片数量和 knowledge id |
| `url_objectid = /knowledge/cards` | `resolveChapter`, `collectChaoxingResources` | 逐卡解析 objectid/live/file/resource |

## 视频, 直播, 资料

| Python 源码 | Go 实现 | 覆盖点 |
|---|---|---|
| `url_source = /ananas/status/{objectid}` | `resolveObjectResource`, `resolveResource` | video/audio objectid -> media URL |
| `url_live = /ananas/live/liveinfo` | `resolveResource`, `resolveZhiboLiveEntry` | liveid/jobid/uuid, zhibo.chaoxing.com |
| `url_meet_review = k.chaoxing.com/apis/chapter/getMeetReview4Job` | `resolveResource` review 分支 | meet review 回放 |
| `url_yun_file = k.chaoxing.com/apis/file/getYunFile` | `resolveResource`, resources helpers | 云盘文件资料 |
| `url_files`, `url_download` | `resolveFileEntries`, `portal/resource helpers` | 课程资料, `coursedata/downloadData`, object download URL |
| `url_sub = /richvideo/subtitle` | subtitle parsing helpers | 字幕链接解析输出 |
| portal-node-list / portal-node-resource | `resolvePortalResourceEntries` | 公开课 portal 资源 |

## 静态审计

| 检查 | 当前证据 |
|---|---|
| `url.Parse` 错误处理 | parse 调用均检查 err 或在条件中保护 |
| `json.Unmarshal` 错误处理 | JSON 解析路径显式判断 err, 可选嵌套 JSON 使用受控 fallback |
| 死代码/不可达分支 | AST 扫描无 return/panic 后不可达语句 |
| stub | 未发现 `not implemented` / stub sentinel |

## 阻塞步骤

无。公开课/学校域名差异通过 host/context 抽象兜底, 解析不到资源时返回明确错误而不是空 Streams。
