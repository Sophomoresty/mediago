// Package lexueyun implements an extractor for lexue-cloud.com (乐学云) courses.
//
// Endpoints from decompiled Mooc/Courses/Lexueyun/:
//   https://my.lexue-cloud.com
//   https://video.sunlands.com/video       (video CDN)
package lexueyun

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMy       = "https://my.lexue-cloud.com"
	urlSunlands = "https://video.sunlands.com/video"
)

var patterns = []string{`(?:[\w-]+\.)?lexue-cloud\.com/`}

func init() {
	extractor.Register(&Lexueyun{}, extractor.SiteInfo{Name: "Lexueyun", URL: "lexue-cloud.com", NeedAuth: true})
}

type Lexueyun struct{}

func (l *Lexueyun) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:course|class|video)/(\w+)|courseId=(\w+)`)

func (l *Lexueyun) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("lexueyun requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse lexueyun id from URL")
	}
	return nil, fmt.Errorf("lexueyun → sunlands video chain not yet implemented")
}
