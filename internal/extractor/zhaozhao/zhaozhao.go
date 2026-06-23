// Package zhaozhao implements an extractor for yikao88.com courses.
//
// API endpoints from decompiled Mooc/Courses/Zhaozhao/:
//
//	https://api.yikao88.com/api-order/order/pc/v5/myBuyProductList
//	https://api.yikao88.com/api-play/play-safe/token
//	https://api.yikao88.com/api-shop/course/pc/v5/getPackagelistByProduct
//	https://api.yikao88.com/api-shop/course/pc/v5/getPlaySafe
//	https://api.yikao88.com/api-shop/course/pc/v5/getPlayToken
//	https://api.yikao88.com/api-shop/course/pc/v5/getPlayTokenByVideoId
//	https://api.yikao88.com/api-shop/course/pc/v5/getPolyvPlaySafe
//	https://api.yikao88.com/api-shop/course/pc/v5/getVideoPlayToken
package zhaozhao

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api.yikao88.com/api-order/order/pc/v5/myBuyProductList"
	url1 = "https://api.yikao88.com/api-play/play-safe/token"
	url2 = "https://api.yikao88.com/api-shop/course/pc/v5/getPackagelistByProduct"
	url3 = "https://api.yikao88.com/api-shop/course/pc/v5/getPlaySafe"
	url4 = "https://api.yikao88.com/api-shop/course/pc/v5/getPlayToken"
	url5 = "https://api.yikao88.com/api-shop/course/pc/v5/getPlayTokenByVideoId"
	url6 = "https://api.yikao88.com/api-shop/course/pc/v5/getPolyvPlaySafe"
	url7 = "https://api.yikao88.com/api-shop/course/pc/v5/getVideoPlayToken"
)

var patterns = []string{`(?:[\w-]+\.)?yikao88\.com/`}

func init() {
	extractor.Register(&Zhaozhao{}, extractor.SiteInfo{Name: "Zhaozhao", URL: "yikao88.com", NeedAuth: true})
}

type Zhaozhao struct{}

func (s *Zhaozhao) Patterns() []string { return patterns }

func (s *Zhaozhao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zhaozhao requires login cookies")
	}
	return nil, fmt.Errorf("zhaozhao chain not yet implemented; 8 source URL(s) recorded")
}
