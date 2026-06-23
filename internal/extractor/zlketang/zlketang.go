// Package zlketang implements an extractor for zlketang.com courses.
//
// API endpoints from decompiled Mooc/Courses/Zlketang/:
//
//	https://playvideo.qcloud.com/getplayinfo/v4/{}/{}
//	https://www.zlketang.com/public/wxpub/page/zl_course/commodity.html?product_id={}
//	https://www.zlketang.com/wxpub/api/course
//	https://www.zlketang.com/wxpub/api/course_detail
//	https://www.zlketang.com/wxpub/api/course_video_switchv2
//	https://www.zlketang.com/wxpub/api/goods_detailv3
//	https://www.zlketang.com/wxpub/api/live_detail
//	https://www.zlketang.com/wxpub/api/orderv2
package zlketang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://playvideo.qcloud.com/getplayinfo/v4/{}/{}"
	url1 = "https://www.zlketang.com/public/wxpub/page/zl_course/commodity.html?product_id={}"
	url2 = "https://www.zlketang.com/wxpub/api/course"
	url3 = "https://www.zlketang.com/wxpub/api/course_detail"
	url4 = "https://www.zlketang.com/wxpub/api/course_video_switchv2"
	url5 = "https://www.zlketang.com/wxpub/api/goods_detailv3"
	url6 = "https://www.zlketang.com/wxpub/api/live_detail"
	url7 = "https://www.zlketang.com/wxpub/api/orderv2"
)

var patterns = []string{`(?:[\w-]+\.)?zlketang\.com/`}

func init() {
	extractor.Register(&Zlketang{}, extractor.SiteInfo{Name: "Zlketang", URL: "zlketang.com", NeedAuth: true})
}

type Zlketang struct{}

func (s *Zlketang) Patterns() []string { return patterns }

func (s *Zlketang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zlketang requires login cookies")
	}
	return nil, fmt.Errorf("zlketang chain not yet implemented; 8 source URL(s) recorded")
}
