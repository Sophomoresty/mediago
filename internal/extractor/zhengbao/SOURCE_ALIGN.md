# Zhengbao source alignment

Source reviewed:
- `decompiled_full/Mooc/Courses/Zhengbao/Zhengbao_Config.pyc.1shot.cdc.py`
- `decompiled_full/Mooc/Courses/Zhengbao/Zhengbao_Base.pyc.1shot.cdc.py`
- `decompiled_full/Mooc/Courses/Zhengbao/Zhengbao_Course.pyc.1shot.cdc.py`
- `decrypted_source/Zhengbao.py`

| Source behavior | Python source evidence | Go alignment |
| --- | --- | --- |
| Cookie state extracts `cdeluid` and `sid`, then uses member/elearning headers. | `Zhengbao_Base._apply_cookie`: `cdeluid`, `sid`, `ZHENGBAO_HEADERS`, `Referer = MEMBER_HOME_URL`; config lines for `MEMBER_HOME_URL`, `ELEARNING_HOME_URL`. | `headersFromJar()` reads the same cookie names and sets `Origin`, `Referer`, `Accept`, `User-Agent`, `cookie`. |
| Doorman base URLs and crypto constants. | Config: `DOORMAN_BASE_URL`, `DOORMAN_APP_ID`, `DOORMAN_AES_KEY`, `DOORMAN_AES_IV`; base `_get_public_key`, `_get_time_differ`, `_encrypt_params`, `_encrypt_aes_key`, `_doorman_request`. | `doormanURL()`, `getPublicKey()`, `getTimeDiffer()`, `encryptParams()`, `encryptAESKey()`, and `doormanRequest()` reproduce the POST JSON envelope and AES-CBC/PKCS1 token flow. |
| Course list and courseware metadata come from three doorman resource paths. | Course constants: `course_group_path`, `course_detail_path`, `courseware_info_path`; methods `_get_course_groups`, `_get_group_courses`, `_get_coursewares`. | `loadCoursewares()` calls `userCourseClassList`, `getUserHomeCourse`, and `courseWareInfo`, then parses `courseId/courseIds` and courseware list keys. |
| Recorded courseware detection is based on form and directory URL fields. | `_is_recorded_cware`: `courseFormName`, `courseForm`, `cwDirURL`, `dirURL`, `videoList`, `courseView`. | `isRecordedCware()` checks the same field names and URL substrings before adding courseware. |
| Video tree page is fetched and parses `continueStudyVideo` / `window.open(...)`. | `_parse_video_tree`: GET normalized `cwDirURL/dirURL`, skip `课程暂未开通`, find `onclick`, `_extract_open_url`, parse query `videoID`, `cwareID`, `identity`. | `parseVideoTree()` GETs `DirURL`, skips unavailable text, regexes `continueStudyVideo`, `window.open`, `videoID`, and preserves `cware_id/identity`. |
| Materials page and download URL. | `materials_url = https://elearning.chinaacc.com/xcware/myhome/teachingMaterials.shtm?cwareIDs={cware_id}&identity={identity}`; `_parse_material_tree` reads `data-fileurl`, `data-pdfurl`, `data-sepurl`, `data-seppdfurl`; `_build_material_url` formats `getWordVipFile`. | `parseMaterialTree()` fetches the same URL template and parses those attributes; `buildMaterialURL()` formats the exact `material_download_url` placeholders. |
| Play-page JSON parse. | `_resolve_video_play_info`: regex `window.cdelmedia.h5Vars = JSON.parse('(.*?)')`, unicode-unescape, `json.loads`, then use `videoPath` and `srtPath`. | `resolveVideo()` fetches play page, `parseH5Vars()` applies the same regex/unescape/`json.Unmarshal`, then emits `videoPath` and subtitle metadata. |
