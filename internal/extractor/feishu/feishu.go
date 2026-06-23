package feishu

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
	"github.com/tidwall/gjson"
)

var patterns = []string{
	`feishu\.cn`,
	`meetings\.feishu\.cn`,
	`lark\.com`,
}

func init() {
	extractor.Register(&Feishu{}, extractor.SiteInfo{
		Name:     "Feishu",
		URL:      "feishu.cn",
		NeedAuth: true,
	})
}

type Feishu struct{}

func (f *Feishu) Patterns() []string { return patterns }

func (f *Feishu) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("feishu requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)

	videoID := extractVideoID(url)
	if videoID == "" {
		return nil, fmt.Errorf("cannot extract video/meeting ID from URL")
	}

	apiURL := fmt.Sprintf("https://meetings.feishu.cn/minutes/api/space/target/video_info?target_token=%s", videoID)
	body, err := client.GetString(apiURL, map[string]string{
		"Referer": "https://meetings.feishu.cn/",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video info: %w", err)
	}

	title := gjson.Get(body, "data.title").String()
	if title == "" {
		title = fmt.Sprintf("feishu_%s", videoID)
	}

	videoURL := gjson.Get(body, "data.video_url").String()
	streams := make(map[string]extractor.Stream)
	if videoURL != "" {
		streams["default"] = extractor.Stream{
			Quality: "default",
			URLs:    []string{videoURL},
			Format:  "mp4",
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no video URL found")
	}

	return &extractor.MediaInfo{
		Site:    "feishu",
		Title:   title,
		Streams: streams,
	}, nil
}

var feishuIDRe = regexp.MustCompile(`(?:minutes|recording)/([a-zA-Z0-9_-]+)`)

func extractVideoID(url string) string {
	if m := feishuIDRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	return ""
}

var _ = util.RandomUA
