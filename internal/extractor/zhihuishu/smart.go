// smart.go implements the Zhihuishu_Smart sub-brand extractor.
//
// Source: decompiled Mooc/Courses/Zhihuishu/Zhihuishu_Smart.pyc
//
// URL pattern from Mooc_Config courses_re:
//
//	Zhihuishu_Smart: (?:https?://(?:ai-smart-course-student-pro|smartcoursestudent)\.zhihuishu\.com/
//	  (?=[^?#]*(?:/\d+){2,})[^?#]+(?:\?[^#]*)?|
//	  https?://wisdomh5\.zhihuishu\.com/[^?]*?(?P<map_uid>\d{15,})[^?]*\?[^#]*courseId=(?P<cid3>11\d+))
//
// Endpoints from Zhihuishu_Smart class attributes:
//
//	url_get_map_uid       = "https://kg-ai-run.zhihuishu.com/run/gateway/t/common/course/get-course-mapUid"
//	url_map_detail        = "https://kg-knowledge-graph.zhihuishu.com/knowledgegraph/gateway/t/map/v2/get-map-detail"
//	url_knowledge_dic     = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/knowledge-study/get-course-knowledge-dic"
//	url_map_knowledge_dic = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/knowledge-study/get-map-knowledge-dic"
//	url_node_resources    = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/resources/list-node-resources"
//	url_wisdom_resources  = "https://kg-knowledge-graph.zhihuishu.com/knowledgegraph/gateway/t/resources/list-node-resources"
//	url_task_list         = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/task/get-user-tasks"
//	url_task_detail       = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/class/task/ai-task-details"
//	url_task_resources    = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/task/resource"
//	url_video_init        = "https://newbase.zhihuishu.com/video/initVideoNew"
//	url_video_change      = "https://newbase.zhihuishu.com/video/changeVideoLine"
//	url_course_resource   = "https://ai-course-platform.zhihuishu.com/api/v1/coursehome/AtlasCourseResource/queryCourseResourceInfo"
//	url_course_preview    = "https://coursehome.zhihuishu.com/home/resource/queryPreviewFilePath/{}/{}"
package zhihuishu

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlSmartGetMapUID       = "https://kg-ai-run.zhihuishu.com/run/gateway/t/common/course/get-course-mapUid"
	urlSmartMapDetail       = "https://kg-knowledge-graph.zhihuishu.com/knowledgegraph/gateway/t/map/v2/get-map-detail"
	urlSmartKnowledgeDic    = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/knowledge-study/get-course-knowledge-dic"
	urlSmartMapKnowledgeDic = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/knowledge-study/get-map-knowledge-dic"
	urlSmartNodeResources   = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/resources/list-node-resources"
	urlSmartWisdomResources = "https://kg-knowledge-graph.zhihuishu.com/knowledgegraph/gateway/t/resources/list-node-resources"
	urlSmartTaskList        = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/task/get-user-tasks"
	urlSmartTaskDetail      = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/class/task/ai-task-details"
	urlSmartTaskResources   = "https://kg-ai-run.zhihuishu.com/run/gateway/t/stu/task/resource"
	urlSmartVideoInit       = "https://newbase.zhihuishu.com/video/initVideoNew"
	urlSmartVideoChange     = "https://newbase.zhihuishu.com/video/changeVideoLine"
	urlSmartCourseResource  = "https://ai-course-platform.zhihuishu.com/api/v1/coursehome/AtlasCourseResource/queryCourseResourceInfo"
	urlSmartCoursePreview   = "https://coursehome.zhihuishu.com/home/resource/queryPreviewFilePath/%s/%s"
	urlSmartHasAESKey       = "https://appcomm-user.zhihuishu.com/app-commserv-user/c/has"
	smartRSAPublicKeyB64    = "MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCgfZmpLpPEpEFRKBe+ZjWJUjPe+7qg7pGqcfN3j2egJ8H2mrKwaEqZEnPnpi2O3hN8HRyaFozDOp8gwZiYfiIZjWy0Jr/FNAiiKYh5bq0GsEn+ieMmRyJg/+i1rqizhvCXvFdrdGhFTw5EBwTpsGdwe1utdlrvIJUAFWj9Yh4qbQIDAQAB"
)

var smartHostRe = regexp.MustCompile(`(?i)(?:ai-smart-course-student-pro|smartcoursestudent|wisdomh5)\.zhihuishu\.com`)

func isSmartURL(u string) bool {
	return smartHostRe.MatchString(u)
}

type smartContext struct {
	cid     string
	classID string
	mapUID  string
}

func parseSmartURL(rawURL string) smartContext {
	ctx := smartContext{}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ctx
	}

	q := parsed.Query()

	// Check for mapUid in query
	if uid := q.Get("mapUid"); uid != "" {
		ctx.mapUID = uid
	}

	// wisdomh5 URLs: mapUID in path, courseId in query
	if strings.Contains(parsed.Host, "wisdomh5") {
		m := regexp.MustCompile(`(\d{15,})`).FindStringSubmatch(parsed.Path)
		if len(m) > 1 {
			ctx.mapUID = m[1]
		}
		if cid := q.Get("courseId"); cid != "" {
			ctx.cid = cid
		}
		return ctx
	}

	// ai-smart-course / smartcoursestudent: extract numeric path segments
	pathSegs := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	var numSegs []string
	for _, seg := range pathSegs {
		if seg != "" && regexp.MustCompile(`^\d+$`).MatchString(seg) {
			numSegs = append(numSegs, seg)
		}
	}
	if len(numSegs) >= 2 {
		ctx.cid = numSegs[0]
		// Check if path starts with myTaskDetail
		if len(pathSegs) > 0 && pathSegs[0] == "myTaskDetail" && len(numSegs) >= 3 {
			ctx.classID = numSegs[1]
		} else {
			ctx.classID = numSegs[len(numSegs)-1]
		}
	}

	return ctx
}

func extractSmart(rawURL string, opts *extractor.ExtractOpts, mode zhsMode) (*extractor.MediaInfo, error) {
	ctx := parseSmartURL(rawURL)
	if ctx.cid == "" && ctx.mapUID == "" {
		return nil, fmt.Errorf("cannot parse zhihuishu smart URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := zhihuishuHeaders("https://ai-smart-course-student-pro.zhihuishu.com/")
	cookie := smartCookieHeader(opts.Cookies)
	if cookie != "" {
		h["Cookie"] = cookie
	}
	session := &smartSession{c: c, h: h, cookie: cookie}

	title := "zhihuishu_smart_" + firstNonEmpty(ctx.cid, ctx.mapUID)
	if resolved := session.resolveTitle(&ctx); resolved != "" {
		title = resolved
	}

	var entries []*extractor.MediaInfo
	if ctx.cid != "" {
		entries = append(entries, collectSmartCourseResources(c, ctx.cid, h, mode)...)
	}
	var encryptedErr error
	if nodeEntries, err := session.collectNodeEntries(ctx, mode); err == nil {
		entries = append(entries, nodeEntries...)
	} else {
		encryptedErr = err
	}
	if taskEntries, err := session.collectTaskEntries(ctx, mode); err == nil {
		entries = append(entries, taskEntries...)
	} else if encryptedErr == nil {
		encryptedErr = err
	}

	if len(entries) > 0 {
		return &extractor.MediaInfo{
			Site:    "zhihuishu",
			Title:   title,
			Entries: entries,
			Extra: map[string]any{
				"course_id":          ctx.cid,
				"class_id":           ctx.classID,
				"map_uid":            ctx.mapUID,
				"discovered_entries": len(entries),
				"sub_brand":          "smart",
			},
		}, nil
	}

	if encryptedErr != nil {
		return nil, fmt.Errorf("zhihuishu smart course %s returned no downloadable items; encrypted knowledge APIs failed: %w", firstNonEmpty(ctx.cid, ctx.mapUID), encryptedErr)
	}
	return nil, fmt.Errorf("zhihuishu smart course %s returned no downloadable items", firstNonEmpty(ctx.cid, ctx.mapUID))
}

// collectSmartCourseResources uses the non-encrypted course resource API
// (Zhihuishu_Smart._get_course_resource_list + _download_course_resource_tree).
func collectSmartCourseResources(c *util.Client, cid string, h map[string]string, mode zhsMode) []*extractor.MediaInfo {
	items := getSmartCourseResourceList(c, cid, "", h)
	if len(items) == 0 {
		return nil
	}
	visited := make(map[string]bool)
	return walkSmartResourceTree(c, cid, items, h, visited, "", mode)
}

// getSmartCourseResourceList implements Zhihuishu_Smart._get_course_resource_list.
func getSmartCourseResourceList(c *util.Client, cid, folderID string, h map[string]string) []smartResourceItem {
	if cid == "" {
		return nil
	}
	data := map[string]string{"courseId": cid}
	if folderID != "" {
		data["folderId"] = folderID
	} else {
		data["chapter"] = "-1"
	}
	body, err := c.PostForm(urlSmartCourseResource, data, h)
	if err != nil {
		return nil
	}
	var resp struct {
		Result struct {
			DataInfosRt []smartResourceItem `json:"dataInfosRt"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return nil
	}
	return resp.Result.DataInfosRt
}

type smartResourceItem struct {
	DataType          string      `json:"dataType"`
	ResourcesDataType string      `json:"resourcesDataType"`
	FolderID          string      `json:"folderId"`
	Name              string      `json:"name"`
	ResourcesName     string      `json:"resourcesName"`
	URL               string      `json:"url"`
	ResourcesURL      string      `json:"resourcesUrl"`
	FileID            string      `json:"fileId"`
	ResourcesFileID   string      `json:"resourcesFileId"`
	Suffix            string      `json:"suffix"`
	ResourcesSuffix   string      `json:"resourcesSuffix"`
	Size              json.Number `json:"size"`
}

func walkSmartResourceTree(c *util.Client, cid string, items []smartResourceItem, h map[string]string, visited map[string]bool, prefix string, mode zhsMode) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for i, item := range items {
		idx := fmt.Sprintf("%d", i+1)
		if prefix != "" {
			idx = prefix + "." + idx
		}
		dt := firstNonEmpty(item.DataType, item.ResourcesDataType)
		if dt == "folder" && item.FolderID != "" {
			fid := item.FolderID
			if visited[fid] {
				continue
			}
			visited[fid] = true
			subItems := getSmartCourseResourceList(c, cid, fid, h)
			sub := walkSmartResourceTree(c, cid, subItems, h, visited, idx, mode)
			out = append(out, sub...)
			continue
		}
		// Resolve URL
		fileURL := resolveSmartResourceURL(c, cid, item, h)
		if fileURL == "" {
			continue
		}
		suffix := getSmartResourceSuffix(item, fileURL)
		name := firstNonEmpty(item.Name, item.ResourcesName, "resource")
		entryName := fmt.Sprintf("(%s)--%s", idx, sanitize(name))

		// Check if video type
		if dt == "video" || suffix == "mp4" {
			if mode.onlyFiles {
				continue
			}
			// Try to get best quality video URL
			fid := firstNonEmpty(item.FileID, item.ResourcesFileID)
			if fid != "" {
				if betterURL := getSmartVideoURL(c, fid, fileURL, h, mode); betterURL != "" {
					fileURL = betterURL
				}
			}
			out = append(out, &extractor.MediaInfo{
				Site:  "zhihuishu",
				Title: entryName,
				Streams: map[string]extractor.Stream{
					"default": {
						Quality: "best",
						URLs:    []string{fileURL},
						Format:  pickFormat(fileURL),
						Headers: h,
					},
				},
			})
		} else if suffix != "" {
			out = append(out, &extractor.MediaInfo{
				Site:  "zhihuishu",
				Title: entryName,
				Streams: map[string]extractor.Stream{
					"default": {
						Quality: "default",
						URLs:    []string{fileURL},
						Format:  suffix,
						Headers: h,
					},
				},
			})
		}
	}
	return out
}

func resolveSmartResourceURL(c *util.Client, cid string, item smartResourceItem, h map[string]string) string {
	fileURL := firstNonEmpty(item.URL, item.ResourcesURL)
	if strings.HasPrefix(fileURL, "//") {
		fileURL = "https:" + fileURL
	}
	fid := firstNonEmpty(item.FileID, item.ResourcesFileID)
	if fid != "" && (strings.Contains(fileURL, "/able-commons/resources/") || strings.Contains(fileURL, "swfReader.jsp")) {
		resolved := getSmartCourseFileURL(c, cid, fid, h)
		if resolved != "" {
			fileURL = resolved
		}
	}
	if strings.HasPrefix(fileURL, "//") {
		fileURL = "https:" + fileURL
	}
	if fileURL != "" && regexp.MustCompile(`^https?://`).MatchString(fileURL) {
		return fileURL
	}
	return ""
}

// getSmartCourseFileURL implements Zhihuishu_Smart._get_course_file_url.
func getSmartCourseFileURL(c *util.Client, cid, fileID string, h map[string]string) string {
	if cid == "" || fileID == "" {
		return ""
	}
	apiURL := fmt.Sprintf(urlSmartCoursePreview, cid, fileID)
	body, err := c.PostForm(apiURL, map[string]string{}, h)
	if err != nil {
		return ""
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "//") {
		body = "https:" + body
	}
	if !regexp.MustCompile(`^https?://`).MatchString(body) {
		return ""
	}
	result := body
	// Follow redirect and extract WOPISrc
	resp, err := c.Get(body, h)
	if err == nil {
		resp.Body.Close()
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL := resp.Request.URL.String()
			parsed, pErr := url.Parse(finalURL)
			if pErr == nil {
				if wopi := parsed.Query().Get("WOPISrc"); wopi != "" {
					result = wopi
				} else if parts := strings.SplitN(finalURL, "?WOPISrc=", 2); len(parts) == 2 {
					result = parts[1]
				}
			}
		}
	}
	if strings.HasPrefix(result, "//") {
		result = "https:" + result
	}
	return result
}

func getSmartResourceSuffix(item smartResourceItem, _ string) string {
	s := firstNonEmpty(item.Suffix, item.ResourcesSuffix)
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), ".")
	return s
}

// getSmartVideoURL implements Zhihuishu_Smart._get_video_url.
// Uses initVideoNew + changeVideoLine with query params (not encrypted).
// Compares file sizes across lines to pick best quality.
func getSmartVideoURL(c *util.Client, fileID, fallbackURL string, h map[string]string, mode zhsMode) string {
	if fileID == "" || len(fileID) > 12 {
		return fallbackURL
	}

	initURL := fmt.Sprintf("%s?videoID=%s", urlSmartVideoInit, fileID)
	body, err := c.GetString(initURL, h)
	if err != nil {
		return fallbackURL
	}
	var initResp struct {
		Result struct {
			UUID  string `json:"uuid"`
			Lines []struct {
				LineID int `json:"lineID"`
			} `json:"lines"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(body), &initResp) != nil {
		return fallbackURL
	}
	uuid := initResp.Result.UUID
	lines := initResp.Result.Lines
	if len(lines) == 0 || uuid == "" {
		return fallbackURL
	}

	// Collect video URLs from all lines
	var urls []string
	if fallbackURL != "" {
		urls = append(urls, fallbackURL)
	}
	for _, line := range lines {
		if line.LineID == 0 {
			continue
		}
		changeURL := fmt.Sprintf("%s?videoID=%s&lineID=%d&uuid=%s",
			urlSmartVideoChange, fileID, line.LineID, uuid)
		changeBody, err := c.GetString(changeURL, h)
		if err != nil {
			continue
		}
		var changeResp struct {
			Result string `json:"result"`
		}
		if json.Unmarshal([]byte(changeBody), &changeResp) == nil && changeResp.Result != "" {
			urls = append(urls, changeResp.Result)
		}
	}

	if len(urls) == 0 {
		return fallbackURL
	}
	if mode.hd {
		return urls[len(urls)-1]
	}
	return urls[0]
}

type smartSession struct {
	c      *util.Client
	h      map[string]string
	cookie string
	aesKey string
}

type smartNode struct {
	id   string
	name string
	path string
}

func (s *smartSession) resolveTitle(ctx *smartContext) string {
	if ctx.mapUID != "" {
		root, err := s.postEncrypted(urlSmartMapDetail, map[string]any{"mapUid": ctx.mapUID})
		if err != nil {
			return ""
		}
		return smartMapDetailTitle(root)
	}
	if ctx.cid == "" || ctx.classID != "" {
		return ""
	}
	root, err := s.postEncrypted(urlSmartGetMapUID, map[string]any{"courseId": ctx.cid})
	if err != nil {
		return ""
	}
	data := smartMap(root["data"])
	if uid := firstNonEmpty(smartString(data["scMapUid"]), smartString(data["mapUid"])); uid != "" {
		if detail, err := s.postEncrypted(urlSmartMapDetail, map[string]any{"mapUid": uid}); err == nil {
			if title := smartMapDetailTitle(detail); title != "" {
				return title
			}
		}
	}
	return sanitize(firstNonEmpty(smartString(data["mapName"]), ""))
}

func (s *smartSession) collectNodeEntries(ctx smartContext, mode zhsMode) ([]*extractor.MediaInfo, error) {
	nodes, err := s.smartNodes(ctx)
	if err != nil || len(nodes) == 0 {
		return nil, err
	}
	var out []*extractor.MediaInfo
	for i, node := range nodes {
		resources, err := s.nodeResources(ctx, node.id)
		if err != nil {
			continue
		}
		prefix := fmt.Sprintf("%d", i+1)
		if node.path != "" {
			prefix = node.path
		}
		out = append(out, smartResourcesToEntries(s.c, ctx.cid, resources, s.h, prefix, node.name, map[string]any{"node_id": node.id}, mode)...)
	}
	return out, nil
}

func (s *smartSession) smartNodes(ctx smartContext) ([]smartNode, error) {
	if ctx.mapUID != "" {
		root, err := s.postEncrypted(urlSmartMapKnowledgeDic, map[string]any{"mapUid": ctx.mapUID})
		if err != nil {
			return nil, err
		}
		return smartNodesFromThemes(smartList(smartMap(root["data"])["themeList"]), true), nil
	}
	if ctx.cid == "" || ctx.classID == "" {
		return nil, nil
	}
	payload := map[string]any{
		"dateFormate": time.Now().UnixMilli(),
		"uuid":        smartUUIDFromCookie(s.cookie),
		"classId":     ctx.classID,
		"courseId":    ctx.cid,
	}
	root, err := s.postEncrypted(urlSmartKnowledgeDic, payload)
	if err != nil {
		return nil, err
	}
	return smartNodesFromThemes(smartList(smartMap(root["data"])["themeList"]), false), nil
}

func smartNodesFromThemes(themes []map[string]any, mapMode bool) []smartNode {
	var out []smartNode
	for ti, theme := range themes {
		themeName := sanitize(firstNonEmpty(smartString(theme["themeName"]), "主题"))
		themePrefix := fmt.Sprintf("%d", ti+1)
		if mapMode {
			out = append(out, smartKnowledgeNodes(smartList(theme["knowledgeList"]), themePrefix, themeName)...)
			continue
		}
		subThemes := smartList(theme["subThemeList"])
		if len(subThemes) == 0 {
			out = append(out, smartKnowledgeNodes(smartList(theme["knowledgeList"]), themePrefix, themeName)...)
			continue
		}
		for si, sub := range subThemes {
			subName := sanitize(firstNonEmpty(smartString(sub["themeName"]), "default"))
			prefix := themePrefix
			group := themeName
			if subName != "" && !strings.EqualFold(subName, "default") && len(subThemes) > 1 {
				prefix = fmt.Sprintf("%s.%d", themePrefix, si+1)
				group = themeName + "--" + subName
			}
			out = append(out, smartKnowledgeNodes(smartList(sub["knowledgeList"]), prefix, group)...)
		}
	}
	return out
}

func smartKnowledgeNodes(list []map[string]any, prefix, group string) []smartNode {
	var out []smartNode
	for i, item := range list {
		id := smartString(item["knowledgeId"])
		if id == "" {
			continue
		}
		name := sanitize(firstNonEmpty(smartString(item["knowledgeName"]), "知识点"))
		path := fmt.Sprintf("%s.%d", prefix, i+1)
		out = append(out, smartNode{id: id, name: name, path: path + "--" + firstNonEmpty(group, name)})
	}
	return out
}

func (s *smartSession) nodeResources(ctx smartContext, nodeID string) ([]map[string]any, error) {
	if nodeID == "" {
		return nil, nil
	}
	payload := map[string]any{
		"dateFormate": time.Now().UnixMilli(),
		"uuid":        smartUUIDFromCookie(s.cookie),
	}
	endpoint := urlSmartNodeResources
	if ctx.mapUID != "" {
		payload["mapUid"] = ctx.mapUID
		payload["nodeUid"] = nodeID
		endpoint = urlSmartWisdomResources
	} else {
		payload["classId"] = ctx.classID
		payload["courseId"] = ctx.cid
		payload["knowledgeId"] = nodeID
	}
	root, err := s.postEncrypted(endpoint, payload)
	if err != nil {
		return nil, err
	}
	code := smartString(root["code"])
	if code != "" && code != "200" && code != "0" {
		return nil, nil
	}
	data := smartMap(root["data"])
	if list := smartList(data["resourcesList"]); len(list) > 0 {
		return list, nil
	}
	return smartList(root["data"]), nil
}

func (s *smartSession) collectTaskEntries(ctx smartContext, mode zhsMode) ([]*extractor.MediaInfo, error) {
	if ctx.cid == "" || ctx.classID == "" {
		return nil, nil
	}
	root, err := s.postEncrypted(urlSmartTaskList, map[string]any{"courseId": ctx.cid, "classId": ctx.classID})
	if err != nil {
		return nil, err
	}
	tasks := smartList(root["data"])
	var out []*extractor.MediaInfo
	for i, task := range tasks {
		taskID := firstNonEmpty(smartString(task["taskId"]), smartString(task["id"]))
		if taskID == "" {
			continue
		}
		detail, _ := s.postEncrypted(urlSmartTaskDetail, map[string]any{"courseId": ctx.cid, "classId": ctx.classID, "taskId": taskID})
		resRoot, err := s.postEncrypted(urlSmartTaskResources, map[string]any{"courseId": ctx.cid, "taskId": taskID})
		if err != nil {
			continue
		}
		taskName := firstNonEmpty(smartString(smartMap(detail["data"])["taskName"]), smartString(task["taskName"]), "资料任务")
		extra := map[string]any{"task_id": taskID}
		out = append(out, smartResourcesToEntries(s.c, ctx.cid, smartList(resRoot["data"]), s.h, fmt.Sprintf("task.%d", i+1), taskName, extra, mode)...)
	}
	return out, nil
}

func smartResourcesToEntries(c *util.Client, cid string, resources []map[string]any, h map[string]string, prefix, group string, extraBase map[string]any, mode zhsMode) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	for i, item := range resources {
		dataType := firstNonEmpty(smartString(item["dataType"]), smartString(item["resourcesDataType"]))
		if dataType == "21" {
			continue
		}
		fileURL := smartHTTPURL(firstNonEmpty(smartString(item["resourcesUrl"]), smartString(item["url"]), smartString(item["fileUrl"]), smartString(item["downloadUrl"])))
		fileID := firstNonEmpty(smartString(item["resourcesFileId"]), smartString(item["fileId"]), smartString(item["videoId"]))
		if cid != "" && fileID != "" && (fileURL == "" || strings.Contains(fileURL, "/able-commons/resources/") || strings.Contains(fileURL, "swfReader.jsp")) {
			if resolved := getSmartCourseFileURL(c, cid, fileID, h); resolved != "" {
				fileURL = resolved
			}
		}
		suffix := strings.TrimPrefix(strings.ToLower(firstNonEmpty(smartString(item["resourcesSuffix"]), smartString(item["suffix"]))), ".")
		if dataType == "22" || dataType == "video" {
			suffix = "mp4"
		}
		if suffix == "mp4" && mode.onlyFiles {
			continue
		}
		if suffix == "mp4" && fileID != "" {
			fileURL = getSmartVideoURL(c, fileID, fileURL, h, mode)
		}
		if fileURL == "" {
			continue
		}
		name := sanitize(firstNonEmpty(smartString(item["resourcesName"]), smartString(item["name"]), smartString(item["title"]), "资源"))
		entryTitle := fmt.Sprintf("(%s.%d)--%s", prefix, i+1, name)
		if group != "" {
			entryTitle = fmt.Sprintf("(%s.%d)--%s--%s", prefix, i+1, sanitize(group), name)
		}
		format := suffix
		if format == "" {
			format = pickFormat(fileURL)
		}
		extra := map[string]any{"resource_file_id": fileID, "resource_data_type": dataType}
		for k, v := range extraBase {
			extra[k] = v
		}
		out = append(out, &extractor.MediaInfo{
			Site:  "zhihuishu",
			Title: entryTitle,
			Streams: map[string]extractor.Stream{
				"default": {
					Quality:   "best",
					URLs:      []string{fileURL},
					Format:    format,
					NeedMerge: format == "m3u8",
					Headers:   h,
				},
			},
			Extra: extra,
		})
	}
	return out
}

func (s *smartSession) postEncrypted(endpoint string, data map[string]any) (map[string]any, error) {
	if s.aesKey == "" {
		key, err := smartGetAESKey(s.c, s.cookie)
		if err != nil {
			return nil, err
		}
		s.aesKey = key
	}
	plain, err := smartCompactJSON(data)
	if err != nil {
		return nil, err
	}
	secret, err := smartAESEncrypt(s.aesKey, plain)
	if err != nil {
		return nil, err
	}
	headerToken, _ := smartAESEncrypt(s.aesKey, "")
	payload := map[string]any{"secretStr": secret, "date": time.Now().UnixMilli()}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	h := smartCloneHeaders(s.h)
	h["Content-Type"] = "application/json;charset=UTF-8"
	h["XQJZXHIZ"] = headerToken
	resp, err := s.c.Post(endpoint, bytes.NewReader(body), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	return root, nil
}

func smartGetAESKey(c *util.Client, cookie string) (string, error) {
	pub, err := smartRSAPublicKey()
	if err != nil {
		return "", err
	}
	requestJSON, _ := smartCompactJSON(map[string]any{"module": 6})
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(requestJSON))
	if err != nil {
		return "", err
	}
	hasURL := urlSmartHasAESKey + "?uid=" + url.QueryEscape(base64.StdEncoding.EncodeToString(encrypted))
	h := map[string]string{
		"Referer":    "https://ai-smart-course-student-pro.zhihuishu.com/",
		"User-Agent": "Mozilla/5.0",
	}
	if cookie != "" {
		h["Cookie"] = cookie
	}
	body, err := c.GetString(hasURL, h)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return "", err
	}
	sl := smartString(smartMap(root["rt"])["sl"])
	if sl == "" {
		return "", fmt.Errorf("zhihuishu smart AES key response missing rt.sl")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(sl)
	if err != nil {
		return "", err
	}
	keyJSON, err := rsaPublicUnpad(ciphertext, pub)
	if err != nil {
		return "", err
	}
	var decoded map[string]any
	if err := json.Unmarshal(keyJSON, &decoded); err != nil {
		return "", err
	}
	key := smartString(decoded["cKey"])
	if key == "" {
		return "", fmt.Errorf("zhihuishu smart AES key response missing cKey")
	}
	return key, nil
}

func smartRSAPublicKey() (*rsa.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(smartRSAPublicKeyB64)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("zhihuishu smart public key is %T, not RSA", parsed)
	}
	return pub, nil
}

func rsaPublicUnpad(ciphertext []byte, pub *rsa.PublicKey) ([]byte, error) {
	m := new(big.Int).SetBytes(ciphertext)
	e := big.NewInt(int64(pub.E))
	out := m.Exp(m, e, pub.N).Bytes()
	if size := pub.Size(); len(out) < size {
		padded := make([]byte, size)
		copy(padded[size-len(out):], out)
		out = padded
	}
	if len(out) >= 3 && out[0] == 0 && out[1] == 1 {
		if idx := bytes.IndexByte(out[2:], 0); idx >= 0 {
			return out[idx+3:], nil
		}
	}
	if len(out) >= 2 && out[0] == 1 {
		if idx := bytes.IndexByte(out[1:], 0); idx >= 0 {
			return out[idx+2:], nil
		}
	}
	if idx := bytes.IndexByte(out, '{'); idx >= 0 {
		return out[idx:], nil
	}
	return nil, fmt.Errorf("zhihuishu smart AES key RSA block has no JSON payload")
}

func smartAESEncrypt(key, plaintext string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(zhsAESIV)).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out), nil
}

func smartCompactJSON(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

func smartCookieHeader(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	hosts := []string{
		"https://ai-smart-course-student-pro.zhihuishu.com/",
		"https://smartcoursestudent.zhihuishu.com/",
		"https://wisdomh5.zhihuishu.com/",
		"https://appcomm-user.zhihuishu.com/",
		"https://kg-ai-run.zhihuishu.com/",
		"https://kg-knowledge-graph.zhihuishu.com/",
		"https://www.zhihuishu.com/",
		"https://zhihuishu.com/",
	}
	seen := map[string]bool{}
	var parts []string
	for _, raw := range hosts {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		for _, ck := range jar.Cookies(u) {
			key := ck.Name + "=" + ck.Value
			if ck.Name == "" || seen[key] {
				continue
			}
			seen[key] = true
			parts = append(parts, key)
		}
	}
	return strings.Join(parts, "; ")
}

func smartUUIDFromCookie(cookie string) string {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "CASLOGC=") {
			continue
		}
		raw := strings.TrimPrefix(part, "CASLOGC=")
		if decoded, err := url.QueryUnescape(raw); err == nil {
			raw = decoded
		}
		var m map[string]any
		if json.Unmarshal([]byte(raw), &m) == nil {
			return smartString(m["uuid"])
		}
	}
	return ""
}

func smartMapDetailTitle(root map[string]any) string {
	data := smartMap(root["data"])
	name := smartString(data["mapName"])
	school := ""
	if schools := smartList(data["mapSchoolList"]); len(schools) > 0 {
		school = smartString(schools[0]["schoolName"])
	}
	return sanitize(firstNonEmpty(name, "") + map[bool]string{true: "_" + school, false: ""}[school != ""])
}

func smartCloneHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func smartMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func smartList(v any) []map[string]any {
	switch x := v.(type) {
	case []map[string]any:
		return x
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		if list := smartList(x["list"]); len(list) > 0 {
			return list
		}
		if list := smartList(x["resourcesList"]); len(list) > 0 {
			return list
		}
	}
	return nil
}

func smartString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func smartHTTPURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, "/"))
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if regexp.MustCompile(`^https?://`).MatchString(raw) {
		return raw
	}
	return ""
}
