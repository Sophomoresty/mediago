package xuetang

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
	"github.com/tidwall/gjson"
)

var patterns = []string{
	`xuetangx\.com`,
	`next\.xuetangx\.com`,
}

func init() {
	extractor.Register(&Xuetang{}, extractor.SiteInfo{
		Name:     "Xuetang",
		URL:      "xuetangx.com",
		NeedAuth: true,
	})
}

type Xuetang struct{}

func (x *Xuetang) Patterns() []string { return patterns }

func (x *Xuetang) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xuetang requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)

	courseID := extractID(url)
	if courseID == "" {
		return nil, fmt.Errorf("cannot extract course ID from URL")
	}

	apiURL := fmt.Sprintf("https://next.xuetangx.com/api/v1/lms/learn/leaf/detail/%s/?sign=", courseID)
	body, err := client.GetString(apiURL, map[string]string{
		"Referer":  "https://next.xuetangx.com/",
		"X-Client": "web",
		"xtbz":     "cloud",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video info: %w", err)
	}

	title := gjson.Get(body, "data.content_info.name").String()
	if title == "" {
		title = "xuetang_video"
	}
	videoURL := gjson.Get(body, "data.content_info.media.ccurl").String()

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
		Site:    "xuetang",
		Title:   title,
		Streams: streams,
	}, nil
}

var idRe = regexp.MustCompile(`/(\d+)`)

func extractID(url string) string {
	if m := idRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	return ""
}

var _ = util.RandomUA
