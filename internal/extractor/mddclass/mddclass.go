// Package mddclass implements an extractor for mddclass.com.
//
// API endpoints from decompiled Mooc/Courses/Mddclass/:
//
//	https://pass-api.sksight.com
//	https://lexue.mddclass.com
//	https://access.mddclass.com
package mddclass

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://pass-api.sksight.com"
	url1 = "https://lexue.mddclass.com"
	url2 = "https://access.mddclass.com"
)

var patterns = []string{`(?:[\w-]+\.)?mddclass\.com/`}

func init() {
	extractor.Register(&Mddclass{}, extractor.SiteInfo{Name: "Mddclass", URL: "mddclass.com", NeedAuth: true})
}

type Mddclass struct{}

func (s *Mddclass) Patterns() []string { return patterns }

func (s *Mddclass) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("mddclass requires login cookies")
	}
	return nil, fmt.Errorf("mddclass chain not yet implemented; source URL constants recorded")
}
