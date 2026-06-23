// Package orangevip implements an extractor for orangevip.com courses.
//
// API endpoints from decompiled Mooc/Courses/Orangevip/:
//
//	https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id:}&token={token:}&use_encrypt=0&render=jsonp
//	https://clapp.orangevip.com/otm/web/course/list
//	https://clapp.orangevip.com/otm/web/course/query/coursePeriod
//	https://clapp.orangevip.com/otm/web/course/v2/reviewPlayInfo
//	https://clapp.orangevip.com/otm/web/order/orderList
//	https://clapp.orangevip.com/otm/web/student/myCourseModelFile
//	https://u.api.orangevip.com/Api/Index/getUserInfo
//	https://www.baijiayun.com/vod/video/getPlayUrl?vid={live_id:}&render=jsonp&token={token:}&use_encrypt=0
package orangevip

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id:}&token={token:}&use_encrypt=0&render=jsonp"
	url1 = "https://clapp.orangevip.com/otm/web/course/list"
	url2 = "https://clapp.orangevip.com/otm/web/course/query/coursePeriod"
	url3 = "https://clapp.orangevip.com/otm/web/course/v2/reviewPlayInfo"
	url4 = "https://clapp.orangevip.com/otm/web/order/orderList"
	url5 = "https://clapp.orangevip.com/otm/web/student/myCourseModelFile"
	url6 = "https://u.api.orangevip.com/Api/Index/getUserInfo"
	url7 = "https://www.baijiayun.com/vod/video/getPlayUrl?vid={live_id:}&render=jsonp&token={token:}&use_encrypt=0"
)

var patterns = []string{`(?:[\w-]+\.)?orangevip\.com/`}

func init() {
	extractor.Register(&Orangevip{}, extractor.SiteInfo{Name: "Orangevip", URL: "orangevip.com", NeedAuth: true})
}

type Orangevip struct{}

func (s *Orangevip) Patterns() []string { return patterns }

func (s *Orangevip) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("orangevip requires login cookies")
	}
	return nil, fmt.Errorf("orangevip chain not yet implemented; 8 source URL(s) recorded")
}
