// Package houdu implements an extractor for houduweilai.com courses.
//
// API endpoints from decompiled Mooc/Courses/Houdu/:
//
//	https://api.houduweilai.com/mini/student/othersStudents
//	https://api.houduweilai.com{}
package houdu

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api.houduweilai.com/mini/student/othersStudents"
	url1 = "https://api.houduweilai.com{}"
)

var patterns = []string{`(?:[\w-]+\.)?houduweilai\.com/`}

func init() {
	extractor.Register(&Houdu{}, extractor.SiteInfo{Name: "Houdu", URL: "houduweilai.com", NeedAuth: true})
}

type Houdu struct{}

func (s *Houdu) Patterns() []string { return patterns }

func (s *Houdu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("houdu requires login cookies")
	}
	return nil, fmt.Errorf("houdu chain not yet implemented; 2 source URL(s) recorded")
}
