// interest.go implements the Zhihuishu_Interest sub-brand extractor.
//
// Source: decompiled Mooc/Courses/Zhihuishu/Zhihuishu_Interest.pyc
//
// URL pattern from Mooc_Config courses_re:
//
//	Zhihuishu_Interest: https?://.*?zhihuishu\.com/portals_h5/2clearning\.html.*?/(?P<cid>\d+)
//
// Endpoints from Zhihuishu_Interest class attributes:
//
//	url_course    = "https://www.zhihuishu.com/portals_h5/2clearning.html#/courseInfo/{}"
//	url_detail    = "https://b2cpush.zhihuishu.com/b2cpush/courseDetail/query2CCourseInfo?courseId={}"
//	url_info      = "https://b2cpush.zhihuishu.com/b2cpush/courseDetail/query2CCourseCatalog"
//	url_purchased = "https://b2cpush.zhihuishu.com/b2cpush/courseDetail/userIfBuyCourseForH5"
//	url_video     = "https://newbase.zhihuishu.com/video/initVideoToC?videoID={}"
//
// Note: Interest uses initVideoToC (not initVideo) which returns lineUrl
// directly in lines[] sorted by lineID.
package zhihuishu

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlInterestDetail = "https://b2cpush.zhihuishu.com/b2cpush/courseDetail/query2CCourseInfo?courseId=%s"
	urlInterestInfo   = "https://b2cpush.zhihuishu.com/b2cpush/courseDetail/query2CCourseCatalog"
	urlInterestVideo  = "https://newbase.zhihuishu.com/video/initVideoToC?videoID=%s"
)

var interestPathRe = regexp.MustCompile(`(?i)portals_h5/2clearning\.html`)

func isInterestURL(u string) bool {
	return interestPathRe.MatchString(u)
}

func extractInterestCID(u string) string {
	// Extract courseId from path fragment like #/courseInfo/2028566
	m := regexp.MustCompile(`/(\d+)\s*$`).FindStringSubmatch(u)
	if len(m) > 1 {
		return m[1]
	}
	// Also try query param
	m = regexp.MustCompile(`(?i)courseId=(\d+)`).FindStringSubmatch(u)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractInterest implements the Zhihuishu_Interest flow.
func extractInterest(rawURL string, opts *extractor.ExtractOpts, mode zhsMode) (*extractor.MediaInfo, error) {
	cid := extractInterestCID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse zhihuishu interest URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := zhihuishuHeaders("https://www.zhihuishu.com")

	// _get_title: query2CCourseInfo -> rt.courseName, rt.schoolName
	title := "zhihuishu_interest_" + cid
	body, err := c.GetString(fmt.Sprintf(urlInterestDetail, cid), h)
	if err == nil {
		var resp struct {
			Rt struct {
				CourseName string `json:"courseName"`
				SchoolName string `json:"schoolName"`
			} `json:"rt"`
		}
		if json.Unmarshal([]byte(body), &resp) == nil && resp.Rt.CourseName != "" {
			if resp.Rt.SchoolName != "" {
				title = sanitize(resp.Rt.CourseName + "_" + resp.Rt.SchoolName)
			} else {
				title = sanitize(resp.Rt.CourseName)
			}
		}
	}

	// _get_infos: query2CCourseCatalog -> rt[].chapterName, lesssonList[].lessonName/lessonVideoId
	body, err = c.PostForm(urlInterestInfo, map[string]string{"courseId": cid}, h)
	if err != nil {
		return nil, fmt.Errorf("query2CCourseCatalog: %w", err)
	}
	var catalogResp struct {
		Rt []struct {
			ChapterName string `json:"chapterName"`
			LessonList  []struct {
				LessonName    string `json:"lessonName"`
				LessonVideoID string `json:"lessonVideoId"`
			} `json:"lesssonList"` // Note: source has triple 's' in "lesssonList"
		} `json:"rt"`
	}
	if err := json.Unmarshal([]byte(body), &catalogResp); err != nil {
		return nil, fmt.Errorf("parse interest catalog: %w", err)
	}

	var entries []*extractor.MediaInfo
	if !mode.onlyFiles {
		for ci, chapter := range catalogResp.Rt {
			for li, lesson := range chapter.LessonList {
				if lesson.LessonName == "" || lesson.LessonVideoID == "" {
					continue
				}
				videoName := fmt.Sprintf("[%d.%d]--%s", ci+1, li+1, sanitize(lesson.LessonName))
				videoURL := getInterestVideoURL(c, lesson.LessonVideoID, h, mode)
				if videoURL == "" {
					continue
				}
				entries = append(entries, &extractor.MediaInfo{
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
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("zhihuishu interest %s returned no playable videos", cid)
	}

	return &extractor.MediaInfo{
		Site:    "zhihuishu",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"course_id":          cid,
			"discovered_entries": len(entries),
			"sub_brand":          "interest",
		},
	}, nil
}

// getInterestVideoURL implements Zhihuishu_Interest._get_video_url.
// Uses initVideoToC which returns lineUrl directly in lines[].
func getInterestVideoURL(c *util.Client, videoID string, h map[string]string, mode zhsMode) string {
	body, err := c.GetString(fmt.Sprintf(urlInterestVideo, videoID), h)
	if err != nil {
		return ""
	}
	var resp struct {
		Result struct {
			Lines []struct {
				LineID  int    `json:"lineID"`
				LineURL string `json:"lineUrl"`
			} `json:"lines"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ""
	}
	lines := resp.Result.Lines
	// Sort by lineID ascending (source: sorted by lambda line: line.get('lineID'))
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].LineID < lines[j].LineID
	})
	// Collect non-empty lineUrls
	var urls []string
	for _, l := range lines {
		if l.LineURL != "" {
			urls = append(urls, l.LineURL)
		}
	}
	if len(urls) == 0 {
		return ""
	}
	if mode.hd {
		return urls[0]
	}
	return urls[len(urls)-1]
}
