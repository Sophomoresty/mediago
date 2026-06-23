package imooc

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`imooc\.com`,
	`coding\.imooc\.com`,
}

func init() {
	extractor.Register(&Imooc{}, extractor.SiteInfo{
		Name:     "imooc",
		URL:      "imooc.com",
		NeedAuth: true,
	})
}

type Imooc struct{}

func (i *Imooc) Patterns() []string { return patterns }

func (i *Imooc) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("imooc requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)

	courseID, videoID := extractIDs(url)
	if courseID == "" {
		return nil, fmt.Errorf("cannot extract course ID from URL")
	}

	var m3u8URL string
	if videoID != "" {
		m3u8URL = fmt.Sprintf("https://coding.imooc.com/lesson/m3u8h5?mid=%s&cid=%s&ssl=1&cdn=aliyun1", videoID, courseID)
	} else {
		m3u8URL = fmt.Sprintf("https://www.imooc.com/course/playlist/%s?t=m3u8&cdn=aliyun1", courseID)
	}

	body, err := client.GetString(m3u8URL, map[string]string{
		"Referer": "https://coding.imooc.com/",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	if len(body) < 10 {
		return nil, fmt.Errorf("empty m3u8 response (may need auth)")
	}

	return &extractor.MediaInfo{
		Site:  "imooc",
		Title: fmt.Sprintf("imooc_%s", courseID),
		Streams: map[string]extractor.Stream{
			"default": {
				Quality: "default",
				URLs:    []string{m3u8URL},
				Format:  "m3u8",
				Headers: map[string]string{"Referer": "https://coding.imooc.com/"},
			},
		},
	}, nil
}

var courseRe = regexp.MustCompile(`(?:class|learn|course)/(\d+)`)
var midRe = regexp.MustCompile(`(?:video|mid=)(\d+)`)

func extractIDs(url string) (string, string) {
	courseID := ""
	videoID := ""
	if m := courseRe.FindStringSubmatch(url); len(m) > 1 {
		courseID = m[1]
	}
	if m := midRe.FindStringSubmatch(url); len(m) > 1 {
		videoID = m[1]
	}
	return courseID, videoID
}

var _ = util.RandomUA
