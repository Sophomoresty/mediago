// Package koolearn implements an extractor for koolearn.com (新东方在线).
//
// API endpoints from decompiled Mooc/Courses/Koolearn/:
//
//	https://www.koolearn.com
//	https://order.koolearn.com/ordercenter/user_order/index?status=1&page={}
//	https://study.koolearn.com
//	https://study.koolearn.com/my-data?type={type}
//	https://i.koolearn.com/logininfo
//	https://api.roombox.xdf.cn/api/login/fetchToken/{}     (roombox playback token)
package koolearn

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlHome       = "https://www.koolearn.com"
	urlOrderIndex = "https://order.koolearn.com/ordercenter/user_order/index?status=1&page={page}"
	urlStudyHome  = "https://study.koolearn.com"
	urlMyData     = "https://study.koolearn.com/my-data?type={type}"
	urlLoginInfo  = "https://i.koolearn.com/logininfo"
	urlFetchToken = "https://api.roombox.xdf.cn/api/login/fetchToken/{user_id}"
)

var patterns = []string{`(?:[\w-]+\.)?koolearn\.com/`}

func init() {
	extractor.Register(&Koolearn{}, extractor.SiteInfo{Name: "Koolearn", URL: "koolearn.com", NeedAuth: true})
}

type Koolearn struct{}

func (k *Koolearn) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:courseId|orderId|productId)=(\d+)`)

func (k *Koolearn) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("koolearn requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse koolearn id from URL")
	}
	return nil, fmt.Errorf("koolearn roombox/xdf playback chain not yet implemented")
}
