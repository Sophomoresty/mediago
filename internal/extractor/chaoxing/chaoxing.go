package chaoxing

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
	"github.com/tidwall/gjson"
)

var patterns = []string{
	`chaoxing\.com`,
	`xueyinonline\.com`,
}

func init() {
	extractor.Register(&Chaoxing{}, extractor.SiteInfo{
		Name:     "Chaoxing",
		URL:      "chaoxing.com",
		NeedAuth: true,
	})
}

type Chaoxing struct{}

func (c *Chaoxing) Patterns() []string { return patterns }

func (c *Chaoxing) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("chaoxing requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)

	objectID := extractObjectID(url)
	if objectID == "" {
		body, err := client.GetString(url, map[string]string{
			"Referer": "https://mooc1.chaoxing.com/",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page: %w", err)
		}
		objectID = extractObjectIDFromPage(body)
	}

	if objectID == "" {
		return nil, fmt.Errorf("cannot extract video object ID from URL")
	}

	apiURL := fmt.Sprintf("https://mooc1.chaoxing.com/ananas/status/%s", objectID)
	body, err := client.GetString(apiURL, map[string]string{
		"Referer": "https://mooc1.chaoxing.com/",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video status: %w", err)
	}

	title := gjson.Get(body, "filename").String()
	if title == "" {
		title = "chaoxing_video"
	}

	streams := make(map[string]extractor.Stream)
	if mp4 := gjson.Get(body, "http").String(); mp4 != "" {
		streams["mp4"] = extractor.Stream{
			Quality: "default",
			URLs:    []string{mp4},
			Format:  "mp4",
		}
	}
	if hls := gjson.Get(body, "hls").String(); hls != "" {
		streams["hls"] = extractor.Stream{
			Quality: "default",
			URLs:    []string{hls},
			Format:  "m3u8",
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found (video may be restricted)")
	}

	return &extractor.MediaInfo{
		Site:    "chaoxing",
		Title:   title,
		Streams: streams,
	}, nil
}

var objectIDRe = regexp.MustCompile(`objectId=([a-f0-9]+)`)
var objectIDPageRe = regexp.MustCompile(`objectid\s*[:=]\s*["']([a-f0-9]+)["']`)

func extractObjectID(url string) string {
	if m := objectIDRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractObjectIDFromPage(html string) string {
	if m := objectIDPageRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	return ""
}
