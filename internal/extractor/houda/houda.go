// Package houda implements an extractor for csslcloud.net courses.
//
// API endpoints from decompiled Mooc/Courses/Houda/:
//
//	https://view.csslcloud.net/replay/data/meta
//	https://view.csslcloud.net/replay/user/login
//	https://view.csslcloud.net/replay/video/play
package houda

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://view.csslcloud.net/replay/data/meta"
	url1 = "https://view.csslcloud.net/replay/user/login"
	url2 = "https://view.csslcloud.net/replay/video/play"
)

var patterns = []string{`(?:[\w-]+\.)?csslcloud\.net/`}

func init() {
	extractor.Register(&Houda{}, extractor.SiteInfo{Name: "Houda", URL: "csslcloud.net", NeedAuth: true})
}

type Houda struct{}

func (s *Houda) Patterns() []string { return patterns }

func (s *Houda) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("houda requires login cookies")
	}
	return nil, fmt.Errorf("houda chain not yet implemented; 3 source URL(s) recorded")
}
