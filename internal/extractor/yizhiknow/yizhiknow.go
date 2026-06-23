// Package yizhiknow implements an extractor for yizhiknow.com courses.
//
// API endpoints from decompiled Mooc/Courses/Yizhiknow/:
//
//	https://curriculum-api.yizhiknow.com
//	https://user.yizhiknow.com
package yizhiknow

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://curriculum-api.yizhiknow.com"
	url1 = "https://user.yizhiknow.com"
)

var patterns = []string{`(?:[\w-]+\.)?yizhiknow\.com/`}

func init() {
	extractor.Register(&Yizhiknow{}, extractor.SiteInfo{Name: "Yizhiknow", URL: "yizhiknow.com", NeedAuth: true})
}

type Yizhiknow struct{}

func (s *Yizhiknow) Patterns() []string { return patterns }

func (s *Yizhiknow) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("yizhiknow requires login cookies")
	}
	return nil, fmt.Errorf("yizhiknow chain not yet implemented; 2 source URL(s) recorded")
}
