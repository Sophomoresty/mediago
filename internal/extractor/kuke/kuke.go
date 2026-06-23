// Package kuke implements an extractor for kuke99.com courses.
//
// API endpoints from decompiled Mooc/Courses/Kuke/:
//
//	https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//	https://player.polyv.net/secure/{vid}.js
//	https://www.kuke99.com/prod-api/kukecoregoods/pc/goods/getPackageList
//	https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyClassRoomGoodsPageProt
//	https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyCourseDetailNewProt
//	https://www.kuke99.com/prod-api/kukeonlineorder/pc/order/myOrderListProt
//	https://www.kuke99.com/prod-api/kukesearch/pc/kssUserBuyUnitGoods/v1/listMyOrderGoodsProt
//	https://www.kuke99.com/prod-api/kukestudentservice/userBroadcast/getPolyvNodeInfoProt
package kuke

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}"
	url1 = "https://player.polyv.net/secure/{vid}.js"
	url2 = "https://www.kuke99.com/prod-api/kukecoregoods/pc/goods/getPackageList"
	url3 = "https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyClassRoomGoodsPageProt"
	url4 = "https://www.kuke99.com/prod-api/kukecoregoods/pc/kgUserBuyUnitGoods/getMyCourseDetailNewProt"
	url5 = "https://www.kuke99.com/prod-api/kukeonlineorder/pc/order/myOrderListProt"
	url6 = "https://www.kuke99.com/prod-api/kukesearch/pc/kssUserBuyUnitGoods/v1/listMyOrderGoodsProt"
	url7 = "https://www.kuke99.com/prod-api/kukestudentservice/userBroadcast/getPolyvNodeInfoProt"
)

var patterns = []string{`(?:[\w-]+\.)?kuke99\.com/`}

func init() {
	extractor.Register(&Kuke{}, extractor.SiteInfo{Name: "Kuke", URL: "kuke99.com", NeedAuth: true})
}

type Kuke struct{}

func (s *Kuke) Patterns() []string { return patterns }

func (s *Kuke) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("kuke requires login cookies")
	}
	return nil, fmt.Errorf("kuke chain not yet implemented; 8 source URL(s) recorded")
}
