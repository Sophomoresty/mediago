// Package wendao implements an extractor for wendao101.com courses.
//
// API endpoints from decompiled Mooc/Courses/Wendao/:
//
//	https://pc.wendao101.com/prod-api
//	https://wap.wendao101.com/#/pages_mine/myCourse/myCourse
package wendao

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://pc.wendao101.com/prod-api"
	url1 = "https://wap.wendao101.com/#/pages_mine/myCourse/myCourse"
)

var patterns = []string{`(?:[\w-]+\.)?wendao101\.com/`}

func init() {
	extractor.Register(&Wendao{}, extractor.SiteInfo{Name: "Wendao", URL: "wendao101.com", NeedAuth: true})
}

type Wendao struct{}

func (s *Wendao) Patterns() []string { return patterns }

func (s *Wendao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wendao requires login cookies")
	}
	return nil, fmt.Errorf("wendao chain not yet implemented; 2 source URL(s) recorded")
}
