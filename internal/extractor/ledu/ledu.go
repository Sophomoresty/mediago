// Package ledu implements an extractor for ledupeiyou.com courses.
//
// API endpoints from decompiled Mooc/Courses/Ledu/:
//
//	https://classroom-api-online.saasp.vdyoo.com
//	https://classroom-api.ledupeiyou.com
//	https://course-api-online.saasp.vdyoo.com
package ledu

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://classroom-api-online.saasp.vdyoo.com"
	url1 = "https://classroom-api.ledupeiyou.com"
	url2 = "https://course-api-online.saasp.vdyoo.com"
)

var patterns = []string{`(?:[\w-]+\.)?ledupeiyou\.com/`}

func init() {
	extractor.Register(&Ledu{}, extractor.SiteInfo{Name: "Ledu", URL: "ledupeiyou.com", NeedAuth: true})
}

type Ledu struct{}

func (s *Ledu) Patterns() []string { return patterns }

func (s *Ledu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("ledu requires login cookies")
	}
	return nil, fmt.Errorf("ledu chain not yet implemented; 3 source URL(s) recorded")
}
