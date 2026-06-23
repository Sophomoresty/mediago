package dingtalk

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
	"github.com/tidwall/gjson"
)

var patterns = []string{
	`dingtalk\.com`,
	`ding\.yunzhan365\.com`,
}

func init() {
	extractor.Register(&DingTalk{}, extractor.SiteInfo{
		Name:     "DingTalk",
		URL:      "dingtalk.com",
		NeedAuth: true,
	})
}

type DingTalk struct{}

func (d *DingTalk) Patterns() []string { return patterns }

func (d *DingTalk) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("dingtalk requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)

	liveID := extractLiveID(url)
	if liveID == "" {
		return nil, fmt.Errorf("cannot extract live/video ID from URL")
	}

	apiURL := fmt.Sprintf("https://h5.m.taobao.com/alicare/dingtalk-live.html?liveId=%s", liveID)
	body, err := client.GetString(apiURL, map[string]string{
		"Referer": "https://www.dingtalk.com/",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch live info: %w", err)
	}

	title := gjson.Get(body, "data.title").String()
	if title == "" {
		title = fmt.Sprintf("dingtalk_%s", liveID)
	}

	videoURL := gjson.Get(body, "data.playUrl").String()
	streams := make(map[string]extractor.Stream)
	if videoURL != "" {
		format := "mp4"
		if len(videoURL) > 4 && videoURL[len(videoURL)-5:] == ".m3u8" {
			format = "m3u8"
		}
		streams["default"] = extractor.Stream{
			Quality: "default",
			URLs:    []string{videoURL},
			Format:  format,
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no playable stream found")
	}

	return &extractor.MediaInfo{
		Site:    "dingtalk",
		Title:   title,
		Streams: streams,
	}, nil
}

var liveIDRe = regexp.MustCompile(`(?:liveId|roomId|live_id)=(\w+)`)
var pathIDRe = regexp.MustCompile(`/(\d{10,})`)

func extractLiveID(url string) string {
	if m := liveIDRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	if m := pathIDRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	return ""
}

var _ = util.RandomUA
