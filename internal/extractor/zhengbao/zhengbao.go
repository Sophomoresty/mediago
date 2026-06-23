// Package zhengbao implements an extractor for chinaacc.com courses.
//
// API endpoints from decompiled Mooc/Courses/Zhengbao/:
//
//	https://elearning.chinaacc.com/xcware/myhome/teachingMaterials.shtm?cwareIDs={cware_id}&identity={identity}
package zhengbao

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://elearning.chinaacc.com/xcware/myhome/teachingMaterials.shtm?cwareIDs={cware_id}&identity={identity}"
)

var patterns = []string{`(?:[\w-]+\.)?chinaacc\.com/`}

func init() {
	extractor.Register(&Zhengbao{}, extractor.SiteInfo{Name: "Zhengbao", URL: "chinaacc.com", NeedAuth: true})
}

type Zhengbao struct{}

func (s *Zhengbao) Patterns() []string { return patterns }

func (s *Zhengbao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zhengbao requires login cookies")
	}
	return nil, fmt.Errorf("zhengbao chain not yet implemented; 1 source URL(s) recorded")
}
