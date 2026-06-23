package sites

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

type genericExtractor struct {
	siteName    string
	domain      string
	pats        []string
	needAuth    bool
	apiTemplate string
}

func newGenericExtractor(name, domain string, patterns []string, needAuth bool, apiTemplate string) *genericExtractor {
	return &genericExtractor{
		siteName:    name,
		domain:      domain,
		pats:        patterns,
		needAuth:    needAuth,
		apiTemplate: apiTemplate,
	}
}

func (g *genericExtractor) Patterns() []string { return g.pats }

func (g *genericExtractor) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if g.needAuth && (opts == nil || opts.Cookies == nil) {
		return nil, fmt.Errorf("%s requires login cookies (use --cookies or --cookies-from-browser)", g.siteName)
	}

	client := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		client.SetCookieJar(opts.Cookies)
	}

	body, err := client.GetString(url, map[string]string{
		"Referer": fmt.Sprintf("https://%s/", g.domain),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page: %w", err)
	}

	title := extractPageTitle(body)
	if title == "" {
		title = fmt.Sprintf("%s_video", g.siteName)
	}

	videoURL := extractVideoURL(body)
	if videoURL == "" {
		if g.needAuth {
			return nil, fmt.Errorf("no video URL found (login may be required or expired)")
		}
		return nil, fmt.Errorf("no video URL found on page")
	}

	format := detectFormat(videoURL)
	return &extractor.MediaInfo{
		Site:  g.siteName,
		Title: title,
		Streams: map[string]extractor.Stream{
			"default": {
				Quality: "default",
				URLs:    []string{videoURL},
				Format:  format,
				Headers: map[string]string{"Referer": fmt.Sprintf("https://%s/", g.domain)},
			},
		},
	}, nil
}

var titleRe = regexp.MustCompile(`<title>([^<]+)</title>`)
var videoURLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`["']((https?://[^"']+\.m3u8[^"']*))['"]\s*`),
	regexp.MustCompile(`["']((https?://[^"']+\.mp4[^"']*))['"]\s*`),
	regexp.MustCompile(`(?:videoUrl|playUrl|video_url|play_url|source)\s*[:=]\s*["']((https?://[^"']+))["']`),
}

func extractPageTitle(html string) string {
	if m := titleRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractVideoURL(html string) string {
	for _, re := range videoURLPatterns {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func detectFormat(url string) string {
	if matched, _ := regexp.MatchString(`\.m3u8`, url); matched {
		return "m3u8"
	}
	if matched, _ := regexp.MatchString(`\.flv`, url); matched {
		return "flv"
	}
	return "mp4"
}
