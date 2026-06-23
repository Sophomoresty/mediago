package cctv

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`tv\.cctv\.com/.+\.shtml`,
	`cctv\.com/.+/VIDE\w+\.shtml`,
	`cctv\.com/.+/index\.shtml`,
}

func init() {
	extractor.Register(&CCTV{}, extractor.SiteInfo{
		Name: "CCTV",
		URL:  "tv.cctv.com",
	})
}

type CCTV struct{}

func (c *CCTV) Patterns() []string { return patterns }

func (c *CCTV) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	client := util.NewClient()

	body, err := client.GetString(url, map[string]string{
		"Referer": "https://www.cctv.cn/",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CCTV page: %w", err)
	}

	guid := extractGUID(body)
	if guid == "" {
		return nil, fmt.Errorf("cannot find video GUID in page")
	}

	title := extractTitle(body)
	if title == "" {
		title = "cctv_video"
	}

	apiURL := fmt.Sprintf("https://vdn.apps.cntv.cn/api/getHttpVideoInfo.do?pid=%s", guid)
	apiBody, err := client.GetString(apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video info: %w", err)
	}

	var info struct {
		Title       string `json:"title"`
		HLSUrl      string `json:"hls_url"`
		VideoUrl    string `json:"video_url"`
		ChaptersUrl string `json:"chapters_url"`
	}
	if err := json.Unmarshal([]byte(apiBody), &info); err != nil {
		return nil, fmt.Errorf("failed to parse CCTV API response: %w", err)
	}

	if info.Title != "" {
		title = info.Title
	}

	streams := make(map[string]extractor.Stream)

	if info.HLSUrl != "" {
		streams["hls"] = extractor.Stream{
			Quality: "default",
			URLs:    []string{info.HLSUrl},
			Format:  "m3u8",
			Headers: map[string]string{
				"Referer": "https://www.cctv.cn/",
			},
		}
	}

	if info.VideoUrl != "" {
		streams["mp4"] = extractor.Stream{
			Quality: "default",
			URLs:    []string{info.VideoUrl},
			Format:  "mp4",
			Headers: map[string]string{
				"Referer": "https://www.cctv.cn/",
			},
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found for CCTV video")
	}

	return &extractor.MediaInfo{
		Site:    "cctv",
		Title:   title,
		Streams: streams,
	}, nil
}

var guidPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bvar\s+guid\s*=\s*["']([0-9a-fA-F]{32})["']`),
	regexp.MustCompile(`\bguid\s*[:=]\s*["']([0-9a-fA-F]{32})["']`),
	regexp.MustCompile(`\bvideoCenterId\s*[:=]\s*["']([0-9a-fA-F]{32})["']`),
	regexp.MustCompile(`\bpid\s*[:=]\s*["']([0-9a-fA-F]{32})["']`),
}

func extractGUID(html string) string {
	for _, re := range guidPatterns {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

var titlePatterns = []*regexp.Regexp{
	regexp.MustCompile(`<meta\s+property=["']og:title["']\s+content=["']([^"']+)["']`),
	regexp.MustCompile(`<title>([^<]+)</title>`),
}

func extractTitle(html string) string {
	for _, re := range titlePatterns {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			title := m[1]
			cleanRe := regexp.MustCompile(`[_-].*?(?:cctv\.com|央视网).*$`)
			title = cleanRe.ReplaceAllString(title, "")
			return title
		}
	}
	return ""
}
