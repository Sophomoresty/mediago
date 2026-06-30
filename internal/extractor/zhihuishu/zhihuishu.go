// Package zhihuishu implements an extractor for www.zhihuishu.com courses.
//
// Video URL chain ported from decompiled Mooc/Courses/Zhihuishu/Zhihuishu_Course.pyc:
//  1. /video/initVideo?videoID={vid}             → result.uuid + result.lines[].lineID
//  2. /video/changeVideoLine?videoID=&lineID=&uuid={uuid}
//     → result (string mp4 URL, per-quality)
//     The Python source sorts lineIDs desc and probes top 2 (HD + Sd fallback).
//
// Course traversal follows the source _get_infos courseHome HTML scrape:
// courseHome page -> /home/communication/content/{courseId}/{termId} -> videoID
// list -> initVideo/changeVideoLine. Direct videoID URLs still extract cleanly.
package zhihuishu

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

var patterns = []string{
	`(?:[\w-]+\.)*zhihuishu\.com/`,
}

func init() {
	extractor.Register(&Zhihuishu{}, extractor.SiteInfo{
		Name:     "Zhihuishu",
		URL:      "zhihuishu.com",
		NeedAuth: true,
	})
}

type Zhihuishu struct{}

func (z *Zhihuishu) Patterns() []string { return patterns }

func (z *Zhihuishu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zhihuishu requires login cookies (use --cookies or --cookies-from-browser)")
	}
	mode := zhihuishuMode(opts.Quality)

	// Route to sub-brand handlers in priority order matching the Python source's
	// courses_re matching order: Smart > Live > Interest > School > Course.
	// Each handler returns early if it matches; otherwise we fall through.

	// 1. Smart: ai-smart-course-student-pro / smartcoursestudent / wisdomh5
	if isSmartURL(rawURL) {
		return extractSmart(rawURL, opts, mode)
	}

	// 2. Live: liveId= in URL on zhihuishu.com/live
	if isLiveURL(rawURL) {
		return extractLive(rawURL, opts, mode)
	}

	// 3. Interest: portals_h5/2clearning.html
	if isInterestURL(rawURL) {
		return extractInterest(rawURL, opts, mode)
	}

	// 4. School: wenda / hiexam-server / studyresources hosts
	if isSchoolURL(rawURL) {
		return extractSchool(rawURL, opts, mode)
	}

	// 5. Direct videoID URL (not a course page)
	videoID := extractVideoID(rawURL)
	if videoID != "" {
		if mode.onlyFiles {
			return nil, fmt.Errorf("zhihuishu: only-files mode has no direct-video courseware for videoID %s", videoID)
		}
		c := util.NewClient()
		c.SetCookieJar(opts.Cookies)
		h := zhihuishuHeaders("https://www.zhihuishu.com/")

		url, err := getVideoURL(c, videoID, h, mode)
		if err != nil {
			return nil, err
		}
		subURL, _ := getSubtitleURL(c, videoID, h)

		return &extractor.MediaInfo{
			Site:  "zhihuishu",
			Title: "zhihuishu_" + videoID,
			Streams: map[string]extractor.Stream{
				"best": {
					Quality: "best",
					URLs:    []string{url},
					Format:  pickFormat(url),
					Headers: h,
				},
			},
			Subtitles: subtitleFromURL(subURL),
		}, nil
	}

	// 6. Course (courseHome / recruitAndCourseId / etc.)
	courseID := extractCourseHomeID(rawURL)
	if courseID == "" && extractRecruitAndCourseID(rawURL) == "" {
		return nil, fmt.Errorf("cannot parse zhihuishu URL: %s", rawURL)
	}
	return extractCourseHomeCourse(rawURL, courseID, opts, mode)
}

type zhsMode struct {
	raw       string
	onlyFiles bool
	hd        bool
}

func zhihuishuMode(quality string) zhsMode {
	mode := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(quality)))
	out := zhsMode{raw: mode, hd: true}
	switch mode {
	case "3", "pdf", "onlypdf", "file", "files", "material", "materials", "courseware", "coursewares", "attachment", "attachments", "资料", "课件":
		out.onlyFiles = true
		out.hd = false
	case "2", "sd", "smooth", "ld", "流畅", "标清":
		out.hd = false
	default:
		out.hd = true
	}
	return out
}

// getVideoURL implements the initVideo + changeVideoLine chain. Returns the
// highest-quality mp4 URL or an error.
func getVideoURL(c *util.Client, videoID string, h map[string]string, mode zhsMode) (string, error) {
	initBody, err := c.GetString(
		fmt.Sprintf("https://newbase.zhihuishu.com/video/initVideo?videoID=%s", videoID), h)
	if err != nil {
		return "", fmt.Errorf("initVideo: %w", err)
	}
	var init struct {
		Result struct {
			UUID  string `json:"uuid"`
			Lines []struct {
				LineID int `json:"lineID"`
			} `json:"lines"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(initBody), &init); err != nil {
		return "", fmt.Errorf("parse initVideo: %w", err)
	}
	if init.Result.UUID == "" || len(init.Result.Lines) == 0 {
		return "", fmt.Errorf("initVideo returned empty result.uuid or result.lines")
	}

	ids := make([]int, 0, len(init.Result.Lines))
	for _, l := range init.Result.Lines {
		ids = append(ids, l.LineID)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	if len(ids) > 2 {
		ids = ids[:2]
	}

	urls := make([]string, 0, len(ids))
	for _, lineID := range ids {
		changeBody, err := c.GetString(
			fmt.Sprintf("https://newbase.zhihuishu.com/video/changeVideoLine?videoID=%s&lineID=%d&uuid=%s",
				videoID, lineID, init.Result.UUID), h)
		if err != nil {
			continue
		}
		var ch struct {
			Result string `json:"result"`
		}
		if json.Unmarshal([]byte(changeBody), &ch) != nil || ch.Result == "" {
			continue
		}
		urls = append(urls, ch.Result)
	}
	if len(urls) == 0 {
		return "", fmt.Errorf("changeVideoLine returned no playable URL")
	}
	if mode.hd {
		return urls[0], nil
	}
	return urls[len(urls)-1], nil
}

var (
	videoIDRe      = regexp.MustCompile(`(?i)videoID=([\w-]+)`)
	vidRe2         = regexp.MustCompile(`/video/(?:initVideo\?videoID=)?([\w-]{8,})`)
	courseHomeIDRe = regexp.MustCompile(`(?i)(?:courseHome/|[?&](?:courseId|proCourseId)=)(\d+)`)
)

func extractVideoID(u string) string {
	if m := videoIDRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	if m := vidRe2.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractCourseHomeID(u string) string {
	if m := courseHomeIDRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func pickFormat(u string) string {
	if strings.Contains(u, ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}

func zhihuishuHeaders(referer string) map[string]string {
	return map[string]string{"Referer": referer}
}
