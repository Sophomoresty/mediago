// Package youdao implements an extractor for ydshengxue.com courses.
//
// API endpoints from decompiled Mooc/Courses/Youdao/:
//
//	https://ai.ydshengxue.com/ai-gw-sale/api/app/v2/order/my-orders
//	https://ai.ydshengxue.com/ai-product/api/app/v1/products/after-sale
//	https://ai.ydshengxue.com/ai-product/api/app/v2/products/after-sale/{cid:}
//	https://ec-server-c.ydlingshi.com/ai-gw-sale/api/app/v2/order/my-orders
//	https://ec-server-c.ydlingshi.com/ai-product/api/app/v1/products/after-sale
//	https://ec-server-c.ydlingshi.com/ai-product/api/app/v2/products/after-sale/{cid:}
//	https://live.ydshengxue.com/hikari-live/api/consumer/v1/key
package youdao

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://ai.ydshengxue.com/ai-gw-sale/api/app/v2/order/my-orders"
	url1 = "https://ai.ydshengxue.com/ai-product/api/app/v1/products/after-sale"
	url2 = "https://ai.ydshengxue.com/ai-product/api/app/v2/products/after-sale/{cid:}"
	url3 = "https://ec-server-c.ydlingshi.com/ai-gw-sale/api/app/v2/order/my-orders"
	url4 = "https://ec-server-c.ydlingshi.com/ai-product/api/app/v1/products/after-sale"
	url5 = "https://ec-server-c.ydlingshi.com/ai-product/api/app/v2/products/after-sale/{cid:}"
	url6 = "https://live.ydshengxue.com/hikari-live/api/consumer/v1/key"
)

var patterns = []string{`(?:[\w-]+\.)?ydshengxue\.com/`}

func init() {
	extractor.Register(&Youdao{}, extractor.SiteInfo{Name: "Youdao", URL: "ydshengxue.com", NeedAuth: true})
}

type Youdao struct{}

func (s *Youdao) Patterns() []string { return patterns }

func (s *Youdao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("youdao requires login cookies")
	}
	return nil, fmt.Errorf("youdao chain not yet implemented; 7 source URL(s) recorded")
}
