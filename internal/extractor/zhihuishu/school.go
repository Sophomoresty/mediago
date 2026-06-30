// school.go implements the Zhihuishu_School sub-brand extractor.
//
// Source: decompiled Mooc/Courses/Zhihuishu/Zhihuishu_School.pyc
//
// URL pattern from Mooc_Config courses_re:
//
//	Zhihuishu_School: https?://.*?zhihuishu\.com/(?!virtual_portals_h5).*?courseId=(?P<cid1>\d+)
//
// Note: School URLs overlap with Course URLs but are distinguished by the
// host being wenda.zhihuishu.com, hiexam-server.zhihuishu.com, or
// studyresources.zhihuishu.com. The router checks for School-specific
// hosts before falling through to Course.
//
// Endpoints from Zhihuishu_School class attributes:
//
//	url_course   = "https://hiexam-server.zhihuishu.com/zhsathome/atCourse/findCourseInfo?courseId={cid}"
//	url_info     = "https://studyresources.zhihuishu.com/studyResources/stuResouce/queryResourceTree?courseId={cid}"
//	url_video_id = "https://studyresources.zhihuishu.com/studyResources/stuResouce/stuViewFile?courseId={cid}&fileId={vid}"
//	file_url     = "https://studyresources.zhihuishu.com/studyResources/stuResouce/stuViewFile?courseId={cid}&fileId={fid}"
//	url_video_init   = "https://newbase.zhihuishu.com/video/initVideo?videoID={}"
//	url_video_change = "https://newbase.zhihuishu.com/video/changeVideoLine?videoID={}&lineID={}&uuid={}"
package zhihuishu

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlSchoolCourse  = "https://hiexam-server.zhihuishu.com/zhsathome/atCourse/findCourseInfo?courseId=%s"
	urlSchoolInfo    = "https://studyresources.zhihuishu.com/studyResources/stuResouce/queryResourceTree?courseId=%s"
	urlSchoolVideoID = "https://studyresources.zhihuishu.com/studyResources/stuResouce/stuViewFile?courseId=%s&fileId=%s"
	urlSchoolFileURL = "https://studyresources.zhihuishu.com/studyResources/stuResouce/stuViewFile?courseId=%s&fileId=%s"
)

var schoolHostRe = regexp.MustCompile(`(?i)(?:wenda|hiexam-server|studyresources)\.zhihuishu\.com`)

func isSchoolURL(u string) bool {
	return schoolHostRe.MatchString(u) && extractSchoolCID(u) != ""
}

func extractSchoolCID(u string) string {
	m := regexp.MustCompile(`(?i)courseId=(\d+)`).FindStringSubmatch(u)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractSchool implements the Zhihuishu_School flow.
func extractSchool(rawURL string, opts *extractor.ExtractOpts, mode zhsMode) (*extractor.MediaInfo, error) {
	cid := extractSchoolCID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse zhihuishu school URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := zhihuishuHeaders("https://www.zhihuishu.com")

	// _get_title: findCourseInfo -> "name"
	title := "zhihuishu_school_" + cid
	body, err := c.GetString(fmt.Sprintf(urlSchoolCourse, cid), h)
	if err == nil {
		if name := match1(body, `"name"\s*:\s*"(.*?)"`); name != "" {
			title = sanitize(name)
		}
	}

	// _get_infos: queryResourceTree -> rt (recursive tree with dataType 3=video, else=file)
	body, err = c.GetString(fmt.Sprintf(urlSchoolInfo, cid), h)
	if err != nil {
		return nil, fmt.Errorf("queryResourceTree: %w", err)
	}
	var treeResp struct {
		Rt []schoolTreeNode `json:"rt"`
	}
	if err := json.Unmarshal([]byte(body), &treeResp); err != nil {
		return nil, fmt.Errorf("parse school resource tree: %w", err)
	}

	// Walk the tree and collect video/file entries
	entries := walkSchoolTree(c, cid, treeResp.Rt, h, "", mode)

	if len(entries) == 0 {
		return nil, fmt.Errorf("zhihuishu school %s returned no downloadable resources", cid)
	}

	return &extractor.MediaInfo{
		Site:    "zhihuishu",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"course_id":          cid,
			"discovered_entries": len(entries),
			"sub_brand":          "school",
		},
	}, nil
}

type schoolTreeNode struct {
	ID        json.Number      `json:"id"`
	Name      string           `json:"name"`
	DataType  json.Number      `json:"dataType"`
	ChildList []schoolTreeNode `json:"childList"`
}

func walkSchoolTree(c *util.Client, cid string, nodes []schoolTreeNode, h map[string]string, prefix string, mode zhsMode) []*extractor.MediaInfo {
	var out []*extractor.MediaInfo
	videoIdx := 1
	fileIdx := 1

	for _, node := range nodes {
		nodeID := node.ID.String()
		dt, _ := node.DataType.Int64()
		name := sanitize(node.Name)

		// Leaf node with id and dataType
		if nodeID != "" && nodeID != "0" && dt > 0 {
			if dt == 3 {
				if mode.onlyFiles {
					continue
				}
				// Video type
				idx := fmt.Sprintf("%d", videoIdx)
				if prefix != "" {
					idx = prefix + "." + idx
				}
				videoName := fmt.Sprintf("[%s]--%s", idx, name)
				videoIdx++

				videoURL := getSchoolVideoURL(c, cid, nodeID, h, mode)
				if videoURL != "" {
					out = append(out, &extractor.MediaInfo{
						Site:  "zhihuishu",
						Title: videoName,
						Streams: map[string]extractor.Stream{
							"default": {
								Quality: "best",
								URLs:    []string{videoURL},
								Format:  pickFormat(videoURL),
								Headers: h,
							},
						},
					})
				}
			} else {
				// File type
				idx := fmt.Sprintf("%d", fileIdx)
				if prefix != "" {
					idx = prefix + "." + idx
				}
				fileName := fmt.Sprintf("(%s)--%s", idx, name)
				fileIdx++

				fileURL := getSchoolFileURL(c, cid, nodeID, h)
				if fileURL != "" {
					ext := resourceExtension(fileURL, node.Name)
					out = append(out, &extractor.MediaInfo{
						Site:  "zhihuishu",
						Title: fileName,
						Streams: map[string]extractor.Stream{
							"default": {
								Quality: "default",
								URLs:    []string{fileURL},
								Format:  ext,
								Headers: h,
							},
						},
					})
				}
			}
			continue
		}

		// Non-leaf node: recurse into children
		if len(node.ChildList) > 0 {
			sub := walkSchoolTree(c, cid, node.ChildList, h, prefix, mode)
			out = append(out, sub...)
		}
	}
	return out
}

// getSchoolVideoURL implements Zhihuishu_School._get_video_url.
// stuViewFile -> dataId -> initVideo + changeVideoLine
func getSchoolVideoURL(c *util.Client, cid, fileID string, h map[string]string, mode zhsMode) string {
	dataID := getSchoolVideoDataID(c, cid, fileID, h)
	if dataID == "" {
		return ""
	}
	url, err := getVideoURL(c, dataID, h, mode)
	if err != nil {
		return ""
	}
	return url
}

// getSchoolVideoDataID implements Zhihuishu_School._get_video_data_id.
func getSchoolVideoDataID(c *util.Client, cid, videoID string, h map[string]string) string {
	body, err := c.GetString(fmt.Sprintf(urlSchoolVideoID, cid, videoID), h)
	if err != nil {
		return ""
	}
	var resp struct {
		Rt struct {
			DataID string `json:"dataId"`
		} `json:"rt"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ""
	}
	return resp.Rt.DataID
}

// getSchoolFileURL implements Zhihuishu_School._get_file_url.
// stuViewFile -> filePath or originalPath, extract WOPISrc if present
func getSchoolFileURL(c *util.Client, cid, fileID string, h map[string]string) string {
	body, err := c.GetString(fmt.Sprintf(urlSchoolFileURL, cid, fileID), h)
	if err != nil {
		return ""
	}
	// Try filePath first
	fileURL := ""
	if m := regexp.MustCompile(`"filePath"\s*:\s*"(https?://.*?)"`).FindStringSubmatch(body); len(m) > 1 {
		rawURL := m[1]
		parts := strings.SplitN(rawURL, "?WOPISrc=", 2)
		if len(parts) == 2 {
			fileURL = parts[1]
		}
	}
	// Fallback: originalPath
	if fileURL == "" {
		if m := regexp.MustCompile(`"originalPath"\s*:\s*"(https?://.*?)"`).FindStringSubmatch(body); len(m) > 1 {
			fileURL = m[1]
		}
	}
	return fileURL
}
