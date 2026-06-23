// Package lizhiweike implements an extractor for lizhiweike.com (荔枝微课) courses.
package lizhiweike

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

// Endpoints from decompiled Mooc/Courses/Lizhiweike/:
const (
	urlCheckToken = "https://open.lizhiweike.com/oauth2/check_token?token={token}"
	urlBuyRecord  = "https://apiv1.lizhiweike.com/api/history/buy_record"
	urlMobile     = "https://m.lizhiweike.com"
)

var patterns = []string{`(?:[\w-]+\.)?lizhiweike\.com/`}

func init() {
	extractor.Register(&Lizhiweike{}, extractor.SiteInfo{Name: "Lizhiweike", URL: "lizhiweike.com", NeedAuth: true})
}

type Lizhiweike struct{}

func (l *Lizhiweike) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:lecture|course|column)/(\w+)|columnId=(\w+)`)

func (l *Lizhiweike) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("lizhiweike requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse lizhiweike id from URL")
	}
	return nil, fmt.Errorf("lizhiweike column playback flow not yet implemented")
}
