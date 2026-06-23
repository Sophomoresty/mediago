// Package haiyangknow implements an extractor for haiyangknow.com courses.
//
// API endpoints from decompiled Mooc/Courses/Haiyangknow/:
//
//	https://user.haiyangknow.com
//	https://user.haiyangknow.com/
//	https://user.haiyangknow.com/prod-api
//	https://vod.{}.aliyuncs.com/?{}
package haiyangknow

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://user.haiyangknow.com"
	url1 = "https://user.haiyangknow.com/"
	url2 = "https://user.haiyangknow.com/prod-api"
	url3 = "https://vod.{}.aliyuncs.com/?{}"
)

var patterns = []string{`(?:[\w-]+\.)?haiyangknow\.com/`}

func init() {
	extractor.Register(&Haiyangknow{}, extractor.SiteInfo{Name: "Haiyangknow", URL: "haiyangknow.com", NeedAuth: true})
}

type Haiyangknow struct{}

func (s *Haiyangknow) Patterns() []string { return patterns }

func (s *Haiyangknow) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("haiyangknow requires login cookies")
	}
	return nil, fmt.Errorf("haiyangknow chain not yet implemented; 4 source URL(s) recorded")
}
