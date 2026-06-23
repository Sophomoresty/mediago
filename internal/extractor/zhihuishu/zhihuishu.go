// Package zhihuishu implements an extractor for www.zhihuishu.com courses.
//
// Video URL chain ported from decompiled Mooc/Courses/Zhihuishu/Zhihuishu_Course.pyc:
//  1. /video/initVideo?videoID={vid}             → result.uuid + result.lines[].lineID
//  2. /video/changeVideoLine?videoID=&lineID=&uuid={uuid}
//     → result (string mp4 URL, per-quality)
//     The Python source sorts lineIDs desc and probes top 2 (HD + Sd fallback).
//
// Course traversal from courseHome HTML uses BeautifulSoup-style scraping that
// returns blocked-flagged when only courseHome URL is given. Direct videoID URLs
// extract cleanly.
package zhihuishu

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
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

	videoID := extractVideoID(rawURL)
	if videoID == "" {
		return nil, fmt.Errorf("zhihuishu course-tree traversal needs HTML scraping that's not implemented (please pass a videoID URL)")
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": "https://onlineweb.zhihuishu.com/"}

	url, err := getVideoURL(c, videoID, h)
	if err != nil {
		return nil, err
	}

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
	}, nil
}

// getVideoURL implements the initVideo + changeVideoLine chain. Returns the
// highest-quality mp4 URL or an error.
func getVideoURL(c *util.Client, videoID string, h map[string]string) (string, error) {
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
		return ch.Result, nil
	}
	return "", fmt.Errorf("changeVideoLine returned no playable URL")
}

var (
	videoIDRe = regexp.MustCompile(`(?i)videoID=([\w-]+)`)
	vidRe2    = regexp.MustCompile(`/video/(?:initVideo\?videoID=)?([\w-]{8,})`)
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

func pickFormat(u string) string {
	if strings.Contains(u, ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
