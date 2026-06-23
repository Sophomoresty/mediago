package zhihuishu

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
	"github.com/tidwall/gjson"
)

var patterns = []string{
	`zhihuishu\.com`,
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

func (z *Zhihuishu) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zhihuishu requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)

	videoID := extractVideoID(url)
	if videoID == "" {
		return nil, fmt.Errorf("cannot extract video ID from URL")
	}

	apiURL := fmt.Sprintf("https://newbase.zhihuishu.com/video/initVideo?videoID=%s", videoID)
	body, err := client.GetString(apiURL, map[string]string{
		"Referer": "https://studyh5.zhihuishu.com/",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video info: %w", err)
	}

	title := gjson.Get(body, "data.videoName").String()
	if title == "" {
		title = "zhihuishu_video"
	}

	streams := make(map[string]extractor.Stream)
	lines := gjson.Get(body, "data.lines").Array()
	for _, line := range lines {
		lineURL := line.Get("lineUrl").String()
		lineName := line.Get("lineName").String()
		if lineURL != "" {
			streams[lineName] = extractor.Stream{
				Quality: "default",
				URLs:    []string{lineURL},
				Format:  "mp4",
			}
			break
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no video streams found")
	}

	return &extractor.MediaInfo{
		Site:    "zhihuishu",
		Title:   title,
		Streams: streams,
	}, nil
}

var videoIDRe = regexp.MustCompile(`videoId=(\w+)`)
var courseIDRe = regexp.MustCompile(`recruitAndCourseId=(\d+)`)

func extractVideoID(url string) string {
	if m := videoIDRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	if m := courseIDRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	return ""
}

var _ = util.RandomUA
