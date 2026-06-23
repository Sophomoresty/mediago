package icourse163

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`icourse163\.org`,
	`study\.163\.com`,
}

func init() {
	extractor.Register(&ICourse163{}, extractor.SiteInfo{
		Name:     "icourse163",
		URL:      "icourse163.org",
		NeedAuth: true,
	})
}

type ICourse163 struct{}

func (i *ICourse163) Patterns() []string { return patterns }

func (i *ICourse163) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("icourse163 requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)

	courseID := extractCourseID(url)
	if courseID == "" {
		return nil, fmt.Errorf("cannot extract course ID from URL: %s", url)
	}

	body, err := client.GetString(url, map[string]string{
		"Referer": "https://www.icourse163.org/",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch course page: %w", err)
	}

	termID := extractTermID(body)

	_ = termID
	_ = courseID

	return nil, fmt.Errorf("icourse163 extraction requires session API (login first, then use --cookies)")
}

var courseIDRe = regexp.MustCompile(`course/([A-Za-z0-9_-]+)`)
var termIDRe = regexp.MustCompile(`termId\s*[:=]\s*["']?(\d+)`)

func extractCourseID(url string) string {
	if m := courseIDRe.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractTermID(html string) string {
	if m := termIDRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	return ""
}

var _ = util.RandomUA
