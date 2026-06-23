// Package wowtiku implements an extractor for wowtiku.com.
//
// API endpoints from decompiled Mooc/Courses/Wowtiku/:
//
//	https://www.wowtiku.com/
//	https://www.wowtiku.com
//	https://new.wowtiku.net
package wowtiku

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://www.wowtiku.com/"
	url1 = "https://www.wowtiku.com"
	url2 = "https://new.wowtiku.net"
)

var patterns = []string{`(?:[\w-]+\.)?wowtiku\.com/`}

func init() {
	extractor.Register(&Wowtiku{}, extractor.SiteInfo{Name: "Wowtiku", URL: "wowtiku.com", NeedAuth: true})
}

type Wowtiku struct{}

func (s *Wowtiku) Patterns() []string { return patterns }

func (s *Wowtiku) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wowtiku requires login cookies")
	}
	return nil, fmt.Errorf("wowtiku chain not yet implemented; source URL constants recorded")
}
