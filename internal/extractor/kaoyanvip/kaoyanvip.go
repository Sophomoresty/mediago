// Package kaoyanvip implements an extractor for kaoyanvip.cn (考研VIP) courses.
//
// API chain ported from decompiled Mooc/Courses/Kaoyanvip/Kaoyanvip_Course.pyc:
//
//	https://ytky.kaoyanvip.cn/api/v1/account/auth/user/info
//	https://ytky.kaoyanvip.cn/api/v1/course/myorder?page=1&size=99
//	https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse
//	https://api.kaoyanvip.cn/learn/v1/delivery/pc/my_delivery/info/?my_delivery_id={cid}
//	https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/{cid}
//	https://api.kaoyanvip.cn/learn/v1/delivery/my_unified_outline/structure/?my_delivery_id={cid}&delivery_outline_id={outline_id}
//	https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/{cid}/{outline_id}/no_stage/
//
// Video playback uses polyv (hls.videocc.net).
package kaoyanvip

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlUserInfo      = "https://ytky.kaoyanvip.cn/api/v1/account/auth/user/info"
	urlMyOrder       = "https://ytky.kaoyanvip.cn/api/v1/course/myorder?page=1&size=99"
	urlMyCoursePC    = "https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse"
	urlDeliveryInfo  = "https://api.kaoyanvip.cn/learn/v1/delivery/pc/my_delivery/info/?my_delivery_id={cid}"
	urlMyCourseCID   = "https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/{cid}"
	urlOutlineStruct = "https://api.kaoyanvip.cn/learn/v1/delivery/my_unified_outline/structure/?my_delivery_id={cid}&delivery_outline_id={outline_id}"
	urlNoStage       = "https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/{cid}/{outline_id}/no_stage/"
)

var patterns = []string{`(?:[\w-]+\.)?kaoyanvip\.cn/`}

func init() {
	extractor.Register(&Kaoyanvip{}, extractor.SiteInfo{Name: "Kaoyanvip", URL: "kaoyanvip.cn", NeedAuth: true})
}

type Kaoyanvip struct{}

func (k *Kaoyanvip) Patterns() []string { return patterns }

var deliveryRe = regexp.MustCompile(`(?:my_delivery_id|delivery_id)=(\d+)|/detail/(\w+)|uuid=(\w+)`)

func (k *Kaoyanvip) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("kaoyanvip requires login cookies")
	}
	if !deliveryRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse kaoyanvip delivery id from URL")
	}
	return nil, fmt.Errorf("kaoyanvip delivery+outline+polyv chain not yet implemented")
}
