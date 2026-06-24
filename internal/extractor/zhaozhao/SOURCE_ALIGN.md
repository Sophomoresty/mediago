# Zhaozhao source alignment

Source reviewed:
- `decompiled_full/Mooc/Courses/Zhaozhao/Zhaozhao_Base.pyc.1shot.cdc.py`
- `decompiled_full/Mooc/Courses/Zhaozhao/Zhaozhao_Course.pyc.1shot.cdc.py`
- `decrypted_source/Zhaozhao.py`

| Source behavior | Python source evidence | Go alignment |
| --- | --- | --- |
| Login/auth probe uses `myBuyProductList` with `productTypeId=1,7`. | `Zhaozhao_Base._check_cookie`; `Zhaozhao_Course._get_course_list`; URL `https://api.yikao88.com/api-order/order/pc/v5/myBuyProductList`. | `loadCoursePayloads()` calls `signedGet(myProductAPI, {productTypeId:"1,7"})` and parses JSON with `json.Unmarshal`. |
| Request headers include wx-web client metadata and MD5 `apiSign`. | `Zhaozhao_Base._build_request_headers`: `client`, `version`, `appId`, `platform`, `ts`, `apiSign=md5(appId+platform+version+ts+secret)`. | `buildRequestHeaders()` reproduces the same header fields and signature input order. |
| Product/package/detail chain. | `product_detail_api`, `package_list_api`, `course_detail_api`; `_get_product_detail(productId)`, `_get_product_packages(productId)`, `_get_infos()` calls `selectDetail` with `courseId` and `productId`. | `loadCoursePayloads()` calls `selectPcProductById`, `getPackagelistByProduct`, and `selectDetail` with the matching query keys. |
| Course tree parse extracts stations, chapters, children, and video ids. | `_parse_course_detail`: `courseStationList`, `courseChapterList`, `childVideoList`, `videoId`, `definitionList`, `childId`, `productId`, `courseId`. | `collectVideos()` walks JSON recursively and records `videoId/video_id/polyvVideoId/vid`, path titles, `productId`, `courseId`, `childId`, and `definitionList`. |
| Child/files parse extracts courseware/document URLs. | `_extract_direct_files`, `_fetch_child_files`, `_build_file_dict`: `coursewareUrl`, `learningUrl`, `fileUrl`, `downloadUrl`, `url`, `previewUrl`, `ossUrl`; `child_file_api` with `childId`. | `collectFiles()` handles the same URL/name/format key families and emits file entries when source payload exposes direct URLs. |
| Play-safe token resolution tries RSA token API, then multiple course/learningPackage/video play-token APIs. | `_get_play_safe_token()` posts to `https://api.yikao88.com/api-play/play-safe/token`; `get_play_token()` iterates `getPolyvPlaySafe`, `getPlaySafe`, `getPlayToken`, `getPlayTokenByVideoId`, `getVideoPlayToken` variants. | `getPlaySafeToken()` RSA-encrypts `{"videoId","viewerId"}` and POSTs the source API; `getPlayToken()` iterates all source URLs and source query key variants. |
| Polyv secure playback is delegated, not rewritten inline. | `polyv_secure_url = https://player.polyv.net/secure/{vid}.json`; source decrypts/loads Polyv secure and m3u8 metadata. | `resolveVideo()` calls `shared.PolyvResolveSecure()` and `shared.PolyvPickBestManifest()`; `polyvSecureURL`, `polyvKeyURL`, and `polyvPDXLibPlayerURL` are kept as source constants for review traceability. |
