// Package xiwang implements an extractor for xiwang.com courses.
//
// API endpoints from decompiled Mooc/Courses/Xiwang/:
//
//	https://api.xue.wen-su.com/login/V1/Web/checkLogin?X-Businessline-Id=30
//	https://api.xue.wen-su.com/mall/detail/1/{cid}
//	https://api.xue.xi-xue.com/login/V1/Web/checkLogin?X-Businessline-Id=40
//	https://api.xue.xiwang.com/login/V1/Web/checkLogin?X-Businessline-Id=30
//	https://api.xue.xiwang.com/mall/detail/1/{cid}
//	https://gslbsaturnbcc.saasw.vdyoo.com/v1/player/vodshow?appid={app_id}&fid={fid}&bid={bid}
//	https://i.bcc.wen-su.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList
//	https://i.bcc.wen-su.com/icenter-go/App/StudyCenter/MyPlans/planListV2
package xiwang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api.xue.wen-su.com/login/V1/Web/checkLogin?X-Businessline-Id=30"
	url1 = "https://api.xue.wen-su.com/mall/detail/1/{cid}"
	url2 = "https://api.xue.xi-xue.com/login/V1/Web/checkLogin?X-Businessline-Id=40"
	url3 = "https://api.xue.xiwang.com/login/V1/Web/checkLogin?X-Businessline-Id=30"
	url4 = "https://api.xue.xiwang.com/mall/detail/1/{cid}"
	url5 = "https://gslbsaturnbcc.saasw.vdyoo.com/v1/player/vodshow?appid={app_id}&fid={fid}&bid={bid}"
	url6 = "https://i.bcc.wen-su.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList"
	url7 = "https://i.bcc.wen-su.com/icenter-go/App/StudyCenter/MyPlans/planListV2"
)

var patterns = []string{`(?:[\w-]+\.)?xiwang\.com/`}

func init() {
	extractor.Register(&Xiwang{}, extractor.SiteInfo{Name: "Xiwang", URL: "xiwang.com", NeedAuth: true})
}

type Xiwang struct{}

func (s *Xiwang) Patterns() []string { return patterns }

func (s *Xiwang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xiwang requires login cookies")
	}
	return nil, fmt.Errorf("xiwang chain not yet implemented; 8 source URL(s) recorded")
}
