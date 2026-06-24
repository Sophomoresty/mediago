# ledu 源码对齐对照

## URL 常量

| .cdc.py 行 | Go 行/名 | 一致? |
|---|---|---|
| `Ledu_Base.pyc.1shot.cdc.py:199-240` | `internal/extractor/ledu/ledu.go:27-53` | ✓ |
| `Ledu_Course.pyc.1shot.cdc.py:316-391` | `internal/extractor/ledu/ledu.go:66-105` | ✓ |

## HTTP 调用

| 源码方法 | Go 函数 | method | 一致? |
|---|---|---|---|
| `Ledu_Base._validate_pc_cookie` (`Ledu_Base.pyc.1shot.cdc.py:2004-2022`) | `Extract` / `leduGetJSON` (`ledu.go:68-80,204-220`) | GET | ✓ |
| `Ledu_Base.get_class_list` (`Ledu_Base.pyc.1shot.cdc.py:2588-2664`) | `fetchClasses` (`ledu.go:118-145`) | GET | ✓ |
| `Ledu_Base.get_course_detail_list` (`Ledu_Base.pyc.1shot.cdc.py:2696-2712`) | `fetchCourseDetails` (`ledu.go:147-166`) | GET | ✓ |
| `Ledu_Base.get_video_info` (`Ledu_Base.pyc.1shot.cdc.py:3343-3354`) | `buildEntries` (`ledu.go:168-201`) | GET | ✓ |
| `Ledu_Base.get_handout_pdf` (`Ledu_Base.pyc.1shot.cdc.py:2928-2940`) | `buildEntries` (`ledu.go:181-199`) | GET | ✓ |

## JSON 字段映射

| 源码 key 链 | Go 访问 | 一致? |
|---|---|---|
| `result.get('list', [])` / `rows` / `records` | `extractRecords(extractPayload(payload))` | ✓ |
| `result.get('data', {})` / `result.get('content', {})` | `extractPayload` | ✓ |
| `classId` / `id` / `class_id` | `chooseClass` / `parseClassID` | ✓ |
| `stdCourseId` / `pcStdCourseId` / `stdCourseIdForDetail` | `courseID := firstText(...)` | ✓ |
| `liveId` / `taskId` / `noteId` / `paperId` / `coursewareId` | `fetchCourseDetails` / `buildEntries` | ✓ |
| `m3u8Url` / `videoM3u8Url` / `mp4Url` / `fileUrl` / `itemUrl` / `pdfUrl` | `mediaURL` / `mediaFormat` | ✓ |

## 阻塞步骤

无。
