# Zlketang source alignment

Source reviewed:
- `decompiled_full/Mooc/Courses/Zlketang/Zlketang_Config.pyc.1shot.cdc.py`
- `decompiled_full/Mooc/Courses/Zlketang/Zlketang_Base.pyc.1shot.cdc.py`
- `decompiled_full/Mooc/Courses/Zlketang/Zlketang_Course.pyc.1shot.cdc.py`
- `decrypted_source/Zlketang.py`

| Source behavior | Python source evidence | Go alignment |
| --- | --- | --- |
| Base login probe and headers. | Config `ZLKETANG_HEADERS`: `Log-Platform-Type=pc_web`, `Origin`, `Referer`, `Accept`, Edge UA; base `check_url = https://www.zlketang.com/wxpub/api/user_info`. | `headersFromJar()` copies the same header family and cookies; `Extract()` probes `checkURL` and parses JSON with `json.Unmarshal`. |
| API params include web platform metadata and millisecond timestamp. | `_build_api_params`: `t`, `platform_type=web`, `devtype=web`, `channel=web`, `from=web`. | `apiParams()` emits the same parameter keys for course/detail/video APIs. |
| Course/product discovery endpoints. | Constants: `user_profession_coursev3`, `course_package`, `category_tree_course_v2`, `course_detail`, `goods_detailv3`, `api/course`, `orderv2`, `my_course_list`. | `loadTopLevelPayloads()` calls course list, product detail, package, shicao tree, course catalog, and order list endpoints with matching query keys. |
| Course target parsing supports course and product hint names. | `_get_cid` decrypted strings: `zl_play_course_id`, `zl_commodity_course_id`, `zl_product_id`, `zl_commodity_product_id`, query parse helpers. | `parseIDs()` accepts the same course/product aliases plus canonical `course_id/courseId/product_id/productId`. |
| Available node and detail loading. | `_build_available_nodes`, `_build_node_request_candidates`, `_get_course_detail`: `sub_course_id`, `year`, `teacher_id`, `subject_id`, `course_id`. | `buildAvailableNodes()`, `nodeParamCandidates()`, and `loadNodePayloads()` preserve those JSON keys and call `course_detail`/catalog. |
| Video data endpoints. | `_get_video_data`: paid `course_video_switchv2`, plus `free_api/course_video_switch_v2` and `free_api/practice_course_video_switch_v2`; `shicao_video_course_types = ('1','15','16')`. | `getVideoData()` calls all three source video endpoints with source parameter names and keeps the source shicao type set. |
| Video list parsing. | `_parse_video_list`: walks `options/children`, item types `1/2`, `item_id`, `course_section_id`, `item_name`, `dir_name`. | `parseVideoList()` walks decoded JSON and extracts `item_type`, `item_id`, `course_section_id`, `item_name`, `dir_name`, `live_id`, and direct media URLs. |
| Playback authorization/decryption. | `_get_video_play_auth`, `_decode_signed_data`, `_decrypt_hex_text`, `PLAY_AES_KEY`, `LIVE_AES_KEY`, `_normalize_hls_url`. | `getVideoPlayData()`, `decodeSignedData()`, `decryptHexText()`, `normalizeMediaURL()` reproduce AES-ECB hex decrypt, JSON decode, domain selection, and HLS URL normalization. |
| Live/QCloud playback. | `_get_live_detail`, `_pick_live_play_auth`, `_request_qcloud_play_info`, `qcloud_play_api = https://playvideo.qcloud.com/getplayinfo/v4/{}/{}` and RSA overlay fields. | `getLiveDetail()`, `pickLivePlayAuth()`, and `requestQCloudPlayInfo()` call the source APIs and include `psign`, `cipheredOverlayKey`, `cipheredOverlayIv`, `keyId=1`. |
| Course file extraction. | `_extract_direct_files`: `file_url`, `fileUrl`, `download_url`, `downloadUrl`, `attach_url`, `attachUrl`, `resource_url`, `resourceUrl`, `pdf_url`, `pdfUrl`, `ppt_url`, `pptUrl`, `doc_url`, `docUrl`, `url`. | `collectFiles()` uses the same key set and emits file entries with source referer headers. |
