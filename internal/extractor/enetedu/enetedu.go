// Package enetedu implements an extractor for enetedu.com courses.
//
// API endpoints from decompiled Mooc/Courses/Enetedu/:
//
//	https://www.enetedu.com/site/course/liveCourseDetails?id=2033384670799990785
package enetedu

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://www.enetedu.com/site/course/liveCourseDetails?id=2033384670799990785"
)

var patterns = []string{`(?:[\w-]+\.)?enetedu\.com/`}

func init() {
	extractor.Register(&Enetedu{}, extractor.SiteInfo{Name: "Enetedu", URL: "enetedu.com", NeedAuth: true})
}

type Enetedu struct{}

func (s *Enetedu) Patterns() []string { return patterns }

func (s *Enetedu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("enetedu requires login cookies")
	}
	return nil, fmt.Errorf("enetedu chain not yet implemented; 1 source URL(s) recorded")
}
