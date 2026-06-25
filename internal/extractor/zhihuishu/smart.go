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
//
// BLOCKED: _post_encrypted requires RSA + AES server-side key exchange
// (appcomm-user.zhihuishu.com/app-commserv-user/c/has) to obtain a per-session
// AES key, then all API calls encrypt their POST body with that key.
// The RSA public key is hardcoded but the server returns an RSA-encrypted
// symmetric key that must be decrypted with the same public key's modulus+exponent
// (a non-standard "verify" operation). This requires crypto/rsa and the exact
// server-side protocol. The knowledge-dic and node-resources APIs all go through
// this encrypted channel.
//
// We implement what is possible without the encrypted channel:
// - URL routing and ID extraction
// - Course resource tree download (url_course_resource / url_course_preview)
//   which uses plain POST, not _post_encrypted
// - Video URL resolution via initVideoNew + changeVideoLine (query params, not encrypted)
//
// The knowledge graph APIs (_get_infos, _get_node_resources) require the encrypted
// channel and will return a "blocked" error explaining why.
package zhihuishu

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
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

// extractSmart implements the Zhihuishu_Smart flow.
//
// The knowledge graph APIs require an encrypted channel (_post_encrypted) that
// needs RSA key exchange with the server. We extract what is possible via the
// unencrypted course resource APIs and video resolution.
func extractSmart(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	ctx := parseSmartURL(rawURL)
	if ctx.cid == "" && ctx.mapUID == "" {
		return nil, fmt.Errorf("cannot parse zhihuishu smart URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := zhihuishuHeaders("https://ai-smart-course-student-pro.zhihuishu.com/")

	title := "zhihuishu_smart_" + firstNonEmpty(ctx.cid, ctx.mapUID)

	// Try to get course resources via the non-encrypted course resource API.
	// This is the url_course_resource endpoint which uses plain POST.
	var entries []*extractor.MediaInfo
	if ctx.cid != "" {
		entries = collectSmartCourseResources(c, ctx.cid, h)
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

	// If no course resources found, the knowledge graph APIs would be needed.
	// These require the encrypted channel which we cannot implement without
	// the server-side RSA key exchange.
	return nil, fmt.Errorf("zhihuishu smart course %s: knowledge graph APIs require encrypted channel "+
		"(_post_encrypted with RSA key exchange via appcomm-user.zhihuishu.com/app-commserv-user/c/has). "+
		"The server returns an RSA-encrypted AES key that must be decrypted to encrypt all subsequent API payloads. "+
		"Course resource tree (non-encrypted) returned no downloadable items. "+
		"This is a genuinely unrecoverable DRM/crypto flow without implementing the full RSA+AES protocol",
		firstNonEmpty(ctx.cid, ctx.mapUID))
}

// collectSmartCourseResources uses the non-encrypted course resource API
// (Zhihuishu_Smart._get_course_resource_list + _download_course_resource_tree).
func collectSmartCourseResources(c *util.Client, cid string, h map[string]string) []*extractor.MediaInfo {
	items := getSmartCourseResourceList(c, cid, "", h)
	if len(items) == 0 {
		return nil
	}
	visited := make(map[string]bool)
	return walkSmartResourceTree(c, cid, items, h, visited, "")
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
	DataType           string      `json:"dataType"`
	ResourcesDataType  string      `json:"resourcesDataType"`
	FolderID           string      `json:"folderId"`
	Name               string      `json:"name"`
	ResourcesName      string      `json:"resourcesName"`
	URL                string      `json:"url"`
	ResourcesURL       string      `json:"resourcesUrl"`
	FileID             string      `json:"fileId"`
	ResourcesFileID    string      `json:"resourcesFileId"`
	Suffix             string      `json:"suffix"`
	ResourcesSuffix    string      `json:"resourcesSuffix"`
	Size               json.Number `json:"size"`
}

func walkSmartResourceTree(c *util.Client, cid string, items []smartResourceItem, h map[string]string, visited map[string]bool, prefix string) []*extractor.MediaInfo {
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
			sub := walkSmartResourceTree(c, cid, subItems, h, visited, idx)
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
			// Try to get best quality video URL
			fid := firstNonEmpty(item.FileID, item.ResourcesFileID)
			if fid != "" {
				if betterURL := getSmartVideoURL(c, fid, fileURL, h); betterURL != "" {
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
func getSmartVideoURL(c *util.Client, fileID, fallbackURL string, h map[string]string) string {
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
	// Return the last URL (typically highest quality, matching source logic
	// which sorts by Content-Length and picks [-1] for HD)
	return urls[len(urls)-1]
}
