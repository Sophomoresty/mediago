// Package wangxiao implements an extractor for k.wangxiao.cn (网校).
//
// API chain ported from decompiled Mooc/Courses/Wangxiao/Wangxiao_Course.pyc:
//
//	https://k.wangxiao.cn/play?activityid={activity_id}&productsid={product_id}
//	https://k.wangxiao.cn/item/{item_num}.html
//	https://ke.wangxiao.cn/apis//products/skuSingleContent
//	https://k.wangxiao.cn/Course/ProductsDirectory?isfromusercenter=1&ProductsId={product_id}&ordernumber={course_order}
//	https://k.wangxiao.cn/Course/GetClasshours?cid={course_id}&pid={product_id}
//	https://users.wangxiao.cn/player/Index.aspx?Id={activity_id}
//	https://users.wangxiao.cn/player/down.aspx?Id={activity_id}
package wangxiao

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlPlay       = "https://k.wangxiao.cn/play?activityid={activity_id}&productsid={product_id}"
	urlItem       = "https://k.wangxiao.cn/item/{item_num}.html"
	urlSku        = "https://ke.wangxiao.cn/apis//products/skuSingleContent"
	urlDirectory  = "https://k.wangxiao.cn/Course/ProductsDirectory?isfromusercenter=1&ProductsId={product_id}&ordernumber={course_order}"
	urlClasshours = "https://k.wangxiao.cn/Course/GetClasshours?cid={course_id}&pid={product_id}"
	urlPlayer     = "https://users.wangxiao.cn/player/Index.aspx?Id={activity_id}"
	urlPlayerDown = "https://users.wangxiao.cn/player/down.aspx?Id={activity_id}"
)

var patterns = []string{`(?:[\w-]+\.)?wangxiao\.cn/(?:play|item|Course|player)`}

func init() {
	extractor.Register(&Wangxiao{}, extractor.SiteInfo{Name: "Wangxiao", URL: "wangxiao.cn", NeedAuth: true})
}

type Wangxiao struct{}

func (w *Wangxiao) Patterns() []string { return patterns }

var activityRe = regexp.MustCompile(`activityid=(\d+)|productsid=(\d+)|/item/(\d+)`)

func (w *Wangxiao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wangxiao requires login cookies")
	}
	if !activityRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse wangxiao activity/products id from URL")
	}
	return nil, fmt.Errorf("wangxiao classhours/player chain not yet implemented")
}
