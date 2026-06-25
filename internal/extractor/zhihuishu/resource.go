// resource.go implements course resource / file download flows grounded in the
// decompiled Zhihuishu_Course._get_file_list, _get_file_url, _get_course_file_url,
// _download_source and Zhihuishu_Smart._get_course_resource_list,
// _download_course_resource_tree, _download_course_resource_file.
//
// Endpoints (all from Zhihuishu_Course class attributes):
//
//	url_source    = "https://coursehome.zhihuishu.com/home/resource/queryCourseResourceInfo"
//	url_ai_source = "https://ai-course-platform.zhihuishu.com/api/v1/coursehome/AtlasCourseResource/queryCourseResourceInfo"
//	url_file      = "https://coursehome.zhihuishu.com/home/resource/queryPreviewFilePath/{courseId}/{fileId}"
//	url_data      = "https://stuonline.zhihuishu.com/stuonline/json/data/updateHit"
//	url_download  = "https://stuonline.zhihuishu.com/stuonline/json/data/downloadMaterial1"
//
// Also from Zhihuishu_Course:
//
//	url_hash_file_list = "https://share-course-map-service.zhihuishu.com/gateway/t/node/queryNodeDescription"
package zhihuishu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

// Endpoints from Zhihuishu_Course class attributes.
const (
	urlSource       = "https://coursehome.zhihuishu.com/home/resource/queryCourseResourceInfo"
	urlAISource     = "https://ai-course-platform.zhihuishu.com/api/v1/coursehome/AtlasCourseResource/queryCourseResourceInfo"
	urlFile         = "https://coursehome.zhihuishu.com/home/resource/queryPreviewFilePath/%s/%s"
	urlData         = "https://stuonline.zhihuishu.com/stuonline/json/data/updateHit"
	urlDownload     = "https://stuonline.zhihuishu.com/stuonline/json/data/downloadMaterial1"
	urlHashFileList = "https://share-course-map-service.zhihuishu.com/gateway/t/node/queryNodeDescription"
)

// resourceItem maps items from _get_file_list (url_source / url_ai_source).
type resourceItem struct {
	DataType string  `json:"dataType"`
	FolderID string  `json:"folderId"`
	Name     string  `json:"name"`
	URL      string  `json:"url"`
	ID       string  `json:"id"`
	FileID   string  `json:"fileId"`
	SizeMB   float64 `json:"-"`
}

// hashFileItem maps items from _get_hash_file_list (queryNodeDescription).
type hashFileItem struct {
	FileID   string
	FileURL  string
	FileName string
	FileType string // "video" or "file"
}

// getFileList implements Zhihuishu_Course._get_file_list.
// Tries url_ai_source first; falls back to url_source.
func getFileList(c *util.Client, cid, rid, tid string, folderID *string, h map[string]string) []resourceItem {
	if cid == "" || (rid == "" && tid == "") {
		return nil
	}
	data := map[string]string{
		"chapter":   "-1",
		"type":      "0",
		"termId":    tid,
		"recruitId": rid,
		"courseId":   cid,
	}
	if folderID != nil {
		data["folderId"] = *folderID
	}

	// Try AI source first (source: _get_file_list tries url_ai_source before url_source)
	items := tryParseResourceList(c, urlAISource, data, h, true)
	if len(items) == 0 {
		items = tryParseResourceList(c, urlSource, data, h, false)
	}
	return items
}

func tryParseResourceList(c *util.Client, apiURL string, data map[string]string, h map[string]string, isAI bool) []resourceItem {
	body, err := c.PostForm(apiURL, data, h)
	if err != nil {
		return nil
	}

	var items []resourceItem
	if isAI {
		// AI source returns {result: {dataInfosRt: [...]}}
		var resp struct {
			Result struct {
				DataInfosRt []json.RawMessage `json:"dataInfosRt"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(body), &resp) != nil || len(resp.Result.DataInfosRt) == 0 {
			return nil
		}
		for _, raw := range resp.Result.DataInfosRt {
			items = append(items, parseResourceRaw(raw))
		}
	} else {
		// url_source returns a bare array
		var arr []json.RawMessage
		if json.Unmarshal([]byte(body), &arr) != nil {
			return nil
		}
		for _, raw := range arr {
			items = append(items, parseResourceRaw(raw))
		}
	}
	return items
}

func parseResourceRaw(raw json.RawMessage) resourceItem {
	var item struct {
		DataType string      `json:"dataType"`
		FolderID string      `json:"folderId"`
		Name     string      `json:"name"`
		URL      string      `json:"url"`
		ID       json.Number `json:"id"`
		FileID   string      `json:"fileId"`
		Size     json.Number `json:"size"`
	}
	_ = json.Unmarshal(raw, &item)
	var sizeMB float64
	if s, err := item.Size.Float64(); err == nil && s > 0 {
		sizeMB = s / 1048576
	}
	return resourceItem{
		DataType: item.DataType,
		FolderID: item.FolderID,
		Name:     item.Name,
		URL:      item.URL,
		ID:       item.ID.String(),
		FileID:   item.FileID,
		SizeMB:   sizeMB,
	}
}

// getFileURL implements Zhihuishu_Course._get_file_url.
// url_data (updateHit) -> dataDto.interfaceDataId -> url_download (downloadMaterial1) -> dataUrl
func getFileURL(c *util.Client, fileID string, h map[string]string) string {
	if fileID == "" {
		return ""
	}
	body, err := c.PostForm(urlData, map[string]string{"id": fileID}, h)
	if err != nil {
		return ""
	}
	var resp struct {
		DataDto struct {
			InterfaceDataID string `json:"interfaceDataId"`
		} `json:"dataDto"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil || resp.DataDto.InterfaceDataID == "" {
		return ""
	}
	body2, err := c.PostForm(urlDownload, map[string]string{"dataDto.dataId": resp.DataDto.InterfaceDataID}, h)
	if err != nil {
		return ""
	}
	var resp2 struct {
		DataURL string `json:"dataUrl"`
	}
	if json.Unmarshal([]byte(body2), &resp2) != nil {
		return ""
	}
	return resp2.DataURL
}

// getCourseFileURL implements Zhihuishu_Course._get_course_file_url.
// queryPreviewFilePath -> follow redirect -> extract ?WOPISrc= value
func getCourseFileURL(c *util.Client, cid, fileID string, h map[string]string) string {
	if cid == "" || fileID == "" {
		return ""
	}
	apiURL := fmt.Sprintf(urlFile, cid, fileID)
	body, err := c.PostForm(apiURL, map[string]string{}, h)
	if err != nil || body == "" {
		return ""
	}
	body = strings.TrimSpace(body)
	if !regexp.MustCompile(`^https?://`).MatchString(body) {
		return ""
	}
	// Follow redirect and check for WOPISrc
	resp, err := c.Get(body, h)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if finalURL == "" {
		return ""
	}
	parts := strings.SplitN(finalURL, "?WOPISrc=", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// getHashFileList implements Zhihuishu_Course._get_hash_file_list.
// queryNodeDescription with idHash + idStr -> nodeRespResourceVos
func getHashFileList(c *util.Client, idStr, idHash string, h map[string]string) []hashFileItem {
	if idStr == "" || idHash == "" {
		return nil
	}
	body, err := zhsPostJSON(c, urlHashFileList, map[string]any{
		"idHash": idHash,
		"idStr":  idStr,
	}, h)
	if err != nil {
		return nil
	}
	var resp struct {
		Data struct {
			Resources []struct {
				ResourcesSuffix string `json:"resourcesSuffix"`
				ResourcesFileID string `json:"resourcesFileId"`
				ResourcesURL    string `json:"resourcesUrl"`
				ResourcesName   string `json:"resourcesName"`
			} `json:"nodeRespResourceVos"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return nil
	}
	var out []hashFileItem
	for _, r := range resp.Data.Resources {
		ft := "file"
		if r.ResourcesSuffix == "mp4" {
			ft = "video"
		}
		out = append(out, hashFileItem{
			FileID:   r.ResourcesFileID,
			FileURL:  r.ResourcesURL,
			FileName: r.ResourcesName,
			FileType: ft,
		})
	}
	return out
}

// collectCourseResources gathers all downloadable resources from the course
// resource tree into MediaInfo entries. This is the extraction-time equivalent
// of _download_source - we don't download here, we just return URLs.
func collectCourseResources(c *util.Client, ctx *courseContext, h map[string]string) []*extractor.MediaInfo {
	items := getFileList(c, ctx.cid, ctx.rid, ctx.tid, nil, h)
	if len(items) == 0 {
		return nil
	}
	return walkResourceItems(c, ctx, items, h, "")
}

func walkResourceItems(c *util.Client, ctx *courseContext, items []resourceItem, h map[string]string, prefix string) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for i, item := range items {
		idx := fmt.Sprintf("%d", i+1)
		if prefix != "" {
			idx = prefix + "." + idx
		}
		if item.DataType == "folder" && item.FolderID != "" {
			folderID := item.FolderID
			subItems := getFileList(c, ctx.cid, ctx.rid, ctx.tid, &folderID, h)
			sub := walkResourceItems(c, ctx, subItems, h, idx)
			out = append(out, sub...)
			continue
		}
		fileURL := resolveResourceURL(c, ctx, item, h)
		if fileURL == "" {
			continue
		}
		ext := resourceExtension(fileURL, item.Name)
		name := fmt.Sprintf("(%s)--%s", idx, sanitize(item.Name))
		entry := &extractor.MediaInfo{
			Site:  "zhihuishu",
			Title: name,
			Streams: map[string]extractor.Stream{
				"default": {
					Quality: "default",
					URLs:    []string{fileURL},
					Format:  ext,
					Headers: h,
				},
			},
		}
		out = append(out, entry)
	}
	return out
}

// resolveResourceURL resolves the download URL for a resource item.
// Mirrors the source logic: if url contains able-commons/resources and fileId
// exists, use _get_course_file_url; otherwise use the url directly.
func resolveResourceURL(c *util.Client, ctx *courseContext, item resourceItem, h map[string]string) string {
	fileURL := item.URL
	if fileURL != "" && item.FileID != "" && strings.Contains(fileURL, "base1.zhihuishu.com/able-commons/resources") {
		if resolved := getCourseFileURL(c, ctx.cid, item.FileID, h); resolved != "" {
			fileURL = resolved
		}
	}
	if fileURL != "" && regexp.MustCompile(`^https?://`).MatchString(fileURL) {
		return fileURL
	}
	return ""
}

func resourceExtension(fileURL, name string) string {
	// Extract extension from URL (before query string)
	clean := strings.SplitN(fileURL, "?", 2)[0]
	parts := strings.Split(clean, ".")
	if len(parts) > 1 {
		ext := strings.ToLower(parts[len(parts)-1])
		if len(ext) <= 4 {
			return ext
		}
	}
	// Fallback: from name
	nameParts := strings.Split(name, ".")
	if len(nameParts) > 1 {
		return strings.ToLower(nameParts[len(nameParts)-1])
	}
	return ""
}

// zhsPostJSON sends a JSON POST request and returns the raw response body bytes.
func zhsPostJSON(c *util.Client, apiURL string, payload map[string]any, headers map[string]string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	h := make(map[string]string)
	for k, v := range headers {
		h[k] = v
	}
	h["Content-Type"] = "application/json"
	resp, err := c.Post(apiURL, bytes.NewReader(body), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// collectHashFileEntries attempts to resolve AI-tree hash-based resources
// via queryNodeDescription. These are share-course-map resources identified
// by idHash/idStr rather than numeric videoId.
func collectHashFileEntries(_ *util.Client, _ *courseContext, _ map[string]string) []*extractor.MediaInfo {
	// The AI tree's idHash/idStr resources require the share-course-map-service
	// queryNodeDescription API which returns HTML content, not downloadable media.
	// Skipped as non-downloadable content.
	return nil
}
